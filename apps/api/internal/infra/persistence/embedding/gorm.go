package infraembedding

import (
	"context"
	"errors"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db          *gorm.DB
	auditWriter AuditWriter
}

type Option func(*Repository)

type AuditWriter interface {
	AppendInTransaction(context.Context, *gorm.DB, *domainadminaudit.Fact) error
	RecordCommittedWrite(*domainadminaudit.Fact)
}

func WithAdminAuditWriter(writer AuditWriter) Option {
	return func(repository *Repository) { repository.auditWriter = writer }
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

// SaveVideoEmbedding conditionally upserts one model fact for a video.
func (r *Repository) SaveVideoEmbedding(
	ctx context.Context,
	embedding *domainembedding.VideoEmbedding,
) error {
	model := VideoEmbeddingModel{
		VideoID:       embedding.VideoID,
		Model:         embedding.Model,
		Dimension:     embedding.Dimension,
		EmbeddingJSON: embedding.EmbeddingJSON,
		TextHash:      embedding.TextHash,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "video_id"},
			{Name: "model"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"dimension",
			"embedding_json",
			"text_hash",
			"updated_at",
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: `video_embedding.text_hash IS DISTINCT FROM EXCLUDED.text_hash OR
				video_embedding.dimension IS DISTINCT FROM EXCLUDED.dimension OR
				video_embedding.embedding_json IS DISTINCT FROM EXCLUDED.embedding_json`},
		}},
	}).Create(&model).Error
}

// FindVideoEmbedding returns one video/model embedding fact.
func (r *Repository) FindVideoEmbedding(
	ctx context.Context,
	videoID int64,
	model string,
) (*domainembedding.VideoEmbedding, error) {
	var item VideoEmbeddingModel
	err := r.db.WithContext(ctx).
		Where("video_id = ? AND model = ?", videoID, model).
		Take(&item).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainembedding.ErrVideoEmbeddingNotFound
		}
		return nil, err
	}
	return domainembedding.RestoreVideoEmbedding(
		item.VideoID,
		item.Model,
		item.Dimension,
		item.EmbeddingJSON,
		item.TextHash,
		item.CreatedAt,
		item.UpdatedAt,
	), nil
}

func (r *Repository) PublicationIntakeParity(
	ctx context.Context,
	videoID int64,
	title string,
	description string,
) (bool, bool, error) {
	text, err := domainembedding.BuildValidatedVideoText(title, description)
	if err != nil {
		return true, true, nil
	}
	embedding, err := r.FindVideoEmbedding(
		ctx, videoID, domainembedding.HashNgramModel,
	)
	if err != nil {
		if errors.Is(err, domainembedding.ErrVideoEmbeddingNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	textHash := domainembedding.TextHash(text)
	return true, embedding.TextHash == textHash, nil
}
