package applicationmedia

import (
	"context"
	"errors"
	"testing"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestAdminProcessingOverviewHydratesVideos(t *testing.T) {
	now := time.Now().UTC()
	repository := &adminProcessingRepositoryStub{
		summary: &domainmedia.AdminProcessingSummary{Waiting: 1},
		active: []*domainmedia.MediaProcessingJob{{
			ID: 7, AssetID: 11, State: domainmedia.JobStatePending,
			ProcessingStep: domainmedia.ProcessingStepWaiting,
		}},
	}
	service := NewAdminProcessing(
		repository,
		adminProcessingVideoCatalogStub{videos: map[int64]AdminProcessingVideo{
			11: {VideoID: 5, AssetID: 11, AuthorID: 3, Title: "Waiting"},
		}},
		"secret",
		WithAdminProcessingClock(func() time.Time { return now }),
	)
	overview, err := service.Overview(context.Background())
	if err != nil || overview.Summary.Waiting != 1 ||
		len(overview.ActiveItems) != 1 ||
		overview.ActiveItems[0].VideoID != 5 ||
		overview.ActiveItems[0].Title != "Waiting" {
		t.Fatalf("overview=%+v err=%v", overview, err)
	}
}

func TestAdminProcessingHistoryCursorBindsFilters(t *testing.T) {
	now := time.Now().UTC()
	repository := &adminProcessingRepositoryStub{
		history: []*domainmedia.MediaProcessingJob{
			{ID: 3, AssetID: 12, State: domainmedia.JobStateFailed, CompletedAt: timePointer(now)},
			{ID: 2, AssetID: 13, State: domainmedia.JobStateFailed, CompletedAt: timePointer(now.Add(-time.Minute))},
		},
	}
	service := NewAdminProcessing(
		repository,
		adminProcessingVideoCatalogStub{videos: map[int64]AdminProcessingVideo{
			12: {VideoID: 6, AssetID: 12}, 13: {VideoID: 7, AssetID: 13},
		}},
		"secret",
	)
	page, err := service.History(context.Background(), AdminProcessingHistoryRequest{
		State: domainmedia.JobStateFailed, Limit: 1,
	})
	if err != nil || !page.HasMore || page.NextCursor == "" || len(page.Items) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := service.History(context.Background(), AdminProcessingHistoryRequest{
		State: domainmedia.JobStateCompleted, Limit: 1, Cursor: page.NextCursor,
	}); !errors.Is(err, domainmedia.ErrInvalidProcessingAdminCursor) {
		t.Fatalf("cross-filter cursor error=%v", err)
	}
}

func TestAdminProcessingRetryAndBulkOutcomes(t *testing.T) {
	now := time.Now().UTC()
	repository := &adminProcessingRepositoryStub{
		jobs: map[int64]*domainmedia.MediaProcessingJob{
			1: {ID: 1, AssetID: 21, State: domainmedia.JobStateFailed, Attempts: 5},
			2: {ID: 2, AssetID: 22, State: domainmedia.JobStateCompleted},
		},
	}
	service := NewAdminProcessing(
		repository,
		adminProcessingVideoCatalogStub{videos: map[int64]AdminProcessingVideo{
			21: {VideoID: 31, AssetID: 21, Title: "Failed"},
			22: {VideoID: 32, AssetID: 22, Title: "Done"},
		}},
		"secret",
		WithAdminProcessingClock(func() time.Time { return now }),
	)
	result, err := service.Retry(context.Background(), AdminProcessingRetryRequest{
		ActorID: 9, JobID: 1,
		ReasonCode:     domainmedia.ProcessingRetryReasonTemporaryFailure,
		IdempotencyKey: "single-key",
	})
	if err != nil || result.Status != "retried" || result.Item.Job.State != domainmedia.JobStateRetryable {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	repository.jobs[1].State = domainmedia.JobStateFailed
	repository.jobs[1].Attempts = 1
	results, err := service.BulkRetry(context.Background(), AdminProcessingBulkRetryRequest{
		ActorID: 9, JobIDs: []int64{1, 2},
		ReasonCode:     domainmedia.ProcessingRetryReasonOperatorRetry,
		IdempotencyKey: "bulk-key",
	})
	if err != nil || len(results) != 2 ||
		results[0].Status != "retried" || results[1].Status != "conflict" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

type adminProcessingRepositoryStub struct {
	summary *domainmedia.AdminProcessingSummary
	active  []*domainmedia.MediaProcessingJob
	history []*domainmedia.MediaProcessingJob
	jobs    map[int64]*domainmedia.MediaProcessingJob
}

func (s *adminProcessingRepositoryStub) SummarizeAdminProcessing(
	context.Context,
) (*domainmedia.AdminProcessingSummary, error) {
	return s.summary, nil
}

func (s *adminProcessingRepositoryStub) ListActiveAdminProcessing(
	context.Context, int,
) ([]*domainmedia.MediaProcessingJob, error) {
	return s.active, nil
}

func (s *adminProcessingRepositoryStub) ListAdminProcessingHistory(
	_ context.Context,
	query domainmedia.AdminProcessingHistoryQuery,
) ([]*domainmedia.MediaProcessingJob, error) {
	result := make([]*domainmedia.MediaProcessingJob, 0, len(s.history))
	for _, job := range s.history {
		if query.State == "" || job.State == query.State {
			result = append(result, job)
		}
	}
	return result, nil
}

func (s *adminProcessingRepositoryStub) FindProcessingJobByID(
	_ context.Context,
	jobID int64,
) (*domainmedia.MediaProcessingJob, error) {
	job := s.jobs[jobID]
	if job == nil {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	copy := *job
	return &copy, nil
}

func (s *adminProcessingRepositoryStub) CommitAdminProcessingRetry(
	_ context.Context,
	command domainmedia.AdminProcessingRetryCommand,
	buildAudit func(domainmedia.ProcessingRetryAuditInput) (*domainadminaudit.Fact, error),
) (*domainmedia.AdminProcessingRetryResult, error) {
	job := s.jobs[command.JobID]
	if job == nil {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	if job.State != domainmedia.JobStateFailed {
		return nil, domainmedia.ErrProcessingRetryConflict
	}
	if _, err := buildAudit(domainmedia.ProcessingRetryAuditInput{
		AssetID: job.AssetID, VideoID: command.VideoID,
		PreviousState:    domainmedia.JobStateFailed,
		NewState:         domainmedia.JobStateRetryable,
		PreviousAttempts: job.Attempts,
	}); err != nil {
		return nil, err
	}
	copy := *job
	copy.State = domainmedia.JobStateRetryable
	copy.Attempts = 0
	copy.ProcessingStep = domainmedia.ProcessingStepWaiting
	s.jobs[command.JobID] = &copy
	return &domainmedia.AdminProcessingRetryResult{Job: &copy}, nil
}

type adminProcessingVideoCatalogStub struct {
	videos map[int64]AdminProcessingVideo
}

func (s adminProcessingVideoCatalogStub) FindAdminProcessingVideosByAssetIDs(
	_ context.Context,
	assetIDs []int64,
) (map[int64]AdminProcessingVideo, error) {
	result := make(map[int64]AdminProcessingVideo, len(assetIDs))
	for _, assetID := range assetIDs {
		if video, ok := s.videos[assetID]; ok {
			result[assetID] = video
		}
	}
	return result, nil
}

func (s adminProcessingVideoCatalogStub) FindAdminProcessingVideo(
	_ context.Context,
	videoID int64,
) (*AdminProcessingVideo, error) {
	for _, video := range s.videos {
		if video.VideoID == videoID {
			copy := video
			return &copy, nil
		}
	}
	return nil, domainmedia.ErrProcessingRetryConflict
}
