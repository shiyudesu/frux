package applicationaccount

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

type accountNotificationStoreStub struct {
	item      *domainaccount.AccountNotificationOutboxItem
	delivered bool
	terminal  bool
}

func (s *accountNotificationStoreStub) ClaimAccountNotifications(
	_ context.Context,
	owner string,
	_ int,
	now time.Time,
	leaseUntil time.Time,
) ([]*domainaccount.AccountNotificationOutboxItem, error) {
	if s.item == nil || s.delivered || s.terminal ||
		s.item.AvailableAt.After(now) {
		return nil, nil
	}
	s.item.Attempts++
	s.item.LeaseOwner = owner
	s.item.LeaseUntil = &leaseUntil
	copyItem := *s.item
	return []*domainaccount.AccountNotificationOutboxItem{&copyItem}, nil
}

func (s *accountNotificationStoreStub) MarkAccountNotificationDelivered(
	context.Context, string, string, time.Time,
) error {
	s.delivered = true
	return nil
}

func (s *accountNotificationStoreStub) MarkAccountNotificationFailed(
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

type accountNotificationWriterStub struct {
	failures int
	terminal bool
	items    []domainaccount.AccountLifecycleNotification
}

func (w *accountNotificationWriterStub) WriteAccountLifecycle(
	_ context.Context,
	notification domainaccount.AccountLifecycleNotification,
) error {
	if w.terminal {
		return ErrTerminalAccountNotification
	}
	if w.failures > 0 {
		w.failures--
		return errors.New("message unavailable")
	}
	w.items = append(w.items, notification)
	return nil
}

func TestAccountNotificationWorkerRetriesAndTerminates(t *testing.T) {
	now := time.Now().UTC()
	notification, err := domainaccount.NewAccountLifecycleNotification(
		7, domainaccount.AccountOperationFreeze,
		domainaccount.AccountReasonAbuse, 2, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	item, err := domainaccount.RestoreAccountNotificationOutboxItem(
		*notification, domainaccount.AccountNotificationPending,
		0, now, "", nil, "", nil, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &accountNotificationStoreStub{item: item}
	writer := &accountNotificationWriterStub{failures: 1}
	worker := NewAccountNotificationWorker(store, writer, nil)
	worker.now = func() time.Time { return now }
	worker.batchSize = 1
	if processed, err := worker.DispatchOnce(context.Background()); err == nil ||
		processed != 0 {
		t.Fatalf("first dispatch processed=%d err=%v", processed, err)
	}
	now = now.Add(2 * time.Second)
	if processed, err := worker.DispatchOnce(context.Background()); err != nil ||
		processed != 1 || !store.delivered || len(writer.items) != 1 {
		t.Fatalf(
			"retry processed=%d err=%v delivered=%v items=%d",
			processed, err, store.delivered, len(writer.items),
		)
	}

	terminalStore := &accountNotificationStoreStub{item: item}
	terminalWriter := &accountNotificationWriterStub{terminal: true}
	terminalWorker := NewAccountNotificationWorker(
		terminalStore, terminalWriter, nil,
	)
	terminalWorker.now = func() time.Time { return now }
	terminalWorker.batchSize = 1
	if _, err := terminalWorker.DispatchOnce(context.Background()); err == nil ||
		!terminalStore.terminal {
		t.Fatalf("terminal dispatch err=%v terminal=%v", err, terminalStore.terminal)
	}
}
