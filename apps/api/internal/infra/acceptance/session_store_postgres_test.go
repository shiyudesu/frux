package infraacceptance

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	applicationacceptance "github.com/shiyudesu/frux/internal/application/acceptance"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	multimodalprofile "github.com/shiyudesu/frux/internal/infra/multimodalprofile"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
	infraskrecommendation "github.com/shiyudesu/frux/internal/infra/persistence/recommendation"
)

func TestSessionStoreAgainstIsolatedPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping session acceptance PostgreSQL test")
	}
	base, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "session_acceptance_" + hex.EncodeToString(random[:])
	if _, err := base.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = base.Exec(`DROP SCHEMA "` + schema + `" CASCADE`) }()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	store, err := NewSessionStore(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.db.AutoMigrate(
		&infraskrecommendation.PolicyModel{}, &infraskrecommendation.RequestLogModel{},
		&infraembedding.MultimodalVectorFactModel{}, &infraembedding.MultimodalProjectionModel{},
	); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE video (id BIGINT PRIMARY KEY, status INTEGER NOT NULL, visibility TEXT NOT NULL, media_status TEXT NOT NULL, published_at TIMESTAMPTZ)`,
		`CREATE TABLE interaction_action (user_id BIGINT NOT NULL, video_id BIGINT NOT NULL, action_type TEXT NOT NULL, status INTEGER NOT NULL)`,
	} {
		if err := store.db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	profile, err := multimodalprofile.Resolve(multimodalprofile.TongyiFlashSnapshotProfile)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, videoID := range []int64{11, 12, 13} {
		if err := store.db.Exec(
			`INSERT INTO video (id,status,visibility,media_status,published_at) VALUES (?,?,?,?,?)`,
			videoID, 2, "public", "ready", now.Add(-time.Duration(videoID)*time.Minute),
		).Error; err != nil {
			t.Fatal(err)
		}
	}
	insertSessionAcceptanceVector(t, store, profile.Contract, 11, 0, now)
	insertSessionAcceptanceVector(t, store, profile.Contract, 12, 1, now)
	insertSessionAcceptanceVector(t, store, profile.Contract, 13, 0, now)
	config := applicationacceptance.SessionSemanticConfig{
		ExpectedProfile: profile.ID, PositiveSeedVideoID: 11,
		NegativeSeedVideoID: 12, ExpectedTargetVideoID: 13,
	}
	contract, fixtures, err := store.VerifyFixtures(context.Background(), config)
	if err != nil || contract.Dimension != profile.Contract.Dimension || fixtures.TargetSimilarity <= 0 {
		t.Fatalf("contract=%#v fixtures=%#v err=%v", contract, fixtures, err)
	}
	active, err := store.FavoriteActive(context.Background(), 7, 11)
	if err != nil || active {
		t.Fatalf("active=%v err=%v", active, err)
	}
	if err := store.db.Exec(`INSERT INTO interaction_action VALUES (7,11,'FAVORITE',1)`).Error; err != nil {
		t.Fatal(err)
	}
	active, err = store.FavoriteActive(context.Background(), 7, 11)
	if err != nil || !active {
		t.Fatalf("active=%v err=%v", active, err)
	}
	createSessionAcceptanceBasePolicy(t, store, 1, true, now)
	createSessionAcceptanceBasePolicy(t, store, 2, false, now)
	policy, requestID, err := store.InstallPolicy(context.Background(), "session-run", 7, profile.Contract.Key())
	if err != nil || policy.ID <= 0 || policy.Version != 3 || policy.RolloutPercent != 1 ||
		domainrecommendation.PolicyCohortPercent(7, "recommend", requestID) >= 1 {
		t.Fatalf("policy=%#v request=%q err=%v", policy, requestID, err)
	}
	var states []infraskrecommendation.PolicyModel
	if err := store.db.Order("version ASC").Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 || !states[0].Enabled || states[1].Enabled || !states[2].Enabled {
		t.Fatalf("states=%#v", states)
	}
	if err := store.DisablePolicy(context.Background(), policy.ID, policy.Version); err != nil {
		t.Fatal(err)
	}
	states = nil
	if err := store.db.Order("version ASC").Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	if !states[0].Enabled || states[1].Enabled || states[2].Enabled {
		t.Fatalf("states after disable=%#v", states)
	}
	insertSessionAcceptanceRequestLog(t, store, policy.Version, requestID, profile.Contract.Key(), now)
	evidence, err := store.RequestLog(context.Background(), 7, requestID, 13)
	if err != nil || evidence.PolicyVersion != policy.Version || !evidence.ExpectedTargetSeen ||
		evidence.SemanticSimilarity != 0.8 || evidence.Semantic == nil || evidence.Semantic.Confidence <= 0 {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
	if err := store.DeleteDisabledPolicy(context.Background(), policy.ID, policy.Version); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := store.db.Model(&infraskrecommendation.PolicyModel{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.VerifyFixtures(cancelled, config); err == nil {
		t.Fatal("cancelled fixture verification succeeded")
	}
}

func insertSessionAcceptanceVector(
	t testing.TB,
	store *SessionStore,
	contract domainembedding.MultimodalContractIdentity,
	videoID int64,
	axis int,
	now time.Time,
) {
	t.Helper()
	values := make([]float64, contract.Dimension)
	values[axis] = 1
	sourceHash := domainembedding.MultimodalSourceHash([]byte{byte(videoID)})
	digest := domainembedding.MultimodalVectorDigest(values)
	encoded, _ := json.Marshal(values)
	columns := infraembedding.MultimodalContractColumns{
		ProviderAlias: contract.ProviderAlias, ModelAlias: contract.ModelAlias,
		RevisionAlias: contract.RevisionAlias, Dimension: contract.Dimension,
		TextCanonicalizer: contract.TextCanonicalizer, FrameSamplingPolicy: contract.FrameSamplingPolicy,
		ImagePreprocessingPolicy: contract.ImagePreprocessingPolicy, FusionPolicy: contract.FusionPolicy,
	}
	if err := store.db.Create(&infraembedding.MultimodalVectorFactModel{
		VideoID: videoID, ContractKey: contract.Key(), MultimodalContractColumns: columns,
		SourceHash: sourceHash, VectorDigest: digest, EmbeddingJSON: string(encoded), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.db.Create(&infraembedding.MultimodalProjectionModel{
		VideoID: videoID, ContractKey: contract.Key(), MultimodalContractColumns: columns,
		SourceHash: sourceHash, VectorDigest: digest, EmbeddingJSON: string(encoded),
		PublishedAt: now.Add(-time.Duration(videoID) * time.Minute), UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func createSessionAcceptanceBasePolicy(t testing.TB, store *SessionStore, version int, enabled bool, now time.Time) {
	t.Helper()
	policy, err := domainrecommendation.NewPolicy(
		"recommend", version, enabled, domainrecommendation.InitialRecommendationPolicyConfiguration(), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.recommendation.CreatePolicy(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
}

func insertSessionAcceptanceRequestLog(
	t testing.TB,
	store *SessionStore,
	policyVersion int,
	requestID string,
	contractKey string,
	now time.Time,
) {
	t.Helper()
	semantic, err := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1, ContractKey: contractKey,
		Result:     domainrecommendation.SessionSemanticResultSuccess,
		Confidence: 0.75, ConfidenceBand: domainrecommendation.SessionSemanticConfidenceHigh,
		EligibleCount: 2, PositiveCount: 3, NegativeCount: 1, CompatibleCount: 2,
		ExcludedCount: 2, InputDigest: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	log, err := domainrecommendation.NewRecommendationRequestLog(domainrecommendation.RequestLogInput{
		RequestID: requestID, UserID: 7, Scene: "recommend", PolicyVersion: policyVersion,
		CreatedAt: now, SessionSemantic: semantic,
		Candidates: []domainrecommendation.LoggedCandidate{{
			VideoID: 13, Reasons: []string{domainrecommendation.RecallProviderSemanticSession},
			ScoreComponents: map[string]float64{domainrecommendation.FeatureSemanticSimilarity: 0.8},
		}},
		RecallDiagnostics: []domainrecommendation.RecallDiagnostic{{
			Phase: "final", Provider: domainrecommendation.RecallProviderSemanticSession,
			Result: "underfill", Reason: "insufficient_readable", Count: 9,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := log.CompactPayload()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Create(&infraskrecommendation.RequestLogModel{
		RequestID: requestID, UserID: 7, Scene: "recommend", PolicyVersion: policyVersion,
		PayloadJSON: string(payload), CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}
