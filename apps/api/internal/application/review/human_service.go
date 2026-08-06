package applicationreview

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

const (
	DefaultHumanQueueLimit  = 20
	MaxHumanQueueLimit      = 100
	humanQueueCursorVersion = 1
)

type HumanRepository interface {
	ListHumanQueue(ctx context.Context, filter domainreview.HumanQueueFilter) ([]*domainreview.HumanQueueItem, error)
	HumanQueueStats(ctx context.Context, minPriority, maxPriority int) (int, time.Time, error)
	GetHumanCaseDetail(ctx context.Context, caseID int64) (*domainreview.HumanCaseDetail, error)
	ClaimHumanCase(ctx context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error)
	RenewHumanLease(ctx context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int, duration time.Duration) (*domainreview.ReviewCase, error)
	ReleaseHumanLease(ctx context.Context, caseID, reviewerID int64, tokenHash string, expectedVersion int) (*domainreview.ReviewCase, error)
	CommitHumanDecision(ctx context.Context, decision *domainreview.HumanDecision, tokenHash string, auditFact *domainadminaudit.Fact) (*domainreview.HumanDecisionResult, error)
}

type HumanObserver interface {
	ObserveHuman(operation, result string)
	ObserveHumanQueue(available int, oldestAge time.Duration)
}

type HumanQueueRequest struct {
	MinPriority int
	MaxPriority int
	Cursor      string
	Limit       int
}

type HumanQueuePage struct {
	Items      []*domainreview.HumanQueueItem
	NextCursor string
	HasMore    bool
}

type ClaimRequest struct {
	CaseID              int64
	ReviewerID          int64
	ExpectedCaseVersion int
}

type RenewLeaseRequest struct {
	CaseID              int64
	ReviewerID          int64
	LeaseToken          string
	ExpectedCaseVersion int
}

type ReleaseLeaseRequest = RenewLeaseRequest

type DecisionRequest struct {
	CaseID              int64
	ReviewerID          int64
	LeaseToken          string
	ExpectedCaseVersion int
	ReviewVersion       int
	Outcome             string
	ReasonCode          string
	Note                string
	IdempotencyKey      string
}

type humanQueueCursorEnvelope struct {
	Version     int    `json:"v"`
	MinPriority int    `json:"min"`
	MaxPriority int    `json:"max"`
	Priority    int    `json:"p"`
	CreatedAt   string `json:"t"`
	CaseID      int64  `json:"id"`
}

func WithHumanRepository(repository HumanRepository) Option {
	return func(service *Service) { service.humanRepo = repository }
}

func WithHumanCursorSecret(secret string) Option {
	return func(service *Service) {
		if secret = strings.TrimSpace(secret); secret != "" {
			service.humanCursorSecret = []byte(secret)
		}
	}
}

func WithHumanTokenReader(reader io.Reader) Option {
	return func(service *Service) {
		if reader != nil {
			service.humanTokenReader = reader
		}
	}
}

func WithHumanObserver(observer HumanObserver) Option {
	return func(service *Service) { service.humanObserver = observer }
}

func (s *Service) ListHumanQueue(ctx context.Context, request HumanQueueRequest) (*HumanQueuePage, error) {
	if s == nil || s.humanRepo == nil || len(s.humanCursorSecret) == 0 {
		return nil, domainreview.ErrReviewSubjectState
	}
	minPriority, maxPriority := request.MinPriority, request.MaxPriority
	if minPriority == 0 && maxPriority == 0 {
		maxPriority = 100
	}
	if !domainreview.ValidPriority(minPriority) || !domainreview.ValidPriority(maxPriority) ||
		minPriority > maxPriority {
		return nil, domainreview.ErrInvalidQueueFilter
	}
	limit := request.Limit
	if limit == 0 {
		limit = DefaultHumanQueueLimit
	}
	if limit < 1 || limit > MaxHumanQueueLimit {
		return nil, domainreview.ErrInvalidQueueFilter
	}
	cursor, err := s.decodeHumanQueueCursor(request.Cursor, minPriority, maxPriority)
	if err != nil {
		return nil, err
	}
	items, err := s.humanRepo.ListHumanQueue(ctx, domainreview.HumanQueueFilter{
		MinPriority: minPriority, MaxPriority: maxPriority, Cursor: cursor, Limit: limit + 1,
	})
	if err != nil {
		s.observeHuman("queue", humanErrorResult(err))
		return nil, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1].Case
		nextCursor = s.encodeHumanQueueCursor(minPriority, maxPriority, &domainreview.QueueCursor{
			Priority: last.Priority, CreatedAt: last.CreatedAt, CaseID: last.ID,
		})
	}
	available, oldest, statsErr := s.humanRepo.HumanQueueStats(ctx, minPriority, maxPriority)
	if statsErr != nil {
		s.observeHuman("queue", "retry")
		return nil, statsErr
	}
	oldestAge := time.Duration(0)
	if !oldest.IsZero() {
		oldestAge = time.Since(oldest)
		if oldestAge < 0 {
			oldestAge = 0
		}
	}
	if s.humanObserver != nil {
		s.humanObserver.ObserveHumanQueue(available, oldestAge)
	}
	s.observeHuman("queue", "success")
	return &HumanQueuePage{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *Service) GetHumanCase(ctx context.Context, caseID int64) (*domainreview.HumanCaseDetail, error) {
	if s == nil || s.humanRepo == nil {
		return nil, domainreview.ErrReviewSubjectState
	}
	detail, err := s.humanRepo.GetHumanCaseDetail(ctx, caseID)
	s.observeHuman("detail", humanResult(err))
	return detail, err
}

