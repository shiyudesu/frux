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
	existing   *domainembedding.VideoEmbedding
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
	extend     func(context.Context) error
}

func (s *semanticRepositoryStub) FindVideoEmbedding(
	context.Context, int64, string,
) (*domainembedding.VideoEmbedding, error) {
	if s.existing != nil {
		return s.existing, nil
	}
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
	ctx context.Context,
	job *domainembedding.SemanticJob,
	_ time.Time,
	leaseUntil time.Time,
) error {
	s.extended++
	if s.extend != nil {
		if err := s.extend(ctx); err != nil {
			return err
		}
	}
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
	generate    func(context.Context) error
}

func (g *semanticGeneratorStub) ValidateMetadata(context.Context) error {
	return g.metadataErr
}
func (g *semanticGeneratorStub) Generate(
	ctx context.Context, _ []SemanticInput,
) ([][]float64, error) {
	if g.generate != nil {
		if err := g.generate(ctx); err != nil {
			return nil, err
		}
	}
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

func TestSemanticWorkerRejectsInvalidVectorAsTerminal(t *testing.T) {
	now := time.Now().UTC()
	job := &domainembedding.SemanticJob{
		VideoID: 8, Model: domainembedding.SemanticModelKey,
		TextHash: "hash", Title: "title", State: domainembedding.SemanticJobProcessing,
		Attempts: 1, LeaseOwner: "owner", AvailableAt: now,
	}
	repository := &semanticRepositoryStub{jobs: []*domainembedding.SemanticJob{job}}
	worker := NewSemanticWorker(
		repository,
		&semanticGeneratorStub{vector: []float64{1}},
		true, 1, time.Second, time.Hour,
	)
	worker.now = func() time.Time { return now }
	if _, err := worker.ProcessPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repository.retried != 1 || !repository.terminal ||
		repository.errorClass != string(SemanticContract) ||
		repository.completed != 0 {
		t.Fatalf("invalid vector outcome = %+v", repository)
	}
}

func TestSemanticWorkerCompletesExistingSameTextWithoutInference(t *testing.T) {
	now := time.Now().UTC()
	job := &domainembedding.SemanticJob{
		VideoID: 11, Model: domainembedding.SemanticModelKey,
		TextHash: "same", Title: "title", State: domainembedding.SemanticJobProcessing,
		Attempts: 1, LeaseOwner: "owner", AvailableAt: now,
	}
	repository := &semanticRepositoryStub{
		jobs: []*domainembedding.SemanticJob{job},
		existing: domainembedding.NewVideoEmbedding(
			job.VideoID, job.Model, unitApplicationSemanticVector(), job.TextHash, "[]",
		),
	}
	generatorCalled := false
	worker := NewSemanticWorker(
		repository,
		&semanticGeneratorStub{generate: func(context.Context) error {
			generatorCalled = true
			return nil
		}},
		true, 1, time.Second, time.Hour,
	)
	worker.now = func() time.Time { return now }
	if _, err := worker.ProcessPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if generatorCalled || repository.completed != 1 {
		t.Fatalf("generator=%v completed=%d", generatorCalled, repository.completed)
	}
}

func unitApplicationSemanticVector() []float64 {
	vector := make([]float64, domainembedding.SemanticDimension)
	value := 1 / math.Sqrt(float64(domainembedding.SemanticDimension))
	for index := range vector {
		vector[index] = value
	}
	return vector
}

func TestSemanticHeartbeatDatabaseStallIsBounded(t *testing.T) {
	now := time.Now().UTC()
	job := &domainembedding.SemanticJob{
		VideoID: 9, Model: domainembedding.SemanticModelKey,
		TextHash: "hash", Title: "title", State: domainembedding.SemanticJobProcessing,
		LeaseOwner: "claim", AvailableAt: now,
	}
	repository := &semanticRepositoryStub{
		jobs: []*domainembedding.SemanticJob{job},
		extend: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	generator := &semanticGeneratorStub{generate: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	worker := NewSemanticWorker(
		repository, generator, true, 1, 300*time.Millisecond, time.Hour,
	)
	worker.now = func() time.Time { return now }
	started := time.Now()
	_, err := worker.ProcessPending(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("heartbeat error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled heartbeat blocked processor for %v", elapsed)
	}
	if repository.completed != 0 || repository.retried != 0 {
		t.Fatalf(
			"stale processor completed=%d retried=%d",
			repository.completed, repository.retried,
		)
	}
}

func TestSemanticHeartbeatStopsPromptlyOnShutdown(t *testing.T) {
	repository := &semanticRepositoryStub{
		extend: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	worker := NewSemanticWorker(
		repository, &semanticGeneratorStub{}, true,
		1, time.Second, time.Hour,
	)
	ctx, cancel := context.WithCancel(context.Background())
	processCtx, processCancel := context.WithCancel(ctx)
	done := worker.startLeaseHeartbeat(
		processCtx,
		processCancel,
		&domainembedding.SemanticJob{
			VideoID: 1, Model: domainembedding.SemanticModelKey,
			TextHash: "hash", LeaseOwner: "claim",
		},
	)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown heartbeat error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("heartbeat did not stop after shutdown")
	}
}
