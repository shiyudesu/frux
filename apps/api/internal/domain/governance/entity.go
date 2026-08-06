package domaingovernance

import (
	"strings"
	"time"
)

const (
	MaxReasonLength = 256
	MaxListLimit    = 100
)

type RevisionInput struct {
	Key                  Key
	Revision             int64
	Value                Value
	Reason               string
	ExpiresAt            *time.Time
	ActorID              int64
	CreatedAt            time.Time
	RollbackFromRevision int64
}

type Revision struct {
	key                  Key
	revision             int64
	value                Value
	reason               string
	expiresAt            *time.Time
	actorID              int64
	createdAt            time.Time
	rollbackFromRevision int64
}

func NewRevision(registry *Registry, input RevisionInput, now time.Time) (*Revision, error) {
	revision, err := normalizeRevision(registry, input)
	if err != nil {
		return nil, err
	}
	if revision.expiresAt != nil && !revision.expiresAt.After(now.UTC()) {
		return nil, ErrInvalidExpiry
	}
	return revision, nil
}

func RestoreRevision(registry *Registry, input RevisionInput) (*Revision, error) {
	return normalizeRevision(registry, input)
}

func normalizeRevision(registry *Registry, input RevisionInput) (*Revision, error) {
	definition, err := registry.Require(input.Key)
	if err != nil {
		return nil, err
	}
	if input.Revision <= 0 || input.RollbackFromRevision < 0 ||
		(input.RollbackFromRevision > 0 && input.RollbackFromRevision >= input.Revision) {
		return nil, ErrInvalidRevision
	}
	if err := input.Value.Validate(definition.ValueType); err != nil {
		return nil, err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		return nil, ErrInvalidReason
	}
	if len([]rune(input.Reason)) > MaxReasonLength {
		return nil, ErrReasonTooLong
	}
	if input.ActorID <= 0 {
		return nil, ErrInvalidActorID
	}
	if input.CreatedAt.IsZero() {
		return nil, ErrInvalidCreatedAt
	}
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	var expiresAt *time.Time
	if input.ExpiresAt != nil {
		value := input.ExpiresAt.UTC().Truncate(time.Microsecond)
		expiresAt = &value
	}
	return &Revision{
		key: input.Key, revision: input.Revision, value: input.Value,
		reason: input.Reason, expiresAt: expiresAt, actorID: input.ActorID,
		createdAt: createdAt, rollbackFromRevision: input.RollbackFromRevision,
	}, nil
}

func (r *Revision) Key() Key                    { return r.key }
func (r *Revision) Number() int64               { return r.revision }
func (r *Revision) Value() Value                { return r.value }
func (r *Revision) Reason() string              { return r.reason }
func (r *Revision) ActorID() int64              { return r.actorID }
func (r *Revision) CreatedAt() time.Time        { return r.createdAt }
func (r *Revision) RollbackFromRevision() int64 { return r.rollbackFromRevision }

func (r *Revision) ExpiresAt() *time.Time {
	if r == nil || r.expiresAt == nil {
		return nil
	}
	value := *r.expiresAt
	return &value
}

func (r *Revision) Expired(now time.Time) bool {
	return r != nil && r.expiresAt != nil && !now.UTC().Before(*r.expiresAt)
}
