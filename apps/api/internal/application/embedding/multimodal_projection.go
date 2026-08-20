package applicationembedding

import (
	"context"
	"errors"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type MultimodalProjectionRepository interface {
	ListMultimodalReconciliationVideoIDs(context.Context, domainembedding.MultimodalContractIdentity, int64, int) ([]int64, error)
	FindMultimodalVectorFact(context.Context, int64, domainembedding.MultimodalContractIdentity) (*domainembedding.MultimodalVectorFact, error)
	UpsertMultimodalProjection(context.Context, *domainembedding.MultimodalProjection) (bool, error)
	DeleteMultimodalProjection(context.Context, int64, string) (bool, error)
}

type MultimodalProjectionReconcileResult struct {
	Examined    int
	Upserted    int
	Deleted     int
	Unchanged   int
	NextVideoID int64
	Complete    bool
}

type MultimodalProjectionReconciler struct {
	repository        MultimodalProjectionRepository
	videos            MultimodalVideoReader
	assets            MultimodalMediaAssetReader
	contract          domainembedding.MultimodalContractIdentity
	maxVideoTextRunes int
	now               func() time.Time
}

func NewMultimodalProjectionReconciler(
	repository MultimodalProjectionRepository,
	videos MultimodalVideoReader,
	assets MultimodalMediaAssetReader,
	contract domainembedding.MultimodalContractIdentity,
	maxVideoTextRunes int,
) (*MultimodalProjectionReconciler, error) {
	validated, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	if repository == nil || videos == nil || assets == nil || err != nil ||
		!validated.Equal(contract) || maxVideoTextRunes < 1 || maxVideoTextRunes > 8192 {
		return nil, domainembedding.ErrInvalidMultimodalProjection
	}
	return &MultimodalProjectionReconciler{
		repository: repository, videos: videos, assets: assets,
		contract: contract, maxVideoTextRunes: maxVideoTextRunes,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *MultimodalProjectionReconciler) ReconcileBatch(
	ctx context.Context,
	afterVideoID int64,
	limit int,
) (MultimodalProjectionReconcileResult, error) {
	if r == nil || afterVideoID < 0 || limit < 1 || limit > 1000 {
		return MultimodalProjectionReconcileResult{}, domainembedding.ErrInvalidMultimodalProjection
	}
	videoIDs, err := r.repository.ListMultimodalReconciliationVideoIDs(ctx, r.contract, afterVideoID, limit)
	if err != nil {
		return MultimodalProjectionReconcileResult{}, err
	}
	result := MultimodalProjectionReconcileResult{Complete: len(videoIDs) < limit}
	for _, videoID := range videoIDs {
		result.Examined++
		result.NextVideoID = videoID
		fact, err := r.repository.FindMultimodalVectorFact(ctx, videoID, r.contract)
		if errors.Is(err, domainembedding.ErrMultimodalVectorFactNotFound) {
			deleted, deleteErr := r.repository.DeleteMultimodalProjection(ctx, videoID, r.contract.Key())
			if deleteErr != nil {
				return result, deleteErr
			}
			if deleted {
				result.Deleted++
				inframetrics.ObserveMultimodalProjection("missing_fact", 1)
			} else {
				result.Unchanged++
			}
			continue
		}
		if err != nil {
			return result, err
		}
		source, sourceErr := loadCurrentMultimodalSource(
			ctx, r.videos, r.assets, videoID, r.contract, r.maxVideoTextRunes,
		)
		if sourceErr != nil || source.sourceHash != fact.Identity.SourceHash {
			if sourceErr != nil && !errors.Is(sourceErr, ErrIneligibleMultimodalContent) &&
				!errors.Is(sourceErr, ErrInvalidMultimodalHandoff) {
				return result, sourceErr
			}
			deleted, deleteErr := r.repository.DeleteMultimodalProjection(ctx, videoID, r.contract.Key())
			if deleteErr != nil {
				return result, deleteErr
			}
			if deleted {
				result.Deleted++
				metricResult := "source_stale"
				if sourceErr != nil {
					metricResult = "unreadable"
				}
				inframetrics.ObserveMultimodalProjection(metricResult, 1)
			} else {
				result.Unchanged++
			}
			continue
		}
		projection, err := domainembedding.NewMultimodalProjection(fact, source.publishedAt, r.now())
		if err != nil {
			return result, err
		}
		changed, err := r.repository.UpsertMultimodalProjection(ctx, projection)
		if err != nil {
			return result, err
		}
		if changed {
			result.Upserted++
			inframetrics.ObserveMultimodalProjection("upserted", 1)
		} else {
			result.Unchanged++
			inframetrics.ObserveMultimodalProjection("unchanged", 1)
		}
	}
	return result, nil
}
