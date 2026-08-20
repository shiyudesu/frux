package applicationembedding

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type adminMultimodalRepositoryStub struct {
	jobs  []*domainembedding.MultimodalEmbeddingJob
	audit *domainadminaudit.Fact
}

func (r *adminMultimodalRepositoryStub) ListAdminMultimodalJobs(_ context.Context, state string, afterID int64, limit int) ([]*domainembedding.MultimodalEmbeddingJob, error) {
	items := make([]*domainembedding.MultimodalEmbeddingJob, 0, limit)
	for _, job := range r.jobs {
		if job.ID > afterID && (state == "" || job.State == state) {
			items = append(items, job.Clone())
		}
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

func (r *adminMultimodalRepositoryStub) CommitAdminMultimodalRequeue(
	_ context.Context,
	jobID int64,
	_ string,
	buildAudit func(*domainembedding.MultimodalEmbeddingJob, *domainembedding.MultimodalEmbeddingJob) (*domainadminaudit.Fact, error),
) (*domainembedding.MultimodalEmbeddingJob, bool, error) {
	for index, job := range r.jobs {
		if job.ID != jobID {
			continue
		}
		previous := job.Clone()
		next, err := domainembedding.NewMultimodalEmbeddingJob(
			job.VideoID, job.Contract, job.SourceHash, job.MaxAttempts, job.UpdatedAt.Add(time.Second),
		)
		if err != nil {
			return nil, false, err
		}
		next.ID = job.ID
		r.audit, err = buildAudit(previous, next)
		if err != nil {
			return nil, false, err
		}
		r.jobs[index] = next
		return next.Clone(), false, nil
	}
	return nil, false, domainembedding.ErrMultimodalJobNotFound
}

func TestAdminMultimodalServicePaginatesSafeDetailsAndAuditsRequeue(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	contract := testWorkerMultimodalContract(t)
	repository := &adminMultimodalRepositoryStub{jobs: []*domainembedding.MultimodalEmbeddingJob{
		terminalAdminMultimodalJob(t, 1, contract, now),
		terminalAdminMultimodalJob(t, 2, contract, now.Add(time.Second)),
	}}
	service := NewAdminMultimodalService(repository)
	service.now = func() time.Time { return now }
	first, err := service.List(context.Background(), "terminal", "", 1)
	if err != nil || len(first.Items) != 1 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	second, err := service.List(context.Background(), "terminal", first.NextCursor, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].JobID != 2 || second.HasMore {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	fields := make([]string, 0, reflect.TypeOf(AdminMultimodalJobItem{}).NumField())
	for index := 0; index < reflect.TypeOf(AdminMultimodalJobItem{}).NumField(); index++ {
		fields = append(fields, reflect.TypeOf(AdminMultimodalJobItem{}).Field(index).Name)
	}
	sort.Strings(fields)
	for _, forbidden := range []string{"ClaimToken", "SourceHash", "Values", "Embedding", "RawError", "Image", "Query", "URL"} {
		if sort.SearchStrings(fields, forbidden) < len(fields) && fields[sort.SearchStrings(fields, forbidden)] == forbidden {
			t.Fatalf("admin item exposes forbidden field %q: %v", forbidden, fields)
		}
	}
	item, replayed, err := service.Requeue(context.Background(), AdminMultimodalRequeueRequest{
		ActorID: 7, JobID: 1, ReasonCode: "operator_retry", IdempotencyKey: "requeue-1",
	})
	if err != nil || replayed || item.State != domainembedding.MultimodalJobStatePending || repository.audit == nil {
		t.Fatalf("requeue item=%#v replayed=%v audit=%#v err=%v", item, replayed, repository.audit, err)
	}
	if repository.audit.Action() != domainadminaudit.ActionMultimodalJobRequeue ||
		repository.audit.TargetType() != domainadminaudit.TargetMultimodalJob ||
		repository.audit.Detail()["previous_state"] != domainembedding.MultimodalJobStateTerminal {
		t.Fatalf("unexpected audit=%#v", repository.audit)
	}
}

func terminalAdminMultimodalJob(
	t testing.TB,
	id int64,
	contract domainembedding.MultimodalContractIdentity,
	now time.Time,
) *domainembedding.MultimodalEmbeddingJob {
	t.Helper()
	completedAt := now
	job := domainembedding.RestoreMultimodalEmbeddingJob(
		id, id+100, contract, domainembedding.MultimodalSourceHash([]byte("source")),
		domainembedding.MultimodalJobStateTerminal, 5, 5, "", nil, now,
		domainembedding.MultimodalFailureProviderTerminal, now, now, &completedAt,
	)
	if job == nil {
		t.Fatal("failed to restore terminal job")
	}
	return job
}
