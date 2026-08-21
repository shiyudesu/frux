package infraacceptance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
	infraskrecommendation "github.com/shiyudesu/frux/internal/infra/persistence/recommendation"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type SessionStore struct {
	sqlDB          *sql.DB
	db             *gorm.DB
	recommendation *infraskrecommendation.Repository
	embedding      *infraembedding.Repository
}

type SessionRequestLogEvidence struct {
	PolicyVersion      int
	Semantic           *domainrecommendation.SessionSemanticEvidence
	ExpectedTargetSeen bool
	SemanticSimilarity float64
	SemanticUnderfill  int
}

func NewSessionStore(dsn string) (*SessionStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, ErrInvalidAcceptanceConfig
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, ErrInvalidAcceptanceConfig
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(2 * time.Minute)
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		_ = sqlDB.Close()
		return nil, ErrInvalidAcceptanceConfig
	}
	return &SessionStore{
		sqlDB: sqlDB, db: db,
		recommendation: infraskrecommendation.New(db), embedding: infraembedding.New(db),
	}, nil
}

func (s *SessionStore) Close() error {
	if s == nil || s.sqlDB == nil {
		return nil
	}
	return s.sqlDB.Close()
}

func (s *SessionStore) Ping(ctx context.Context) error {
	if s == nil || s.sqlDB == nil {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	if err := s.sqlDB.PingContext(ctx); err != nil {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	return nil
}

func (s *SessionStore) VerifyFixtures(
	ctx context.Context,
	config applicationacceptance.SessionSemanticConfig,
) (applicationacceptance.ContractEvidence, applicationacceptance.SessionFixtureEvidence, error) {
	profile, err := multimodalprofile.Resolve(config.ExpectedProfile)
	if s == nil || s.db == nil || err != nil {
		return applicationacceptance.ContractEvidence{}, applicationacceptance.SessionFixtureEvidence{}, &EvidenceError{Code: EvidenceContractMismatch}
	}
	ids := []int64{config.PositiveSeedVideoID, config.NegativeSeedVideoID, config.ExpectedTargetVideoID}
	var readable int64
	err = s.db.WithContext(ctx).Table("video").
		Where("id IN ? AND status = ? AND visibility = ? AND media_status IN ? AND published_at IS NOT NULL",
			ids, domainvideo.StatusPublished, domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady},
		).Count(&readable).Error
	if err != nil || readable != int64(len(ids)) {
		return applicationacceptance.ContractEvidence{}, applicationacceptance.SessionFixtureEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	vectors, err := s.embedding.LoadSessionSemanticVectors(ctx, ids, profile.Contract)
	if err != nil || len(vectors) != len(ids) {
		return applicationacceptance.ContractEvidence{}, applicationacceptance.SessionFixtureEvidence{}, &EvidenceError{Code: EvidenceFactMissing}
	}
	positive := vectors[config.PositiveSeedVideoID]
	if positive == nil {
		return applicationacceptance.ContractEvidence{}, applicationacceptance.SessionFixtureEvidence{}, &EvidenceError{Code: EvidenceFactMissing}
	}
	candidates, err := s.embedding.ExactMultimodalSearch(
		ctx, profile.Contract, positive.Values,
		[]int64{config.PositiveSeedVideoID, config.NegativeSeedVideoID}, 500,
	)
	if err != nil {
		return applicationacceptance.ContractEvidence{}, applicationacceptance.SessionFixtureEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	targetSimilarity := 0.0
	for _, candidate := range candidates {
		if candidate.VideoID == config.ExpectedTargetVideoID {
			targetSimilarity = candidate.Similarity
			break
		}
	}
	if targetSimilarity <= 0 || math.IsNaN(targetSimilarity) || math.IsInf(targetSimilarity, 0) {
		return applicationacceptance.ContractEvidence{}, applicationacceptance.SessionFixtureEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	contract := applicationacceptance.ContractEvidence{
		ProviderAlias: profile.Contract.ProviderAlias, ModelAlias: profile.Contract.ModelAlias,
		RevisionAlias: profile.Contract.RevisionAlias, Dimension: profile.Contract.Dimension,
		TextCanonicalizer:        profile.Contract.TextCanonicalizer,
		FrameSamplingPolicy:      profile.Contract.FrameSamplingPolicy,
		ImagePreprocessingPolicy: profile.Contract.ImagePreprocessingPolicy,
		FusionPolicy:             profile.Contract.FusionPolicy,
	}
	fixtures := applicationacceptance.SessionFixtureEvidence{
		PositiveSeedVideoID:   config.PositiveSeedVideoID,
		NegativeSeedVideoID:   config.NegativeSeedVideoID,
		ExpectedTargetVideoID: config.ExpectedTargetVideoID,
		TargetSimilarity:      targetSimilarity,
	}
	return contract, fixtures, nil
}

func (s *SessionStore) FavoriteActive(ctx context.Context, userID, videoID int64) (bool, error) {
	if s == nil || s.db == nil || userID <= 0 || videoID <= 0 {
		return false, &EvidenceError{Code: EvidenceUnavailable}
	}
	var count int64
	err := s.db.WithContext(ctx).Table("interaction_action").
		Where("user_id = ? AND video_id = ? AND action_type = ? AND status = ?", userID, videoID, "FAVORITE", 1).
		Count(&count).Error
	return count > 0, err
}

func (s *SessionStore) InstallPolicy(
	ctx context.Context,
	runID string,
	userID int64,
	contractKey string,
) (applicationacceptance.SessionPolicyEvidence, string, error) {
	if s == nil || s.recommendation == nil || userID <= 0 || strings.TrimSpace(contractKey) == "" {
		return applicationacceptance.SessionPolicyEvidence{}, "", &EvidenceError{Code: EvidenceUnavailable}
	}
	policies, err := s.recommendation.ListPolicies(ctx, "recommend")
	if err != nil {
		return applicationacceptance.SessionPolicyEvidence{}, "", err
	}
	version := 1
	for _, policy := range policies {
		if policy != nil && policy.Version >= version {
			version = policy.Version + 1
		}
	}
	if version <= 0 || version > domainrecommendation.MaxPolicyVersion {
		return applicationacceptance.SessionPolicyEvidence{}, "", domainrecommendation.ErrInvalidPolicyVersion
	}
	config := domainrecommendation.InitialRecommendationPolicyConfiguration()
	config.FeatureWeights[domainrecommendation.FeatureSemanticSimilarity] = 1
	config.RecallBudgets[domainrecommendation.RecallProviderSemanticSession] = 50
	config.ProviderDeadlinesMS[domainrecommendation.RecallProviderSemanticSession] = 500
	config.PreRankPoolLimit = domainrecommendation.MaxPolicyPreRankCandidates
	config.RecallProviderOrder = []string{
		domainrecommendation.RecallProviderFresh,
		domainrecommendation.RecallProviderHot,
		domainrecommendation.RecallProviderContentSimilarity,
		domainrecommendation.RecallProviderFollowedAuthor,
		domainrecommendation.RecallProviderSessionContinuation,
		domainrecommendation.RecallProviderSemanticSession,
	}
	config.RecallProviderReservations = map[string]int{
		domainrecommendation.RecallProviderFresh:               0,
		domainrecommendation.RecallProviderHot:                 0,
		domainrecommendation.RecallProviderContentSimilarity:   0,
		domainrecommendation.RecallProviderFollowedAuthor:      0,
		domainrecommendation.RecallProviderSessionContinuation: 0,
		domainrecommendation.RecallProviderSemanticSession:     10,
	}
	config.RolloutPercentage = 1
	config.SamplingRatePPM = domainrecommendation.MaxSamplingRatePPM
	config.HardSuppressExposures = false
	config.SessionSemantic = &domainrecommendation.SessionSemanticPolicyConfiguration{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1,
		ContractKey:    contractKey, LookbackSeconds: 2 * 60 * 60,
		MaxSeeds:           domainrecommendation.MaxSessionSemanticSeeds,
		MinPositiveSignals: 2, MinConfidence: 0.1,
	}
	policy, err := domainrecommendation.NewPolicy("recommend", version, true, config, time.Now().UTC())
	if err != nil {
		return applicationacceptance.SessionPolicyEvidence{}, "", err
	}
	created, err := s.recommendation.CreatePolicy(ctx, policy)
	if err != nil {
		return applicationacceptance.SessionPolicyEvidence{}, "", err
	}
	requestID, err := sessionAcceptanceCohortRequestID(runID, userID)
	if err != nil {
		_ = s.DisablePolicy(ctx, created.ID, created.Version)
		return applicationacceptance.SessionPolicyEvidence{}, "", err
	}
	return applicationacceptance.SessionPolicyEvidence{
		ID: created.ID, Version: created.Version, RolloutPercent: created.Config.RolloutPercentage,
	}, requestID, nil
}

func sessionAcceptanceCohortRequestID(runID string, userID int64) (string, error) {
	base := strings.TrimSpace(runID)
	if len(base) > 40 {
		base = base[:40]
	}
	for index := range 10_000 {
		candidate := fmt.Sprintf("%s-%04d", base, index)
		if len(candidate) <= domainrecommendation.MaxRequestIDLength &&
			domainrecommendation.PolicyCohortPercent(userID, "recommend", candidate) < 1 {
			return candidate, nil
		}
	}
	return "", errors.New("session acceptance cohort unavailable")
}

func (s *SessionStore) DisablePolicy(ctx context.Context, policyID int64, version int) error {
	if s == nil || s.db == nil || policyID <= 0 || version <= 0 {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	result := s.db.WithContext(ctx).Model(&infraskrecommendation.PolicyModel{}).
		Where("id = ? AND scene = ? AND version = ?", policyID, "recommend", version).
		Update("enabled", false)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	return nil
}

func (s *SessionStore) DeleteDisabledPolicy(ctx context.Context, policyID int64, version int) error {
	if s == nil || s.db == nil || policyID <= 0 || version <= 0 {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	result := s.db.WithContext(ctx).
		Where("id = ? AND scene = ? AND version = ? AND enabled = ?", policyID, "recommend", version, false).
		Delete(&infraskrecommendation.PolicyModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return &EvidenceError{Code: EvidenceUnavailable}
	}
	return nil
}

func (s *SessionStore) RequestLog(
	ctx context.Context,
	userID int64,
	requestID string,
	targetVideoID int64,
) (SessionRequestLogEvidence, error) {
	if s == nil || s.db == nil || userID <= 0 || strings.TrimSpace(requestID) == "" || targetVideoID <= 0 {
		return SessionRequestLogEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	var model infraskrecommendation.RequestLogModel
	err := s.db.WithContext(ctx).Where("user_id = ? AND request_id = ?", userID, requestID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SessionRequestLogEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	if err != nil {
		return SessionRequestLogEvidence{}, err
	}
	payloadBytes := []byte(model.PayloadJSON)
	for _, forbidden := range [][]byte{
		[]byte("embedding_json"), []byte("raw_event"), []byte("access_token"),
		[]byte("signed_url"), []byte("vector_values"),
	} {
		if bytes.Contains(payloadBytes, forbidden) {
			return SessionRequestLogEvidence{}, &EvidenceError{Code: EvidenceVectorInvalid}
		}
	}
	var payload struct {
		Candidates        []domainrecommendation.LoggedCandidate        `json:"candidates"`
		RecallDiagnostics []domainrecommendation.RecallDiagnostic       `json:"recall_diagnostics"`
		SessionSemantic   *domainrecommendation.SessionSemanticEvidence `json:"session_semantic"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	if err := decoder.Decode(&payload); err != nil || payload.SessionSemantic == nil {
		return SessionRequestLogEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	validated, err := domainrecommendation.NewSessionSemanticEvidence(*payload.SessionSemantic)
	if err != nil || validated.Result != domainrecommendation.SessionSemanticResultSuccess ||
		validated.Confidence <= 0 || validated.ConfidenceBand == domainrecommendation.SessionSemanticConfidenceNone {
		return SessionRequestLogEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	evidence := SessionRequestLogEvidence{PolicyVersion: model.PolicyVersion, Semantic: validated}
	for _, candidate := range payload.Candidates {
		if candidate.VideoID != targetVideoID {
			continue
		}
		for _, reason := range candidate.Reasons {
			if reason == domainrecommendation.RecallProviderSemanticSession {
				evidence.ExpectedTargetSeen = true
			}
		}
		evidence.SemanticSimilarity = candidate.ScoreComponents[domainrecommendation.FeatureSemanticSimilarity]
	}
	for _, diagnostic := range payload.RecallDiagnostics {
		if diagnostic.Provider == domainrecommendation.RecallProviderSemanticSession && diagnostic.Result == "underfill" {
			evidence.SemanticUnderfill = diagnostic.Count
		}
	}
	if !evidence.ExpectedTargetSeen || evidence.SemanticSimilarity <= 0 ||
		math.IsNaN(evidence.SemanticSimilarity) || math.IsInf(evidence.SemanticSimilarity, 0) {
		return SessionRequestLogEvidence{}, &EvidenceError{Code: EvidenceUnavailable}
	}
	return evidence, nil
}