func (s *Service) ClaimHumanCase(ctx context.Context, request ClaimRequest) (*domainreview.LeaseResult, error) {
	if s == nil || s.humanRepo == nil {
		return nil, domainreview.ErrReviewSubjectState
	}
	token, tokenHash, err := s.newLeaseToken()
	if err != nil {
		return nil, err
	}
	reviewCase, err := s.humanRepo.ClaimHumanCase(
		ctx, request.CaseID, request.ReviewerID, tokenHash,
		request.ExpectedCaseVersion, domainreview.DefaultHumanLeaseDuration,
	)
	s.observeHuman("claim", humanResult(err))
	if err != nil {
		return nil, err
	}
	return &domainreview.LeaseResult{Case: reviewCase, LeaseToken: token}, nil
}

func (s *Service) RenewHumanLease(ctx context.Context, request RenewLeaseRequest) (*domainreview.LeaseResult, error) {
	if s == nil || s.humanRepo == nil {
		return nil, domainreview.ErrReviewSubjectState
	}
	tokenHash, err := digestLeaseToken(request.LeaseToken)
	if err != nil {
		s.observeHuman("renew", "invalid")
		return nil, err
	}
	reviewCase, err := s.humanRepo.RenewHumanLease(
		ctx, request.CaseID, request.ReviewerID, tokenHash,
		request.ExpectedCaseVersion, domainreview.DefaultHumanLeaseDuration,
	)
	s.observeHuman("renew", humanResult(err))
	if err != nil {
		return nil, err
	}
	return &domainreview.LeaseResult{Case: reviewCase, LeaseToken: strings.TrimSpace(request.LeaseToken)}, nil
}

func (s *Service) ReleaseHumanLease(ctx context.Context, request ReleaseLeaseRequest) (*domainreview.ReviewCase, error) {
	if s == nil || s.humanRepo == nil {
		return nil, domainreview.ErrReviewSubjectState
	}
	tokenHash, err := digestLeaseToken(request.LeaseToken)
	if err != nil {
		s.observeHuman("release", "invalid")
		return nil, err
	}
	reviewCase, err := s.humanRepo.ReleaseHumanLease(
		ctx, request.CaseID, request.ReviewerID, tokenHash, request.ExpectedCaseVersion,
	)
	s.observeHuman("release", humanResult(err))
	return reviewCase, err
}

