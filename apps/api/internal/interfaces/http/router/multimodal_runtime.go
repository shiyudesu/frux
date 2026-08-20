package interfaceshttprouter

import (
	"context"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	applicationsearch "github.com/shiyudesu/frux/internal/application/search"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
)

type multimodalSearchVideoRepository interface {
	domainsearch.VideoSearchIndex
	applicationsearch.PublicVideoLoader
}

type readyMultimodalProvider interface {
	applicationembedding.MultimodalEmbeddingProvider
	Contract() domainembedding.MultimodalContractIdentity
}

type multimodalProviderFactory func(
	context.Context,
	infraconfig.MultimodalConfig,
	string,
) (readyMultimodalProvider, error)

func newReadyMultimodalProvider(
	ctx context.Context,
	cfg infraconfig.MultimodalConfig,
	capability string,
) (readyMultimodalProvider, error) {
	return infraembedding.NewReadyHTTPMultimodalProvider(ctx, cfg, capability)
}

func newMultimodalSearchService(
	ctx context.Context,
	cfg infraconfig.MultimodalConfig,
	videos multimodalSearchVideoRepository,
	users domainsearch.UserSearchIndex,
	semantic applicationsearch.SemanticVideoIndex,
	providerFactory multimodalProviderFactory,
) (*applicationsearch.Service, error) {
	dependencies := infraconfig.MultimodalRuntimeDependencies{ExactRetrieval: semantic != nil}
	options := []applicationsearch.Option{}
	if cfg.QueryEmbeddingEnabled || cfg.HybridSearchEnabled {
		if providerFactory == nil {
			return nil, infraconfig.ErrMissingMultimodalDependency
		}
		provider, err := providerFactory(
			ctx, cfg, infraembedding.MultimodalProviderCapabilityQuery,
		)
		if err != nil {
			return nil, err
		}
		contract := provider.Contract()
		cacheTTL, err := time.ParseDuration(cfg.Query.CacheTTL)
		if err != nil {
			return nil, err
		}
		queryCache, err := applicationembedding.NewBoundedMultimodalQueryCache(
			cfg.Query.CacheEntries, cacheTTL,
		)
		if err != nil {
			return nil, err
		}
		providerDeadline, err := time.ParseDuration(cfg.Provider.Deadline)
		if err != nil {
			return nil, err
		}
		queryEmbedder, err := applicationembedding.NewMultimodalQueryEmbedder(
			provider,
			queryCache,
			applicationembedding.MultimodalQueryEmbedderConfig{
				Contract: contract, MaxQueryRunes: cfg.Query.MaxRunes,
				Deadline: providerDeadline, AdmissionLimit: cfg.Provider.AdmissionLimit,
			},
		)
		if err != nil {
			return nil, err
		}
		dependencies.ProviderContract = &contract
		dependencies.QueryCache = true
		if cfg.HybridSearchEnabled {
			cursorTTL, err := time.ParseDuration(cfg.Hybrid.CursorTTL)
			if err != nil {
				return nil, err
			}
			hybridConfig, err := applicationsearch.NewHybridVideoSearchConfig(
				contract, cfg.Hybrid.Version, cfg.Hybrid.PoolLimit,
				cfg.Hybrid.LexicalReservation, cfg.Hybrid.SemanticReservation, cursorTTL,
			)
			if err != nil {
				return nil, err
			}
			options = append(options, applicationsearch.WithHybridVideoSearch(
				queryEmbedder, semantic, videos, hybridConfig,
			))
		}
	}
	if err := infraconfig.ValidateMultimodalAPIRuntime(cfg, dependencies); err != nil {
		return nil, err
	}
	return applicationsearch.New(videos, users, options...), nil
}

var _ readyMultimodalProvider = (*infraembedding.HTTPMultimodalProvider)(nil)
