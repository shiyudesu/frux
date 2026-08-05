package applicationrecommendation

import (
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"context"
	"errors"
	"time"
)

var ErrRecommendationPolicyRepositoryUnavailable = errors.New("recommendation policy repository is unavailable")
var ErrRecommendationProfileRepositoryUnavailable = errors.New("recommendation profile repository is unavailable")
var ErrRecommendationRequestLogRepositoryUnavailable = errors.New("recommendation request log repository is unavailable")
var ErrRecommendationPolicyNotSelected = errors.New("recommendation policy is not selected for this cohort")

type PolicyService struct {
	repo domainrecommendation.PolicyRepository
	now  func() time.Time
}

type PolicyInput struct {
	Scene   string
	Version int
	Enabled bool
	Config  domainrecommendation.PolicyConfiguration
}

func NewPolicyService(repo domainrecommendation.PolicyRepository, now func() time.Time) *PolicyService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PolicyService{repo: repo, now: now}
}

func (s *PolicyService) Create(ctx context.Context, input PolicyInput) (*domainrecommendation.Policy, error) {
	if s == nil || s.repo == nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	policy, err := domainrecommendation.NewPolicy(input.Scene, input.Version, input.Enabled, input.Config, s.now())
	if err != nil {
		return nil, err
	}
	return s.repo.CreatePolicy(ctx, policy)
}

func (s *PolicyService) Activate(ctx context.Context, scene string, version int) (*domainrecommendation.Policy, error) {
	if s == nil || s.repo == nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	return s.repo.ActivatePolicy(ctx, scene, version)
}

func (s *PolicyService) Rollback(ctx context.Context, scene string, version int) (*domainrecommendation.Policy, error) {
	if s == nil || s.repo == nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	return s.repo.RollbackPolicy(ctx, scene, version)
}

func (s *PolicyService) Select(ctx context.Context, scene string, userID int64, requestID string) (*domainrecommendation.Policy, error) {
	if s == nil || s.repo == nil {
		return nil, ErrRecommendationPolicyRepositoryUnavailable
	}
	policies, err := s.repo.ListEnabledPolicies(ctx, scene)
	if err != nil {
		return nil, err
	}
	policy := domainrecommendation.SelectPolicy(policies, userID, requestID)
	if policy == nil {
		if len(policies) > 0 {
			return nil, ErrRecommendationPolicyNotSelected
		}
		return nil, domainrecommendation.ErrPolicyNotFound
	}
	return policy, nil
}

type ProfileService struct {
	repo domainrecommendation.ProfileRepository
}

func NewProfileService(repo domainrecommendation.ProfileRepository) *ProfileService {
	return &ProfileService{repo: repo}
}

func (s *ProfileService) Apply(ctx context.Context, input domainrecommendation.ProfileEventInput) (*domainrecommendation.UserInterestProfile, bool, error) {
	if s == nil || s.repo == nil {
		return nil, false, ErrRecommendationProfileRepositoryUnavailable
	}
	event, err := domainrecommendation.NewProfileEvent(input)
	if err != nil {
		return nil, false, err
	}
	return s.repo.ApplyProfileEvent(ctx, event)
}

type RequestLogService struct {
	repo    domainrecommendation.RequestLogRepository
	control domainrecommendation.RequestLogControl
	now     func() time.Time
}

func NewRequestLogService(repo domainrecommendation.RequestLogRepository, control domainrecommendation.RequestLogControl, now func() time.Time) *RequestLogService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &RequestLogService{repo: repo, control: control, now: now}
}

func (s *RequestLogService) Record(ctx context.Context, input domainrecommendation.RequestLogInput) (*domainrecommendation.RecommendationRequestLog, bool, error) {
	if s == nil || s.repo == nil {
		return nil, false, ErrRecommendationRequestLogRepositoryUnavailable
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = s.now()
	}
	log, err := domainrecommendation.NewRecommendationRequestLog(input)
	if err != nil {
		return nil, false, err
	}
	if !domainrecommendation.ShouldSampleRequestLog(s.control, log.UserID, log.Scene, log.RequestID) {
		return nil, false, nil
	}
	return s.repo.SaveRequestLog(ctx, log)
}

func (s *RequestLogService) Cleanup(ctx context.Context, limit int) (int64, error) {
	if s == nil || s.repo == nil {
		return 0, ErrRecommendationRequestLogRepositoryUnavailable
	}
	return s.repo.DeleteRequestLogsBefore(ctx, s.now().UTC().AddDate(0, 0, -s.control.RetentionDays), limit)
}
