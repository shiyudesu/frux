package applicationembedding

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type multimodalQueryProviderStub struct {
	mutex   sync.Mutex
	calls   int
	run     func(context.Context, MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error)
	started chan struct{}
}

func (p *multimodalQueryProviderStub) EmbedVideoContent(context.Context, MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	return nil, errors.New("unexpected video embedding")
}

func (p *multimodalQueryProviderStub) EmbedQueryText(ctx context.Context, request MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	p.mutex.Lock()
	p.calls++
	started := p.started
	p.mutex.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	return p.run(ctx, request)
}

func TestBoundedMultimodalQueryCacheUsesHashedContractScopedKeysAndLRU(t *testing.T) {
	contract := testWorkerMultimodalContract(t)
	other := contract
	other.RevisionAlias = "revision-2"
	cache, err := NewBoundedMultimodalQueryCache(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	vector := &domainembedding.MultimodalQueryVector{Contract: contract, Values: unitMultimodalVector(contract.Dimension)}
	cache.Put("private-looking query", vector)
	for key := range cache.items {
		if strings.Contains(key, "private-looking") || len(key) != domainembedding.MultimodalDigestHexLength {
			t.Fatalf("cache key exposed query: %q", key)
		}
	}
	got, ok := cache.Get("private-looking query", contract)
	if !ok || got == vector || got.Values[0] != 1 {
		t.Fatalf("cache hit=%v vector=%#v", ok, got)
	}
	got.Values[0] = 0
	again, ok := cache.Get("private-looking query", contract)
	if !ok || again.Values[0] != 1 {
		t.Fatal("cache returned aliased vector")
	}
	if _, ok := cache.Get("private-looking query", other); ok {
		t.Fatal("cache reused a vector across contracts")
	}
	cache.Put("two", vector)
	cache.Put("three", vector)
	if _, ok := cache.Get("private-looking query", contract); ok {
		t.Fatal("least recently used cache entry was not evicted")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := cache.Get("three", contract); ok {
		t.Fatal("expired cache entry was returned")
	}
}

func TestMultimodalQueryEmbedderCachesOneValidatedAttempt(t *testing.T) {
	contract := testWorkerMultimodalContract(t)
	cache, err := NewBoundedMultimodalQueryCache(10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	provider := &multimodalQueryProviderStub{}
	provider.run = func(_ context.Context, request MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		values := unitMultimodalVector(request.Contract.Dimension)
		return &MultimodalEmbeddingResult{
			Identity: domainembedding.MultimodalVectorIdentity{
				Contract: request.Contract, SourceHash: request.SourceHash,
				VectorDigest: domainembedding.MultimodalVectorDigest(values),
			}, Vector: values,
		}, nil
	}
	embedder, err := NewMultimodalQueryEmbedder(provider, cache, MultimodalQueryEmbedderConfig{
		Contract: contract, MaxQueryRunes: 64, Deadline: time.Second, AdmissionLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := embedder.EmbedPublicQuery(context.Background(), "  猫咪   视频 ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := embedder.EmbedPublicQuery(context.Background(), "猫咪 视频")
	if err != nil {
		t.Fatal(err)
	}
	provider.mutex.Lock()
	calls := provider.calls
	provider.mutex.Unlock()
	if calls != 1 || first == second || first.Values[0] != second.Values[0] {
		t.Fatalf("provider calls=%d first=%#v second=%#v", calls, first, second)
	}
}

func TestMultimodalQueryEmbedderHasNoRetryAndBoundsIgnoringProvider(t *testing.T) {
	contract := testWorkerMultimodalContract(t)
	cache, err := NewBoundedMultimodalQueryCache(10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	provider := &multimodalQueryProviderStub{started: started}
	provider.run = func(context.Context, MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		<-release
		return nil, errors.New("provider unavailable")
	}
	embedder, err := NewMultimodalQueryEmbedder(provider, cache, MultimodalQueryEmbedderConfig{
		Contract: contract, MaxQueryRunes: 64, Deadline: 100 * time.Millisecond, AdmissionLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := embedder.EmbedPublicQuery(context.Background(), "query-one")
		firstDone <- err
	}()
	<-started
	if _, err := embedder.EmbedPublicQuery(context.Background(), "query-two"); !errors.Is(err, ErrMultimodalQuerySaturated) {
		t.Fatalf("saturated error=%v", err)
	}
	select {
	case err := <-firstDone:
		if !errors.Is(err, ErrMultimodalQueryTimeout) {
			t.Fatalf("timeout error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("query deadline did not bound provider")
	}
	provider.mutex.Lock()
	calls := provider.calls
	provider.mutex.Unlock()
	if calls != 1 {
		t.Fatalf("HTTP query path retried provider %d times", calls)
	}
	close(release)
}

func TestMultimodalQueryEmbedderRejectsInvalidProviderVector(t *testing.T) {
	contract := testWorkerMultimodalContract(t)
	cache, err := NewBoundedMultimodalQueryCache(10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	provider := &multimodalQueryProviderStub{}
	provider.run = func(_ context.Context, request MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		values := make([]float64, request.Contract.Dimension)
		return &MultimodalEmbeddingResult{Identity: domainembedding.MultimodalVectorIdentity{
			Contract: request.Contract, SourceHash: request.SourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(values),
		}, Vector: values}, nil
	}
	embedder, err := NewMultimodalQueryEmbedder(provider, cache, MultimodalQueryEmbedderConfig{
		Contract: contract, MaxQueryRunes: 64, Deadline: time.Second, AdmissionLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := embedder.EmbedPublicQuery(context.Background(), "query"); !errors.Is(err, ErrInvalidMultimodalQueryVector) {
		t.Fatalf("invalid vector error=%v", err)
	}
	if _, ok := cache.Get("query", contract); ok {
		t.Fatal("invalid provider vector entered cache")
	}
}

func unitMultimodalVector(dimension int) []float64 {
	values := make([]float64, dimension)
	values[0] = 1
	return values
}
