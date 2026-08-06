package applicationgovernance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
)

type memoryRepository struct {
	mu        sync.Mutex
	active    map[domaingovernance.Key]*domaingovernance.Revision
	revisions map[domaingovernance.Key][]*domaingovernance.Revision
	audits    []*domainadminaudit.Fact
	failAudit bool
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		active:    make(map[domaingovernance.Key]*domaingovernance.Revision),
		revisions: make(map[domaingovernance.Key][]*domaingovernance.Revision),
	}
}

func (r *memoryRepository) ListActive(context.Context) ([]*domaingovernance.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domaingovernance.Revision, 0, len(r.active))
	for _, revision := range r.active {
		result = append(result, revision)
	}
	return result, nil
}

func (r *memoryRepository) ListRevisions(
	_ context.Context,
	key domaingovernance.Key,
	limit int,
) ([]*domaingovernance.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.revisions[key]
	result := make([]*domaingovernance.Revision, 0, min(limit, len(stored)))
	for index := len(stored) - 1; index >= 0 && len(result) < limit; index-- {
		result = append(result, stored[index])
	}
	return result, nil
}

func (r *memoryRepository) FindRevision(
	_ context.Context,
	key domaingovernance.Key,
	revision int64,
) (*domaingovernance.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, candidate := range r.revisions[key] {
		if candidate.Number() == revision {
			return candidate, nil
		}
	}
	return nil, domaingovernance.ErrRevisionNotFound
}

func (r *memoryRepository) CommitRevision(
	_ context.Context,
	expectedRevision int64,
	revision *domaingovernance.Revision,
	fact *domainadminaudit.Fact,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := int64(0)
	if active := r.active[revision.Key()]; active != nil {
		current = active.Number()
	}
	if current != expectedRevision {
		return domaingovernance.ErrRevisionConflict
	}
	if r.failAudit {
		return errors.New("forced audit failure")
	}
	r.revisions[revision.Key()] = append(r.revisions[revision.Key()], revision)
	r.active[revision.Key()] = revision
	r.audits = append(r.audits, fact)
	return nil
}

func TestServiceConcurrentUpdateRollbackAndAuditAtomicity(t *testing.T) {
	registry := domaingovernance.DefaultRegistry()
	repository := newMemoryRepository()
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	service := New(registry, repository, WithClock(func() time.Time { return now }))

	start := make(chan struct{})
	results := make(chan error, 2)
	for actorID := int64(1); actorID <= 2; actorID++ {
		go func(actor int64) {
			<-start
			_, err := service.Update(context.Background(), UpdateRequest{
				Key: domaingovernance.FeedPreloadEnabled, ActorID: actor,
				ExpectedRevision: 0, Value: domaingovernance.BooleanValue(false),
				Reason: "concurrent incident response",
			})
			results <- err
		}(actorID)
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domaingovernance.ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent update error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}

	second, err := service.Update(context.Background(), UpdateRequest{
		Key: domaingovernance.FeedPreloadEnabled, ActorID: 3,
		ExpectedRevision: 1, Value: domaingovernance.BooleanValue(true),
		Reason: "restore optional capability",
	})
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	rolledBack, err := service.Rollback(context.Background(), RollbackRequest{
		Key: domaingovernance.FeedPreloadEnabled, ActorID: 4,
		ExpectedRevision: second.Number(), TargetRevision: 1,
		Reason: "rollback restored regression",
	})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	value, _ := rolledBack.Value().Boolean()
	if rolledBack.Number() != 3 || rolledBack.RollbackFromRevision() != 1 || value {
		t.Fatalf("unexpected rollback revision: %+v", rolledBack)
	}
	if len(repository.audits) != 3 ||
		repository.audits[2].Detail()["operation"] != "rollback" {
		t.Fatalf("unexpected audit facts: %#v", repository.audits)
	}

	repository.failAudit = true
	if _, err := service.Update(context.Background(), UpdateRequest{
		Key: domaingovernance.FeedPreloadEnabled, ActorID: 5,
		ExpectedRevision: 3, Value: domaingovernance.BooleanValue(true),
		Reason: "must roll back with audit",
	}); !errors.Is(err, ErrUpdateControlFailed) {
		t.Fatalf("audit failure error = %v", err)
	}
	if repository.active[domaingovernance.FeedPreloadEnabled].Number() != 3 ||
		len(repository.revisions[domaingovernance.FeedPreloadEnabled]) != 3 {
		t.Fatal("audit failure changed active state")
	}
}
