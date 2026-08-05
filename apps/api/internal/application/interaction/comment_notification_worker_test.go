package applicationinteraction

import (
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	"context"
	"errors"
	"testing"
	"time"
)

type commentNotificationStoreStub struct {
	item        *domaininteraction.CommentNotification
	terminal    bool
	delivered   bool
	failedAt    time.Time
	lastReason  string
	claimLimits []int
}

func (s *commentNotificationStoreStub) ClaimCommentNotifications(_ context.Context, owner string, limit int, now time.Time, leaseUntil time.Time) ([]*domaininteraction.CommentNotification, error) {
	s.claimLimits = append(s.claimLimits, limit)
	if s.item == nil || s.delivered || s.terminal || limit <= 0 || s.item.AvailableAt.After(now) {
		return []*domaininteraction.CommentNotification{}, nil
	}
	s.item.Attempts++
	s.item.LeaseOwner = owner
	s.item.LeaseUntil = &leaseUntil
	copy := *s.item
	return []*domaininteraction.CommentNotification{&copy}, nil
}

func (s *commentNotificationStoreStub) MarkCommentNotificationDelivered(_ context.Context, eventID string, owner string, deliveredAt time.Time) error {
	if s.item.EventID == eventID && s.item.LeaseOwner == owner {
		s.delivered = true
		s.item.DeliveredAt = &deliveredAt
	}
	return nil
}

func (s *commentNotificationStoreStub) MarkCommentNotificationFailed(_ context.Context, eventID string, owner string, availableAt time.Time, reason string, terminal bool) error {
	if s.item.EventID == eventID && s.item.LeaseOwner == owner {
		s.terminal = terminal
		s.failedAt = availableAt
		s.lastReason = reason
		s.item.AvailableAt = availableAt
		s.item.LeaseOwner = ""
		s.item.LeaseUntil = nil
	}
	return nil
}

type commentNotificationWriterStub struct {
	failures   int
	deliveries []CommentNotificationDelivery
	err        error
}

func (w *commentNotificationWriterStub) WriteCommentNotification(_ context.Context, notification CommentNotificationDelivery) error {
	w.deliveries = append(w.deliveries, notification)
	if w.failures > 0 {
		w.failures--
		return w.err
	}
	return nil
}

type commentNotificationActorStub struct{}

func (commentNotificationActorStub) GetUserProfile(context.Context, int64) (*domaininteraction.UserProfile, error) {
	return &domaininteraction.UserProfile{Nickname: "Actor", AvatarURL: "/avatar.png"}, nil
}

func TestCommentNotificationWorkerRetriesTransientFailure(t *testing.T) {
	now := time.Date(2026, 8, 3, 8, 0, 0, 0, time.UTC)
	item, err := domaininteraction.NewCommentNotification(
		"interaction:comment:31", 7, 9, domaininteraction.CommentNotificationTypeReply,
		"收到回复", "reply", 11, 30, 31, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &commentNotificationStoreStub{item: item}
	writer := &commentNotificationWriterStub{failures: 1, err: errors.New("database unavailable")}
	worker := NewCommentNotificationWorker(
		store, commentNotificationActorStub{}, writer,
		WithCommentNotificationWorkerOwner("worker-a"),
		WithCommentNotificationWorkerNow(func() time.Time { return now }),
	)

	if processed, err := worker.DispatchOnce(context.Background()); err == nil || processed != 0 {
		t.Fatalf("expected transient failure, processed=%d err=%v", processed, err)
	}
	if store.terminal || !store.failedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("unexpected retry state: terminal=%v available=%v", store.terminal, store.failedAt)
	}

	now = now.Add(time.Second)
	if processed, err := worker.DispatchOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("expected retry delivery, processed=%d err=%v", processed, err)
	}
	if !store.delivered || len(writer.deliveries) != 2 {
		t.Fatalf("notification was not delivered exactly after retry: delivered=%v writes=%d", store.delivered, len(writer.deliveries))
	}
	for _, limit := range store.claimLimits {
		if limit != 1 {
			t.Fatalf("worker pre-leased a notification batch with limit %d", limit)
		}
	}
	delivery := writer.deliveries[1]
	if delivery.VideoID != 11 || delivery.RootCommentID != 30 || delivery.CommentID != 31 ||
		delivery.ActorID != 9 || delivery.ActorNickname != "Actor" {
		t.Fatalf("structured delivery fields were lost: %+v", delivery)
	}
}

func TestCommentNotificationWorkerTerminatesValidationFailure(t *testing.T) {
	now := time.Date(2026, 8, 3, 8, 0, 0, 0, time.UTC)
	item, err := domaininteraction.NewCommentNotification(
		"interaction:comment-like:31:9", 7, 9, domaininteraction.CommentNotificationTypeLike,
		"评论获赞", "liked", 11, 30, 31, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &commentNotificationStoreStub{item: item}
	writer := &commentNotificationWriterStub{failures: 1, err: domainmessage.ErrInvalidMessageType}
	worker := NewCommentNotificationWorker(
		store, nil, writer,
		WithCommentNotificationWorkerOwner("worker-b"),
		WithCommentNotificationWorkerNow(func() time.Time { return now }),
	)

	if _, err := worker.DispatchOnce(context.Background()); err == nil {
		t.Fatal("expected terminal validation error")
	}
	if !store.terminal {
		t.Fatal("validation error was not marked terminal")
	}
}

func TestCommentNotificationWorkerKeepsRetryingTransientFailure(t *testing.T) {
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	item, err := domaininteraction.NewCommentNotification(
		"interaction:comment:41", 7, 9, domaininteraction.CommentNotificationTypeReply,
		"收到回复", "reply", 11, 40, 41, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	item.Attempts = 100
	store := &commentNotificationStoreStub{item: item}
	writer := &commentNotificationWriterStub{failures: 1, err: errors.New("message database unavailable")}
	worker := NewCommentNotificationWorker(
		store, nil, writer,
		WithCommentNotificationWorkerOwner("worker-c"),
		WithCommentNotificationWorkerNow(func() time.Time { return now }),
	)

	if _, err := worker.DispatchOnce(context.Background()); err == nil {
		t.Fatal("expected transient delivery error")
	}
	if store.terminal {
		t.Fatal("transient delivery error was permanently dead-lettered")
	}
	if !store.failedAt.Equal(now.Add(maxCommentNotificationRetryDelay)) {
		t.Fatalf("unexpected capped retry time: %v", store.failedAt)
	}
}
