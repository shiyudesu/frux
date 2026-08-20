package applicationembedding

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidMultimodalProjectionWorker = errors.New("invalid multimodal projection worker")

type MultimodalProjectionWorker struct {
	reconciler *MultimodalProjectionReconciler
	interval   time.Duration
	batchSize  int
}

func NewMultimodalProjectionWorker(
	reconciler *MultimodalProjectionReconciler,
	interval time.Duration,
	batchSize int,
) (*MultimodalProjectionWorker, error) {
	if reconciler == nil || interval < time.Second || interval > time.Hour || batchSize < 1 || batchSize > 1000 {
		return nil, ErrInvalidMultimodalProjectionWorker
	}
	return &MultimodalProjectionWorker{
		reconciler: reconciler, interval: interval, batchSize: batchSize,
	}, nil
}

func (w *MultimodalProjectionWorker) Run(ctx context.Context) error {
	if w == nil || w.reconciler == nil {
		return ErrInvalidMultimodalProjectionWorker
	}
	if err := w.reconcileAll(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.reconcileAll(ctx); err != nil {
				return err
			}
		}
	}
}

func (w *MultimodalProjectionWorker) reconcileAll(ctx context.Context) error {
	var afterVideoID int64
	for {
		result, err := w.reconciler.ReconcileBatch(ctx, afterVideoID, w.batchSize)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if result.Complete {
			return nil
		}
		if result.NextVideoID <= afterVideoID {
			return ErrInvalidMultimodalProjectionWorker
		}
		afterVideoID = result.NextVideoID
	}
}
