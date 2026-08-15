package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrajwt "github.com/shiyudesu/frux/internal/infra/jwt"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmedia "github.com/shiyudesu/frux/internal/interfaces/http/media"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
)

type adminMediaAPIMemoryRepo struct {
	mu         sync.Mutex
	jobs       map[int64]*domainmedia.MediaProcessingJob
	operations map[string]string
}

func (r *adminMediaAPIMemoryRepo) SummarizeAdminProcessing(
	context.Context,
) (*domainmedia.AdminProcessingSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	summary := &domainmedia.AdminProcessingSummary{}
	for _, job := range r.jobs {
		switch job.State {
		case domainmedia.JobStatePending, domainmedia.JobStateRetryable:
			summary.Waiting++
		case domainmedia.JobStateProcessing:
			summary.Processing++
		case domainmedia.JobStateFailed:
			summary.Failed++
		case domainmedia.JobStateCompleted:
			summary.Completed++
		}
	}
	return summary, nil
}

func (r *adminMediaAPIMemoryRepo) ListActiveAdminProcessing(
	context.Context, int,
) ([]*domainmedia.MediaProcessingJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []*domainmedia.MediaProcessingJob{}
	for _, job := range r.jobs {
		if job.State == domainmedia.JobStatePending ||
			job.State == domainmedia.JobStateProcessing ||
			job.State == domainmedia.JobStateRetryable {
			copy := *job
			result = append(result, &copy)
		}
	}
	return result, nil
}

func (r *adminMediaAPIMemoryRepo) ListAdminProcessingHistory(
	_ context.Context,
	query domainmedia.AdminProcessingHistoryQuery,
) ([]*domainmedia.MediaProcessingJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []*domainmedia.MediaProcessingJob{}
	for _, job := range r.jobs {
		if job.State != domainmedia.JobStateFailed &&
			job.State != domainmedia.JobStateCompleted {
			continue
		}
		if query.State != "" && job.State != query.State {
			continue
		}
		copy := *job
		result = append(result, &copy)
	}
	return result, nil
}

