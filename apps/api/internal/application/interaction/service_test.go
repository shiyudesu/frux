package applicationinteraction

import (
	"context"
	"errors"
	"testing"
	"time"

	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
)

type synchronousActionRepositoryStub struct {
	accepted         *domaininteraction.AcceptedActionEvent
	synchronousCalls int
	legacyCalls      int
	persistCalls     int
	persistErr       error
	stat             *domaininteraction.VideoStat
	statErr          error
}

func (r *synchronousActionRepositoryStub) PersistAcceptedActionEvent(context.Context, *domaininteraction.AcceptedActionEvent) error {
	r.persistCalls++
	return r.persistErr
}

type actionStateStoreStub struct {
	state          *ActionStateResult
	rollbackResult bool
	rollbackErr    error
	rollbackCalls  int
	confirmCalls   int
	err            error
}

func (s *actionStateStoreStub) SetActionState(
	context.Context,
	int64,
	int64,
	string,
	bool,
	string,
	*domaininteraction.VideoStat,
	*domaininteraction.ActionStateSnapshot,
	ActionMutation,
) (*ActionStateResult, error) {
	return s.state, s.err
}

func (s *actionStateStoreStub) RollbackActionState(context.Context, *ActionStateResult) (bool, error) {
	s.rollbackCalls++
	return s.rollbackResult, s.rollbackErr
}

func (s *actionStateStoreStub) ConfirmActionStateHandoff(context.Context, *ActionStateResult) error {
	s.confirmCalls++
	return nil
}

type actionPublisherStub struct {
	err       error
	onPublish func()
}

func (p actionPublisherStub) PublishActionChanged(context.Context, *ActionChangedEvent) error {
	if p.onPublish != nil {
		p.onPublish()
	}
	return p.err
}

type possiblyAcknowledgedError struct{ err error }

func (e possiblyAcknowledgedError) Error() string { return e.err.Error() }
func (e possiblyAcknowledgedError) Unwrap() error { return e.err }
func (possiblyAcknowledgedError) MayHaveAcknowledged() bool {
	return true
}

type actionDeliveryObserverStub struct {
	fallback []string
	rollback []string
}

type hotScoreRecorderStub struct {
	calls int
	delta int
}

func (s *hotScoreRecorderStub) AddHotScore(
	context.Context,
	int64,
	int,
	time.Time,
) error {
	s.calls++
	s.delta += hotScoreLikeWeight
	return nil
}

func (o *actionDeliveryObserverStub) ObserveActionFallback(result string) {
	o.fallback = append(o.fallback, result)
}

func (o *actionDeliveryObserverStub) ObserveActionRollback(result string) {
	o.rollback = append(o.rollback, result)
}

func (r *synchronousActionRepositoryStub) GetVideoStat(context.Context, int64) (*domaininteraction.VideoStat, error) {
	if r.statErr != nil {
		return nil, r.statErr
	}
	if r.stat != nil {
		stat := *r.stat
		return &stat, nil
	}
	return &domaininteraction.VideoStat{VideoID: 11}, nil
}

func (*synchronousActionRepositoryStub) GetActionState(context.Context, int64, int64, string) (*domaininteraction.ActionStateSnapshot, error) {
	return &domaininteraction.ActionStateSnapshot{}, nil
}

func (*synchronousActionRepositoryStub) GetVideoAuthorID(context.Context, int64) (int64, error) {
	return 1, nil
}

func (*synchronousActionRepositoryStub) GetUserProfile(context.Context, int64) (*domaininteraction.UserProfile, error) {
	return &domaininteraction.UserProfile{}, nil
}

func (r *synchronousActionRepositoryStub) SetAction(context.Context, int64, int64, string, bool, string) (*domaininteraction.Action, int, int, error) {
	r.legacyCalls++
	return nil, 0, 0, nil
}

func (r *synchronousActionRepositoryStub) SetActionWithAcceptedEvent(_ context.Context, event *domaininteraction.AcceptedActionEvent) (*domaininteraction.Action, int, int, error) {
	r.synchronousCalls++
	cloned := *event
	r.accepted = &cloned
	return domaininteraction.RestoreAction(
		1,
		event.UserID,
		event.VideoID,
		event.ActionType,
		domaininteraction.ActionStatusActive,
		event.IdempotencyKey,
		time.Now().UTC(),
		time.Now().UTC(),
	), 1, 1, nil
}

