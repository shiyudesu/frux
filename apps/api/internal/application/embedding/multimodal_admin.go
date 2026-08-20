package applicationembedding

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

var ErrLoadAdminMultimodalJobs = errors.New("failed to load multimodal jobs")
var ErrRequeueAdminMultimodalJob = errors.New("failed to requeue multimodal job")

type AdminMultimodalRepository interface {
	ListAdminMultimodalJobs(context.Context, string, int64, int) ([]*domainembedding.MultimodalEmbeddingJob, error)
	CommitAdminMultimodalRequeue(
		context.Context,
		int64,
		string,
		func(*domainembedding.MultimodalEmbeddingJob, *domainembedding.MultimodalEmbeddingJob) (*domainadminaudit.Fact, error),
	) (*domainembedding.MultimodalEmbeddingJob, bool, error)
}

type AdminMultimodalService struct {
	repository AdminMultimodalRepository
	now        func() time.Time
}

type AdminMultimodalJobItem struct {
	JobID                    int64
	State                    string
	Attempts                 int
	MaxAttempts              int
	FailureCode              string
	ProviderAlias            string
	ModelAlias               string
	RevisionAlias            string
	Dimension                int
	TextCanonicalizer        string
	FrameSamplingPolicy      string
	ImagePreprocessingPolicy string
	FusionPolicy             string
	NextAttemptAt            time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
	CompletedAt              *time.Time
}

type AdminMultimodalJobPage struct {
	Items      []AdminMultimodalJobItem
	NextCursor string
	HasMore    bool
}

type AdminMultimodalRequeueRequest struct {
	ActorID        int64
	JobID          int64
	ReasonCode     string
	IdempotencyKey string
}

type adminMultimodalCursor struct {
	Version int    `json:"v"`
	State   string `json:"s"`
	JobID   int64  `json:"id"`
}

func NewAdminMultimodalService(repository AdminMultimodalRepository) *AdminMultimodalService {
	return &AdminMultimodalService{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

func (s *AdminMultimodalService) List(
	ctx context.Context,
	state string,
	cursor string,
	limit int,
) (*AdminMultimodalJobPage, error) {
	state = strings.ToLower(strings.TrimSpace(state))
	if state != "" && !domainembedding.ValidMultimodalJobState(state) {
		return nil, domainembedding.ErrInvalidMultimodalJob
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 || s == nil || s.repository == nil {
		return nil, domainembedding.ErrInvalidMultimodalJob
	}
	afterID, err := decodeAdminMultimodalCursor(cursor, state)
	if err != nil {
		return nil, err
	}
	jobs, err := s.repository.ListAdminMultimodalJobs(ctx, state, afterID, limit+1)
	if err != nil {
		return nil, ErrLoadAdminMultimodalJobs
	}
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	page := &AdminMultimodalJobPage{Items: make([]AdminMultimodalJobItem, 0, len(jobs)), HasMore: hasMore}
	for _, job := range jobs {
		page.Items = append(page.Items, adminMultimodalItem(job))
	}
	if hasMore && len(jobs) > 0 {
		page.NextCursor = encodeAdminMultimodalCursor(state, jobs[len(jobs)-1].ID)
	}
	return page, nil
}

func (s *AdminMultimodalService) Requeue(
	ctx context.Context,
	request AdminMultimodalRequeueRequest,
) (AdminMultimodalJobItem, bool, error) {
	reason := strings.ToLower(strings.TrimSpace(request.ReasonCode))
	key := strings.TrimSpace(request.IdempotencyKey)
	if s == nil || s.repository == nil || request.ActorID <= 0 || request.JobID <= 0 ||
		(reason != "operator_retry" && reason != "configuration_changed") || key == "" || len(key) > 128 {
		return AdminMultimodalJobItem{}, false, domainembedding.ErrInvalidMultimodalJob
	}
	job, replayed, err := s.repository.CommitAdminMultimodalRequeue(
		ctx, request.JobID, key,
		func(previous, next *domainembedding.MultimodalEmbeddingJob) (*domainadminaudit.Fact, error) {
			return applicationadminaudit.BuildSuccessFact(applicationadminaudit.BuildInput{
				ActorID: request.ActorID, Permission: domainaccount.PermissionGovernanceExecute,
				Action:     domainadminaudit.ActionMultimodalJobRequeue,
				TargetType: domainadminaudit.TargetMultimodalJob,
				TargetID:   strconv.FormatInt(request.JobID, 10),
				RequestID:  domainadminaudit.NewRequestID(), IdempotencyKey: key,
				Detail: map[string]string{
					"http_method": "POST", "route": "/api/admin/multimodal-jobs/:jobId/requeue",
					"reason_code": reason, "previous_state": previous.State,
					"new_state": next.State, "previous_attempts": strconv.Itoa(previous.Attempts),
				},
			}, s.now())
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, domainembedding.ErrMultimodalJobNotFound),
			errors.Is(err, domainembedding.ErrMultimodalOperationConflict),
			errors.Is(err, domainembedding.ErrInvalidMultimodalJob):
			return AdminMultimodalJobItem{}, false, err
		default:
			return AdminMultimodalJobItem{}, false, ErrRequeueAdminMultimodalJob
		}
	}
	return adminMultimodalItem(job), replayed, nil
}

func adminMultimodalItem(job *domainembedding.MultimodalEmbeddingJob) AdminMultimodalJobItem {
	if job == nil {
		return AdminMultimodalJobItem{}
	}
	return AdminMultimodalJobItem{
		JobID: job.ID, State: job.State, Attempts: job.Attempts, MaxAttempts: job.MaxAttempts,
		FailureCode: job.FailureCode, ProviderAlias: job.Contract.ProviderAlias,
		ModelAlias: job.Contract.ModelAlias, RevisionAlias: job.Contract.RevisionAlias,
		Dimension: job.Contract.Dimension, TextCanonicalizer: job.Contract.TextCanonicalizer,
		FrameSamplingPolicy:      job.Contract.FrameSamplingPolicy,
		ImagePreprocessingPolicy: job.Contract.ImagePreprocessingPolicy,
		FusionPolicy:             job.Contract.FusionPolicy, NextAttemptAt: job.NextAttemptAt,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, CompletedAt: job.CompletedAt,
	}
}

func encodeAdminMultimodalCursor(state string, jobID int64) string {
	content, _ := json.Marshal(adminMultimodalCursor{Version: 1, State: state, JobID: jobID})
	return base64.RawURLEncoding.EncodeToString(content)
}

func decodeAdminMultimodalCursor(value, state string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	content, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return 0, domainembedding.ErrInvalidMultimodalJob
	}
	var cursor adminMultimodalCursor
	if err := json.Unmarshal(content, &cursor); err != nil || cursor.Version != 1 ||
		cursor.State != state || cursor.JobID <= 0 {
		return 0, domainembedding.ErrInvalidMultimodalJob
	}
	return cursor.JobID, nil
}
