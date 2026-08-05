package applicationinteraction

import (
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	"context"
	"testing"
	"time"
)

type synchronousActionRepositoryStub struct {
	accepted         *domaininteraction.AcceptedActionEvent
	synchronousCalls int
	legacyCalls      int
}

func (*synchronousActionRepositoryStub) PersistAcceptedActionEvent(context.Context, *domaininteraction.AcceptedActionEvent) error {
	return nil
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
