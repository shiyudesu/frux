package infraembedding

import (
	"context"
	"errors"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// SaveVideoEmbedding 使用 video_id + model upsert，重复发布事件会覆盖同模型向量。
func (r *Repository) SaveVideoEmbedding(ctx context.Context, embedding *domainembedding.VideoEmbedding) error {
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

// FindVideoEmbedding 按 video_id + model 查询视频向量。
func (r *Repository) FindVideoEmbedding(ctx context.Context, videoID int64, model string) (*domainembedding.VideoEmbedding, error) {
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

func (r *Repository) PersistHashAndSemanticJob(
	ctx context.Context,
	embedding *domainembedding.VideoEmbedding,
	job *domainembedding.SemanticJob,
) error {
	if embedding == nil || job == nil {
		return domainembedding.ErrInvalidSemanticText
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repository := &Repository{db: tx}
		if err := repository.SaveVideoEmbedding(ctx, embedding); err != nil {
			return err
		}
		return repository.UpsertSemanticJob(ctx, job)
	})
}

func (r *Repository) PublicationIntakeParity(
	ctx context.Context,
	videoID int64,
	title string,
	description string,
) (bool, bool, error) {
	canonicalTitle, canonicalDescription, text, err := domainembedding.CanonicalVideoText(
		title, description,
	)
	if err != nil {
		return true, false, nil
	}
	textHash := domainembedding.TextHash(text)
	var embedding VideoEmbeddingModel
	if err := r.db.WithContext(ctx).
		Where("video_id = ? AND model = ?", videoID, domainembedding.HashNgramModel).
		Take(&embedding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	var job SemanticJobModel
	if err := r.db.WithContext(ctx).
		Where("video_id = ? AND model = ?", videoID, domainembedding.SemanticModelKey).
		Take(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, false, nil
		}
		return false, false, err
	}
	matches := embedding.TextHash == textHash && job.TextHash == textHash &&
		job.Title == canonicalTitle && job.Description == canonicalDescription
	return true, matches, nil
}

func (r *Repository) UpsertSemanticJob(
	ctx context.Context,
	job *domainembedding.SemanticJob,
) error {
	if job == nil || job.VideoID <= 0 || strings.TrimSpace(job.Model) == "" ||
		strings.TrimSpace(job.TextHash) == "" {
		return domainembedding.ErrInvalidSemanticText
	}
	model := semanticJobModel(job)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "video_id"}, {Name: "model"}},
		DoUpdates: clause.Assignments(map[string]any{
			"text_hash": model.TextHash, "title": model.Title, "description": model.Description,
			"state": model.State, "attempts": 0, "available_at": model.AvailableAt,
			"lease_owner": "", "lease_until": nil, "last_error_class": "",
			"completed_at": nil, "updated_at": model.UpdatedAt,
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "semantic_embedding_job.text_hash IS DISTINCT FROM EXCLUDED.text_hash"},
		}},
	}).Create(&model).Error
}

func (r *Repository) ClaimSemanticJobs(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseUntil time.Time,
	limit int,
) ([]*domainembedding.SemanticJob, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || limit <= 0 {
		return []*domainembedding.SemanticJob{}, nil
	}
	jobs := make([]*domainembedding.SemanticJob, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []SemanticJobModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`(
					state IN ? AND available_at <= ?
				) OR (
					state = ? AND lease_until <= ?
				)`,
				[]string{domainembedding.SemanticJobPending, domainembedding.SemanticJobRetry},
				now,
				domainembedding.SemanticJobProcessing,
				now,
			).
			Order("available_at ASC").Order("video_id ASC").Order("model ASC").
			Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for index := range models {
			models[index].State = domainembedding.SemanticJobProcessing
			models[index].Attempts++
			models[index].LeaseOwner = owner
			models[index].LeaseUntil = &leaseUntil
			models[index].UpdatedAt = now
			result := tx.Model(&SemanticJobModel{}).
				Where("video_id = ? AND model = ?", models[index].VideoID, models[index].Model).
				Updates(map[string]any{
					"state":    domainembedding.SemanticJobProcessing,
					"attempts": gorm.Expr("attempts + 1"), "lease_owner": owner,
					"lease_until": leaseUntil, "updated_at": now,
				})
			if result.Error != nil {
				return result.Error
			}
			jobs = append(jobs, restoreSemanticJob(models[index]))
		}
		return nil
	})
	return jobs, err
}