func (s *Service) DecideHumanCase(ctx context.Context, request DecisionRequest) (*domainreview.HumanDecisionResult, error) {
	if s == nil || s.humanRepo == nil {
		return nil, domainreview.ErrReviewSubjectState
	}
	tokenHash, err := digestLeaseToken(request.LeaseToken)
	if err != nil {
		s.observeHuman("decision", "invalid")
		return nil, err
	}
	decision, err := domainreview.NewHumanDecision(domainreview.HumanDecisionInput{
		CaseID: request.CaseID, ReviewerID: request.ReviewerID, Outcome: request.Outcome,
		ReasonCode: request.ReasonCode, Note: request.Note, ReviewVersion: request.ReviewVersion,
		ExpectedCaseVersion: request.ExpectedCaseVersion, IdempotencyKey: request.IdempotencyKey,
		DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		s.observeHuman("decision", "invalid")
		return nil, err
	}
	auditFact, err := applicationadminaudit.BuildSuccessFact(applicationadminaudit.BuildInput{
		ActorID: decision.ReviewerID, Permission: domainaccount.PermissionReviewDecide,
		Action: domainadminaudit.ActionReviewDecide, TargetType: domainadminaudit.TargetReviewCase,
		TargetID: strconv.FormatInt(decision.CaseID, 10), RequestID: domainadminaudit.NewRequestID(),
		IdempotencyKey: request.IdempotencyKey,
		Detail: map[string]string{
			"decision": humanAuditDecision(decision.Outcome), "http_method": "POST",
			"reason_code": decision.ReasonCode, "review_version": strconv.Itoa(decision.ReviewVersion),
			"route": "/api/admin/review/cases/:caseId/decision",
		},
	}, decision.CreatedAt)
	if err != nil {
		s.observeHuman("decision", "invalid")
		return nil, err
	}
	result, err := s.humanRepo.CommitHumanDecision(ctx, decision, tokenHash, auditFact)
	s.observeHuman("decision", humanDecisionResult(decision.Outcome, err))
	if err != nil {
		return nil, err
	}
	if s.outcomeApplier != nil && result != nil && result.Case != nil &&
		result.Decision != nil && result.ApplySideEffects {
		applyResult := &domainreview.ProcessingResult{
			Case: result.Case,
			Decision: &domainreview.AutomatedDecision{
				ID: result.Decision.ID, CaseID: result.Case.ID,
				Outcome: result.Decision.Outcome, CreatedAt: result.Decision.CreatedAt,
			},
			ApplySideEffects: true,
			MediaAssetID:     result.MediaAssetID, CoverAssetID: result.CoverAssetID,
		}
		if err := s.outcomeApplier.ApplyReviewOutcome(ctx, applyResult); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) encodeHumanQueueCursor(minPriority, maxPriority int, cursor *domainreview.QueueCursor) string {
	if cursor == nil {
		return ""
	}
	payload, err := json.Marshal(humanQueueCursorEnvelope{
		Version: humanQueueCursorVersion, MinPriority: minPriority, MaxPriority: maxPriority,
		Priority: cursor.Priority, CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		CaseID: cursor.CaseID,
	})
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, s.humanCursorSecret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) decodeHumanQueueCursor(raw string, minPriority, maxPriority int) (*domainreview.QueueCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, domainreview.ErrInvalidQueueCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, domainreview.ErrInvalidQueueCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, domainreview.ErrInvalidQueueCursor
	}
	mac := hmac.New(sha256.New, s.humanCursorSecret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, domainreview.ErrInvalidQueueCursor
	}
	var envelope humanQueueCursorEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil ||
		envelope.Version != humanQueueCursorVersion ||
		envelope.MinPriority != minPriority || envelope.MaxPriority != maxPriority ||
		!domainreview.ValidPriority(envelope.Priority) || envelope.CaseID <= 0 {
		return nil, domainreview.ErrInvalidQueueCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil {
		return nil, domainreview.ErrInvalidQueueCursor
	}
	return &domainreview.QueueCursor{
		Priority: envelope.Priority, CreatedAt: createdAt.UTC(), CaseID: envelope.CaseID,
	}, nil
}

func (s *Service) newLeaseToken() (string, string, error) {
	reader := s.humanTokenReader
	if reader == nil {
		reader = rand.Reader
	}
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(reader, bytes); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func digestLeaseToken(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		return "", domainreview.ErrInvalidLeaseToken
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) observeHuman(operation, result string) {
	if s != nil && s.humanObserver != nil {
		s.humanObserver.ObserveHuman(operation, result)
	}
}

func humanAuditDecision(outcome string) string {
	if outcome == domainreview.OutcomeApprove {
		return "approved"
	}
	return "rejected"
}

func humanResult(err error) string {
	if err == nil {
		return "success"
	}
	return humanErrorResult(err)
}

func humanDecisionResult(outcome string, err error) string {
	if err != nil {
		return humanErrorResult(err)
	}
	if outcome == domainreview.OutcomeApprove {
		return "approve"
	}
	return "reject"
}

func humanErrorResult(err error) string {
	switch {
	case errors.Is(err, domainreview.ErrInvalidCaseID),
		errors.Is(err, domainreview.ErrInvalidReviewerID),
		errors.Is(err, domainreview.ErrInvalidCaseVersion),
		errors.Is(err, domainreview.ErrInvalidLeaseToken),
		errors.Is(err, domainreview.ErrInvalidReasonCode),
		errors.Is(err, domainreview.ErrInvalidDecisionOutcome),
		errors.Is(err, domainreview.ErrReviewNoteTooLong),
		errors.Is(err, domainreview.ErrReviewNoteRequired),
		errors.Is(err, domainreview.ErrInvalidIdempotencyKey),
		errors.Is(err, domainreview.ErrInvalidQueueCursor),
		errors.Is(err, domainreview.ErrInvalidQueueFilter):
		return "invalid"
	case errors.Is(err, domainreview.ErrReviewCaseClaimed),
		errors.Is(err, domainreview.ErrReviewLeaseNotOwned),
		errors.Is(err, domainreview.ErrReviewLeaseExpired),
		errors.Is(err, domainreview.ErrReviewCaseVersion),
		errors.Is(err, domainreview.ErrReviewSubjectStale),
		errors.Is(err, domainreview.ErrReviewSubjectState),
		errors.Is(err, domainreview.ErrReviewCaseNotHuman),
		errors.Is(err, domainreview.ErrDecisionIdentityConflict):
		return "conflict"
	default:
		return "retry"
	}
}
