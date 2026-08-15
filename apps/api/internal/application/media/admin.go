package applicationmedia

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const adminProcessingCursorVersion = 1

var ErrLoadAdminProcessingFailed = errors.New("failed to load media processing operations")
var ErrRetryAdminProcessingFailed = errors.New("failed to retry media processing")

type AdminProcessingRepository interface {
	SummarizeAdminProcessing(ctx context.Context) (*domainmedia.AdminProcessingSummary, error)
	ListActiveAdminProcessing(
		ctx context.Context,
		limit int,
	) ([]*domainmedia.MediaProcessingJob, error)
	ListAdminProcessingHistory(
		ctx context.Context,
		query domainmedia.AdminProcessingHistoryQuery,
	) ([]*domainmedia.MediaProcessingJob, error)
	FindProcessingJobByID(
		ctx context.Context,
		jobID int64,
	) (*domainmedia.MediaProcessingJob, error)
	CommitAdminProcessingRetry(
		ctx context.Context,
		command domainmedia.AdminProcessingRetryCommand,
		buildAudit func(domainmedia.ProcessingRetryAuditInput) (*domainadminaudit.Fact, error),
	) (*domainmedia.AdminProcessingRetryResult, error)
}

type AdminProcessingVideo struct {
	VideoID  int64
	AssetID  int64
	AuthorID int64
	Title    string
}

type AdminProcessingVideoCatalog interface {
	FindAdminProcessingVideosByAssetIDs(
		ctx context.Context,
		assetIDs []int64,
	) (map[int64]AdminProcessingVideo, error)
	FindAdminProcessingVideo(
		ctx context.Context,
		videoID int64,
	) (*AdminProcessingVideo, error)
}

type AdminProcessingService struct {
	repository   AdminProcessingRepository
	videos       AdminProcessingVideoCatalog
	cursorSecret []byte
	now          func() time.Time
}

type AdminProcessingOption func(*AdminProcessingService)

type AdminProcessingItem struct {
	Job      *domainmedia.MediaProcessingJob
	VideoID  int64
	AuthorID int64
	Title    string
}

type AdminProcessingOverview struct {
	Summary     *domainmedia.AdminProcessingSummary
	ActiveItems []*AdminProcessingItem
	RefreshedAt time.Time
}

type AdminProcessingHistoryRequest struct {
	State         string
	Step          string
	ErrorCode     string
	VideoID       int64
	CompletedFrom *time.Time
	CompletedTo   *time.Time
	Cursor        string
	Limit         int
}

type AdminProcessingHistoryPage struct {
	Items      []*AdminProcessingItem
	NextCursor string
	HasMore    bool
}

type AdminProcessingRetryRequest struct {
	ActorID        int64
	JobID          int64
	ReasonCode     string
	Note           string
	IdempotencyKey string
	Route          string
}

type AdminProcessingBulkRetryRequest struct {
	ActorID        int64
	JobIDs         []int64
	ReasonCode     string
	Note           string
	IdempotencyKey string
}

type AdminProcessingRetryItemResult struct {
	JobID     int64
	Status    string
	Item      *AdminProcessingItem
	ErrorCode string
	Replayed  bool
}

type adminProcessingCursorEnvelope struct {
	Version     int    `json:"v"`
	FilterHash  string `json:"f"`
	CompletedAt string `json:"t"`
	JobID       int64  `json:"id"`
}

