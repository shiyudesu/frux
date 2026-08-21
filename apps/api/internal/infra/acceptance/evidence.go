package infraacceptance

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type EvidenceFailureCode string

const (
	EvidenceUnavailable       EvidenceFailureCode = "unavailable"
	EvidenceJobTerminal       EvidenceFailureCode = "job_terminal"
	EvidenceJobIncomplete     EvidenceFailureCode = "job_incomplete"
	EvidenceFactMissing       EvidenceFailureCode = "fact_missing"
	EvidenceProjectionMissing EvidenceFailureCode = "projection_missing"
	EvidenceContractMismatch  EvidenceFailureCode = "contract_mismatch"
	EvidenceVectorInvalid     EvidenceFailureCode = "vector_invalid"
	EvidenceDigestMismatch    EvidenceFailureCode = "digest_mismatch"
	EvidenceSourceMismatch    EvidenceFailureCode = "source_mismatch"
)

type EvidenceError struct{ Code EvidenceFailureCode }

func (e *EvidenceError) Error() string {
	if e == nil {
		return "acceptance evidence failure"
	}
	return "acceptance evidence failure: " + string(e.Code)
}

type ReviewCaseEvidence struct {
	ID            int64
	Version       int
	ReviewVersion int
	Status        string
}

type DatabaseEvidence struct {
	JobID                int64
	JobState             string
	Attempts             int
	FailureCode          string
	Contract             domainembedding.MultimodalContractIdentity
	FactPresent          bool
	ProjectionPresent    bool
	VectorLength         int
	VectorNorm           float64
	FactDigest           string
	ProjectionDigest     string
	FactSourceHash       string
	ProjectionSourceHash string
}

type EvidenceStore struct{ db *sql.DB }

func NewEvidenceStore(dsn string) (*EvidenceStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrInvalidAcceptanceConfig
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, ErrInvalidAcceptanceConfig
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)
	return &EvidenceStore{db: db}, nil
}

