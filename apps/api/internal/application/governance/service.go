package applicationgovernance

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"
)

var (
	ErrLoadControlsFailed  = errors.New("failed to load degradation controls")
	ErrUpdateControlFailed = errors.New("failed to update degradation control")
)

type Service struct {
	registry   *domaingovernance.Registry
	repository domaingovernance.Repository
	now        func() time.Time
}

type ServiceOption func(*Service)

type Control struct {
	Definition     domaingovernance.Definition
	ActiveRevision *domaingovernance.Revision
}

type UpdateRequest struct {
	Key              domaingovernance.Key
	ActorID          int64
	ExpectedRevision int64
	Value            domaingovernance.Value
	Reason           string
	ExpiresAt        *time.Time
}

type RollbackRequest struct {
	Key              domaingovernance.Key
	ActorID          int64
	ExpectedRevision int64
	TargetRevision   int64
	Reason           string
}

func New(
	registry *domaingovernance.Registry,
	repository domaingovernance.Repository,
	options ...ServiceOption,
) *Service {
	service := &Service{
		registry: registry, repository: repository,
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithClock(now func() time.Time) ServiceOption {
	return func(service *Service) {
		if now != nil {
			service.now = now
		}
	}
}

func (s *Service) ListControls(ctx context.Context) ([]Control, error) {
	if s == nil || s.registry == nil || s.repository == nil {
		return nil, ErrLoadControlsFailed
	}
	active, err := s.repository.ListActive(ctx)
	if err != nil {
		return nil, ErrLoadControlsFailed
	}
	byKey := make(map[domaingovernance.Key]*domaingovernance.Revision, len(active))
	for _, revision := range active {
		if revision == nil {
			return nil, ErrLoadControlsFailed
		}
		if _, err := s.registry.Require(revision.Key()); err != nil {
			return nil, ErrLoadControlsFailed
		}
		byKey[revision.Key()] = revision
	}
	definitions := s.registry.Definitions()
	result := make([]Control, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, Control{
			Definition: definition, ActiveRevision: byKey[definition.Key],
		})
	}
	return result, nil
}

func (s *Service) ListRevisions(
	ctx context.Context,
	key domaingovernance.Key,
	limit int,
) ([]*domaingovernance.Revision, error) {
	if s == nil || s.registry == nil || s.repository == nil {
		return nil, ErrLoadControlsFailed
	}
	if _, err := s.registry.Require(key); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > domaingovernance.MaxListLimit {
		return nil, domaingovernance.ErrInvalidLimit
	}
	revisions, err := s.repository.ListRevisions(ctx, key, limit)
	if err != nil {
		return nil, ErrLoadControlsFailed
	}
	return revisions, nil
}

func (s *Service) Update(
	ctx context.Context,
	request UpdateRequest,
) (*domaingovernance.Revision, error) {
	return s.commit(ctx, request.Key, request.ActorID, request.ExpectedRevision, 0,
		request.Value, request.Reason, request.ExpiresAt, "update")
}

func (s *Service) Rollback(
	ctx context.Context,
	request RollbackRequest,
) (*domaingovernance.Revision, error) {
	if s == nil || s.registry == nil || s.repository == nil {
		return nil, ErrUpdateControlFailed
	}
	if request.TargetRevision <= 0 || request.ExpectedRevision <= 0 ||
		request.TargetRevision >= request.ExpectedRevision {
		return nil, domaingovernance.ErrInvalidRevision
	}
	if _, err := s.registry.Require(request.Key); err != nil {
		return nil, err
	}
	target, err := s.repository.FindRevision(ctx, request.Key, request.TargetRevision)
	if err != nil {
		if errors.Is(err, domaingovernance.ErrRevisionNotFound) {
			return nil, err
		}
		return nil, ErrLoadControlsFailed
	}
	now := s.now().UTC()
	if target == nil || target.Expired(now) {
		return nil, domaingovernance.ErrInvalidExpiry
	}
	return s.commit(ctx, request.Key, request.ActorID, request.ExpectedRevision,
		request.TargetRevision, target.Value(), request.Reason, target.ExpiresAt(), "rollback")
}

func (s *Service) commit(
	ctx context.Context,
	key domaingovernance.Key,
	actorID, expectedRevision, rollbackFrom int64,
	value domaingovernance.Value,
	reason string,
	expiresAt *time.Time,
	operation string,
) (*domaingovernance.Revision, error) {
	if s == nil || s.registry == nil || s.repository == nil {
		return nil, ErrUpdateControlFailed
	}
	if expectedRevision < 0 {
		return nil, domaingovernance.ErrInvalidRevision
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	revision, err := domaingovernance.NewRevision(s.registry, domaingovernance.RevisionInput{
		Key: key, Revision: expectedRevision + 1, Value: value,
		Reason: reason, ExpiresAt: expiresAt, ActorID: actorID,
		CreatedAt: now, RollbackFromRevision: rollbackFrom,
	}, now)
	if err != nil {
		return nil, err
	}
	route := "/api/admin/governance/controls/:key"
	method := "PATCH"
	if operation == "rollback" {
		route = "/api/admin/governance/controls/:key/rollback"
		method = "POST"
	}
	fact, err := applicationadminaudit.BuildSuccessFact(applicationadminaudit.BuildInput{
		ActorID: actorID, Permission: domainaccount.PermissionGovernanceExecute,
		Action:     domainadminaudit.ActionGovernanceExecute,
		TargetType: domainadminaudit.TargetGovernanceControl,
		TargetID:   string(key), RequestID: domainadminaudit.NewRequestID(),
		Detail: map[string]string{
			"http_method": method, "route": route, "operation": operation,
			"reason_code":       "governance_changed",
			"previous_revision": strconv.FormatInt(expectedRevision, 10),
			"new_revision":      strconv.FormatInt(revision.Number(), 10),
		},
	}, now)
	if err != nil {
		return nil, err
	}
	if err := s.repository.CommitRevision(ctx, expectedRevision, revision, fact); err != nil {
		if errors.Is(err, domaingovernance.ErrRevisionConflict) {
			return nil, err
		}
		return nil, ErrUpdateControlFailed
	}
	return revision, nil
}

func NormalizeKey(raw string) domaingovernance.Key {
	return domaingovernance.Key(strings.TrimSpace(raw))
}