func NewAdminProcessing(
	repository AdminProcessingRepository,
	videos AdminProcessingVideoCatalog,
	cursorSecret string,
	options ...AdminProcessingOption,
) *AdminProcessingService {
	service := &AdminProcessingService{
		repository: repository, videos: videos,
		cursorSecret: []byte(strings.TrimSpace(cursorSecret)),
		now:          func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithAdminProcessingClock(now func() time.Time) AdminProcessingOption {
	return func(service *AdminProcessingService) {
		if now != nil {
			service.now = now
		}
	}
}

func (s *AdminProcessingService) Overview(
	ctx context.Context,
) (*AdminProcessingOverview, error) {
	if s == nil || s.repository == nil || s.videos == nil {
		return nil, ErrLoadAdminProcessingFailed
	}
	summary, err := s.repository.SummarizeAdminProcessing(ctx)
	inframetrics.ObserveMediaAdminOverview(summary, err)
	if err != nil {
		return nil, ErrLoadAdminProcessingFailed
	}
	jobs, err := s.repository.ListActiveAdminProcessing(ctx, 100)
	if err != nil {
		return nil, ErrLoadAdminProcessingFailed
	}
	items, err := s.hydrate(ctx, jobs)
	if err != nil {
		return nil, ErrLoadAdminProcessingFailed
	}
	return &AdminProcessingOverview{
		Summary: summary, ActiveItems: items, RefreshedAt: s.now(),
	}, nil
}

func (s *AdminProcessingService) History(
	ctx context.Context,
	request AdminProcessingHistoryRequest,
) (*AdminProcessingHistoryPage, error) {
	if s == nil || s.repository == nil || s.videos == nil ||
		len(s.cursorSecret) == 0 {
		return nil, ErrLoadAdminProcessingFailed
	}
	query, filterHash, err := s.normalizeHistoryRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	query.Cursor, err = s.decodeCursor(request.Cursor, filterHash)
	if err != nil {
		return nil, err
	}
	jobs, err := s.repository.ListAdminProcessingHistory(ctx, query)
	if err != nil {
		return nil, ErrLoadAdminProcessingFailed
	}
	limit := query.Limit - 1
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	items, err := s.hydrate(ctx, jobs)
	if err != nil {
		return nil, ErrLoadAdminProcessingFailed
	}
	nextCursor := ""
	if hasMore && len(jobs) > 0 {
		last := jobs[len(jobs)-1]
		if last.CompletedAt != nil {
			nextCursor = s.encodeCursor(filterHash, &domainmedia.AdminProcessingCursor{
				CompletedAt: *last.CompletedAt, JobID: last.ID,
			})
		}
	}
	return &AdminProcessingHistoryPage{
		Items: items, NextCursor: nextCursor, HasMore: hasMore,
	}, nil
}

func (s *AdminProcessingService) Retry(
	ctx context.Context,
	request AdminProcessingRetryRequest,
) (*AdminProcessingRetryItemResult, error) {
	if s == nil || s.repository == nil || s.videos == nil || s.now == nil {
		return nil, ErrRetryAdminProcessingFailed
	}
	job, err := s.repository.FindProcessingJobByID(ctx, request.JobID)
	if err != nil {
		if errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
			return nil, err
		}
		return nil, ErrRetryAdminProcessingFailed
	}
	video, err := s.videoForAsset(ctx, job.AssetID)
	if err != nil {
		return nil, err
	}
	command, err := domainmedia.NormalizeAdminProcessingRetryCommand(
		domainmedia.AdminProcessingRetryCommand{
			ActorID: request.ActorID, JobID: request.JobID, VideoID: video.VideoID,
			ReasonCode: request.ReasonCode, Note: request.Note,
			Route:          retryRoute(request.Route),
			IdempotencyKey: request.IdempotencyKey, OccurredAt: s.now(),
		},
	)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.CommitAdminProcessingRetry(
		ctx,
		command,
		func(input domainmedia.ProcessingRetryAuditInput) (*domainadminaudit.Fact, error) {
			return applicationadminaudit.BuildSuccessFact(applicationadminaudit.BuildInput{
				ActorID:        command.ActorID,
				Permission:     domainaccount.PermissionContentEnforce,
				Action:         domainadminaudit.ActionMediaProcessingRetry,
				TargetType:     domainadminaudit.TargetMediaProcessingJob,
				TargetID:       strconv.FormatInt(command.JobID, 10),
				RequestID:      domainadminaudit.NewRequestID(),
				IdempotencyKey: command.IdempotencyKey,
				Detail: map[string]string{
					"http_method":       "POST",
					"route":             command.Route,
					"reason_code":       command.ReasonCode,
					"video_id":          strconv.FormatInt(input.VideoID, 10),
					"previous_state":    input.PreviousState,
					"new_state":         input.NewState,
					"previous_attempts": strconv.Itoa(input.PreviousAttempts),
				},
			}, command.OccurredAt)
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, domainmedia.ErrProcessingJobNotFound),
			errors.Is(err, domainmedia.ErrProcessingRetryConflict),
			errors.Is(err, domainmedia.ErrProcessingRetryIdempotencyConflict):
			inframetrics.ObserveMediaAdminRetry("conflict")
			return nil, err
		default:
			inframetrics.ObserveMediaAdminRetry("failure")
			return nil, ErrRetryAdminProcessingFailed
		}
	}
	items, err := s.hydrate(ctx, []*domainmedia.MediaProcessingJob{result.Job})
	if err != nil || len(items) != 1 {
		return nil, ErrRetryAdminProcessingFailed
	}
	metricResult := "retried"
	if result.Replayed {
		metricResult = "replayed"
	}
	inframetrics.ObserveMediaAdminRetry(metricResult)
	return &AdminProcessingRetryItemResult{
		JobID: result.Job.ID, Status: "retried", Item: items[0],
		Replayed: result.Replayed,
	}, nil
}

