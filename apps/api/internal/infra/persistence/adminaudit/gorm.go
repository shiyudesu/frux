package infraadminaudit

import (
	"context"
	"encoding/json"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"

	"gorm.io/gorm"
)

const (
	WriteResultCommitted = "committed"
	WriteResultFailed    = "failed"
)

type WriteObserver interface {
	RecordAdminAuditWrite(outcome, result string)
}

type Option func(*Repository)

type Repository struct {
	db       *gorm.DB
	observer WriteObserver
}

func New(db *gorm.DB, options ...Option) *Repository {
	repository := &Repository{db: db}
	for _, option := range options {
		if option != nil {
			option(repository)
		}
	}
	return repository
}

func WithWriteObserver(observer WriteObserver) Option {
	return func(repository *Repository) {
		repository.observer = observer
	}
}

func (r *Repository) Append(ctx context.Context, fact *domainadminaudit.Fact) error {
	err := AppendInTransaction(ctx, r.db, fact)
	r.observeWrite(fact, writeResult(err))
	return err
}

func (r *Repository) AppendInTransaction(ctx context.Context, tx *gorm.DB, fact *domainadminaudit.Fact) error {
	err := AppendInTransaction(ctx, tx, fact)
	if err != nil {
		r.observeWrite(fact, WriteResultFailed)
	}
	return err
}

func (r *Repository) RecordCommittedWrite(fact *domainadminaudit.Fact) {
	r.observeWrite(fact, WriteResultCommitted)
}

func AppendInTransaction(ctx context.Context, tx *gorm.DB, fact *domainadminaudit.Fact) error {
	if tx == nil || fact == nil {
		return domainadminaudit.ErrInvalidDetail
	}
	model, err := modelFromDomain(fact)
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Create(&model).Error
}

func (r *Repository) List(ctx context.Context, filter domainadminaudit.Query) ([]*domainadminaudit.Fact, error) {
	if filter.Limit < 1 || filter.Limit > domainadminaudit.MaxQueryLimit+1 {
		return nil, domainadminaudit.ErrInvalidLimit
	}
	query := r.db.WithContext(ctx).
		Model(&EventModel{}).
		Where("created_at >= ? AND created_at <= ?", filter.From.UTC(), filter.To.UTC())
	if filter.ActorID > 0 {
		query = query.Where("actor_id = ?", filter.ActorID)
	}
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	if filter.TargetType != "" {
		query = query.Where("target_type = ?", filter.TargetType)
	}
	if filter.Outcome != "" {
		query = query.Where("outcome = ?", filter.Outcome)
	}
	if filter.Cursor != nil {
		query = query.Where(
			"(created_at < ? OR (created_at = ? AND id < ?))",
			filter.Cursor.CreatedAt.UTC(),
			filter.Cursor.CreatedAt.UTC(),
			filter.Cursor.EventID,
		)
	}

	var models []EventModel
	if err := query.
		Order("created_at DESC").
		Order("id DESC").
		Limit(filter.Limit).
		Find(&models).Error; err != nil {
		return nil, err
	}
	facts := make([]*domainadminaudit.Fact, 0, len(models))
	for _, model := range models {
		fact, err := restore(model)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func modelFromDomain(fact *domainadminaudit.Fact) (EventModel, error) {
	validated, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: fact.ActorID(), Permission: fact.Permission(), Action: fact.Action(),
		TargetType: fact.TargetType(), TargetID: fact.TargetID(), Outcome: fact.Outcome(),
		RequestID: fact.RequestID(), IdempotencyKeyHash: fact.IdempotencyKeyHash(),
		Detail: fact.Detail(), CreatedAt: fact.CreatedAt(),
	})
	if err != nil {
		return EventModel{}, err
	}
	detail, err := json.Marshal(validated.Detail())
	if err != nil {
		return EventModel{}, domainadminaudit.ErrInvalidDetail
	}
	return EventModel{
		ActorID: validated.ActorID(), Permission: string(validated.Permission()),
		Action: string(validated.Action()), TargetType: string(validated.TargetType()),
		TargetID: validated.TargetID(), Outcome: string(validated.Outcome()),
		RequestID: validated.RequestID(), IdempotencyKeyHash: validated.IdempotencyKeyHash(),
		DetailJSON: string(detail), CreatedAt: validated.CreatedAt(),
	}, nil
}

func restore(model EventModel) (*domainadminaudit.Fact, error) {
	var detail map[string]string
	if err := json.Unmarshal([]byte(model.DetailJSON), &detail); err != nil {
		return nil, domainadminaudit.ErrInvalidDetail
	}
	return domainadminaudit.RestoreFact(model.ID, domainadminaudit.FactInput{
		ActorID: model.ActorID, Permission: domainaccount.AdminPermission(model.Permission),
		Action: domainadminaudit.Action(model.Action), TargetType: domainadminaudit.TargetType(model.TargetType),
		TargetID: model.TargetID, Outcome: domainadminaudit.Outcome(model.Outcome),
		RequestID: model.RequestID, IdempotencyKeyHash: model.IdempotencyKeyHash,
		Detail: detail, CreatedAt: model.CreatedAt,
	})
}

func (r *Repository) observeWrite(fact *domainadminaudit.Fact, result string) {
	if r.observer == nil {
		return
	}
	outcome := ""
	if fact != nil {
		outcome = string(fact.Outcome())
	}
	r.observer.RecordAdminAuditWrite(outcome, result)
}

func writeResult(err error) string {
	if err != nil {
		return WriteResultFailed
	}
	return WriteResultCommitted
}

var _ domainadminaudit.Repository = (*Repository)(nil)
var _ domainadminaudit.Repository = (*Repository)(nil)