func (*synchronousActionRepositoryStub) CreateComment(context.Context, *domaininteraction.Comment) (*domaininteraction.Comment, int, int, error) {
	return nil, 0, 0, nil
}

func (*synchronousActionRepositoryStub) FindCommentByUserAndIdempotencyKey(context.Context, int64, string) (*domaininteraction.Comment, int, error) {
	return nil, 0, domaininteraction.ErrCommentNotFound
}

func (*synchronousActionRepositoryStub) ListComments(context.Context, int64, *domaininteraction.CommentCursor, int) ([]*domaininteraction.Comment, error) {
	return nil, nil
}

func (*synchronousActionRepositoryStub) DeleteComment(context.Context, int64, int64, string) (*domaininteraction.Comment, int, int, error) {
	return nil, 0, 0, nil
}

func TestSyncRecommendationActionUsesDurableAcceptedEvent(t *testing.T) {
	repo := &synchronousActionRepositoryStub{}
	service := New(repo)
	requestID := "recommendation-request"

	result, err := service.FavoriteWithRecommendation(context.Background(), 7, 11, "favorite-key", requestID)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Active || result.FavoriteCount != 1 {
		t.Fatalf("unexpected synchronous favorite result: %#v", result)
	}
	if repo.legacyCalls != 0 || repo.synchronousCalls != 1 || repo.accepted == nil {
		t.Fatalf("sync fallback bypassed durable accepted-event path: %#v", repo)
	}
	if repo.accepted.RecommendationRequestID != requestID || repo.accepted.Version != 0 || repo.accepted.EventID == "" || repo.accepted.OccurredAt.IsZero() {
		t.Fatalf("recommendation attribution was not preserved in accepted action: %#v", repo.accepted)
	}
}

func TestKafkaUncertainAcknowledgementFallsBackToDurableReceipt(t *testing.T) {
	repo := &synchronousActionRepositoryStub{}
	store := &actionStateStoreStub{state: acceptedAsyncState()}
	observer := &actionDeliveryObserverStub{}
	service := New(
		repo,
		WithAsyncActionPipeline(store, actionPublisherStub{err: errors.New("uncertain acknowledgement")}),
		WithActionDeliveryObserver(observer),
	)
	result, err := service.Like(context.Background(), 7, 11, "like-1")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.Active || repo.persistCalls != 1 ||
		store.rollbackCalls != 0 || store.confirmCalls != 1 {
		t.Fatalf("result=%#v repo=%#v store=%#v", result, repo, store)
	}
	if len(observer.fallback) != 1 || observer.fallback[0] != "success" {
		t.Fatalf("fallback observations = %#v", observer.fallback)
	}
}

func TestKafkaAndFallbackFailureConditionallyRollBackRedis(t *testing.T) {
	repo := &synchronousActionRepositoryStub{persistErr: errors.New("database unavailable")}
	store := &actionStateStoreStub{state: acceptedAsyncState(), rollbackResult: true}
	observer := &actionDeliveryObserverStub{}
	service := New(
		repo,
		WithAsyncActionPipeline(store, actionPublisherStub{err: errors.New("Kafka unavailable")}),
		WithActionDeliveryObserver(observer),
	)
	if _, err := service.Like(context.Background(), 7, 11, "like-1"); !errors.Is(err, ErrUpdateInteractionFailed) {
		t.Fatalf("error = %v", err)
	}

	if store.rollbackCalls != 1 || len(observer.rollback) != 1 || observer.rollback[0] != "success" {
		t.Fatalf("store=%#v observer=%#v", store, observer)
	}
}

func TestFailedDurableAcceptDoesNotPublishToKafka(t *testing.T) {
	persistErr := errors.New("database unavailable")
	repo := &synchronousActionRepositoryStub{persistErr: persistErr}
	store := &actionStateStoreStub{state: acceptedAsyncState(), rollbackResult: true}
	publishCalls := 0
	service := New(
		repo,
		WithAsyncActionPipeline(store, actionPublisherStub{onPublish: func() { publishCalls++ }, err: possiblyAcknowledgedError{
			err: errors.New("Kafka result uncertain"),
		}}),
	)
	_, err := service.Like(context.Background(), 7, 11, "like-1")
	if !errors.Is(err, ErrUpdateInteractionFailed) || !errors.Is(err, persistErr) {
		t.Fatalf("error = %v", err)
	}
	if publishCalls != 0 || store.rollbackCalls != 1 || store.confirmCalls != 0 {
		t.Fatalf("store=%#v", store)
	}
}

