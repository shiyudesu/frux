package infraembedding

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ domainembedding.MultimodalRepository = (*Repository)(nil)

func (r *Repository) HandoffMultimodalJob(
	ctx context.Context,
	job *domainembedding.MultimodalEmbeddingJob,
) (*domainembedding.MultimodalEmbeddingJob, bool, bool, error) {
	if r == nil || r.db == nil || job == nil {
		return nil, false, false, domainembedding.ErrInvalidMultimodalJob
	}
	validated, err := domainembedding.NewMultimodalEmbeddingJob(
		job.VideoID, job.Contract, job.SourceHash, job.MaxAttempts, job.CreatedAt,
	)
	if err != nil {
		return nil, false, false, err
	}
	var stored MultimodalEmbeddingJobModel
	created := false
	refreshed := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := multimodalDatabaseTime(tx)
		if err != nil {
			return err
		}
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("video_id = ? AND contract_key = ?", validated.VideoID, validated.Contract.Key()).
			Take(&stored)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			stored = multimodalJobModel(validated)
			stored.CreatedAt = now
			stored.UpdatedAt = now
			stored.NextAttemptAt = now
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&stored)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 1 {
				created = true
				return nil
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("video_id = ? AND contract_key = ?", validated.VideoID, validated.Contract.Key()).
				Take(&stored).Error; err != nil {
				return err
			}
		} else if query.Error != nil {
			return query.Error
		}
		if stored.SourceHash == validated.SourceHash && stored.MaxAttempts == validated.MaxAttempts {
			return nil
		}
		updates := map[string]any{
			"source_hash": validated.SourceHash, "state": domainembedding.MultimodalJobStatePending,
			"attempts": 0, "max_attempts": validated.MaxAttempts, "claim_token": "",
			"lease_until": nil, "next_attempt_at": now, "failure_code": "",
			"completed_at": nil, "updated_at": now,
		}
		if err := tx.Model(&MultimodalEmbeddingJobModel{}).Where("id = ?", stored.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", stored.ID).Take(&stored).Error; err != nil {
			return err
		}
		refreshed = true
		return nil
	})
	if err != nil {
		return nil, false, false, err
	}
	restored, err := multimodalJobFromModel(stored)
	return restored, created, refreshed, err
}

func (r *Repository) ClaimMultimodalJobs(
	ctx context.Context,
	owner string,
	leaseTTL time.Duration,
	limit int,
) ([]*domainembedding.MultimodalEmbeddingJob, error) {
	owner = strings.TrimSpace(owner)
	if r == nil || r.db == nil || owner == "" || len(owner) > 64 ||
		leaseTTL <= 0 || leaseTTL > 10*time.Minute || limit < 1 || limit > 100 {
		return nil, domainembedding.ErrInvalidMultimodalJob
	}
	claimed := make([]*domainembedding.MultimodalEmbeddingJob, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := multimodalDatabaseTime(tx)
		if err != nil {
			return err
		}
		var models []MultimodalEmbeddingJobModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`attempts < max_attempts AND (
				(state IN ? AND next_attempt_at <= ?) OR
				(state = ? AND lease_until <= ?)
			)`, []string{domainembedding.MultimodalJobStatePending, domainembedding.MultimodalJobStateRetry}, now,
				domainembedding.MultimodalJobStateLeased, now).
			Order("next_attempt_at ASC, id ASC").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		for index := range models {
			token, err := newMultimodalClaimToken(owner)
			if err != nil {
				return err
			}
			leaseUntil := now.Add(leaseTTL)
			if err := tx.Model(&MultimodalEmbeddingJobModel{}).Where("id = ?", models[index].ID).Updates(map[string]any{
				"state": domainembedding.MultimodalJobStateLeased, "attempts": models[index].Attempts + 1,
				"claim_token": token, "lease_until": leaseUntil, "failure_code": "",
				"completed_at": nil, "updated_at": now,
			}).Error; err != nil {
				return err
			}
			models[index].State = domainembedding.MultimodalJobStateLeased
			models[index].Attempts++
			models[index].ClaimToken = token
			models[index].LeaseUntil = &leaseUntil
			models[index].FailureCode = ""
			models[index].CompletedAt = nil
			models[index].UpdatedAt = now
			job, err := multimodalJobFromModel(models[index])
			if err != nil {
				return err
			}
			claimed = append(claimed, job)
		}
		return nil
	})
	return claimed, err
}

