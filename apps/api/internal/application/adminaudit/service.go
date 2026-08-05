package applicationadminaudit

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
)

const DefaultQueryLimit = 20

type Logger interface {
	Printf(format string, values ...any)
}

type AttemptObserver interface {
	RecordDeniedAttemptDrop()
}

type Option func(*Service)

type Service struct {
	repository domainadminaudit.Repository
	now        func() time.Time
	logger     Logger
	observer   AttemptObserver
}

type BuildInput struct {
	ActorID        int64
	Permission     domainaccount.AdminPermission
	Action         domainadminaudit.Action
	TargetType     domainadminaudit.TargetType
	TargetID       string
	RequestID      string
	IdempotencyKey string
	Detail         map[string]string
}

type QueryRequest struct {
	ActorID    int64
	Action     string
	TargetType string
	Outcome    string
	From       time.Time
	To         time.Time
	Cursor     string
	Limit      int
}

type QueryPage struct {
	Items      []*domainadminaudit.Fact
	NextCursor string
	HasMore    bool
}

func New(repository domainadminaudit.Repository, options ...Option) *Service {
	service := &Service{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
		logger:     log.Default(),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func WithLogger(logger Logger) Option {
	return func(service *Service) {
		if logger != nil {
			service.logger = logger
		}
	}
}

func WithAttemptObserver(observer AttemptObserver) Option {
	return func(service *Service) {
		service.observer = observer
	}
}

func BuildSuccessFact(input BuildInput, createdAt time.Time) (*domainadminaudit.Fact, error) {
	return buildFact(input, domainadminaudit.OutcomeSuccess, createdAt)
}

func BuildDeniedAttemptFact(input BuildInput, createdAt time.Time) (*domainadminaudit.Fact, error) {
	return buildFact(input, domainadminaudit.OutcomeDenied, createdAt)
}

func (s *Service) RecordDeniedAttempt(ctx context.Context, input BuildInput) {
	fact, err := BuildDeniedAttemptFact(input, s.now())
	if err != nil {
		s.logger.Printf("admin audit denied-attempt validation failed: %v", err)
		return
	}
	if s.repository == nil {
		s.logger.Printf("admin audit denied-attempt write failed: repository unavailable")
		return
	}
	if err := s.repository.Append(ctx, fact); err != nil {
		s.logger.Printf("admin audit denied-attempt write failed: %v", err)
	}
}

func (s *Service) RecordDeniedAttemptDropped() {
	if s.observer != nil {
		s.observer.RecordDeniedAttemptDrop()
	}
}

func (s *Service) Query(ctx context.Context, request QueryRequest) (*QueryPage, error) {
	query, filterKey, err := normalizeQuery(request)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(request.Cursor, filterKey)
	if err != nil {
		return nil, err
	}
	query.Cursor = cursor
	if s.repository == nil {
		return nil, domainadminaudit.ErrAuditQueryFailed
	}
	items, err := s.repository.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domainadminaudit.ErrAuditQueryFailed, err)
	}
	limit := query.Limit - 1
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeCursor(filterKey, &domainadminaudit.Cursor{
			CreatedAt: last.CreatedAt(),
			EventID:   last.ID(),
		})
	}
	return &QueryPage{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func buildFact(
	input BuildInput,
	outcome domainadminaudit.Outcome,
	createdAt time.Time,
) (*domainadminaudit.Fact, error) {
	idempotencyKeyHash, err := domainadminaudit.DigestIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	return domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: input.ActorID, Permission: input.Permission, Action: input.Action,
		TargetType: input.TargetType, TargetID: input.TargetID, Outcome: outcome,
		RequestID: input.RequestID, IdempotencyKeyHash: idempotencyKeyHash,
		Detail: input.Detail, CreatedAt: createdAt,
	})
}

func normalizeQuery(request QueryRequest) (domainadminaudit.Query, string, error) {
	if request.ActorID < 0 {
		return domainadminaudit.Query{}, "", domainadminaudit.ErrInvalidActorID
	}
	from := request.From.UTC()
	to := request.To.UTC()
	if from.IsZero() || to.IsZero() || from.After(to) {
		return domainadminaudit.Query{}, "", domainadminaudit.ErrInvalidTimeRange
	}
	if to.Sub(from) > domainadminaudit.MaxQueryRange {
		return domainadminaudit.Query{}, "", domainadminaudit.ErrTimeRangeTooLarge
	}
	action := domainadminaudit.Action(strings.TrimSpace(request.Action))
	if action != "" && !domainadminaudit.ValidAction(action) {
		return domainadminaudit.Query{}, "", domainadminaudit.ErrInvalidAction
	}
	targetType := domainadminaudit.TargetType(strings.TrimSpace(request.TargetType))
	if targetType != "" && !domainadminaudit.ValidTargetType(targetType) {
		return domainadminaudit.Query{}, "", domainadminaudit.ErrInvalidTargetType
	}
	outcome := domainadminaudit.Outcome(strings.TrimSpace(request.Outcome))
	if outcome != "" && !domainadminaudit.ValidOutcome(outcome) {
		return domainadminaudit.Query{}, "", domainadminaudit.ErrInvalidOutcome
	}
	limit := request.Limit
	if limit == 0 {
		limit = DefaultQueryLimit
	}
	if limit < 1 || limit > domainadminaudit.MaxQueryLimit {
		return domainadminaudit.Query{}, "", domainadminaudit.ErrInvalidLimit
	}
	query := domainadminaudit.Query{
		ActorID: request.ActorID, Action: action, TargetType: targetType, Outcome: outcome,
		From: from, To: to, Limit: limit + 1,
	}
	return query, queryFilterKey(query), nil
}
