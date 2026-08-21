package domainrecommendation

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestPolicyValidationAndDeterministicStagedSelection(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	baseline, err := NewPolicy("Feed", 1, true, validPolicyConfig(100), now)
	if err != nil {
		t.Fatalf("create baseline policy: %v", err)
	}
	canary, err := NewPolicy("feed", 2, true, validPolicyConfig(20), now)
	if err != nil {
		t.Fatalf("create canary policy: %v", err)
	}
	for requestID := range 100 {
		first := SelectPolicy([]*Policy{baseline, canary}, 42, "request-"+int64String(int64(requestID)))
		second := SelectPolicy([]*Policy{baseline, canary}, 42, "request-"+int64String(int64(requestID)))
		if first == nil || second == nil || first.Version != second.Version {
			t.Fatalf("selection was not deterministic for request %d: %#v %#v", requestID, first, second)
		}
	}

	invalid := validPolicyConfig(100)
	invalid.FeatureWeights["unknown"] = 1
	if _, err := NewPolicy("feed", 3, true, invalid, now); !errors.Is(err, ErrUnknownPolicyFeature) {
		t.Fatalf("unknown feature error = %v, want %v", err, ErrUnknownPolicyFeature)
	}
	invalid = validPolicyConfig(100)
	invalid.ProviderDeadlinesMS[RecallProviderFresh] = MaxProviderDeadlineMS + 1
	if _, err := NewPolicy("feed", 3, true, invalid, now); !errors.Is(err, ErrInvalidPolicyBound) {
		t.Fatalf("overflow deadline error = %v, want %v", err, ErrInvalidPolicyBound)
	}

	exactBound := validPolicyConfig(100)
	exactBound.RecallBudgets = map[string]int{
		RecallProviderFresh:               100,
		RecallProviderHot:                 100,
		RecallProviderContentSimilarity:   100,
		RecallProviderFollowedAuthor:      100,
		RecallProviderSessionContinuation: 100,
	}
	exactBound.ProviderDeadlinesMS = map[string]int{
		RecallProviderFresh:               100,
		RecallProviderHot:                 100,
		RecallProviderContentSimilarity:   100,
		RecallProviderFollowedAuthor:      100,
		RecallProviderSessionContinuation: 100,
	}
	policy, err := NewPolicy("feed", 3, true, exactBound, now)
	if err != nil {
		t.Fatalf("exact pre-rank budget bound was rejected: %v", err)
	}
	if got := totalRecallBudget(policy.Config.RecallBudgets); got != MaxPolicyPreRankCandidates {
		t.Fatalf("normalized total recall budget = %d, want %d", got, MaxPolicyPreRankCandidates)
	}
	exactBound.RecallBudgets[RecallProviderFresh]++
	if _, err := NewPolicy("feed", 4, true, exactBound, now); !errors.Is(err, ErrInvalidPolicyBound) {
		t.Fatalf("over-bound recall budget error = %v, want %v", err, ErrInvalidPolicyBound)
	}

	defensive := validPolicyConfig(100)
	policy, err = NewPolicy("feed", 5, true, defensive, now)
	if err != nil {
		t.Fatalf("create defensive policy: %v", err)
	}
	defensive.RecallBudgets[RecallProviderFresh] = MaxRecallBudget
	if policy.Config.RecallBudgets[RecallProviderFresh] != 50 {
		t.Fatalf("policy recall budgets were aliased: %#v", policy.Config.RecallBudgets)
	}
}

func totalRecallBudget(budgets map[string]int) int {
	total := 0
	for _, budget := range budgets {
		total += budget
	}
	return total
}