func (r *Repository) CompleteSemanticJob(
	ctx context.Context,
	job *domainembedding.SemanticJob,
	embedding *domainembedding.VideoEmbedding,
	completedAt time.Time,
) error {
	if job == nil || embedding == nil {
		return domainembedding.ErrSemanticJobNotFound
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current SemanticJobModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("video_id = ? AND model = ?", job.VideoID, job.Model).
			Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainembedding.ErrSemanticJobNotFound
			}
			return err
		}
		if current.State != domainembedding.SemanticJobProcessing ||
			current.LeaseOwner != job.LeaseOwner || current.TextHash != job.TextHash ||
			current.LeaseUntil == nil || !current.LeaseUntil.After(completedAt) {
			return domainembedding.ErrSemanticJobLeaseLost
		}
		if err := (&Repository{db: tx}).SaveVideoEmbedding(ctx, embedding); err != nil {
			return err
		}
		result := tx.Model(&SemanticJobModel{}).
			Where("video_id = ? AND model = ? AND state = ? AND lease_owner = ? AND text_hash = ?",
				job.VideoID, job.Model, domainembedding.SemanticJobProcessing,
				job.LeaseOwner, job.TextHash).
			Where("lease_until > ?", completedAt.UTC()).
			Updates(map[string]any{
				"state": domainembedding.SemanticJobCompleted, "completed_at": completedAt.UTC(),
				"lease_owner": "", "lease_until": nil, "last_error_class": "",
				"updated_at": completedAt.UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return domainembedding.ErrSemanticJobLeaseLost
		}
		return nil
	})
}

func (r *Repository) RetrySemanticJob(
	ctx context.Context,
	job *domainembedding.SemanticJob,
	availableAt time.Time,
	errorClass string,
	terminal bool,
) error {
	if job == nil {
		return domainembedding.ErrSemanticJobNotFound
	}
	errorClass = boundedErrorClass(errorClass)
	state := domainembedding.SemanticJobRetry
	updates := map[string]any{
		"state": state, "available_at": availableAt.UTC(), "lease_owner": "",
		"lease_until": nil, "last_error_class": errorClass, "updated_at": time.Now().UTC(),
	}
	if terminal {
		state = domainembedding.SemanticJobFailed
		updates["state"] = state
		updates["completed_at"] = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).Model(&SemanticJobModel{}).
		Where("video_id = ? AND model = ? AND state = ? AND lease_owner = ? AND text_hash = ?",
			job.VideoID, job.Model, domainembedding.SemanticJobProcessing,
			job.LeaseOwner, job.TextHash).
		Where("lease_until > ?", time.Now().UTC()).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainembedding.ErrSemanticJobLeaseLost
	}
	return nil
}