func (s *AdminProcessingService) BulkRetry(
	ctx context.Context,
	request AdminProcessingBulkRetryRequest,
) ([]AdminProcessingRetryItemResult, error) {
	if len(request.JobIDs) == 0 ||
		len(request.JobIDs) > domainmedia.MaxAdminRetryBatchSize ||
		strings.TrimSpace(request.IdempotencyKey) == "" {
		return nil, domainmedia.ErrInvalidProcessingRetry
	}
	seen := make(map[int64]struct{}, len(request.JobIDs))
	for _, jobID := range request.JobIDs {
		if jobID <= 0 {
			return nil, domainmedia.ErrInvalidProcessingRetry
		}
		if _, exists := seen[jobID]; exists {
			return nil, domainmedia.ErrInvalidProcessingRetry
		}
		seen[jobID] = struct{}{}
	}
	results := make([]AdminProcessingRetryItemResult, 0, len(request.JobIDs))
	for _, jobID := range request.JobIDs {
		key := deriveBulkIdempotencyKey(request.IdempotencyKey, jobID)
		item, err := s.Retry(ctx, AdminProcessingRetryRequest{
			ActorID: request.ActorID, JobID: jobID,
			ReasonCode: request.ReasonCode, Note: request.Note,
			Route:          "/api/admin/media-processing/jobs/bulk-retry",
			IdempotencyKey: key,
		})
		if err == nil {
			results = append(results, *item)
			continue
		}
		status := "rejected"
		code := "MEDIA_PROCESSING_RETRY_REJECTED"
		if errors.Is(err, domainmedia.ErrProcessingRetryConflict) ||
			errors.Is(err, domainmedia.ErrProcessingRetryIdempotencyConflict) {
			status = "conflict"
			code = "MEDIA_PROCESSING_RETRY_CONFLICT"
		} else if errors.Is(err, domainmedia.ErrProcessingJobNotFound) {
			code = "MEDIA_PROCESSING_JOB_NOT_FOUND"
		}
		results = append(results, AdminProcessingRetryItemResult{
			JobID: jobID, Status: status, ErrorCode: code,
		})
	}
	return results, nil
}

func (s *AdminProcessingService) hydrate(
	ctx context.Context,
	jobs []*domainmedia.MediaProcessingJob,
) ([]*AdminProcessingItem, error) {
	assetIDs := make([]int64, 0, len(jobs))
	for _, job := range jobs {
		if job != nil {
			assetIDs = append(assetIDs, job.AssetID)
		}
	}
	videos, err := s.videos.FindAdminProcessingVideosByAssetIDs(ctx, assetIDs)
	if err != nil {
		return nil, err
	}
	items := make([]*AdminProcessingItem, 0, len(jobs))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		video := videos[job.AssetID]
		items = append(items, &AdminProcessingItem{
			Job: job, VideoID: video.VideoID, AuthorID: video.AuthorID, Title: video.Title,
		})
	}
	return items, nil
}