func (r *Repository) HeartbeatMultimodalJob(
	ctx context.Context,
	jobID int64,
	claimToken string,
	leaseTTL time.Duration,
) (bool, error) {
	if jobID <= 0 || strings.TrimSpace(claimToken) == "" || leaseTTL <= 0 || leaseTTL > 10*time.Minute {
		return false, domainembedding.ErrInvalidMultimodalJob
	}
	now, err := multimodalDatabaseTime(r.db.WithContext(ctx))
	if err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&MultimodalEmbeddingJobModel{}).
		Where("id = ? AND state = ? AND claim_token = ? AND lease_until > ?", jobID, domainembedding.MultimodalJobStateLeased, strings.TrimSpace(claimToken), now).
		Updates(map[string]any{"lease_until": now.Add(leaseTTL), "updated_at": now})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) RetryMultimodalJob(
	ctx context.Context,
	jobID int64,
	claimToken string,
	failureCode string,
	retryAfter time.Duration,
) (bool, error) {
	failureCode = strings.ToLower(strings.TrimSpace(failureCode))
	if jobID <= 0 || strings.TrimSpace(claimToken) == "" || failureCode == "" ||
		!domainembedding.ValidMultimodalFailureCode(failureCode) || retryAfter < time.Second || retryAfter > 24*time.Hour {
		return false, domainembedding.ErrInvalidMultimodalJob
	}
	now, err := multimodalDatabaseTime(r.db.WithContext(ctx))
	if err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&MultimodalEmbeddingJobModel{}).
		Where("id = ? AND state = ? AND claim_token = ? AND lease_until > ? AND attempts < max_attempts", jobID, domainembedding.MultimodalJobStateLeased, strings.TrimSpace(claimToken), now).
		Updates(map[string]any{
			"state": domainembedding.MultimodalJobStateRetry, "claim_token": "", "lease_until": nil,
			"next_attempt_at": now.Add(retryAfter), "failure_code": failureCode,
			"completed_at": nil, "updated_at": now,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) CompleteMultimodalJob(
	ctx context.Context,
	jobID int64,
	claimToken string,
	fact *domainembedding.MultimodalVectorFact,
) (bool, error) {
	if jobID <= 0 || strings.TrimSpace(claimToken) == "" || fact == nil {
		return false, domainembedding.ErrInvalidMultimodalVectorFact
	}
	completed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := multimodalDatabaseTime(tx)
		if err != nil {
			return err
		}
		var job MultimodalEmbeddingJobModel
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND state = ? AND claim_token = ? AND lease_until > ?", jobID, domainembedding.MultimodalJobStateLeased, strings.TrimSpace(claimToken), now).
			Take(&job)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if query.Error != nil {
			return query.Error
		}
		if fact.VideoID != job.VideoID || fact.Identity.Contract.Key() != job.ContractKey ||
			fact.Identity.SourceHash != job.SourceHash {
			return domainembedding.ErrMultimodalOperationConflict
		}
		model, err := multimodalFactModel(fact)
		if err != nil {
			return err
		}
		model.CreatedAt = now
		model.UpdatedAt = now
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "video_id"}, {Name: "contract_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"provider_alias", "model_alias", "revision_alias", "dimension",
				"text_canonicalizer", "frame_sampling_policy", "image_preprocessing_policy",
				"fusion_policy", "source_hash", "vector_digest", "embedding_json", "updated_at",
			}),
			Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: `
				multimodal_vector_fact.source_hash IS DISTINCT FROM EXCLUDED.source_hash OR
				multimodal_vector_fact.vector_digest IS DISTINCT FROM EXCLUDED.vector_digest OR
				multimodal_vector_fact.embedding_json IS DISTINCT FROM EXCLUDED.embedding_json
			`}}},
		}).Create(&model).Error; err != nil {
			return err
		}
		result := tx.Model(&MultimodalEmbeddingJobModel{}).
			Where("id = ? AND state = ? AND claim_token = ?", jobID, domainembedding.MultimodalJobStateLeased, strings.TrimSpace(claimToken)).
			Updates(map[string]any{
				"state": domainembedding.MultimodalJobStateSucceeded, "claim_token": "", "lease_until": nil,
				"failure_code": "", "completed_at": now, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		completed = result.RowsAffected == 1
		return nil
	})
	return completed, err
}

