package interfaceshttprouter

import (
	"context"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
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
		if result.MediaAssetID > 0 && a.mediaPublication != nil {
			return a.mediaPublication.MediaReady(ctx, result.MediaAssetID)
		}
		if a.publisher == nil || a.videoReader == nil {
			return nil
		}
		video, err := a.videoReader.FindByIDAnyStatus(ctx, result.Case.VideoID)
		if err != nil {
			return err
		}
		if event := applicationvideo.NewPublishedEvent(video); event != nil {
			return a.publisher.PublishVideoPublished(ctx, event)
		}
	case domainreview.OutcomeReject:
		if result.MediaAssetID > 0 && a.mediaPublication != nil {
			return a.mediaPublication.ProtectVideo(ctx, result.Case.VideoID, result.MediaAssetID, result.CoverAssetID)
		}
	}
	return nil
}

type videoReviewIntakeAdapter struct {
	service *applicationreview.Service
}

func (a videoReviewIntakeAdapter) EnsureReviewCase(ctx context.Context, videoID int64) error {
	_, _, err := a.service.EnsureCase(ctx, videoID)
	return err
}
