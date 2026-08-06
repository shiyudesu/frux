package applicationreview

import (
	"context"
	"time"
)

type ReconciliationWorker struct {
	service      *Service
	pollInterval time.Duration
	batchSize    int
}

func NewReconciliationWorker(service *Service) *ReconciliationWorker {
	return &ReconciliationWorker{service: service, pollInterval: time.Minute, batchSize: 100}
}

func (w *ReconciliationWorker) RunOnce(ctx context.Context) error {
	if w == nil || w.service == nil {
		return nil
	}
	_, err := w.service.Reconcile(ctx, w.batchSize)
	return err
}

func (w *ReconciliationWorker) Start(ctx context.Context) {
	if w == nil || w.service == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = w.RunOnce(ctx)
			}
		}
	}()
}
