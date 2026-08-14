package applicationaccount

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidRefreshSessionCleanup = errors.New("invalid refresh session cleanup configuration")

const maxRefreshSessionCleanupBatches = 10

type RefreshSessionCleanupOption func(*RefreshSessionCleanupWorker)

type RefreshSessionCleanupWorker struct {
	service          *Service
	interval         time.Duration
	revokedRetention time.Duration
	batchSize        int
	onError          func(error)
}

func NewRefreshSessionCleanupWorker(
	service *Service,
	options ...RefreshSessionCleanupOption,
) *RefreshSessionCleanupWorker {
	worker := &RefreshSessionCleanupWorker{
		service:          service,
		interval:         time.Hour,
		revokedRetention: 30 * 24 * time.Hour,
		batchSize:        100,
	}
	for _, option := range options {
		if option != nil {
			option(worker)
		}
	}
	return worker
}

func WithRefreshSessionCleanupSchedule(
	interval, revokedRetention time.Duration,
	batchSize int,
) RefreshSessionCleanupOption {
	return func(worker *RefreshSessionCleanupWorker) {
		worker.interval = interval
		worker.revokedRetention = revokedRetention
		worker.batchSize = batchSize
	}
}

func WithRefreshSessionCleanupErrorHandler(
	handler func(error),
) RefreshSessionCleanupOption {
	return func(worker *RefreshSessionCleanupWorker) {
		worker.onError = handler
	}
}

func (w *RefreshSessionCleanupWorker) Start(ctx context.Context) error {
	if w == nil || w.service == nil || w.interval <= 0 ||
		w.revokedRetention <= 0 || w.batchSize <= 0 {
		return ErrInvalidRefreshSessionCleanup
	}
	go w.run(ctx)
	return nil
}

func (w *RefreshSessionCleanupWorker) run(ctx context.Context) {
	w.cleanup(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.cleanup(ctx)
		}
	}
}

func (w *RefreshSessionCleanupWorker) cleanup(ctx context.Context) {
	for range maxRefreshSessionCleanupBatches {
		if ctx.Err() != nil {
			return
		}
		deleted, err := w.service.CleanupRefreshSessions(
			ctx, w.revokedRetention, w.batchSize,
		)
		if err != nil {
			if w.onError != nil {
				w.onError(err)
			}
			return
		}
		if deleted < int64(w.batchSize) {
			return
		}
	}
}
