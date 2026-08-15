package applicationaccount

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

const (
	defaultAccountNotificationBatchSize = 50
	defaultAccountNotificationPoll      = time.Second
	defaultAccountNotificationLease     = 30 * time.Second
	defaultAccountNotificationTimeout   = 5 * time.Second
	maxAccountNotificationRetry         = time.Minute
)

var ErrTerminalAccountNotification = errors.New("terminal account notification")

type AccountNotificationRepository interface {
	ClaimAccountNotifications(
		ctx context.Context,
		leaseOwner string,
		limit int,
		now time.Time,
		leaseUntil time.Time,
	) ([]*domainaccount.AccountNotificationOutboxItem, error)
	MarkAccountNotificationDelivered(
		ctx context.Context,
		eventID string,
		leaseOwner string,
		deliveredAt time.Time,
	) error
	MarkAccountNotificationFailed(
		ctx context.Context,
		eventID string,
		leaseOwner string,
		availableAt time.Time,
		reason string,
		terminal bool,
	) error
}

type AccountLifecycleMessageWriter interface {
	WriteAccountLifecycle(
		ctx context.Context,
		notification domainaccount.AccountLifecycleNotification,
	) error
}

type AccountNotificationObserver interface {
	ObserveAccountNotification(result string)
}

type AccountNotificationWorker struct {
	store        AccountNotificationRepository
	writer       AccountLifecycleMessageWriter
	observer     AccountNotificationObserver
	owner        string
	now          func() time.Time
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
	onError      func(error)
}

type AccountNotificationOption func(*AccountNotificationWorker)

func NewAccountNotificationWorker(
	store AccountNotificationRepository,
	writer AccountLifecycleMessageWriter,
	observer AccountNotificationObserver,
	options ...AccountNotificationOption,
) *AccountNotificationWorker {
	worker := &AccountNotificationWorker{
		store: store, writer: writer, observer: observer,
		owner:        fmt.Sprintf("account-notification-%d", time.Now().UTC().UnixNano()),
		now:          func() time.Time { return time.Now().UTC() },
		batchSize:    defaultAccountNotificationBatchSize,
		pollInterval: defaultAccountNotificationPoll,
		lease:        defaultAccountNotificationLease,
	}
	for _, option := range options {
		if option != nil {
			option(worker)
		}
	}
	return worker
}

func WithAccountNotificationErrorHandler(
	handler func(error),
) AccountNotificationOption {
	return func(worker *AccountNotificationWorker) {
		worker.onError = handler
	}
}

func (w *AccountNotificationWorker) Start(ctx context.Context) error {
	if w == nil || w.store == nil || w.writer == nil {
		return nil
	}
	if _, err := w.DispatchOnce(ctx); err != nil {
		w.handleError(err)
	}
	go func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.DispatchOnce(ctx); err != nil {
					w.handleError(err)
				}
			}
		}
	}()
	return nil
}

func (w *AccountNotificationWorker) handleError(err error) {
	if w != nil && w.onError != nil && err != nil {
		w.onError(err)
	}
}

func (w *AccountNotificationWorker) DispatchOnce(ctx context.Context) (int, error) {
	if w == nil || w.store == nil || w.writer == nil {
		return 0, nil
	}
	processed := 0
	var resultErr error
	for claimed := 0; claimed < w.batchSize; claimed++ {
		now := w.now().UTC()
		items, err := w.store.ClaimAccountNotifications(
			ctx, w.owner, 1, now, now.Add(w.lease),
		)
		if err != nil {
			w.observe("retry")
			return processed, err
		}
		if len(items) == 0 {
			break
		}
		item := items[0]
		deliverCtx, cancel := context.WithTimeout(
			ctx, defaultAccountNotificationTimeout,
		)
		err = w.writer.WriteAccountLifecycle(
			deliverCtx, item.AccountLifecycleNotification,
		)
		cancel()
		if err == nil {
			if markErr := w.store.MarkAccountNotificationDelivered(
				ctx, item.EventID, w.owner, w.now().UTC(),
			); markErr != nil {
				w.observe("retry")
				return processed, markErr
			}
			processed++
			w.observe("delivered")
			continue
		}
		terminal := isTerminalAccountNotificationError(err)
		failedAt := w.now().UTC()
		if markErr := w.store.MarkAccountNotificationFailed(
			ctx, item.EventID, w.owner,
			failedAt.Add(accountNotificationRetryDelay(item.Attempts)),
			err.Error(), terminal,
		); markErr != nil {
			w.observe("retry")
			return processed, markErr
		}
		if terminal {
			w.observe("terminal")
		} else {
			w.observe("retry")
		}
		resultErr = errors.Join(resultErr, err)
	}
	return processed, resultErr
}

func accountNotificationRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second << min(attempts-1, 6)
	if delay > maxAccountNotificationRetry {
		return maxAccountNotificationRetry
	}
	return delay
}

func isTerminalAccountNotificationError(err error) bool {
	return errors.Is(err, ErrTerminalAccountNotification) ||
		errors.Is(err, domainaccount.ErrInvalidAccountNotification)
}

func (w *AccountNotificationWorker) observe(result string) {
	if w != nil && w.observer != nil {
		w.observer.ObserveAccountNotification(strings.TrimSpace(result))
	}
}