func (s *AdminProcessingService) videoForAsset(
	ctx context.Context,
	assetID int64,
) (*AdminProcessingVideo, error) {
	videos, err := s.videos.FindAdminProcessingVideosByAssetIDs(ctx, []int64{assetID})
	if err != nil {
		return nil, ErrLoadAdminProcessingFailed
	}
	video, ok := videos[assetID]
	if !ok || video.VideoID <= 0 {
		return nil, domainmedia.ErrProcessingRetryConflict
	}
	return &video, nil
}

func (s *AdminProcessingService) normalizeHistoryRequest(
	ctx context.Context,
	request AdminProcessingHistoryRequest,
) (domainmedia.AdminProcessingHistoryQuery, string, error) {
	assetID := int64(0)
	if request.VideoID > 0 {
		video, err := s.videos.FindAdminProcessingVideo(ctx, request.VideoID)
		if err != nil {
			return domainmedia.AdminProcessingHistoryQuery{}, "", err
		}
		if video == nil || video.AssetID <= 0 {
			return domainmedia.AdminProcessingHistoryQuery{}, "",
				domainmedia.ErrInvalidProcessingAdminQuery
		}
		assetID = video.AssetID
	}
	limit := request.Limit
	if limit == 0 {
		limit = 20
	}
	query, err := domainmedia.NormalizeAdminProcessingHistoryQuery(
		domainmedia.AdminProcessingHistoryQuery{
			State: request.State, Step: request.Step, ErrorCode: request.ErrorCode,
			AssetID: assetID, CompletedFrom: request.CompletedFrom,
			CompletedTo: request.CompletedTo, Limit: limit + 1,
		},
	)
	if err != nil {
		return domainmedia.AdminProcessingHistoryQuery{}, "", err
	}
	payload, _ := json.Marshal(struct {
		State     string `json:"state"`
		Step      string `json:"step"`
		ErrorCode string `json:"error_code"`
		AssetID   int64  `json:"asset_id"`
		From      string `json:"from"`
		To        string `json:"to"`
	}{
		State: query.State, Step: query.Step, ErrorCode: query.ErrorCode, AssetID: query.AssetID,
		From: formatOptionalTime(query.CompletedFrom), To: formatOptionalTime(query.CompletedTo),
	})
	sum := sha256.Sum256(payload)
	return query, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func (s *AdminProcessingService) encodeCursor(
	filterHash string,
	cursor *domainmedia.AdminProcessingCursor,
) string {
	payload, err := json.Marshal(adminProcessingCursorEnvelope{
		Version: adminProcessingCursorVersion, FilterHash: filterHash,
		CompletedAt: cursor.CompletedAt.UTC().Format(time.RFC3339Nano), JobID: cursor.JobID,
	})
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, s.cursorSecret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *AdminProcessingService) decodeCursor(
	raw, filterHash string,
) (*domainmedia.AdminProcessingCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, domainmedia.ErrInvalidProcessingAdminCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, domainmedia.ErrInvalidProcessingAdminCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, domainmedia.ErrInvalidProcessingAdminCursor
	}
	mac := hmac.New(sha256.New, s.cursorSecret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, domainmedia.ErrInvalidProcessingAdminCursor
	}
	var envelope adminProcessingCursorEnvelope
	if json.Unmarshal(payload, &envelope) != nil ||
		envelope.Version != adminProcessingCursorVersion ||
		envelope.FilterHash != filterHash || envelope.JobID <= 0 {
		return nil, domainmedia.ErrInvalidProcessingAdminCursor
	}
	completedAt, err := time.Parse(time.RFC3339Nano, envelope.CompletedAt)
	if err != nil || completedAt.IsZero() {
		return nil, domainmedia.ErrInvalidProcessingAdminCursor
	}
	return &domainmedia.AdminProcessingCursor{
		CompletedAt: completedAt.UTC(), JobID: envelope.JobID,
	}, nil
}

func deriveBulkIdempotencyKey(base string, jobID int64) string {
	sum := sha256.Sum256([]byte(
		strings.TrimSpace(base) + "\x00" + strconv.FormatInt(jobID, 10),
	))
	return "bulk:" + hex.EncodeToString(sum[:])
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func retryRoute(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/api/admin/media-processing/jobs/:jobId/retry"
	}
	return value
}
