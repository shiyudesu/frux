package applicationinteraction

import (
	"context"
	"errors"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	"testing"
	"time"
)

type synchronousActionRepositoryStub struct {
	accepted         *domaininteraction.AcceptedActionEvent
	synchronousCalls int
	legacyCalls      int
	persistCalls     int
	persistErr       error
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
	return s.state, nil
}

func (s *actionStateStoreStub) RollbackActionState(context.Context, *ActionStateResult) (bool, error) {
	s.rollbackCalls++
	return s.rollbackResult, s.rollbackErr
}

func (s *actionStateStoreStub) ConfirmActionStateHandoff(context.Context, *ActionStateResult) error {
	s.confirmCalls++
	return nil
}

type actionPublisherStub struct{ err error }

func (p actionPublisherStub) PublishActionChanged(context.Context, *ActionChangedEvent) error {
	return p.err
}

type actionDeliveryObserverStub struct {
	fallback []string
	rollback []string
}

func (o *actionDeliveryObserverStub) ObserveActionFallback(result string) {
	o.fallback = append(o.fallback, result)
}

func (o *actionDeliveryObserverStub) ObserveActionRollback(result string) {
	o.rollback = append(o.rollback, result)
}

func (*synchronousActionRepositoryStub) GetVideoStat(context.Context, int64) (*domaininteraction.VideoStat, error) {
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
