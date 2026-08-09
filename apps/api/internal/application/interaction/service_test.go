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
}

func (r *synchronousActionRepositoryStub) PersistAcceptedActionEvent(context.Context, *domaininteraction.AcceptedActionEvent) error {
	r.persistCalls++
	return r.persistErr
}

type actionStateStoreStub struct {
	state           *ActionStateResult
	rollbackResult  bool
	rollbackErr     error
	rollbackCalls   int
	confirmCalls    int
	incompleteCalls int
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

func (s *actionStateStoreStub) MarkActionStateDeliveryIncomplete(
	context.Context,
	*ActionStateResult,
) error {
	s.incompleteCalls++
	return nil
}

type actionPublisherStub struct{ err error }

func (p actionPublisherStub) PublishActionChanged(context.Context, *ActionChangedEvent) error {
	return p.err
}

type possiblyAcknowledgedError struct{ err error }

func (e possiblyAcknowledgedError) Error() string { return e.err.Error() }
func (e possiblyAcknowledgedError) Unwrap() error { return e.err }
func (possiblyAcknowledgedError) MayHaveAcknowledged() bool {
	return true
}

type acknowledgedPublicationError struct {
	err          error
	acknowledged map[string]bool
	primary      string
}

func (e acknowledgedPublicationError) Error() string { return e.err.Error() }
func (e acknowledgedPublicationError) Unwrap() error { return e.err }
func (e acknowledgedPublicationError) TransportAcknowledged(transport string) bool {
	return e.acknowledged[transport]
}
func (e acknowledgedPublicationError) AnyTransportAcknowledged() bool {
	for _, acknowledged := range e.acknowledged {
		if acknowledged {
			return true
		}
	}
	return false
}

func (e acknowledgedPublicationError) PrimaryTransportAcknowledged() bool {
	return e.acknowledged[e.primary]
}
func (e acknowledgedPublicationError) PrimaryTransportMayBeAcknowledged() bool {
	return e.PrimaryTransportAcknowledged()
}
func (e acknowledgedPublicationError) AnyTransportMayBeAcknowledged() bool {
	return e.AnyTransportAcknowledged()
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

func TestDualPublicationFailuresAttemptFallbackButRemainUnconfirmed(t *testing.T) {
	for _, test := range []struct {
		name         string
		acknowledged map[string]bool
	}{
		{name: "primary only", acknowledged: map[string]bool{"rabbit": true}},
		{name: "mirror only", acknowledged: map[string]bool{"kafka": true}},
		{name: "neither", acknowledged: map[string]bool{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &synchronousActionRepositoryStub{}
			store := &actionStateStoreStub{state: acceptedAsyncState(), rollbackResult: true}
			service := New(
				repo,
				WithAsyncActionPipeline(store, actionPublisherStub{err: acknowledgedPublicationError{
					err: errors.New("dual publication incomplete"), acknowledged: test.acknowledged,
					primary: "rabbit",
				}}),
			)
			result, err := service.Like(context.Background(), 7, 11, "like-1")
			if !errors.Is(err, ErrUpdateInteractionFailed) {
				t.Fatalf("error = %v", err)
			}
			if result != nil || repo.persistCalls != 1 ||
				store.rollbackCalls != 0 || store.confirmCalls != 0 ||
				store.incompleteCalls != 1 {
				t.Fatalf("result=%#v repo=%#v store=%#v", result, repo, store)
			}
		})
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

func TestUncertainKafkaAndFallbackFailureDoesNotRollBackRedis(t *testing.T) {
	persistErr := errors.New("database unavailable")
	repo := &synchronousActionRepositoryStub{persistErr: persistErr}
	store := &actionStateStoreStub{state: acceptedAsyncState(), rollbackResult: true}
	service := New(
		repo,
		WithAsyncActionPipeline(store, actionPublisherStub{err: possiblyAcknowledgedError{
			err: errors.New("Kafka result uncertain"),
		}}),
	)
	_, err := service.Like(context.Background(), 7, 11, "like-1")
	if !errors.Is(err, ErrUpdateInteractionFailed) || !errors.Is(err, persistErr) {
		t.Fatalf("error = %v", err)
	}
	if store.rollbackCalls != 0 || store.confirmCalls != 0 {
		t.Fatalf("store=%#v", store)
	}
}

func TestPartialDualAcknowledgementAndFallbackFailurePreserveRedisWithoutUnsafeConfirmation(t *testing.T) {
	for _, test := range []struct {
		acknowledged string
		wantConfirm  int
	}{
		{acknowledged: "rabbit", wantConfirm: 1},
		{acknowledged: "kafka", wantConfirm: 0},
	} {
		t.Run(test.acknowledged, func(t *testing.T) {
			persistErr := errors.New("database unavailable")
			publishErr := errors.New("dual publication incomplete")
			repo := &synchronousActionRepositoryStub{persistErr: persistErr}
			store := &actionStateStoreStub{state: acceptedAsyncState(), rollbackResult: true}
			observer := &actionDeliveryObserverStub{}
			service := New(
				repo,
				WithAsyncActionPipeline(store, actionPublisherStub{err: acknowledgedPublicationError{
					err:          publishErr,
					acknowledged: map[string]bool{test.acknowledged: true},
					primary:      "rabbit",
				}}),
				WithActionDeliveryObserver(observer),
			)

			if _, err := service.Like(context.Background(), 7, 11, "like-1"); !errors.Is(err, ErrUpdateInteractionFailed) ||
				!errors.Is(err, publishErr) ||
				!errors.Is(err, persistErr) {
				t.Fatalf("error = %v", err)
			}
			if repo.persistCalls != 1 || store.rollbackCalls != 0 ||
				store.confirmCalls != test.wantConfirm {
				t.Fatalf("repo=%#v store=%#v", repo, store)
			}
			if len(observer.fallback) != 1 || observer.fallback[0] != "failure" ||
				len(observer.rollback) != 0 {
				t.Fatalf("observer=%#v", observer)
			}
		})
	}
}

func TestNoDualAcknowledgementAndFallbackFailureRollsBack(t *testing.T) {
	repo := &synchronousActionRepositoryStub{persistErr: errors.New("database unavailable")}
	store := &actionStateStoreStub{state: acceptedAsyncState(), rollbackResult: true}
	service := New(
		repo,
		WithAsyncActionPipeline(store, actionPublisherStub{err: acknowledgedPublicationError{
			err: errors.New("dual publication failed"),
			acknowledged: map[string]bool{
				"rabbit": false,
				"kafka":  false,
			},
		}}),
	)
	if _, err := service.Like(context.Background(), 7, 11, "like-1"); !errors.Is(err, ErrUpdateInteractionFailed) {
		t.Fatalf("error = %v", err)
	}
	if store.rollbackCalls != 1 || store.confirmCalls != 0 {
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
