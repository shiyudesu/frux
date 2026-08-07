package interfaceshttprouter

import (
	"context"
	"strings"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type reviewMetricsAdapter struct{}

func (reviewMetricsAdapter) Observe(stage, result string) {
	inframetrics.ObserveReview(stage, result)
}

func (reviewMetricsAdapter) ObserveHuman(operation, result string) {
	inframetrics.ObserveHumanReview(operation, result)
}

func (reviewMetricsAdapter) ObserveHumanQueue(available int, oldestAge time.Duration) {
	inframetrics.ObserveHumanReviewQueue(available, oldestAge)
}

func (reviewMetricsAdapter) ObserveHumanNotification(result string) {
	inframetrics.ObserveHumanReviewNotification(result)
}

type reviewOutcomeApplier struct {
	videoReader interface {
		FindByIDAnyStatus(ctx context.Context, videoID int64) (*domainvideo.Video, error)
	}
	mediaPublication interface {
		MediaReady(ctx context.Context, assetID int64) error
		ProtectVideo(ctx context.Context, videoID, mediaAssetID, coverAssetID int64) error
	}
	publisher        applicationvideo.PublishedEventPublisher
	cacheInvalidator applicationvideo.VideoCacheInvalidator
}

func (a reviewOutcomeApplier) ApplyReviewOutcome(ctx context.Context, result *domainreview.ProcessingResult) error {
	if result == nil || result.Decision == nil || result.Case == nil {
		return nil
	}
	if a.cacheInvalidator != nil {
		_ = a.cacheInvalidator.InvalidateVideo(ctx, result.Case.VideoID)
	}
	switch result.Decision.Outcome {
	case domainreview.OutcomeApprove:
		if a.videoReader != nil {
			video, err := a.videoReader.FindByIDAnyStatus(ctx, result.Case.VideoID)
			if err != nil {
				return err
			}
			if video == nil ||
				(!domainmedia.IsPublicReadyStatus(video.MediaStatus) &&
					video.MediaErrorCode != "publication_event_failed") {
				return nil
			}
		}
		if result.MediaAssetID > 0 && a.mediaPublication != nil {
			return a.mediaPublication.MediaReady(ctx, result.MediaAssetID)
		}
		if a.publisher == nil || a.videoReader == nil {
			if tracker, ok := a.videoReader.(reviewPublicationTracker); ok {
				return tracker.MarkLifecyclePublicationReady(
					ctx,
					domainmessage.PublicationEventID(result.Case.VideoID, result.Case.ReviewVersion),
					time.Now().UTC(),
				)
			}
			return nil
		}
		video, err := a.videoReader.FindByIDAnyStatus(ctx, result.Case.VideoID)
		if err != nil {
			return err
		}
		if event := applicationvideo.NewPublishedEvent(video); event != nil {
			if tracker, ok := a.videoReader.(reviewPublicationTracker); ok {
				ready, err := tracker.LifecyclePublicationReady(ctx, event.EventID)
				if err != nil {
					return err
				}
				if ready {
					return nil
				}
			}
			if err := a.publisher.PublishVideoPublished(ctx, event); err != nil {
				return err
			}
			if tracker, ok := a.videoReader.(reviewPublicationTracker); ok {
				return tracker.MarkLifecyclePublicationReady(ctx, event.EventID, time.Now().UTC())
			}
		}
	case domainreview.OutcomeReject:
		if result.MediaAssetID > 0 && a.mediaPublication != nil {
			return a.mediaPublication.ProtectVideo(ctx, result.Case.VideoID, result.MediaAssetID, result.CoverAssetID)
		}
	}
	return nil
}

type reviewPublicationTracker interface {
	LifecyclePublicationReady(ctx context.Context, eventID string) (bool, error)
	MarkLifecyclePublicationReady(ctx context.Context, eventID string, readyAt time.Time) error
}

type videoReviewIntakeAdapter struct {
	service *applicationreview.Service
}

func (a videoReviewIntakeAdapter) EnsureReviewCase(ctx context.Context, videoID int64) error {
	_, _, err := a.service.EnsureCase(ctx, videoID)
	return err
}

type localReviewPreviewSigner interface {
	Sign(objectKey string, expiry time.Duration) (string, time.Time, error)
}

type reviewPreviewMediaRepository interface {
	FindAssetByID(ctx context.Context, assetID int64) (*domainmedia.MediaAsset, error)
	ListReadyVariants(ctx context.Context, assetID int64) ([]*domainmedia.MediaVariant, error)
}

type reviewPreviewProvider struct {
	repository  reviewPreviewMediaRepository
	resolver    domainmedia.MediaURLResolver
	localSigner localReviewPreviewSigner
}

func (p reviewPreviewProvider) ResolveHumanPreview(
	ctx context.Context,
	subject domainreview.ReviewSubject,
	expiry time.Duration,
) (*domainreview.HumanPreviewAccess, error) {
	mediaURL, mediaExpiry, err := p.resolve(
		ctx, subject.MediaAssetID, subject.MediaURL, domainmedia.VariantRoleBaseline, expiry,
	)
	if err != nil || strings.TrimSpace(mediaURL) == "" {
		return nil, domainreview.ErrReviewPreviewUnavailable
	}
	coverURL, coverExpiry, coverErr := p.resolve(
		ctx, subject.CoverAssetID, subject.CoverURL, domainmedia.VariantRoleCover, expiry,
	)
	if coverErr != nil && subject.CoverAssetID > 0 {
		return nil, domainreview.ErrReviewPreviewUnavailable
	}
	expiresAt := mediaExpiry
	if !coverExpiry.IsZero() && (expiresAt.IsZero() || coverExpiry.Before(expiresAt)) {
		expiresAt = coverExpiry
	}
	return &domainreview.HumanPreviewAccess{
		MediaURL: mediaURL, CoverURL: coverURL, ExpiresAt: expiresAt,
	}, nil
}

func (p reviewPreviewProvider) resolve(
	ctx context.Context,
	assetID int64,
	compatibilityURL, role string,
	expiry time.Duration,
) (string, time.Time, error) {
	if assetID > 0 {
		if p.repository == nil {
			return "", time.Time{}, domainreview.ErrReviewPreviewUnavailable
		}
		asset, err := p.repository.FindAssetByID(ctx, assetID)
		if err != nil || asset == nil || asset.State == domainmedia.AssetStateDeleted {
			return "", time.Time{}, domainreview.ErrReviewPreviewUnavailable
		}
		objectKey := asset.ObjectKey
		variants, err := p.repository.ListReadyVariants(ctx, assetID)
		if err != nil {
			return "", time.Time{}, err
		}
		for _, variant := range variants {
			if variant != nil && variant.Role == role {
				objectKey = variant.ObjectKey
				break
			}
		}
		if asset.StorageBackend == domainmedia.StorageBackendLocal {
			if p.localSigner == nil {
				return "", time.Time{}, domainreview.ErrReviewPreviewUnavailable
			}
			return p.localSigner.Sign(objectKey, expiry)
		}
		if p.resolver == nil {
			return "", time.Time{}, domainreview.ErrReviewPreviewUnavailable
		}
		return p.resolver.ProtectedURL(ctx, objectKey, expiry)
	}

	rawURL := strings.TrimSpace(compatibilityURL)
	if rawURL == "" {
		return "", time.Time{}, nil
	}
	for _, prefix := range []string{"/uploads/", "/media/"} {
		if strings.HasPrefix(rawURL, prefix) {
			if p.localSigner == nil {
				return "", time.Time{}, domainreview.ErrReviewPreviewUnavailable
			}
			return p.localSigner.Sign(strings.TrimPrefix(rawURL, prefix), expiry)
		}
	}
	return "", time.Time{}, domainreview.ErrReviewPreviewUnavailable
}
