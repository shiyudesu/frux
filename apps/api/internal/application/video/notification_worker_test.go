package applicationvideo

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
)

type lifecycleNotificationStoreStub struct {
	item      *domainmessage.LifecycleOutboxItem
	delivered bool
	terminal  bool
}

func (s *lifecycleNotificationStoreStub) ClaimLifecycleNotifications(
	_ context.Context,
	owner string,
	_ int,
	now time.Time,
	leaseUntil time.Time,
) ([]*domainmessage.LifecycleOutboxItem, error) {
	if s.item == nil || s.delivered || s.terminal || s.item.AvailableAt.After(now) {
		return nil, nil
	}
	s.item.Attempts++
	s.item.LeaseOwner = owner
	s.item.LeaseUntil = &leaseUntil
	copyItem := *s.item
	return []*domainmessage.LifecycleOutboxItem{&copyItem}, nil
}

func (s *lifecycleNotificationStoreStub) MarkLifecycleNotificationDelivered(
	context.Context, string, string, time.Time,
) error {
	s.delivered = true
	return nil
}

func (s *lifecycleNotificationStoreStub) MarkLifecycleNotificationFailed(
	_ context.Context,
	_ string,
	_ string,
	availableAt time.Time,
	_ string,
	terminal bool,
) error {
	s.item.AvailableAt = availableAt
	s.terminal = terminal
	return nil
}

type lifecycleNotificationWriterStub struct {
	failures int
	items    []domainmessage.LifecycleNotification
}

func (w *lifecycleNotificationWriterStub) WriteLifecycleNotification(
	_ context.Context,
	notification domainmessage.LifecycleNotification,
	_ string,
	_ string,
) error {
	if w.failures > 0 {
		w.failures--
		return errors.New("message unavailable")
	}
	w.items = append(w.items, notification)
	return nil
}

func TestLifecycleNotificationWorkerRetriesAndTerminatesInvalidPayload(t *testing.T) {
	now := time.Now().UTC()
	valid := &domainmessage.LifecycleOutboxItem{
		LifecycleNotification: domainmessage.LifecycleNotification{
			EventID:     domainmessage.PublicationEventID(9, 1),
			RecipientID: 7, VideoID: 9, ReviewVersion: 1,
			Stage:      domainmessage.LifecycleStagePublished,
			Result:     domainmessage.LifecycleResultPublic,
			OccurredAt: now,
		},
		AvailableAt: now,
	}
	store := &lifecycleNotificationStoreStub{item: valid}
	writer := &lifecycleNotificationWriterStub{failures: 1}
	worker := NewLifecycleNotificationWorker(store, writer, nil)
	worker.now = func() time.Time { return now }
	worker.batchSize = 1
	if processed, err := worker.DispatchOnce(context.Background()); err == nil || processed != 0 {
		t.Fatalf("first dispatch processed=%d err=%v", processed, err)
	}
	now = now.Add(2 * time.Second)
	if processed, err := worker.DispatchOnce(context.Background()); err != nil || processed != 1 ||
		!store.delivered || len(writer.items) != 1 {
		t.Fatalf("retry processed=%d err=%v delivered=%v items=%d", processed, err, store.delivered, len(writer.items))
	}

	invalidStore := &lifecycleNotificationStoreStub{item: &domainmessage.LifecycleOutboxItem{
		LifecycleNotification: domainmessage.LifecycleNotification{
			EventID: "invalid", RecipientID: 7, VideoID: 9,
			Stage:      domainmessage.LifecycleStagePublished,
			Result:     domainmessage.LifecycleResultFailed,
			OccurredAt: now,
		},
		AvailableAt: now,
	}}
	invalidWorker := NewLifecycleNotificationWorker(invalidStore, writer, nil)
	invalidWorker.now = func() time.Time { return now }
	invalidWorker.batchSize = 1
	if _, err := invalidWorker.DispatchOnce(context.Background()); err == nil || !invalidStore.terminal {
		t.Fatalf("invalid dispatch err=%v terminal=%v", err, invalidStore.terminal)
	}
}
