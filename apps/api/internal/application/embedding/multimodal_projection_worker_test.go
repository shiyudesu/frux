package applicationembedding

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type projectionWorkerRepositoryStub struct {
	batches atomic.Int32
	fail    error
}

func (s *projectionWorkerRepositoryStub) ListMultimodalReconciliationVideoIDs(
	context.Context,
	domainembedding.MultimodalContractIdentity,
	int64,
	int,
) ([]int64, error) {
	call := s.batches.Add(1)
	if s.fail != nil {
		return nil, s.fail
	}
	if call == 1 {
		return []int64{1}, nil
	}
	return nil, nil
}

func (*projectionWorkerRepositoryStub) FindMultimodalVectorFact(context.Context, int64, domainembedding.MultimodalContractIdentity) (*domainembedding.MultimodalVectorFact, error) {
	return nil, domainembedding.ErrMultimodalVectorFactNotFound
}

func (*projectionWorkerRepositoryStub) UpsertMultimodalProjection(context.Context, *domainembedding.MultimodalProjection) (bool, error) {
	return false, nil
}

func (*projectionWorkerRepositoryStub) DeleteMultimodalProjection(context.Context, int64, string) (bool, error) {
	return false, nil
}

func TestMultimodalProjectionWorkerReconcilesAllPagesAndStops(t *testing.T) {
	contract := testWorkerMultimodalContract(t)
	repository := &projectionWorkerRepositoryStub{}
	reconciler, err := NewMultimodalProjectionReconciler(
		repository, &multimodalVideoReaderStub{}, &multimodalAssetReaderStub{},
		contract, 128,
	)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewMultimodalProjectionWorker(reconciler, time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if repository.batches.Load() != 2 {
		t.Fatalf("batches=%d", repository.batches.Load())
	}
}

func TestMultimodalProjectionWorkerReturnsRepositoryFailure(t *testing.T) {
	contract := testWorkerMultimodalContract(t)
	want := errors.New("database unavailable")
	repository := &projectionWorkerRepositoryStub{fail: want}
	reconciler, err := NewMultimodalProjectionReconciler(
		repository, &multimodalVideoReaderStub{}, &multimodalAssetReaderStub{},
		contract, 128,
	)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewMultimodalProjectionWorker(reconciler, time.Second, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}
