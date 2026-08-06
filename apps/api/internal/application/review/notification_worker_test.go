package applicationreview

import (
	"context"
	"errors"
	"testing"
	"time"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type reviewNotificationStore struct {
	item      *domainreview.ReviewNotification
	delivered bool
	terminal  bool
}

func (s *reviewNotificationStore) ClaimReviewNotifications(_ context.Context, owner string, _ int, now, leaseUntil time.Time) ([]*domainreview.ReviewNotification, error) {
	if s.item == nil || s.delivered || s.terminal || s.item.AvailableAt.After(now) {
		return nil, nil
	}
	s.item.Attempts++
	s.item.LeaseOwner = owner
	s.item.LeaseUntil = &leaseUntil
	copyItem := *s.item
	return []*domainreview.ReviewNotification{&copyItem}, nil
}

func (s *reviewNotificationStore) MarkReviewNotificationDelivered(_ context.Context, _, _ string, _ time.Time) error {
	s.delivered = true
	return nil
}

func (s *reviewNotificationStore) MarkReviewNotificationFailed(_ context.Context, _, _ string, availableAt time.Time, _ string, terminal bool) error {
	s.item.AvailableAt = availableAt
	s.terminal = terminal
	return nil
}

type reviewNotificationWriter struct {
	failures int
	items    []ReviewNotificationDelivery
}

func (w *reviewNotificationWriter) WriteReviewNotification(_ context.Context, item ReviewNotificationDelivery) error {
	if w.failures > 0 {
		w.failures--
		return errors.New("message unavailable")
	}
	w.items = append(w.items, item)
	return nil
}

type reviewNotificationObserver struct{ results []string }

func (o *reviewNotificationObserver) ObserveHumanNotification(result string) {
	o.results = append(o.results, result)
}

func TestReviewNotificationWorkerRetriesDurablyAndDelivers(t *testing.T) {
	now := time.Now().UTC()
	store := &reviewNotificationStore{item: &domainreview.ReviewNotification{
		EventID: "review-decision:1", RecipientID: 7, VideoID: 9,
		Outcome: domainreview.OutcomeReject, AvailableAt: now,
	}}
	writer := &reviewNotificationWriter{failures: 1}
	observer := &reviewNotificationObserver{}
	worker := NewReviewNotificationWorker(store, writer, observer)
	worker.now = func() time.Time { return now }
	worker.batchSize = 1
	if processed, err := worker.DispatchOnce(context.Background()); err == nil || processed != 0 || store.terminal {
		t.Fatalf("first dispatch processed=%d err=%v terminal=%v", processed, err, store.terminal)
	}
	now = now.Add(2 * time.Second)
	if processed, err := worker.DispatchOnce(context.Background()); err != nil || processed != 1 || !store.delivered {
		t.Fatalf("retry processed=%d err=%v delivered=%v", processed, err, store.delivered)
	}
	if len(writer.items) != 1 || writer.items[0].Title != "视频审核未通过" ||
		len(observer.results) != 2 || observer.results[0] != "retry" || observer.results[1] != "delivered" {
		t.Fatalf("writer=%#v observations=%#v", writer.items, observer.results)
	}
}