func TestFailedActionRecoveryDoesNotRollBackSupersedingVersion(t *testing.T) {
	repo := &synchronousActionRepositoryStub{persistErr: errors.New("database unavailable")}
	store := &actionStateStoreStub{state: acceptedAsyncState(), rollbackResult: false}
	observer := &actionDeliveryObserverStub{}
	service := New(
		repo,
		WithAsyncActionPipeline(store, actionPublisherStub{err: errors.New("Kafka unavailable")}),
		WithActionDeliveryObserver(observer),
	)
	if _, err := service.Like(context.Background(), 7, 11, "like-1"); !errors.Is(err, ErrUpdateInteractionFailed) {
		t.Fatalf("error = %v", err)
	}
	if len(observer.rollback) != 1 || observer.rollback[0] != "superseded" {
		t.Fatalf("rollback observations = %#v", observer.rollback)
	}
}

func acceptedAsyncState() *ActionStateResult {
	now := time.Now().UTC()
	return &ActionStateResult{
		UserID: 7, VideoID: 11, ActionType: domaininteraction.ActionTypeLike,
		Active: true, LikeCount: 1, Delta: 1, IdempotencyKey: "like-1",
		Version: 3, EventID: "action-event-1", OccurredAt: now,
		ShouldPublish: true, CanRollback: true,
	}
}

type snapshotStatCacheStub struct {
	saved *domaininteraction.VideoStat
	err   error
}

func (c *snapshotStatCacheStub) SetVideoStat(_ context.Context, stat *domaininteraction.VideoStat) error {
	cloned := *stat
	c.saved = &cloned
	return c.err
}

func TestCommentCacheKeepsSnapshotFieldsWithTheirRevision(t *testing.T) {
	repo := &synchronousActionRepositoryStub{stat: &domaininteraction.VideoStat{VideoID: 11, Revision: 8, LikeCount: 12, CommentCount: 9}}
	cache := &snapshotStatCacheStub{}
	service := New(repo, WithStatCache(cache))
	service.syncCommentCount(context.Background(), 11, 3)
	if cache.saved == nil || cache.saved.CommentCount != 9 || cache.saved.Revision != 8 || cache.saved.LikeCount != 12 {
		t.Fatalf("mixed an old comment result into a new snapshot: %+v", cache.saved)
	}
	cache.saved = nil
	repo.statErr = errors.New("database unavailable")
	service.syncCommentCount(context.Background(), 11, 3)
	if cache.saved != nil {
		t.Fatalf("fabricated a cache value on read failure: %+v", cache.saved)
	}
}

func TestActionPersistsBeforePublishAndIgnoresCacheWriteFailure(t *testing.T) {
	repo := &synchronousActionRepositoryStub{stat: &domaininteraction.VideoStat{VideoID: 11, Revision: 8, LikeCount: 12}}
	cache := &snapshotStatCacheStub{err: errors.New("Redis unavailable")}
	store := &actionStateStoreStub{state: acceptedAsyncState()}
	service := New(repo, WithStatCache(cache), WithAsyncActionPipeline(store, actionPublisherStub{onPublish: func() {
		if repo.persistCalls != 1 {
			t.Fatal("published before durable persistence")
		}
	}}))
	result, err := service.Like(context.Background(), 7, 11, "like-1")
	if err != nil || result.LikeCount != 12 || repo.persistCalls != 1 || store.rollbackCalls != 0 {
		t.Fatalf("cache failure lost durable success: %+v, %v", result, err)
	}
}

func TestRedisUnavailableUsesDurableActionRepository(t *testing.T) {
	repo := &synchronousActionRepositoryStub{}
	store := &actionStateStoreStub{err: errors.New("Redis unavailable")}
	service := New(repo, WithAsyncActionPipeline(store, actionPublisherStub{onPublish: func() { t.Fatal("fallback should use the durable repository handoff") }}))
	result, err := service.Like(context.Background(), 7, 11, "like-1")
	if err != nil || result.LikeCount != 1 || repo.synchronousCalls != 1 {
		t.Fatalf("Redis outage lost write availability: %+v %v", result, err)
	}
}