func TestProfileEventBoundsNormalizationAndApplication(t *testing.T) {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	event, err := NewProfileEvent(ProfileEventInput{
		UserID: 7, SourceEventID: "event-1", EventType: "completion", OccurredAt: now,
		LongTermVector: []float64{3, 4}, RecentVector: []float64{1, 0},
		AuthorAffinities: map[int64]float64{9: 0.8}, NegativeTopicVector: []float64{0, 2},
		NegativeAuthorWeights: map[int64]float64{10: 0.4},
	})
	if err != nil {
		t.Fatalf("new profile event: %v", err)
	}
	if event.PayloadHash == "" || math.Abs(event.LongTermVector[0]-3) > 0.0000001 {
		t.Fatalf("event did not preserve weighted magnitude and hash: %#v", event)
	}
	profile, err := EmptyUserInterestProfile(7, now).Apply(event)
	if err != nil {
		t.Fatalf("apply profile event: %v", err)
	}
	if profile.Version != 1 || profile.AuthorAffinities[9] != 0.8 || profile.NegativeAuthorAffinities[10] != 0.4 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	if _, err := NewProfileEvent(ProfileEventInput{
		UserID: 7, SourceEventID: "large", EventType: "completion", OccurredAt: now,
		LongTermVector: make([]float64, MaxProfileVectorDimensions+1),
	}); !errors.Is(err, ErrProfileVectorTooLarge) {
		t.Fatalf("large vector error = %v, want %v", err, ErrProfileVectorTooLarge)
	}
}

