package applicationembedding

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

var ErrMultimodalQueryUnavailable = errors.New("multimodal query embedding unavailable")
var ErrMultimodalQuerySaturated = errors.New("multimodal query embedding saturated")
var ErrMultimodalQueryTimeout = errors.New("multimodal query embedding timed out")
var ErrInvalidMultimodalQueryVector = errors.New("invalid multimodal query vector")

type MultimodalQueryCache interface {
	Get(string, domainembedding.MultimodalContractIdentity) (*domainembedding.MultimodalQueryVector, bool)
	Put(string, *domainembedding.MultimodalQueryVector)
}

type multimodalQueryCacheEntry struct {
	key       string
	vector    *domainembedding.MultimodalQueryVector
	expiresAt time.Time
}

type BoundedMultimodalQueryCache struct {
	mutex      sync.Mutex
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
	items      map[string]*list.Element
	order      *list.List
}

func NewBoundedMultimodalQueryCache(maxEntries int, ttl time.Duration) (*BoundedMultimodalQueryCache, error) {
	if maxEntries < 1 || maxEntries > 100_000 || ttl < time.Second || ttl > 24*time.Hour {
		return nil, ErrInvalidMultimodalQueryVector
	}
	return &BoundedMultimodalQueryCache{
		maxEntries: maxEntries, ttl: ttl, now: func() time.Time { return time.Now().UTC() },
		items: make(map[string]*list.Element, maxEntries), order: list.New(),
	}, nil
}

func (c *BoundedMultimodalQueryCache) Get(
	query string,
	contract domainembedding.MultimodalContractIdentity,
) (*domainembedding.MultimodalQueryVector, bool) {
	if c == nil {
		return nil, false
	}
	key := multimodalQueryCacheKey(query, contract)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	element := c.items[key]
	if element == nil {
		return nil, false
	}
	entry := element.Value.(*multimodalQueryCacheEntry)
	if !c.now().Before(entry.expiresAt) {
		c.order.Remove(element)
		delete(c.items, key)
		return nil, false
	}
	values, err := domainembedding.ValidateMultimodalQueryVector(entry.vector.Contract, entry.vector.Values)
	if err != nil || !entry.vector.Contract.Equal(contract) {
		c.order.Remove(element)
		delete(c.items, key)
		return nil, false
	}
	c.order.MoveToFront(element)
	return &domainembedding.MultimodalQueryVector{Contract: contract, Values: values}, true
}

func (c *BoundedMultimodalQueryCache) Put(query string, vector *domainembedding.MultimodalQueryVector) {
	if c == nil || vector == nil {
		return
	}
	values, err := domainembedding.ValidateMultimodalQueryVector(vector.Contract, vector.Values)
	if err != nil {
		return
	}
	key := multimodalQueryCacheKey(query, vector.Contract)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if element := c.items[key]; element != nil {
		entry := element.Value.(*multimodalQueryCacheEntry)
		entry.vector = &domainembedding.MultimodalQueryVector{Contract: vector.Contract, Values: values}
		entry.expiresAt = c.now().Add(c.ttl)
		c.order.MoveToFront(element)
		return
	}
	entry := &multimodalQueryCacheEntry{
		key: key, vector: &domainembedding.MultimodalQueryVector{Contract: vector.Contract, Values: values},
		expiresAt: c.now().Add(c.ttl),
	}
	c.items[key] = c.order.PushFront(entry)
	for c.order.Len() > c.maxEntries {
		oldest := c.order.Back()
		delete(c.items, oldest.Value.(*multimodalQueryCacheEntry).key)
		c.order.Remove(oldest)
	}
}

func multimodalQueryCacheKey(query string, contract domainembedding.MultimodalContractIdentity) string {
	sum := sha256.Sum256([]byte(contract.Key() + "\x00" + query))
	return hex.EncodeToString(sum[:])
}

type MultimodalQueryEmbedderConfig struct {
	Contract       domainembedding.MultimodalContractIdentity
	MaxQueryRunes  int
	Deadline       time.Duration
	AdmissionLimit int
}

type MultimodalQueryEmbedder struct {
	provider MultimodalEmbeddingProvider
	cache    MultimodalQueryCache
	config   MultimodalQueryEmbedderConfig
	slots    chan struct{}
}

func NewMultimodalQueryEmbedder(
	provider MultimodalEmbeddingProvider,
	cache MultimodalQueryCache,
	config MultimodalQueryEmbedderConfig,
) (*MultimodalQueryEmbedder, error) {
	contract, err := domainembedding.NewMultimodalContractIdentity(
		config.Contract.ProviderAlias, config.Contract.ModelAlias,
		config.Contract.RevisionAlias, config.Contract.Dimension,
		config.Contract.TextCanonicalizer, config.Contract.FrameSamplingPolicy,
		config.Contract.ImagePreprocessingPolicy, config.Contract.FusionPolicy,
	)
	if provider == nil || cache == nil || err != nil || !contract.Equal(config.Contract) ||
		config.MaxQueryRunes < 1 || config.MaxQueryRunes > 512 ||
		config.Deadline < 100*time.Millisecond || config.Deadline > 2*time.Minute ||
		config.AdmissionLimit < 1 || config.AdmissionLimit > 64 {
		return nil, ErrInvalidMultimodalQueryVector
	}
	return &MultimodalQueryEmbedder{
		provider: provider, cache: cache, config: config,
		slots: make(chan struct{}, config.AdmissionLimit),
	}, nil
}

func (e *MultimodalQueryEmbedder) EmbedPublicQuery(
	ctx context.Context,
	query string,
) (*domainembedding.MultimodalQueryVector, error) {
	if e == nil {
		return nil, ErrMultimodalQueryUnavailable
	}
	canonicalQuery, err := domainembedding.CanonicalizePublicQuery(query, e.config.MaxQueryRunes)
	if err != nil {
		return nil, ErrInvalidMultimodalQueryVector
	}
	if cached, ok := e.cache.Get(canonicalQuery, e.config.Contract); ok {
		return cached, nil
	}
	select {
	case e.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrMultimodalQuerySaturated
	}
	request, err := NewMultimodalQueryEmbeddingRequest(e.config.Contract, canonicalQuery, e.config.MaxQueryRunes)
	if err != nil {
		<-e.slots
		return nil, ErrInvalidMultimodalQueryVector
	}
	type queryResult struct {
		result *MultimodalEmbeddingResult
		err    error
	}
	completed := make(chan queryResult, 1)
	providerCtx, cancel := context.WithTimeout(ctx, e.config.Deadline)
	defer cancel()
	go func() {
		defer func() { <-e.slots }()
		result, err := e.provider.EmbedQueryText(providerCtx, request)
		completed <- queryResult{result: result, err: err}
	}()
	select {
	case output := <-completed:
		if output.err != nil {
			return nil, ErrMultimodalQueryUnavailable
		}
		validated, err := ValidateMultimodalEmbeddingResult(e.config.Contract, request.SourceHash, output.result)
		if err != nil {
			return nil, ErrInvalidMultimodalQueryVector
		}
		vector := &domainembedding.MultimodalQueryVector{Contract: validated.Identity.Contract, Values: validated.Values}
		e.cache.Put(canonicalQuery, vector)
		return vector.Clone(), nil
	case <-providerCtx.Done():
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrMultimodalQueryTimeout
	}
}