func (r *Repository) TerminalMultimodalJob(
	ctx context.Context,
	jobID int64,
	claimToken string,
	failureCode string,
) (bool, error) {
	failureCode = strings.ToLower(strings.TrimSpace(failureCode))
	if jobID <= 0 || strings.TrimSpace(claimToken) == "" || failureCode == "" ||
		!domainembedding.ValidMultimodalFailureCode(failureCode) {
		return false, domainembedding.ErrInvalidMultimodalJob
	}
	now, err := multimodalDatabaseTime(r.db.WithContext(ctx))
	if err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&MultimodalEmbeddingJobModel{}).
		Where("id = ? AND state = ? AND claim_token = ? AND lease_until > ?", jobID, domainembedding.MultimodalJobStateLeased, strings.TrimSpace(claimToken), now).
		Updates(map[string]any{
			"state": domainembedding.MultimodalJobStateTerminal, "claim_token": "", "lease_until": nil,
			"failure_code": failureCode, "completed_at": now, "updated_at": now,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) RequeueMultimodalJob(
	ctx context.Context,
	jobID int64,
	operationKey string,
) (bool, error) {
	operationKey = strings.TrimSpace(operationKey)
	if jobID <= 0 || operationKey == "" || len(operationKey) > 128 {
		return false, domainembedding.ErrInvalidMultimodalJob
	}
	replayed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := multimodalDatabaseTime(tx)
		if err != nil {
			return err
		}
		var job MultimodalEmbeddingJobModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", jobID).Take(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainembedding.ErrMultimodalJobNotFound
			}
			return err
		}
		receipt := MultimodalJobOperationModel{JobID: jobID, OperationKey: operationKey, Operation: "manual_requeue", CreatedAt: now}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			replayed = true
			return nil
		}
		if job.State != domainembedding.MultimodalJobStateTerminal {
			return domainembedding.ErrMultimodalOperationConflict
		}
		return tx.Model(&MultimodalEmbeddingJobModel{}).Where("id = ?", jobID).Updates(map[string]any{
			"state": domainembedding.MultimodalJobStatePending, "attempts": 0,
			"claim_token": "", "lease_until": nil, "next_attempt_at": now,
			"failure_code": "", "completed_at": nil, "updated_at": now,
		}).Error
	})
	return replayed, err
}

