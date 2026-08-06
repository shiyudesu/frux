package applicationdeadletter

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domaindeadletter "github.com/shiyudesu/frux/internal/domain/deadletter"
)

var replayAuditIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

type Inspector interface {
	ListDeadLetterQueues(ctx context.Context) ([]domaindeadletter.QueueSummary, error)
	PreviewDeadLetterQueue(ctx context.Context, queue string, limit int) ([]domaindeadletter.MessagePreview, error)
}

type ReplayClaim interface {
	Metadata() domaindeadletter.ReplayMetadata
	Publish(ctx context.Context, replayID string) error
	Ack() error
	Nack() error
}

type ReplayBroker interface {
	ClaimDeadLetter(ctx context.Context, queue, messageID string) (ReplayClaim, error)
}

type AuditWriter interface {
	Append(ctx context.Context, fact *domainadminaudit.Fact) error
}

type Observer interface {
	ObserveReplay(result string)
}

type Service struct {
	inspector Inspector
	broker    ReplayBroker
	audit     AuditWriter
	observer  Observer
	now       func() time.Time
	newID     func() string
}

type Option func(*Service)

type ReplayRequest struct {
	Queue     string
	MessageID string
	ActorID   int64
	Reason    string
}

type ReplayResult struct {
	Queue           string
	MessageID       string
	OriginalEventID string
	ReplayID        string
}

func New(inspector Inspector, broker ReplayBroker, audit AuditWriter, options ...Option) *Service {
	service := &Service{
		inspector: inspector, broker: broker, audit: audit,
		now:   func() time.Time { return time.Now().UTC() },
		newID: newReplayID,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithObserver(observer Observer) Option {
	return func(service *Service) { service.observer = observer }
}

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func WithReplayIDGenerator(generator func() string) Option {
	return func(service *Service) {
		if generator != nil {
			service.newID = generator
		}
	}
}

func (s *Service) List(ctx context.Context) ([]domaindeadletter.QueueSummary, error) {
	if s == nil || s.inspector == nil {
		return nil, domaindeadletter.ErrInspectionFailed
	}
	items, err := s.inspector.ListDeadLetterQueues(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domaindeadletter.ErrInspectionFailed, err)
	}
	return items, nil
}

func (s *Service) Preview(ctx context.Context, queue string, limit int) ([]domaindeadletter.MessagePreview, error) {
	queue, err := domaindeadletter.NormalizeQueue(queue)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > domaindeadletter.MaxPreviewLimit {
		return nil, domaindeadletter.ErrInvalidLimit
	}
	if s == nil || s.inspector == nil {
		return nil, domaindeadletter.ErrInspectionFailed
	}
	items, err := s.inspector.PreviewDeadLetterQueue(ctx, queue, limit)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domaindeadletter.ErrInspectionFailed, err)
	}
	return items, nil
}

func (s *Service) Replay(ctx context.Context, request ReplayRequest) (*ReplayResult, error) {
	if s == nil || s.broker == nil || s.audit == nil || s.newID == nil {
		return nil, domaindeadletter.ErrReplayFailed
	}
	queue, err := domaindeadletter.NormalizeQueue(request.Queue)
	if err != nil {
		return nil, err
	}
	messageID, err := domaindeadletter.NormalizeMessageID(request.MessageID)
	if err != nil {
		return nil, err
	}
	reason, err := domaindeadletter.NormalizeReason(request.Reason)
	if err != nil || request.ActorID <= 0 {
		return nil, domaindeadletter.ErrInvalidReason
	}
	replayID := s.newID()
	if replayID == "" {
		return nil, domaindeadletter.ErrReplayFailed
	}
	claim, err := s.broker.ClaimDeadLetter(ctx, queue, messageID)
	if err != nil {
		s.observe("claim_failure")
		s.recordFailure(ctx, request.ActorID, queue, messageID, replayID, reason, "claim_failed")
		return nil, err
	}
	metadata := claim.Metadata()
	released := false
	defer func() {
		if !released {
			_ = claim.Nack()
		}
	}()
	fact, err := buildReplayFact(
		request.ActorID, domainadminaudit.OutcomeSuccess, metadata, replayID, reason, "",
		s.now(),
	)
	if err != nil {
		s.observe("audit_fact_failure")
		return nil, domaindeadletter.ErrReplayAuditFailed
	}
	if err := claim.Publish(ctx, replayID); err != nil {
		s.observe(replayFailureResult(err))
		s.recordFailure(ctx, request.ActorID, queue, metadata.OriginalEventID, replayID, reason, replayFailureCode(err))
		return nil, fmt.Errorf("%w: %v", domaindeadletter.ErrReplayFailed, err)
	}
	if s.audit.Append(ctx, fact) != nil {
		s.observe("audit_failure")
		return nil, domaindeadletter.ErrReplayAuditFailed
	}
	if err := claim.Ack(); err != nil {
		s.observe("ack_failure")
		s.recordFailure(ctx, request.ActorID, queue, metadata.OriginalEventID, replayID, reason, "ack_failed")
		return nil, domaindeadletter.ErrReplayAckFailed
	}
	released = true
	s.observe("success")
	return &ReplayResult{
		Queue: queue, MessageID: metadata.MessageID,
		OriginalEventID: metadata.OriginalEventID, ReplayID: replayID,
	}, nil
}

func (s *Service) recordFailure(
	ctx context.Context,
	actorID int64,
	queue, originalEventID, replayID, reason, failureCode string,
) {
	metadata := domaindeadletter.ReplayMetadata{
		Queue: queue, MessageID: originalEventID, OriginalEventID: originalEventID,
	}
	fact, err := buildReplayFact(
		actorID, domainadminaudit.OutcomeFailure, metadata, replayID, reason, failureCode,
		s.now(),
	)
	if err == nil {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
		defer cancel()
		_ = s.audit.Append(auditCtx, fact)
	}
}

func buildReplayFact(
	actorID int64,
	outcome domainadminaudit.Outcome,
	metadata domaindeadletter.ReplayMetadata,
	replayID, reason, failureCode string,
	now time.Time,
) (*domainadminaudit.Fact, error) {
	auditEventID := replayAuditIdentifier(metadata.OriginalEventID)
	detail := map[string]string{
		"http_method":       "POST",
		"route":             "/api/admin/dead-letter-messages/:messageId/replay",
		"reason_code":       reason,
		"queue":             metadata.Queue,
		"original_event_id": auditEventID,
		"replay_id":         replayID,
	}
	if outcome == domainadminaudit.OutcomeFailure {
		detail["failure_code"] = failureCode
	}
	return domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: actorID, Permission: domainaccount.PermissionGovernanceExecute,
		Action:     domainadminaudit.ActionDeadLetterReplay,
		TargetType: domainadminaudit.TargetDeadLetterMessage,
		TargetID:   auditEventID, Outcome: outcome,
		RequestID: domainadminaudit.NewRequestID(), Detail: detail, CreatedAt: now,
	})
}

func replayAuditIdentifier(value string) string {
	if len(value) <= domainadminaudit.MaxTargetIDLength &&
		replayAuditIdentifierPattern.MatchString(value) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Service) observe(result string) {
	if s.observer != nil {
		s.observer.ObserveReplay(result)
	}
}

func replayFailureCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "publish_timeout"
	}
	if errors.Is(err, domaindeadletter.ErrReplayUnconfirmed) {
		return "publish_unconfirmed"
	}
	return "publish_failed"
}

func replayFailureResult(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "publish_failure"
}

func newReplayID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return ""
	}
	return "replay-" + hex.EncodeToString(value)
}