func (r *Repository) ExtendSemanticJobLease(
	ctx context.Context,
	job *domainembedding.SemanticJob,
	now time.Time,
	leaseUntil time.Time,
) error {
	if job == nil || strings.TrimSpace(job.LeaseOwner) == "" {
		return domainembedding.ErrSemanticJobLeaseLost
	}
	result := r.db.WithContext(ctx).Model(&SemanticJobModel{}).
		Where(`video_id = ? AND model = ? AND state = ? AND lease_owner = ?
			AND text_hash = ? AND lease_until > ?`,
			job.VideoID, job.Model, domainembedding.SemanticJobProcessing,
			job.LeaseOwner, job.TextHash, now.UTC()).
		Updates(map[string]any{
			"lease_until": leaseUntil.UTC(), "updated_at": now.UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainembedding.ErrSemanticJobLeaseLost
	}
	job.LeaseUntil = &leaseUntil
	return nil
}

func (r *Repository) SuspendSemanticJobs(ctx context.Context, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Model(&SemanticJobModel{}).
		Where("state IN ?", []string{
			domainembedding.SemanticJobPending,
			domainembedding.SemanticJobRetry,
		}).
		Updates(map[string]any{
			"state":       domainembedding.SemanticJobSuspended,
			"lease_owner": "", "lease_until": nil, "updated_at": now.UTC(),
		})
	return result.RowsAffected, result.Error
}

func (r *Repository) ResumeSemanticJobs(ctx context.Context, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Model(&SemanticJobModel{}).
		Where("state = ?", domainembedding.SemanticJobSuspended).
		Updates(map[string]any{
			"state":        domainembedding.SemanticJobPending,
			"available_at": now.UTC(), "updated_at": now.UTC(),
		})
	return result.RowsAffected, result.Error
}

func (r *Repository) SemanticBacklog(
	ctx context.Context,
) ([]domainembedding.SemanticBacklog, error) {
	var rows []struct {
		State    string
		Count    int64
		OldestAt *time.Time
	}

	err := r.db.WithContext(ctx).Model(&SemanticJobModel{}).
		Select("state, COUNT(*) AS count, MIN(created_at) AS oldest_at").
		Group("state").Order("state ASC").Scan(&rows).Error
	result := make([]domainembedding.SemanticBacklog, 0, len(rows))
	for _, row := range rows {
		result = append(result, domainembedding.SemanticBacklog{
			State: row.State, Count: row.Count, OldestAt: row.OldestAt,
		})
	}
	return result, err
}

func (r *Repository) SemanticCoverage(ctx context.Context) (present, missing int64, err error) {
	var row struct {
		Present int64
		Missing int64
	}
	err = r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) FILTER (WHERE embedding.video_id IS NOT NULL) AS present,
			COUNT(*) FILTER (WHERE embedding.video_id IS NULL) AS missing
		FROM video
		LEFT JOIN video_embedding AS embedding
		  ON embedding.video_id = video.id AND embedding.model = ?
		WHERE video.status = 2
		  AND video.visibility = 'public'
		  AND video.media_status IN ('legacy_ready', 'ready')
	`, domainembedding.SemanticModelKey).Scan(&row).Error
	return row.Present, row.Missing, err
}

func (r *Repository) CleanupSemanticJobs(
	ctx context.Context,
	completedBefore time.Time,
	limit int,
) (int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var keys []SemanticJobModel
	if err := r.db.WithContext(ctx).
		Select("video_id, model").
		Where("state = ? AND completed_at < ?", domainembedding.SemanticJobCompleted, completedBefore).
		Order("completed_at ASC").Limit(limit).Find(&keys).Error; err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	var deleted int64
	for _, key := range keys {
		result := r.db.WithContext(ctx).Where(
			"video_id = ? AND model = ? AND state = ?",
			key.VideoID, key.Model, domainembedding.SemanticJobCompleted,
		).Delete(&SemanticJobModel{})
		if result.Error != nil {
			return deleted, result.Error
		}
		deleted += result.RowsAffected
	}
	return deleted, nil
}

func semanticJobModel(job *domainembedding.SemanticJob) SemanticJobModel {
	return SemanticJobModel{
		VideoID: job.VideoID, Model: strings.TrimSpace(job.Model),
		TextHash: strings.TrimSpace(job.TextHash), Title: job.Title,
		Description: job.Description, State: job.State, Attempts: job.Attempts,
		AvailableAt: job.AvailableAt.UTC(), LeaseOwner: job.LeaseOwner,
		LeaseUntil: job.LeaseUntil, LastErrorClass: boundedErrorClass(job.LastErrorClass),
		CompletedAt: job.CompletedAt, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func restoreSemanticJob(model SemanticJobModel) *domainembedding.SemanticJob {
	return &domainembedding.SemanticJob{
		VideoID: model.VideoID, Model: model.Model, TextHash: model.TextHash,
		Title: model.Title, Description: model.Description, State: model.State,
		Attempts: model.Attempts, AvailableAt: model.AvailableAt,
		LeaseOwner: model.LeaseOwner, LeaseUntil: model.LeaseUntil,
		LastErrorClass: model.LastErrorClass, CompletedAt: model.CompletedAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func boundedErrorClass(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 32 {
		return value[:32]
	}
	return value
}
