package infragovernance

import (
	"context"
	"errors"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domaingovernance "github.com/shiyudesu/frux/internal/domain/governance"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuditWriter interface {
	AppendInTransaction(ctx context.Context, tx *gorm.DB, fact *domainadminaudit.Fact) error
	RecordCommittedWrite(fact *domainadminaudit.Fact)
}

type Repository struct {
	db          *gorm.DB
	registry    *domaingovernance.Registry
	auditWriter AuditWriter
}

func New(
	db *gorm.DB,
	registry *domaingovernance.Registry,
	auditWriter AuditWriter,
) *Repository {
	return &Repository{db: db, registry: registry, auditWriter: auditWriter}
}

func (r *Repository) ListActive(ctx context.Context) ([]*domaingovernance.Revision, error) {
	var models []RevisionModel
	err := r.db.WithContext(ctx).
		Table("governance_control_revision AS revisions").
		Select("revisions.*").
		Joins("JOIN governance_control_active AS active ON active.control_key = revisions.control_key AND active.revision = revisions.revision").
		Order("revisions.control_key ASC").
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	return r.restoreMany(models)
}

func (r *Repository) ListRevisions(
	ctx context.Context,
	key domaingovernance.Key,
	limit int,
) ([]*domaingovernance.Revision, error) {
	if limit < 1 || limit > domaingovernance.MaxListLimit {
		return nil, domaingovernance.ErrInvalidLimit
	}
	var models []RevisionModel
	if err := r.db.WithContext(ctx).
		Where("control_key = ?", string(key)).
		Order("revision DESC").
		Limit(limit).
		Find(&models).Error; err != nil {
		return nil, err
	}
	return r.restoreMany(models)
}

func (r *Repository) FindRevision(
	ctx context.Context,
	key domaingovernance.Key,
	revision int64,
) (*domaingovernance.Revision, error) {
	if revision <= 0 {
		return nil, domaingovernance.ErrInvalidRevision
	}
	var model RevisionModel
	err := r.db.WithContext(ctx).
		Where("control_key = ? AND revision = ?", string(key), revision).
		Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domaingovernance.ErrRevisionNotFound
	}
	if err != nil {
		return nil, err
	}
	return restore(r.registry, model)
}

func (r *Repository) CommitRevision(
	ctx context.Context,
	expectedRevision int64,
	revision *domaingovernance.Revision,
	auditFact *domainadminaudit.Fact,
) error {
	if r == nil || r.db == nil || r.registry == nil || r.auditWriter == nil ||
		revision == nil || auditFact == nil || expectedRevision < 0 ||
		revision.Number() != expectedRevision+1 {
		return domaingovernance.ErrInvalidRevision
	}
	model, err := modelFromDomain(r.registry, revision)
	if err != nil {
		return err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
			string(revision.Key()),
		).Error; err != nil {
			return err
		}
		var active ActiveModel
		current := int64(0)
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("control_key = ?", string(revision.Key())).
			Take(&active).Error
		if err == nil {
			current = active.Revision
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if current != expectedRevision {
			return domaingovernance.ErrRevisionConflict
		}
		if err := tx.Create(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return domaingovernance.ErrRevisionConflict
			}
			return err
		}
		active = ActiveModel{
			Key: string(revision.Key()), Revision: revision.Number(),
			UpdatedAt: revision.CreatedAt(),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "control_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"revision", "updated_at"}),
		}).Create(&active).Error; err != nil {
			return err
		}
		return r.auditWriter.AppendInTransaction(ctx, tx, auditFact)
	})
	if err != nil {
		return err
	}
	r.auditWriter.RecordCommittedWrite(auditFact)
	return nil
}

func (r *Repository) restoreMany(
	models []RevisionModel,
) ([]*domaingovernance.Revision, error) {
	result := make([]*domaingovernance.Revision, 0, len(models))
	for _, model := range models {
		revision, err := restore(r.registry, model)
		if err != nil {
			return nil, err
		}
		result = append(result, revision)
	}
	return result, nil
}

func modelFromDomain(
	registry *domaingovernance.Registry,
	revision *domaingovernance.Revision,
) (RevisionModel, error) {
	if revision == nil {
		return RevisionModel{}, domaingovernance.ErrInvalidRevision
	}
	value, ok := revision.Value().Boolean()
	if !ok {
		return RevisionModel{}, domaingovernance.ErrInvalidControlValue
	}
	validated, err := domaingovernance.RestoreRevision(registry, domaingovernance.RevisionInput{
		Key: revision.Key(), Revision: revision.Number(), Value: revision.Value(),
		Reason: revision.Reason(), ExpiresAt: revision.ExpiresAt(),
		ActorID: revision.ActorID(), CreatedAt: revision.CreatedAt(),
		RollbackFromRevision: revision.RollbackFromRevision(),
	})
	if err != nil {
		return RevisionModel{}, err
	}
	return RevisionModel{
		Key: string(validated.Key()), Revision: validated.Number(),
		ValueType: string(validated.Value().Type()), BooleanValue: value,
		Reason: validated.Reason(), ExpiresAt: validated.ExpiresAt(),
		ActorID: validated.ActorID(), CreatedAt: validated.CreatedAt(),
		RollbackFromRevision: validated.RollbackFromRevision(),
	}, nil
}

func restore(
	registry *domaingovernance.Registry,
	model RevisionModel,
) (*domaingovernance.Revision, error) {
	value, err := domaingovernance.RestoreValue(
		domaingovernance.ValueType(model.ValueType),
		model.BooleanValue,
	)
	if err != nil {
		return nil, err
	}
	return domaingovernance.RestoreRevision(registry, domaingovernance.RevisionInput{
		Key: domaingovernance.Key(model.Key), Revision: model.Revision,
		Value: value, Reason: model.Reason, ExpiresAt: model.ExpiresAt,
		ActorID: model.ActorID, CreatedAt: model.CreatedAt,
		RollbackFromRevision: model.RollbackFromRevision,
	})
}

var _ domaingovernance.Repository = (*Repository)(nil)