func (r *Repository) DeleteCompletedMultimodalJobsBefore(
	ctx context.Context,
	before time.Time,
	limit int,
) (int64, error) {
	if before.IsZero() || limit < 1 || limit > 1000 {
		return 0, domainembedding.ErrInvalidMultimodalJob
	}
	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		if err := tx.Model(&MultimodalEmbeddingJobModel{}).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state IN ? AND completed_at < ?", []string{domainembedding.MultimodalJobStateSucceeded, domainembedding.MultimodalJobStateTerminal}, before.UTC()).
			Order("completed_at ASC, id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Where("job_id IN ?", ids).Delete(&MultimodalJobOperationModel{}).Error; err != nil {
			return err
		}
		result := tx.Where("id IN ?", ids).Delete(&MultimodalEmbeddingJobModel{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}

func (r *Repository) FindMultimodalVectorFact(
	ctx context.Context,
	videoID int64,
	contract domainembedding.MultimodalContractIdentity,
) (*domainembedding.MultimodalVectorFact, error) {
	validatedContract, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	if videoID <= 0 || err != nil || !validatedContract.Equal(contract) {
		return nil, domainembedding.ErrInvalidMultimodalVectorFact
	}
	var model MultimodalVectorFactModel
	err = r.db.WithContext(ctx).Where("video_id = ? AND contract_key = ?", videoID, contract.Key()).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainembedding.ErrMultimodalVectorFactNotFound
	}
	if err != nil {
		return nil, err
	}
	return multimodalFactFromModel(model)
}

func (r *Repository) ListMultimodalReconciliationVideoIDs(
	ctx context.Context,
	contract domainembedding.MultimodalContractIdentity,
	afterVideoID int64,
	limit int,
) ([]int64, error) {
	validated, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	if err != nil || !validated.Equal(contract) || afterVideoID < 0 || limit < 1 || limit > 1000 {
		return nil, domainembedding.ErrInvalidMultimodalProjection
	}
	var videoIDs []int64
	err = r.db.WithContext(ctx).Raw(`
		SELECT video_id
		FROM (
			SELECT video_id FROM multimodal_vector_fact
			WHERE contract_key = ? AND video_id > ?
			UNION
			SELECT video_id FROM multimodal_projection
			WHERE contract_key = ? AND video_id > ?
		) AS candidates
		ORDER BY video_id ASC
		LIMIT ?
	`, contract.Key(), afterVideoID, contract.Key(), afterVideoID, limit).Scan(&videoIDs).Error
	return videoIDs, err
}

func (r *Repository) UpsertMultimodalProjection(
	ctx context.Context,
	projection *domainembedding.MultimodalProjection,
) (bool, error) {
	model, err := multimodalProjectionModel(projection)
	if err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "video_id"}, {Name: "contract_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"provider_alias", "model_alias", "revision_alias", "dimension",
			"text_canonicalizer", "frame_sampling_policy", "image_preprocessing_policy",
			"fusion_policy", "source_hash", "vector_digest", "embedding_json",
			"published_at", "updated_at",
		}),
		Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: `
			multimodal_projection.source_hash IS DISTINCT FROM EXCLUDED.source_hash OR
			multimodal_projection.vector_digest IS DISTINCT FROM EXCLUDED.vector_digest OR
			multimodal_projection.published_at IS DISTINCT FROM EXCLUDED.published_at
		`}}},
	}).Create(&model)
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) DeleteMultimodalProjection(
	ctx context.Context,
	videoID int64,
	contractKey string,
) (bool, error) {
	contractKey = strings.ToLower(strings.TrimSpace(contractKey))
	if videoID <= 0 || len(contractKey) != domainembedding.MultimodalDigestHexLength {
		return false, domainembedding.ErrInvalidMultimodalProjection
	}
	result := r.db.WithContext(ctx).
		Where("video_id = ? AND contract_key = ?", videoID, contractKey).
		Delete(&MultimodalProjectionModel{})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) DeleteMultimodalProjectionIfStale(
	ctx context.Context,
	videoID int64,
	contractKey string,
	expectedSourceHash string,
	expectedVectorDigest string,
) (bool, error) {
	if videoID <= 0 || len(strings.TrimSpace(contractKey)) != domainembedding.MultimodalDigestHexLength {
		return false, domainembedding.ErrInvalidMultimodalProjection
	}
	result := r.db.WithContext(ctx).Where(
		"video_id = ? AND contract_key = ? AND (source_hash IS DISTINCT FROM ? OR vector_digest IS DISTINCT FROM ?)",
		videoID, strings.ToLower(strings.TrimSpace(contractKey)),
		strings.ToLower(strings.TrimSpace(expectedSourceHash)), strings.ToLower(strings.TrimSpace(expectedVectorDigest)),
	).Delete(&MultimodalProjectionModel{})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) ExactMultimodalSearch(
	ctx context.Context,
	contract domainembedding.MultimodalContractIdentity,
	query []float64,
	excludedVideoIDs []int64,
	limit int,
) ([]domainembedding.MultimodalExactCandidate, error) {
	started := time.Now()
	query, err := domainembedding.ValidateMultimodalQueryVector(contract, query)
	if err != nil || limit < 1 || limit > 500 || len(excludedVideoIDs) > 1000 {
		inframetrics.ObserveMultimodalExactQuery("error", time.Since(started), 0)
		return nil, domainembedding.ErrInvalidMultimodalProjection
	}
	queryJSON, err := json.Marshal(query)
	if err != nil {
		inframetrics.ObserveMultimodalExactQuery("error", time.Since(started), 0)
		return nil, domainembedding.ErrInvalidMultimodalProjection
	}
	statement := `
		WITH query_vector AS (
			SELECT value::double precision AS component, ordinality
			FROM jsonb_array_elements_text(?::jsonb) WITH ORDINALITY
		), scored AS (
			SELECT p.video_id, p.published_at,
			       SUM(value::double precision * query_vector.component) AS similarity,
			       COUNT(*) AS dimensions
			FROM multimodal_projection AS p
			CROSS JOIN LATERAL jsonb_array_elements_text(p.embedding_json) WITH ORDINALITY AS vector(value, ordinality)
			JOIN query_vector USING (ordinality)
			WHERE p.contract_key = ?`
	arguments := []any{string(queryJSON), contract.Key()}
	if len(excludedVideoIDs) > 0 {
		statement += " AND p.video_id NOT IN ?"
		arguments = append(arguments, excludedVideoIDs)
	}
	statement += `
			GROUP BY p.video_id, p.published_at
			HAVING COUNT(*) = ?
		)
		SELECT video_id, similarity, published_at
		FROM scored
		WHERE similarity > 0
		ORDER BY similarity DESC, published_at DESC, video_id DESC
		LIMIT ?`
	arguments = append(arguments, contract.Dimension, limit)
	var candidates []domainembedding.MultimodalExactCandidate
	if err := r.db.WithContext(ctx).Raw(statement, arguments...).Scan(&candidates).Error; err != nil {
		result := "error"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result = "cancelled"
		}
		inframetrics.ObserveMultimodalExactQuery(result, time.Since(started), 0)
		return nil, err
	}
	result := "success"
	if len(candidates) == 0 {
		result = "empty"
	}
	inframetrics.ObserveMultimodalExactQuery(result, time.Since(started), len(candidates))
	return candidates, nil
}

