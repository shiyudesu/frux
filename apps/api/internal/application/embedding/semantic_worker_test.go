package applicationembedding

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type semanticRepositoryStub struct {
	jobs       []*domainembedding.SemanticJob
	suspended  int
	resumed    int
	retried    int
	completed  int
	terminal   bool
	errorClass string
	resumeErr  error
	extended   int
	claimLimit int
	owners     []string
}

func (*semanticRepositoryStub) FindVideoEmbedding(
	context.Context, int64, string,
) (*domainembedding.VideoEmbedding, error) {
	return nil, domainembedding.ErrVideoEmbeddingNotFound
}
func (s *semanticRepositoryStub) ClaimSemanticJobs(
	_ context.Context, owner string, _ time.Time, leaseUntil time.Time, limit int,
) ([]*domainembedding.SemanticJob, error) {
	s.claimLimit = limit
	s.owners = append(s.owners, owner)
	if len(s.jobs) == 0 {
		return nil, nil
	}
	count := min(limit, len(s.jobs))
	jobs := append([]*domainembedding.SemanticJob(nil), s.jobs[:count]...)
	s.jobs = s.jobs[count:]
	for _, job := range jobs {
		job.LeaseOwner = owner
		job.LeaseUntil = &leaseUntil
	}
	return jobs, nil
}

func TestSemanticWorkerClaimsOneJobWithUniqueTokenPerProcessor(t *testing.T) {
	now := time.Now().UTC()
	repository := &semanticRepositoryStub{jobs: []*domainembedding.SemanticJob{
		{VideoID: 1, Model: domainembedding.SemanticModelKey, TextHash: "one"},
		{VideoID: 2, Model: domainembedding.SemanticModelKey, TextHash: "two"},
	}}
	worker := NewSemanticWorker(
		repository,
		&semanticGeneratorStub{generateErr: errors.New("down")},
		true, 2, time.Second, time.Hour,
	)
	worker.now = func() time.Time { return now }
	_, _ = worker.ProcessPending(context.Background())
	_, _ = worker.ProcessPending(context.Background())
	if repository.claimLimit != 1 || len(repository.owners) != 2 ||
		repository.owners[0] == repository.owners[1] {
		t.Fatalf("claim limit=%d owners=%v", repository.claimLimit, repository.owners)
	}
}
func (s *semanticRepositoryStub) CompleteSemanticJob(
	context.Context,
	*domainembedding.SemanticJob,
	*domainembedding.VideoEmbedding,
	time.Time,
) error {
	s.completed++
	return nil
}
func (s *semanticRepositoryStub) RetrySemanticJob(
	_ context.Context,
	_ *domainembedding.SemanticJob,
	_ time.Time,
	errorClass string,
	terminal bool,
) error {
	s.retried++
	s.errorClass = errorClass
	s.terminal = terminal
	return nil
}
func (s *semanticRepositoryStub) ExtendSemanticJobLease(
	_ context.Context,
	job *domainembedding.SemanticJob,
	_ time.Time,
	leaseUntil time.Time,
) error {
	s.extended++
	job.LeaseUntil = &leaseUntil
	return nil
}
func (s *semanticRepositoryStub) SuspendSemanticJobs(context.Context, time.Time) (int64, error) {
	s.suspended++
	return 1, nil
}
func (s *semanticRepositoryStub) ResumeSemanticJobs(context.Context, time.Time) (int64, error) {
	s.resumed++
	return 1, s.resumeErr
}

func TestSemanticWorkerPropagatesResumeFailureBeforeStartingProcessors(t *testing.T) {
	repository := &semanticRepositoryStub{resumeErr: errors.New("resume failed")}
	worker := NewSemanticWorker(
		repository,
		&semanticGeneratorStub{},
		true, 1, time.Second, time.Hour,
	)
	if err := worker.Start(context.Background()); !errors.Is(err, repository.resumeErr) {
		t.Fatalf("start error = %v", err)
	}
	if repository.resumed != 1 {
		t.Fatalf("resume attempts = %d", repository.resumed)
	}
}
func (*semanticRepositoryStub) SemanticBacklog(context.Context) ([]domainembedding.SemanticBacklog, error) {
	return nil, nil
}
func (*semanticRepositoryStub) CleanupSemanticJobs(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

type semanticGeneratorStub struct {
	metadataErr error
	generateErr error
	vector      []float64
}

func (g *semanticGeneratorStub) ValidateMetadata(context.Context) error {
	return g.metadataErr
}
func (g *semanticGeneratorStub) Generate(
	context.Context, []SemanticInput,
) ([][]float64, error) {
	if g.generateErr != nil {
		return nil, g.generateErr
	}
	return [][]float64{g.vector}, nil
}

func TestSemanticWorkerSuspendsWhenServiceIsUnavailable(t *testing.T) {
	repository := &semanticRepositoryStub{}
	worker := NewSemanticWorker(
		repository,
		&semanticGeneratorStub{metadataErr: errors.New("unavailable")},
		true, 1, time.Second, time.Hour,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if repository.suspended != 1 {
		t.Fatalf("suspended = %d", repository.suspended)
	}
}

func TestSemanticWorkerRetriesAndCompletesDurableJobs(t *testing.T) {
	now := time.Now().UTC()
	job := &domainembedding.SemanticJob{
		VideoID: 7, Model: domainembedding.SemanticModelKey,
		TextHash: "hash", Title: "title", State: domainembedding.SemanticJobProcessing,
		Attempts: 2, LeaseOwner: "owner", AvailableAt: now,
	}
	repository := &semanticRepositoryStub{jobs: []*domainembedding.SemanticJob{job}}
	worker := NewSemanticWorker(
		repository,
		&semanticGeneratorStub{generateErr: &SemanticError{
			Result: SemanticUnavailable, Err: errors.New("down"),
		}},
		true, 1, time.Second, time.Hour,
	)
	worker.now = func() time.Time { return now }
	if _, err := worker.ProcessPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repository.retried != 1 || repository.errorClass != string(SemanticUnavailable) ||
		repository.terminal {
		t.Fatalf("retry state = %+v", repository)
	}

	vector := make([]float64, domainembedding.SemanticDimension)
	value := 1 / math.Sqrt(float64(domainembedding.SemanticDimension))
	for index := range vector {
		vector[index] = value
	}
	repository.jobs = []*domainembedding.SemanticJob{job}
	worker.generator = &semanticGeneratorStub{vector: vector}
	if _, err := worker.ProcessPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repository.completed != 1 {
		t.Fatalf("completed = %d", repository.completed)
	}
}