func (s *EvidenceStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *EvidenceStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	if err := s.db.PingContext(ctx); err != nil {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	return nil
}

func (s *EvidenceStore) ReviewCase(ctx context.Context, videoID int64) (ReviewCaseEvidence, error) {
	var evidence ReviewCaseEvidence
	err := s.db.QueryRowContext(ctx, `
		SELECT id, version, review_version, status
		FROM review_case
		WHERE video_id = $1
		ORDER BY id DESC
		LIMIT 1`, videoID,
	).Scan(&evidence.ID, &evidence.Version, &evidence.ReviewVersion, &evidence.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewCaseEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	if err != nil {
		return ReviewCaseEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	return evidence, nil
}

func (s *EvidenceStore) Multimodal(ctx context.Context, videoID int64, profileID string) (DatabaseEvidence, error) {
	profile, err := multimodalprofile.Resolve(profileID)
	if err != nil {
		return DatabaseEvidence{}, &EvidenceError{Code: EvidenceContractMismatch}
	}
	contractKey := profile.Contract.Key()
	var evidence DatabaseEvidence
	var provider, model, revision, textPolicy, framePolicy, imagePolicy, fusionPolicy sql.NullString
	var dimension, vectorLength sql.NullInt64
	var norm sql.NullFloat64
	var factDigest, projectionDigest, factSource, projectionSource sql.NullString
	var factVideoID, projectionVideoID sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT j.id, j.state, j.attempts, j.failure_code,
		       f.video_id, f.provider_alias, f.model_alias, f.revision_alias, f.dimension,
		       f.text_canonicalizer, f.frame_sampling_policy, f.image_preprocessing_policy, f.fusion_policy,
		       jsonb_array_length(f.embedding_json::jsonb),
		       (SELECT sqrt(sum((value #>> '{}')::double precision * (value #>> '{}')::double precision))
		          FROM jsonb_array_elements(f.embedding_json::jsonb) AS value),
		       f.vector_digest, f.source_hash,
		       p.video_id, p.vector_digest, p.source_hash
		FROM multimodal_embedding_job j
		LEFT JOIN multimodal_vector_fact f ON f.video_id = j.video_id AND f.contract_key = j.contract_key
		LEFT JOIN multimodal_projection p ON p.video_id = j.video_id AND p.contract_key = j.contract_key
		WHERE j.video_id = $1 AND j.contract_key = $2
		ORDER BY j.id DESC
		LIMIT 1`, videoID, contractKey,
	).Scan(
		&evidence.JobID, &evidence.JobState, &evidence.Attempts, &evidence.FailureCode,
		&factVideoID, &provider, &model, &revision, &dimension,
		&textPolicy, &framePolicy, &imagePolicy, &fusionPolicy,
		&vectorLength, &norm, &factDigest, &factSource,
		&projectionVideoID, &projectionDigest, &projectionSource,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DatabaseEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	if err != nil {
		return DatabaseEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	evidence.FactPresent = factVideoID.Valid
	evidence.ProjectionPresent = projectionVideoID.Valid
	if evidence.FactPresent {
		evidence.Contract, _ = domainembedding.NewMultimodalContractIdentity(
			provider.String, model.String, revision.String, int(dimension.Int64),
			textPolicy.String, framePolicy.String, imagePolicy.String, fusionPolicy.String,
		)
		evidence.VectorLength = int(vectorLength.Int64)
		evidence.VectorNorm = norm.Float64
		evidence.FactDigest = factDigest.String
		evidence.FactSourceHash = factSource.String
	}
	evidence.ProjectionDigest = projectionDigest.String
	evidence.ProjectionSourceHash = projectionSource.String
	if err := ValidateDatabaseEvidence(evidence, profile.Contract); err != nil {
		return evidence, err
	}
	return evidence, nil
}

func ValidateDatabaseEvidence(evidence DatabaseEvidence, expected domainembedding.MultimodalContractIdentity) error {
	switch evidence.JobState {
	case "terminal":
		return &EvidenceError{Code: EvidenceJobTerminal}
	case "succeeded":
	default:
		return &EvidenceError{Code: EvidenceJobIncomplete}
	}
	if !evidence.FactPresent {
		return &EvidenceError{Code: EvidenceFactMissing}
	}
	if !evidence.ProjectionPresent {
		return &EvidenceError{Code: EvidenceProjectionMissing}
	}
	if !evidence.Contract.Equal(expected) {
		return &EvidenceError{Code: EvidenceContractMismatch}
	}
	if evidence.VectorLength != expected.Dimension || math.IsNaN(evidence.VectorNorm) || math.IsInf(evidence.VectorNorm, 0) ||
		math.Abs(evidence.VectorNorm-1) > domainembedding.MultimodalVectorNormTolerance {
		return &EvidenceError{Code: EvidenceVectorInvalid}
	}
	if evidence.FactDigest == "" || evidence.FactDigest != evidence.ProjectionDigest {
		return &EvidenceError{Code: EvidenceDigestMismatch}
	}
	if evidence.FactSourceHash == "" || evidence.FactSourceHash != evidence.ProjectionSourceHash {
		return &EvidenceError{Code: EvidenceSourceMismatch}
	}
	return nil
}

func (e DatabaseEvidence) Report(videoID int64) (applicationacceptance.FixtureEvidence, applicationacceptance.VectorEvidence) {
	return applicationacceptance.FixtureEvidence{
			VideoID: videoID, JobID: e.JobID, JobState: e.JobState, Attempts: e.Attempts,
		}, applicationacceptance.VectorEvidence{
			VideoID: videoID, Dimension: e.VectorLength, Norm: e.VectorNorm,
			DigestMatches:   e.FactDigest != "" && e.FactDigest == e.ProjectionDigest,
			ContractMatches: true,
		}
}