func multimodalJobModel(job *domainembedding.MultimodalEmbeddingJob) MultimodalEmbeddingJobModel {
	return MultimodalEmbeddingJobModel{
		VideoID: job.VideoID, ContractKey: job.Contract.Key(),
		MultimodalContractColumns: multimodalContractColumns(job.Contract),
		SourceHash:                job.SourceHash, State: job.State, Attempts: job.Attempts,
		MaxAttempts: job.MaxAttempts, ClaimToken: job.ClaimToken,
		LeaseUntil: job.LeaseUntil, NextAttemptAt: job.NextAttemptAt,
		FailureCode: job.FailureCode, CompletedAt: job.CompletedAt,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func multimodalJobFromModel(model MultimodalEmbeddingJobModel) (*domainembedding.MultimodalEmbeddingJob, error) {
	contract, err := multimodalContractFromColumns(model.MultimodalContractColumns)
	if err != nil || contract.Key() != model.ContractKey {
		return nil, domainembedding.ErrInvalidMultimodalJob
	}
	job := domainembedding.RestoreMultimodalEmbeddingJob(
		model.ID, model.VideoID, contract, model.SourceHash, model.State,
		model.Attempts, model.MaxAttempts, model.ClaimToken, model.LeaseUntil,
		model.NextAttemptAt, model.FailureCode, model.CreatedAt, model.UpdatedAt, model.CompletedAt,
	)
	if job == nil {
		return nil, domainembedding.ErrInvalidMultimodalJob
	}
	return job, nil
}

func multimodalFactModel(fact *domainembedding.MultimodalVectorFact) (MultimodalVectorFactModel, error) {
	if fact == nil || fact.VideoID <= 0 || fact.CreatedAt.IsZero() || fact.UpdatedAt.IsZero() {
		return MultimodalVectorFactModel{}, domainembedding.ErrInvalidMultimodalVectorFact
	}
	validated, err := domainembedding.ValidateMultimodalVector(
		fact.Identity.Contract, fact.Identity.SourceHash, fact.Identity, fact.Values,
	)
	if err != nil {
		return MultimodalVectorFactModel{}, domainembedding.ErrInvalidMultimodalVectorFact
	}
	embeddingJSON, err := json.Marshal(validated.Values)
	if err != nil {
		return MultimodalVectorFactModel{}, domainembedding.ErrInvalidMultimodalVectorFact
	}
	return MultimodalVectorFactModel{
		ID: fact.ID, VideoID: fact.VideoID, ContractKey: fact.Identity.Contract.Key(),
		MultimodalContractColumns: multimodalContractColumns(fact.Identity.Contract),
		SourceHash:                fact.Identity.SourceHash, VectorDigest: fact.Identity.VectorDigest,
		EmbeddingJSON: string(embeddingJSON), CreatedAt: fact.CreatedAt, UpdatedAt: fact.UpdatedAt,
	}, nil
}

func multimodalFactFromModel(model MultimodalVectorFactModel) (*domainembedding.MultimodalVectorFact, error) {
	contract, err := multimodalContractFromColumns(model.MultimodalContractColumns)
	if err != nil || contract.Key() != model.ContractKey {
		return nil, domainembedding.ErrInvalidMultimodalVectorFact
	}
	var values []float64
	if err := json.Unmarshal([]byte(model.EmbeddingJSON), &values); err != nil {
		return nil, domainembedding.ErrInvalidMultimodalVectorFact
	}
	fact := domainembedding.RestoreMultimodalVectorFact(
		model.ID, model.VideoID,
		domainembedding.MultimodalVectorIdentity{
			Contract: contract, SourceHash: model.SourceHash, VectorDigest: model.VectorDigest,
		},
		values, model.CreatedAt, model.UpdatedAt,
	)
	if fact == nil {
		return nil, domainembedding.ErrInvalidMultimodalVectorFact
	}
	return fact, nil
}

func multimodalProjectionModel(projection *domainembedding.MultimodalProjection) (MultimodalProjectionModel, error) {
	if projection == nil || projection.VideoID <= 0 || projection.PublishedAt.IsZero() || projection.UpdatedAt.IsZero() {
		return MultimodalProjectionModel{}, domainembedding.ErrInvalidMultimodalProjection
	}
	if _, err := domainembedding.ValidateMultimodalVector(
		projection.Identity.Contract, projection.Identity.SourceHash,
		projection.Identity, projection.Values,
	); err != nil {
		return MultimodalProjectionModel{}, domainembedding.ErrInvalidMultimodalProjection
	}
	embeddingJSON, err := json.Marshal(projection.Values)
	if err != nil {
		return MultimodalProjectionModel{}, domainembedding.ErrInvalidMultimodalProjection
	}
	return MultimodalProjectionModel{
		VideoID: projection.VideoID, ContractKey: projection.Identity.Contract.Key(),
		MultimodalContractColumns: multimodalContractColumns(projection.Identity.Contract),
		SourceHash:                projection.Identity.SourceHash, VectorDigest: projection.Identity.VectorDigest,
		EmbeddingJSON: string(embeddingJSON), PublishedAt: projection.PublishedAt, UpdatedAt: projection.UpdatedAt,
	}, nil
}

func multimodalContractColumns(contract domainembedding.MultimodalContractIdentity) MultimodalContractColumns {
	return MultimodalContractColumns{
		ProviderAlias: contract.ProviderAlias, ModelAlias: contract.ModelAlias,
		RevisionAlias: contract.RevisionAlias, Dimension: contract.Dimension,
		TextCanonicalizer:        contract.TextCanonicalizer,
		FrameSamplingPolicy:      contract.FrameSamplingPolicy,
		ImagePreprocessingPolicy: contract.ImagePreprocessingPolicy,
		FusionPolicy:             contract.FusionPolicy,
	}
}

func multimodalContractFromColumns(columns MultimodalContractColumns) (domainembedding.MultimodalContractIdentity, error) {
	return domainembedding.NewMultimodalContractIdentity(
		columns.ProviderAlias, columns.ModelAlias, columns.RevisionAlias, columns.Dimension,
		columns.TextCanonicalizer, columns.FrameSamplingPolicy,
		columns.ImagePreprocessingPolicy, columns.FusionPolicy,
	)
}

func multimodalDatabaseTime(db *gorm.DB) (time.Time, error) {
	var now time.Time
	err := db.Raw("SELECT CURRENT_TIMESTAMP").Scan(&now).Error
	return now.UTC(), err
}

func newMultimodalClaimToken(owner string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return strings.TrimSpace(owner) + ":" + hex.EncodeToString(random[:]), nil
}
