package applicationreview

import (
	"context"
	"errors"
	"io"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type OutcomeApplier interface {
	ApplyReviewOutcome(ctx context.Context, result *domainreview.ProcessingResult) error
}

type Observer interface {
	Observe(stage, result string)
}

type Service struct {
	repo              domainreview.Repository
	outcomeApplier    OutcomeApplier
	observer          Observer
	humanRepo         HumanRepository
	humanObserver     HumanObserver
	humanCursorSecret []byte
	humanTokenReader  io.Reader
	humanPreview      HumanPreviewProvider
}

type Option func(*Service)

func WithOutcomeApplier(applier OutcomeApplier) Option {
	return func(service *Service) { service.outcomeApplier = applier }
}

func WithObserver(observer Observer) Option {
	return func(service *Service) { service.observer = observer }
}

func New(repo domainreview.Repository, options ...Option) *Service {
	service := &Service{repo: repo}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) EnsureCase(ctx context.Context, videoID int64) (*domainreview.ReviewCase, bool, error) {
	if s == nil || s.repo == nil {
		return nil, false, domainreview.ErrReviewSubjectState
	}
	reviewCase, created, err := s.repo.CreateOrGetCase(ctx, videoID)
	result := "created"
	if err != nil {
		result = reviewErrorResult(err)
	} else if !created {
		result = "existing"
	}
	s.observe("intake", result)
	return reviewCase, created, err
}

func (s *Service) SubmitMachineResult(ctx context.Context, input domainreview.MachineResultInput) (*domainreview.ProcessingResult, error) {
	result, err := domainreview.NewMachineResult(input)
	if err != nil {
		s.observe("provider_result", "invalid")
		return nil, err
	}
	processed, err := s.repo.ProcessMachineResult(ctx, result)
	if err != nil {
		s.observe("provider_result", reviewErrorResult(err))
		return nil, err
	}
	if processed.Duplicate {
		s.observe("provider_result", "duplicate")
	} else {
		s.observe("provider_result", "accepted")
	}
	s.observe("routing", processed.Decision.Outcome)
	if err := s.ApplyMachineResultSideEffects(ctx, processed); err != nil {
		return nil, err
	}
	return processed, nil
}

func (s *Service) ApplyMachineResultSideEffects(
	ctx context.Context,
	processed *domainreview.ProcessingResult,
) error {
	if s == nil || processed == nil || !processed.ApplySideEffects ||
		s.outcomeApplier == nil {
		return nil
	}
	if err := s.outcomeApplier.ApplyReviewOutcome(ctx, processed); err != nil {
		s.observe("provider_result", "retry")
		return err
	}
	return nil
}

func (s *Service) Reconcile(ctx context.Context, limit int) (domainreview.ReconciliationStats, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	ids, err := s.repo.ListReviewableVideoIDsWithoutCase(ctx, limit)
	if err != nil {
		s.observe("reconciliation", "retry")
		return domainreview.ReconciliationStats{}, err
	}
	stats := domainreview.ReconciliationStats{Scanned: len(ids)}
	var reconcileErr error
	for _, videoID := range ids {
		_, created, intakeErr := s.EnsureCase(ctx, videoID)
		switch {
		case intakeErr != nil:
			stats.Failed++
			reconcileErr = errors.Join(reconcileErr, intakeErr)
		case created:
			stats.Created++
		default:
			stats.Existing++
		}
	}
	result := "success"
	if reconcileErr != nil {
		result = "retry"
	}
	s.observe("reconciliation", result)
	return stats, reconcileErr
}

func (s *Service) observe(stage, result string) {
	if s != nil && s.observer != nil {
		s.observer.Observe(stage, result)
	}
}

func reviewErrorResult(err error) string {
	switch {
	case errors.Is(err, domainreview.ErrInvalidCaseID),
		errors.Is(err, domainreview.ErrInvalidVideoID),
		errors.Is(err, domainreview.ErrInvalidReviewVersion),
		errors.Is(err, domainreview.ErrInvalidPolicyVersion),
		errors.Is(err, domainreview.ErrInvalidResultIdentity),
		errors.Is(err, domainreview.ErrInvalidProvider),
		errors.Is(err, domainreview.ErrInvalidModelVersion),
		errors.Is(err, domainreview.ErrInvalidMachineSource),
		errors.Is(err, domainreview.ErrInvalidGeneratedAt),
		errors.Is(err, domainreview.ErrInvalidModerationMode),
		errors.Is(err, domainreview.ErrInvalidSignal),
		errors.Is(err, domainreview.ErrInvalidConfidence),
		errors.Is(err, domainreview.ErrTooManySignals),
		errors.Is(err, domainreview.ErrTooManyEvidenceRefs),
		errors.Is(err, domainreview.ErrEvidenceRefTooLong),
		errors.Is(err, domainreview.ErrEvidenceTooLarge):
		return "invalid"
	case errors.Is(err, domainreview.ErrResultIdentityConflict),
		errors.Is(err, domainreview.ErrReviewSubjectStale),
		errors.Is(err, domainreview.ErrReviewCaseNotOpen),
		errors.Is(err, domainreview.ErrReviewSubjectState),
		errors.Is(err, domainreview.ErrModerationJobNotOwned):
		return "conflict"
	default:
		return "retry"
	}
}
