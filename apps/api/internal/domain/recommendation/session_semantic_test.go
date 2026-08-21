package domainrecommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sessionSemanticTestContractKey() string {
	sum := sha256.Sum256([]byte("session-semantic-test-contract"))
	return hex.EncodeToString(sum[:])
}

func validSessionSemanticPolicyConfig() PolicyConfiguration {
	config := validPolicyConfig(0)
	config.FeatureWeights[FeatureSemanticSimilarity] = 0.4
	config.RecallBudgets[RecallProviderSemanticSession] = 25
	config.ProviderDeadlinesMS[RecallProviderSemanticSession] = 250
	config.SessionSemantic = &SessionSemanticPolicyConfiguration{
		BuilderVersion:     SessionSemanticBuilderV1,
		ContractKey:        sessionSemanticTestContractKey(),
		LookbackSeconds:    2 * 60 * 60,
		MaxSeeds:           MaxSessionSemanticSeeds,
		MinPositiveSignals: 2,
		MinConfidence:      0.25,
	}
	return config
}

func TestSessionSemanticClosedTypesAndEvidence(t *testing.T) {
	for _, kind := range []SessionSemanticSignalKind{
		SessionSemanticSignalCurrent, SessionSemanticSignalComplete,
		SessionSemanticSignalSustained, SessionSemanticSignalLike,
		SessionSemanticSignalFavorite, SessionSemanticSignalEarlySkip,
		SessionSemanticSignalNotInterested, SessionSemanticSignalAlreadySeen,
	} {
		if !ValidSessionSemanticSignalKind(kind) {
			t.Fatalf("signal kind %q was not registered", kind)
		}
	}
	if ValidSessionSemanticSignalKind("raw-event") || ValidSessionSemanticResult("provider-body") ||
		ValidSessionSemanticConfidenceBand("user-123") {
		t.Fatal("unregistered session semantic value was accepted")
	}
	now := time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)
	if !(SessionSemanticSignal{VideoID: 7, Kind: SessionSemanticSignalComplete, OccurredAt: now}).Valid() ||
		(SessionSemanticSignal{VideoID: 0, Kind: SessionSemanticSignalComplete, OccurredAt: now}).Valid() {
		t.Fatal("session semantic signal bounds were not enforced")
	}
	digest := sha256.Sum256([]byte("bounded-input"))
	evidence, err := NewSessionSemanticEvidence(SessionSemanticEvidence{
		BuilderVersion: SessionSemanticBuilderV1, ContractKey: sessionSemanticTestContractKey(),
		Result: SessionSemanticResultSuccess, Confidence: 0.75, ConfidenceBand: SessionSemanticConfidenceHigh,
		EligibleCount: 3, PositiveCount: 4, NegativeCount: 1, CompatibleCount: 3, ExcludedCount: 1,
		InputDigest: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	clone := evidence.Clone()
	clone.Result = SessionSemanticResultUnavailable
	if evidence.Result != SessionSemanticResultSuccess {
		t.Fatal("session semantic evidence clone aliased original")
	}
	if _, err := NewSessionSemanticEvidence(SessionSemanticEvidence{
		BuilderVersion: SessionSemanticBuilderV1, ContractKey: sessionSemanticTestContractKey(),
		Result: SessionSemanticResultSuccess, ConfidenceBand: SessionSemanticConfidenceNone,
	}); !errors.Is(err, ErrInvalidSessionSemanticEvidence) {
		t.Fatalf("invalid evidence error=%v", err)
	}
}

func TestSessionSemanticPolicyNormalizationCloneAndJSON(t *testing.T) {
	config := validSessionSemanticPolicyConfig()
	config.SessionSemantic.BuilderVersion = " SESSION-SEMANTIC-V1 "
	config.SessionSemantic.ContractKey = " " + strings.ToUpper(sessionSemanticTestContractKey()) + " "
	policy, err := NewPolicy("recommend", 3, false, config, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.Config.SessionSemantic == nil ||
		policy.Config.SessionSemantic.BuilderVersion != SessionSemanticBuilderV1 ||
		policy.Config.SessionSemantic.ContractKey != sessionSemanticTestContractKey() ||
		policy.Config.FeatureWeights[FeatureSemanticSimilarity] != 0.4 ||
		policy.Config.RecallBudgets[RecallProviderSemanticSession] != 25 {
		t.Fatalf("policy=%#v", policy)
	}
	config.SessionSemantic.ContractKey = strings.Repeat("0", SessionSemanticDigestHexLength)
	if policy.Config.SessionSemantic.ContractKey != sessionSemanticTestContractKey() {
		t.Fatal("policy session semantic config aliased input")
	}
	clone := policy.Clone()
	clone.Config.SessionSemantic.ContractKey = strings.Repeat("f", SessionSemanticDigestHexLength)
	if policy.Config.SessionSemantic.ContractKey != sessionSemanticTestContractKey() {
		t.Fatal("policy clone aliased original session semantic config")
	}
	encoded, err := json.Marshal(policy.Config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PolicyConfiguration
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	restored := RestorePolicy(1, "recommend", 3, false, decoded, time.Unix(1, 0), time.Unix(2, 0))
	if restored == nil || !reflect.DeepEqual(restored.Config.SessionSemantic, policy.Config.SessionSemantic) {
		t.Fatalf("session semantic JSON round trip failed: %s %#v", encoded, restored)
	}
}

func TestSessionSemanticPolicyRejectsPartialAndInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PolicyConfiguration)
		want   error
	}{
		{name: "missing block", mutate: func(c *PolicyConfiguration) { c.SessionSemantic = nil }, want: ErrInvalidSessionSemanticPolicy},
		{name: "missing provider", mutate: func(c *PolicyConfiguration) {
			delete(c.RecallBudgets, RecallProviderSemanticSession)
			delete(c.ProviderDeadlinesMS, RecallProviderSemanticSession)
		}, want: ErrInvalidSessionSemanticPolicy},
		{name: "missing feature", mutate: func(c *PolicyConfiguration) { delete(c.FeatureWeights, FeatureSemanticSimilarity) }, want: ErrInvalidSessionSemanticPolicy},
		{name: "zero feature", mutate: func(c *PolicyConfiguration) { c.FeatureWeights[FeatureSemanticSimilarity] = 0 }, want: ErrInvalidSessionSemanticPolicy},
		{name: "unknown builder", mutate: func(c *PolicyConfiguration) { c.SessionSemantic.BuilderVersion = "v2" }, want: ErrInvalidSessionSemanticPolicy},
		{name: "bad contract", mutate: func(c *PolicyConfiguration) { c.SessionSemantic.ContractKey = "contract" }, want: ErrInvalidSessionSemanticPolicy},
		{name: "short lookback", mutate: func(c *PolicyConfiguration) { c.SessionSemantic.LookbackSeconds = 1 }, want: ErrInvalidSessionSemanticPolicy},
		{name: "too many seeds", mutate: func(c *PolicyConfiguration) { c.SessionSemantic.MaxSeeds = MaxSessionSemanticSeeds + 1 }, want: ErrInvalidSessionSemanticPolicy},
		{name: "too many signals", mutate: func(c *PolicyConfiguration) { c.SessionSemantic.MinPositiveSignals = MaxSessionSemanticSignalCount + 1 }, want: ErrInvalidSessionSemanticPolicy},
		{name: "invalid confidence", mutate: func(c *PolicyConfiguration) { c.SessionSemantic.MinConfidence = 1.1 }, want: ErrInvalidSessionSemanticPolicy},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validSessionSemanticPolicyConfig()
			test.mutate(&config)
			if _, err := NewPolicy("recommend", 3, false, config, time.Unix(1, 0)); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func TestBootstrapPoliciesRemainSessionSemanticFree(t *testing.T) {
	config := InitialRecommendationPolicyConfiguration()
	if config.SessionSemantic != nil || config.FeatureWeights[FeatureSemanticSimilarity] != 0 ||
		config.RecallBudgets[RecallProviderSemanticSession] != 0 ||
		config.ProviderDeadlinesMS[RecallProviderSemanticSession] != 0 {
		t.Fatalf("bootstrap configuration changed: %#v", config)
	}
	policies, err := InitialRecommendationPolicies(time.Unix(1, 0).UTC())
	if err != nil || len(policies) != 2 {
		t.Fatalf("policies=%#v err=%v", policies, err)
	}
	for _, policy := range policies {
		if policy.Config.SessionSemantic != nil || policy.Config.FeatureWeights[FeatureSemanticSimilarity] != 0 ||
			policy.Config.RecallBudgets[RecallProviderSemanticSession] != 0 {
			t.Fatalf("bootstrap policy %d changed: %#v", policy.Version, policy.Config)
		}
	}
}