func TestProfileApplyDecaysAccumulatedSignalsWithoutChangingEventIdentity(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	profile := RestoreUserInterestProfile(
		7, []float64{1, 0}, []float64{1, 0}, map[int64]float64{9: 1},
		[]float64{1, 0}, map[int64]float64{10: 1}, 1, now.Add(-24*time.Hour),
	)
	event, err := NewProfileEvent(ProfileEventInput{
		UserID: 7, SourceEventID: "fresh", EventType: "complete", OccurredAt: now,
		LongTermVector: []float64{0, 1}, RecentVector: []float64{0, 1},
		AuthorAffinities: map[int64]float64{8: 1},
		Decay:            ProfileDecay{LongTermHalfLife: 24 * time.Hour, RecentHalfLife: time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	withDifferentDecay, err := NewProfileEvent(ProfileEventInput{
		UserID: 7, SourceEventID: "fresh", EventType: "complete", OccurredAt: now,
		LongTermVector: []float64{0, 1}, RecentVector: []float64{0, 1},
		AuthorAffinities: map[int64]float64{8: 1},
		Decay:            ProfileDecay{LongTermHalfLife: 12 * time.Hour, RecentHalfLife: 12 * time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.PayloadHash != withDifferentDecay.PayloadHash {
		t.Fatalf("operational decay changed immutable source identity: %q != %q", event.PayloadHash, withDifferentDecay.PayloadHash)
	}
	updated, err := profile.ApplyWithDecay(event, event.Decay)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LongTermVector[0] < .49 || updated.LongTermVector[0] > .51 ||
		updated.RecentVector[0] >= .001 || updated.RecentVector[1] != 1 ||
		updated.AuthorAffinities[9] >= .001 || updated.NegativeAuthorAffinities[10] >= .001 {
		t.Fatalf("accumulated profile was not decayed before fresh event: %#v", updated)
	}
}

func TestProfileApplyDecaysDelayedEventToStableMaterializationTime(t *testing.T) {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	decay := ProfileDecay{LongTermHalfLife: 48 * time.Hour, RecentHalfLife: 24 * time.Hour}
	newEvent := func(id string, occurredAt time.Time, value float64) *ProfileEvent {
		event, err := NewProfileEvent(ProfileEventInput{
			UserID: 7, SourceEventID: id, EventType: "like", OccurredAt: occurredAt,
			LongTermVector: []float64{value}, RecentVector: []float64{value},
			AuthorAffinities: map[int64]float64{9: value}, Decay: decay,
		})
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	older := newEvent("older", now.Add(-24*time.Hour), 1)
	newer := newEvent("newer", now, 1)

	forward, err := EmptyUserInterestProfile(7, older.OccurredAt).ApplyWithDecay(older, decay)
	if err != nil {
		t.Fatal(err)
	}
	forward, err = forward.ApplyWithDecay(newer, decay)
	if err != nil {
		t.Fatal(err)
	}

	reverse, err := EmptyUserInterestProfile(7, newer.OccurredAt).ApplyWithDecay(newer, decay)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err = reverse.ApplyWithDecay(older, decay)
	if err != nil {
		t.Fatal(err)
	}
	if forward.UpdatedAt != now || reverse.UpdatedAt != now ||
		math.Abs(forward.LongTermVector[0]-reverse.LongTermVector[0]) > 1e-9 ||
		math.Abs(forward.RecentVector[0]-reverse.RecentVector[0]) > 1e-9 ||
		math.Abs(forward.AuthorAffinities[9]-reverse.AuthorAffinities[9]) > 1e-9 {
		t.Fatalf("out-of-order profile application diverged: forward=%#v reverse=%#v", forward, reverse)
	}
	if math.Abs(forward.RecentVector[0]-1.5) > 1e-9 {
		t.Fatalf("delayed event was not decayed exactly once: %v", forward.RecentVector[0])
	}
}

func TestRequestLogSamplingAndPayloadBounds(t *testing.T) {
	control, err := NewRequestLogControl(250_000, 30)
	if err != nil {
		t.Fatalf("create request log control: %v", err)
	}
	if ShouldSampleRequestLog(control, 4, RecommendationRequestLogScene, "same-request") != ShouldSampleRequestLog(control, 4, RecommendationRequestLogScene, "same-request") {
		t.Fatal("request-log sampling must be deterministic")
	}
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	log, err := NewRecommendationRequestLog(RequestLogInput{
		RequestID: "r1", UserID: 4, Scene: " Recommend ", PolicyVersion: 1, CreatedAt: now,
		Candidates: []LoggedCandidate{{VideoID: 8, Reasons: []string{"hot"}, ScoreComponents: map[string]float64{FeatureHotness: 0.5}}},
	})
	if err != nil {
		t.Fatalf("create compact request log: %v", err)
	}
	if log.Scene != RecommendationRequestLogScene {
		t.Fatalf("request-log scene was not normalized: %#v", log)
	}
	payload, err := log.CompactPayload()
	if err != nil || len(payload) == 0 {
		t.Fatalf("compact payload = %q, %v", payload, err)
	}
	if strings.Contains(string(payload), "recall_diagnostics") {
		t.Fatalf("legacy request log unexpectedly changed: %s", payload)
	}
	quotaLog, err := NewRecommendationRequestLog(RequestLogInput{
		RequestID: "quota-r1", UserID: 4, Scene: RecommendationRequestLogScene, PolicyVersion: 3, CreatedAt: now,
		Candidates: []LoggedCandidate{{VideoID: 8, Reasons: []string{RecallProviderFresh}}},
		RecallDiagnostics: []RecallDiagnostic{
			{Phase: " Reservation ", Provider: " Fresh ", Result: " Reserved ", Reason: " none ", Count: 12},
			{Phase: "final", Provider: "all", Result: "underfill", Reason: "insufficient_readable", Count: 2},
		},
	})
	if err != nil {
		t.Fatalf("create quota request log: %v", err)
	}
	quotaPayload, err := quotaLog.CompactPayload()
	if err != nil || !strings.Contains(string(quotaPayload), `"recall_diagnostics"`) ||
		strings.Contains(string(quotaPayload), "source_score") || strings.Contains(string(quotaPayload), "provider error") {
		t.Fatalf("quota diagnostics were not bounded: %s err=%v", quotaPayload, err)
	}
	if got := quotaLog.RecallDiagnostics[0]; got.Phase != "reservation" || got.Provider != RecallProviderFresh ||
		got.Result != "reserved" || got.Reason != "none" || got.Count != 12 {
		t.Fatalf("quota diagnostic was not normalized: %#v", got)
	}
	invalidDiagnostics := []RecallDiagnostic{
		{Phase: "request-123", Provider: RecallProviderFresh, Result: "selected", Count: 1},
		{Phase: "final", Provider: "video-99", Result: "selected", Count: 1},
		{Phase: "final", Provider: RecallProviderFresh, Result: "map[payload:true]", Count: 1},
		{Phase: "final", Provider: RecallProviderFresh, Result: "underfill", Reason: "raw provider error", Count: 1},
		{Phase: "final", Provider: RecallProviderFresh, Result: "selected", Count: MaxRequestLogDiagnosticCount + 1},
	}
	for _, diagnostic := range invalidDiagnostics {
		if _, err := NewRecommendationRequestLog(RequestLogInput{
			RequestID: "invalid-diagnostic", UserID: 4, Scene: RecommendationRequestLogScene, PolicyVersion: 3, CreatedAt: now,
			RecallDiagnostics: []RecallDiagnostic{diagnostic},
		}); !errors.Is(err, ErrInvalidRequestLog) {
			t.Fatalf("invalid diagnostic %#v error = %v, want %v", diagnostic, err, ErrInvalidRequestLog)
		}
	}
	maximumPool := make([]LoggedCandidate, MaxRequestLogCandidates)
	componentNames := []string{
		FeatureContentSimilarity,
		FeatureSessionSimilarity,
		FeatureSemanticSimilarity,
		FeatureHotness,
		FeatureFreshness,
		FeatureAuthorAffinity,
		FeatureFollowRelation,
		FeatureNegativePenalty,
		FeatureExposurePenalty,
	}
	for index := range maximumPool {
		reasons := make([]string, MaxRequestLogReasons)
		components := make(map[string]float64, MaxRequestLogScoreComponents)
		for reasonIndex := range reasons {
			reasons[reasonIndex] = "reason-" + int64String(int64(reasonIndex)) + "-" + strings.Repeat("x", 55)
		}
		for componentIndex := range componentNames {
			components[componentNames[componentIndex]] = float64(componentIndex) / 10
		}
		maximumPool[index] = LoggedCandidate{
			VideoID: int64(index + 1), Reasons: reasons, ScoreComponents: components,
		}
	}
	maximum, err := NewRecommendationRequestLog(RequestLogInput{
		RequestID: "maximum-pool", UserID: 4, Scene: RecommendationRequestLogScene, PolicyVersion: 1, CreatedAt: now,
		Candidates: maximumPool,
	})
	if err != nil {
		t.Fatalf("maximum valid request pool was rejected: %v", err)
	}
	maximumPayload, err := maximum.CompactPayload()
	if err != nil || len(maximumPayload) > MaxRequestLogPayloadBytes || len(maximum.Candidates) != MaxRequestLogCandidates {
		t.Fatalf("maximum pool was not preserved within the documented payload bound: candidates=%d bytes=%d err=%v", len(maximum.Candidates), len(maximumPayload), err)
	}
	for index := range maximumPool {
		if maximum.Candidates[index].VideoID != maximumPool[index].VideoID ||
			len(maximum.Candidates[index].Reasons) != MaxRequestLogReasons ||
			len(maximum.Candidates[index].ScoreComponents) != MaxRequestLogScoreComponents {
			t.Fatalf("maximum pool lost ordered explanation at index %d: %#v", index, maximum.Candidates[index])
		}
	}
	tooLarge := make([]LoggedCandidate, MaxRequestLogCandidates+1)
	for index := range tooLarge {
		tooLarge[index] = LoggedCandidate{VideoID: int64(index + 1)}
	}
	if _, err := NewRecommendationRequestLog(RequestLogInput{
		RequestID: "r2", UserID: 4, Scene: RecommendationRequestLogScene, PolicyVersion: 1, CreatedAt: now, Candidates: tooLarge,
	}); !errors.Is(err, ErrInvalidRequestLog) {
		t.Fatalf("large candidate list error = %v, want %v", err, ErrInvalidRequestLog)
	}
	if _, err := NewRecommendationRequestLog(RequestLogInput{
		RequestID: "r3", UserID: 4, Scene: "feed", PolicyVersion: 1, CreatedAt: now,
		Candidates: []LoggedCandidate{{VideoID: 8}},
	}); !errors.Is(err, ErrInvalidRequestLog) {
		t.Fatalf("non-recommend request log error = %v, want %v", err, ErrInvalidRequestLog)
	}
}

func validPolicyConfig(rollout int) PolicyConfiguration {
	return PolicyConfiguration{
		FeatureWeights:         map[string]float64{FeatureHotness: 0.5, FeatureFreshness: 0.2},
		RecallBudgets:          map[string]int{RecallProviderFresh: 50},
		ProviderDeadlinesMS:    map[string]int{RecallProviderFresh: 100},
		FreshnessHalfLifeHours: 72, ExposureWindowHours: 168,
		Diversity:         DiversityRules{MaxPerAuthor: 2, MinAuthorGap: 1},
		RolloutPercentage: rollout, SnapshotTTLSeconds: 900, SamplingRatePPM: 10_000, RetentionDays: 30,
	}
}