func (r *adminMediaAPIMemoryRepo) FindProcessingJobByID(
	_ context.Context,
	jobID int64,
) (*domainmedia.MediaProcessingJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[jobID]
	if job == nil {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	copy := *job
	return &copy, nil
}

func (r *adminMediaAPIMemoryRepo) CommitAdminProcessingRetry(
	_ context.Context,
	command domainmedia.AdminProcessingRetryCommand,
	buildAudit func(domainmedia.ProcessingRetryAuditInput) (*domainadminaudit.Fact, error),
) (*domainmedia.AdminProcessingRetryResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := command.IdempotencyKey
	if fingerprint, ok := r.operations[key]; ok {
		if fingerprint != command.Fingerprint() {
			return nil, domainmedia.ErrProcessingRetryIdempotencyConflict
		}
		copy := *r.jobs[command.JobID]
		return &domainmedia.AdminProcessingRetryResult{Job: &copy, Replayed: true}, nil
	}
	job := r.jobs[command.JobID]
	if job == nil {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	if job.State != domainmedia.JobStateFailed {
		return nil, domainmedia.ErrProcessingRetryConflict
	}
	if _, err := buildAudit(domainmedia.ProcessingRetryAuditInput{
		AssetID: job.AssetID, VideoID: command.VideoID,
		PreviousState: job.State, NewState: domainmedia.JobStateRetryable,
		PreviousAttempts: job.Attempts,
	}); err != nil {
		return nil, err
	}
	job.State = domainmedia.JobStateRetryable
	job.Attempts = 0
	job.ErrorCode, job.ErrorMessage = "", ""
	job.ProcessingStep = domainmedia.ProcessingStepWaiting
	job.CompletedAt = nil
	job.NextAttemptAt = command.OccurredAt
	r.operations[key] = command.Fingerprint()
	copy := *job
	return &domainmedia.AdminProcessingRetryResult{Job: &copy}, nil
}

type adminMediaAPIVideoCatalog struct {
	videos map[int64]applicationmedia.AdminProcessingVideo
}

func (c adminMediaAPIVideoCatalog) FindAdminProcessingVideosByAssetIDs(
	_ context.Context,
	assetIDs []int64,
) (map[int64]applicationmedia.AdminProcessingVideo, error) {
	result := make(map[int64]applicationmedia.AdminProcessingVideo, len(assetIDs))
	for _, assetID := range assetIDs {
		if video, ok := c.videos[assetID]; ok {
			result[assetID] = video
		}
	}
	return result, nil
}

func (c adminMediaAPIVideoCatalog) FindAdminProcessingVideo(
	_ context.Context,
	videoID int64,
) (*applicationmedia.AdminProcessingVideo, error) {
	for _, video := range c.videos {
		if video.VideoID == videoID {
			copy := video
			return &copy, nil
		}
	}
	return nil, errors.New("video not found")
}

func TestAdminMediaProcessingAPIFlow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	completedAt := now.Add(-time.Minute)
	repository := &adminMediaAPIMemoryRepo{
		jobs: map[int64]*domainmedia.MediaProcessingJob{
			1: {
				ID: 1, AssetID: 11, ProfileVersion: "v2",
				State: domainmedia.JobStateProcessing, Attempts: 1, MaxAttempts: 5,
				ProcessingStep: domainmedia.ProcessingStepTranscoding,
				ProgressBPS:    intPointer(4200), ProgressUpdatedAt: &now,
				CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
			},
			2: {
				ID: 2, AssetID: 12, ProfileVersion: "v2",
				State: domainmedia.JobStateFailed, Attempts: 5, MaxAttempts: 5,
				ProcessingStep: domainmedia.ProcessingStepFailed,
				ErrorCode:      "transcode_failed", ErrorMessage: "diagnostic",
				CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
				CompletedAt: &completedAt,
			},
			3: {
				ID: 3, AssetID: 13, ProfileVersion: "v2",
				State: domainmedia.JobStateFailed, Attempts: 2, MaxAttempts: 5,
				ProcessingStep: domainmedia.ProcessingStepFailed,
				ErrorCode:      "temporary_failure", CreatedAt: now.Add(-3 * time.Hour),
				UpdatedAt: now, CompletedAt: &completedAt,
			},
		},
		operations: make(map[string]string),
	}
	catalog := adminMediaAPIVideoCatalog{videos: map[int64]applicationmedia.AdminProcessingVideo{
		11: {VideoID: 21, AssetID: 11, AuthorID: 3, Title: "Processing"},
		12: {VideoID: 22, AssetID: 12, AuthorID: 4, Title: "Failed"},
		13: {VideoID: 23, AssetID: 13, AuthorID: 5, Title: "Also failed"},
	}}
	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatal(err)
	}
	principals := managedAccountPrincipalReader{principals: map[int64]*domainaccount.AdminPrincipal{
		100: domainaccount.RestoreAdminPrincipal(100, domainaccount.StatusNormal, domainaccount.RoleAdmin),
		101: domainaccount.RestoreAdminPrincipal(101, domainaccount.StatusNormal, domainaccount.RoleReviewer),
	}}
	service := applicationmedia.NewAdminProcessing(repository, catalog, "cursor-secret")
	handler := interfaceshttpmedia.NewAdmin(service)
	router := server.New(server.WithDisablePrintRoute(true))
	admin := router.Group("/api/admin", interfaceshttpmiddleware.NewAdminJWTAuth(jwtManager))
	admin.GET("/media-processing/overview", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionContentEnforce,
	), handler.Overview)
	admin.GET("/media-processing/history", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionContentEnforce,
	), handler.History)
	admin.POST("/media-processing/jobs/:jobId/retry", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionContentEnforce,
	), handler.Retry)
	admin.POST("/media-processing/jobs/bulk-retry", interfaceshttpmiddleware.NewRequireAdminPermission(
		principals, domainaccount.PermissionContentEnforce,
	), handler.BulkRetry)

	adminToken := signAdminAuthorizationToken(t, jwtManager, 100, domainaccount.RoleUser)
	reviewerToken := signAdminAuthorizationToken(t, jwtManager, 101, domainaccount.RoleUser)
	overview := performManagedAccountRequest(
		router, http.MethodGet, "/api/admin/media-processing/overview",
		"", adminToken, "",
	)
	if overview.Code != http.StatusOK ||
		!strings.Contains(overview.Body.String(), `"processing":1`) ||
		!strings.Contains(overview.Body.String(), `"stage_progress_bps":4200`) {
		t.Fatalf("overview status=%d body=%s", overview.Code, overview.Body.String())
	}
	forbidden := performManagedAccountRequest(
		router, http.MethodGet, "/api/admin/media-processing/overview",
		"", reviewerToken, "",
	)
	requireManagedAccountAPIError(
		t, forbidden, http.StatusForbidden, interfaceshttpapierror.CodeAdminPermissionDenied,
	)
	history := performManagedAccountRequest(
		router, http.MethodGet,
		"/api/admin/media-processing/history?state=failed&limit=20",
		"", adminToken, "",
	)
	if history.Code != http.StatusOK ||
		!strings.Contains(history.Body.String(), `"error_code":"transcode_failed"`) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	retry := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/media-processing/jobs/2/retry",
		`{"reason_code":"temporary_failure","note":"retry"}`, adminToken, "retry-key",
	)
	if retry.Code != http.StatusOK ||
		!strings.Contains(retry.Body.String(), `"state":"retryable"`) ||
		!strings.Contains(retry.Body.String(), `"audit_committed":true`) {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	replay := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/media-processing/jobs/2/retry",
		`{"reason_code":"temporary_failure","note":"retry"}`, adminToken, "retry-key",
	)
	if replay.Code != http.StatusOK ||
		!strings.Contains(replay.Body.String(), `"replayed":true`) {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	bulk := performManagedAccountRequest(
		router, http.MethodPost, "/api/admin/media-processing/jobs/bulk-retry",
		`{"job_ids":[2,3],"reason_code":"operator_retry","note":""}`,
		adminToken, "bulk-key",
	)
	if bulk.Code != http.StatusOK ||
		!strings.Contains(bulk.Body.String(), `"job_id":2,"status":"conflict"`) ||
		!strings.Contains(bulk.Body.String(), `"job_id":3,"status":"retried"`) {
		t.Fatalf("bulk status=%d body=%s", bulk.Code, bulk.Body.String())
	}
}

func intPointer(value int) *int {
	return &value
}
