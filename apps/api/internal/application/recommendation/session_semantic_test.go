package applicationrecommendation

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

type sessionSemanticFactSourceStub struct {
	facts []SessionSemanticFact
	err   error
	ids   []int64
}

func (s *sessionSemanticFactSourceStub) LoadSessionSemanticFacts(
	_ context.Context,
	_ int64,
	ids []int64,
	_, _ time.Time,
) ([]SessionSemanticFact, error) {
	s.ids = append([]int64(nil), ids...)
	return append([]SessionSemanticFact(nil), s.facts...), s.err
}

type sessionSemanticVectorSourceStub struct {
	vectors map[int64]*domainembedding.MultimodalVectorFact
	err     error
	ids     []int64
}

func (s *sessionSemanticVectorSourceStub) LoadSessionSemanticVectors(
	_ context.Context,
	ids []int64,
	_ domainembedding.MultimodalContractIdentity,
) (map[int64]*domainembedding.MultimodalVectorFact, error) {
	s.ids = append([]int64(nil), ids...)
	return s.vectors, s.err
}

func TestSessionSemanticBuilderGoldenCases(t *testing.T) {
	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "revision-1")
	axisX := sessionSemanticUnitVector(contract.Dimension, 0, 1)
	axisY := sessionSemanticUnitVector(contract.Dimension, 1, 1)
	negativeX := sessionSemanticUnitVector(contract.Dimension, 0, -1)
	tests := []struct {
		name          string
		current       int64
		recent        []int64
		facts         []SessionSemanticFact
		vectors       map[int64]*domainembedding.MultimodalVectorFact
		minSignals    int
		wantResult    domainrecommendation.SessionSemanticResult
		wantAvailable bool
		assert        func(*testing.T, *SessionSemanticInterest)
	}{
		{
			name: "completion and favorite reinforce current direction", current: 1, recent: []int64{1}, minSignals: 2,
			facts: []SessionSemanticFact{sessionSemanticFact(now, 1,
				domainrecommendation.SessionSemanticSignalComplete,
				domainrecommendation.SessionSemanticSignalFavorite,
			)},
			vectors:    map[int64]*domainembedding.MultimodalVectorFact{1: sessionSemanticFactVector(1, contract, axisX)},
			wantResult: domainrecommendation.SessionSemanticResultSuccess, wantAvailable: true,
			assert: func(t *testing.T, interest *SessionSemanticInterest) {
				if interest.Band != domainrecommendation.SessionSemanticConfidenceHigh || interest.OutputLimit != 20 ||
					math.Abs(interest.Vector[0]-1) > 1e-9 || interest.Evidence.PositiveCount != 3 {
					t.Fatalf("interest=%#v", interest)
				}
			},
		},
		{
			name: "negative feedback bends but cannot replace positive direction", current: 1, recent: []int64{2}, minSignals: 1,
			facts: []SessionSemanticFact{
				sessionSemanticFact(now, 1, domainrecommendation.SessionSemanticSignalComplete),
				sessionSemanticFact(now, 2, domainrecommendation.SessionSemanticSignalNotInterested),
			},
			vectors: map[int64]*domainembedding.MultimodalVectorFact{
				1: sessionSemanticFactVector(1, contract, axisX),
				2: sessionSemanticFactVector(2, contract, axisY),
			},
			wantResult: domainrecommendation.SessionSemanticResultSuccess, wantAvailable: true,
			assert: func(t *testing.T, interest *SessionSemanticInterest) {
				if interest.Vector[0] <= 0 || interest.Vector[1] >= 0 || interest.Evidence.NegativeCount != 1 {
					t.Fatalf("interest=%#v", interest)
				}
			},
		},
		{
			name: "not interested overrides implicit current positive", current: 1, minSignals: 1,
			facts: []SessionSemanticFact{sessionSemanticFact(now, 1,
				domainrecommendation.SessionSemanticSignalComplete,
				domainrecommendation.SessionSemanticSignalNotInterested,
			)},
			vectors:    map[int64]*domainembedding.MultimodalVectorFact{1: sessionSemanticFactVector(1, contract, axisX)},
			wantResult: domainrecommendation.SessionSemanticResultInsufficientEvidence,
		},
		{
			name: "already seen excludes without negative direction", current: 1, minSignals: 1,
			facts: []SessionSemanticFact{sessionSemanticFact(now, 1,
				domainrecommendation.SessionSemanticSignalComplete,
				domainrecommendation.SessionSemanticSignalAlreadySeen,
			)},
			vectors:    map[int64]*domainembedding.MultimodalVectorFact{1: sessionSemanticFactVector(1, contract, axisX)},
			wantResult: domainrecommendation.SessionSemanticResultSuccess, wantAvailable: true,
			assert: func(t *testing.T, interest *SessionSemanticInterest) {
				if interest.Evidence.NegativeCount != 0 || !reflect.DeepEqual(interest.Exclusions, []int64{1}) {
					t.Fatalf("interest=%#v", interest)
				}
			},
		},
		{
			name: "missing vector is healthy unavailable", current: 1, minSignals: 1,
			facts:      []SessionSemanticFact{sessionSemanticFact(now, 1, domainrecommendation.SessionSemanticSignalComplete)},
			vectors:    map[int64]*domainembedding.MultimodalVectorFact{},
			wantResult: domainrecommendation.SessionSemanticResultNoCompatibleVectors,
		},
		{
			name: "contract mismatch is skipped", current: 1, minSignals: 1,
			facts: []SessionSemanticFact{sessionSemanticFact(now, 1, domainrecommendation.SessionSemanticSignalComplete)},
			vectors: map[int64]*domainembedding.MultimodalVectorFact{
				1: sessionSemanticFactVector(1, sessionSemanticTestContract(t, "revision-2"), axisX),
			},
			wantResult: domainrecommendation.SessionSemanticResultNoCompatibleVectors,
		},
		{
			name: "opposing positive directions are rejected", recent: []int64{1, 2}, minSignals: 2,
			facts: []SessionSemanticFact{
				sessionSemanticFact(now, 1, domainrecommendation.SessionSemanticSignalComplete),
				sessionSemanticFact(now, 2, domainrecommendation.SessionSemanticSignalComplete),
			},
			vectors: map[int64]*domainembedding.MultimodalVectorFact{
				1: sessionSemanticFactVector(1, contract, axisX),
				2: sessionSemanticFactVector(2, contract, negativeX),
			},
			wantResult: domainrecommendation.SessionSemanticResultInvalidVector,
		},
		{
			name: "arbitrary untrusted context does not contribute", current: 1, minSignals: 1,
			facts:      []SessionSemanticFact{sessionSemanticFact(now, 99, domainrecommendation.SessionSemanticSignalComplete)},
			vectors:    map[int64]*domainembedding.MultimodalVectorFact{99: sessionSemanticFactVector(99, contract, axisX)},
			wantResult: domainrecommendation.SessionSemanticResultInsufficientEvidence,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := &sessionSemanticFactSourceStub{facts: test.facts}
			vectors := &sessionSemanticVectorSourceStub{vectors: test.vectors}
			builder, err := NewSessionSemanticBuilder(facts, vectors)
			if err != nil {
				t.Fatal(err)
			}
			contextValue := sessionSemanticContext(t, test.current, test.recent)
			interest, err := builder.Build(context.Background(), SessionSemanticBuildRequest{
				UserID: 7, Context: contextValue,
				Policy:   sessionSemanticTestPolicy(contract.Key(), test.minSignals),
				Contract: contract, Budget: 20, Now: now,
			})
			if err != nil {
				t.Fatal(err)
			}
			if interest.Available() != test.wantAvailable || interest.Evidence == nil || interest.Evidence.Result != test.wantResult {
				t.Fatalf("interest=%#v want_result=%s", interest, test.wantResult)
			}
			if test.assert != nil {
				test.assert(t, interest)
			}
		})
	}
}

func TestSessionSemanticBuilderIsDeterministicAndDefensive(t *testing.T) {
	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "revision-1")
	facts := []SessionSemanticFact{
		sessionSemanticFact(now, 2, domainrecommendation.SessionSemanticSignalFavorite, domainrecommendation.SessionSemanticSignalSustained),
		sessionSemanticFact(now, 1, domainrecommendation.SessionSemanticSignalComplete, domainrecommendation.SessionSemanticSignalComplete),
	}
	vectors := map[int64]*domainembedding.MultimodalVectorFact{
		1: sessionSemanticFactVector(1, contract, sessionSemanticUnitVector(contract.Dimension, 0, 1)),
		2: sessionSemanticFactVector(2, contract, sessionSemanticUnitVector(contract.Dimension, 1, 1)),
	}
	build := func(values []SessionSemanticFact) *SessionSemanticInterest {
		builder, err := NewSessionSemanticBuilder(
			&sessionSemanticFactSourceStub{facts: values},
			&sessionSemanticVectorSourceStub{vectors: vectors},
		)
		if err != nil {
			t.Fatal(err)
		}
		interest, err := builder.Build(context.Background(), SessionSemanticBuildRequest{
			UserID: 7, Context: sessionSemanticContext(t, 1, []int64{2, 1}),
			Policy: sessionSemanticTestPolicy(contract.Key(), 2), Contract: contract, Budget: 20, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return interest
	}
	forward := build(facts)
	reverse := build([]SessionSemanticFact{facts[1], facts[0]})
	if !reflect.DeepEqual(forward.Vector, reverse.Vector) || forward.Confidence != reverse.Confidence ||
		forward.Evidence.InputDigest != reverse.Evidence.InputDigest || !reflect.DeepEqual(forward.Exclusions, reverse.Exclusions) {
		t.Fatalf("forward=%#v reverse=%#v", forward, reverse)
	}
	clone := forward.Clone()
	clone.Vector[0] = 0
	clone.Exclusions[0] = 99
	clone.Evidence.Result = domainrecommendation.SessionSemanticResultUnavailable
	if forward.Vector[0] == 0 || forward.Exclusions[0] == 99 || forward.Evidence.Result != domainrecommendation.SessionSemanticResultSuccess {
		t.Fatal("interest clone aliased original")
	}
}

func TestSessionSemanticBuilderPropagatesInfrastructureFailuresAndBoundsSeeds(t *testing.T) {
	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "revision-1")
	factFailure := errors.New("fact failure")
	facts := &sessionSemanticFactSourceStub{err: factFailure}
	builder, _ := NewSessionSemanticBuilder(facts, &sessionSemanticVectorSourceStub{})
	recent := make([]int64, domainrecommendation.MaxRecentVideoIDs)
	for index := range recent {
		recent[index] = int64(index + 2)
	}
	_, err := builder.Build(context.Background(), SessionSemanticBuildRequest{
		UserID: 7, Context: sessionSemanticContext(t, 1, recent),
		Policy: sessionSemanticTestPolicy(contract.Key(), 1), Contract: contract, Budget: 20, Now: now,
	})
	if !errors.Is(err, factFailure) || len(facts.ids) != domainrecommendation.MaxSessionSemanticSeeds {
		t.Fatalf("ids=%v err=%v", facts.ids, err)
	}
	vectorFailure := errors.New("vector failure")
	vectors := &sessionSemanticVectorSourceStub{err: vectorFailure}
	builder, _ = NewSessionSemanticBuilder(
		&sessionSemanticFactSourceStub{facts: []SessionSemanticFact{sessionSemanticFact(now, 1, domainrecommendation.SessionSemanticSignalComplete)}},
		vectors,
	)
	_, err = builder.Build(context.Background(), SessionSemanticBuildRequest{
		UserID: 7, Context: sessionSemanticContext(t, 1, nil),
		Policy: sessionSemanticTestPolicy(contract.Key(), 1), Contract: contract, Budget: 20, Now: now,
	})
	if !errors.Is(err, vectorFailure) || !reflect.DeepEqual(vectors.ids, []int64{1}) {
		t.Fatalf("ids=%v err=%v", vectors.ids, err)
	}
}

func TestSessionSemanticBuilderEnforcesRuntimePolicyCeilings(t *testing.T) {
	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	contract := sessionSemanticTestContract(t, "revision-1")
	builder, err := NewSessionSemanticBuilder(
		&sessionSemanticFactSourceStub{}, &sessionSemanticVectorSourceStub{},
		WithSessionSemanticRuntimeLimits(2, time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	policy := sessionSemanticTestPolicy(contract.Key(), 1)
	policy.MaxSeeds = 3
	if _, err := builder.Build(context.Background(), SessionSemanticBuildRequest{
		UserID: 7, Context: sessionSemanticContext(t, 1, nil), Policy: policy,
		Contract: contract, Budget: 20, Now: now,
	}); !errors.Is(err, ErrSessionSemanticUnavailable) {
		t.Fatalf("max-seed error=%v", err)
	}
	policy.MaxSeeds = 2
	policy.LookbackSeconds = 2 * 60 * 60
	if _, err := builder.Build(context.Background(), SessionSemanticBuildRequest{
		UserID: 7, Context: sessionSemanticContext(t, 1, nil), Policy: policy,
		Contract: contract, Budget: 20, Now: now,
	}); !errors.Is(err, ErrSessionSemanticUnavailable) {
		t.Fatalf("lookback error=%v", err)
	}
	if _, err := NewSessionSemanticBuilder(
		&sessionSemanticFactSourceStub{}, &sessionSemanticVectorSourceStub{},
		WithSessionSemanticRuntimeLimits(0, time.Hour),
	); !errors.Is(err, ErrSessionSemanticUnavailable) {
		t.Fatalf("invalid runtime option error=%v", err)
	}
}

func sessionSemanticTestContract(t testing.TB, revision string) domainembedding.MultimodalContractIdentity {
	t.Helper()
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", revision, domainembedding.MinMultimodalDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func sessionSemanticUnitVector(dimension, axis int, value float64) []float64 {
	vector := make([]float64, dimension)
	vector[axis] = value
	return vector
}

func sessionSemanticFactVector(
	videoID int64,
	contract domainembedding.MultimodalContractIdentity,
	vector []float64,
) *domainembedding.MultimodalVectorFact {
	return &domainembedding.MultimodalVectorFact{
		VideoID:  videoID,
		Identity: domainembedding.MultimodalVectorIdentity{Contract: contract},
		Values:   append([]float64(nil), vector...),
	}
}

func sessionSemanticFact(
	now time.Time,
	videoID int64,
	kinds ...domainrecommendation.SessionSemanticSignalKind,
) SessionSemanticFact {
	signals := make([]domainrecommendation.SessionSemanticSignal, 0, len(kinds))
	for index, kind := range kinds {
		signals = append(signals, domainrecommendation.SessionSemanticSignal{
			VideoID: videoID, Kind: kind, OccurredAt: now.Add(-time.Duration(index) * time.Second),
		})
	}
	return SessionSemanticFact{VideoID: videoID, EncounteredAt: now, Signals: signals}
}

func sessionSemanticContext(
	t testing.TB,
	current int64,
	recent []int64,
) *domainrecommendation.RecommendationContext {
	t.Helper()
	value, err := domainrecommendation.NewRecommendationContext(domainrecommendation.RecommendationContextInput{
		RequestID: "request", SessionID: "session", CurrentVideoID: current,
		RecentVideoIDs: recent, NetworkClass: domainrecommendation.NetworkClassUnknown,
		ViewportClass: domainrecommendation.ViewportClassUnknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func sessionSemanticTestPolicy(
	contractKey string,
	minSignals int,
) *domainrecommendation.SessionSemanticPolicyConfiguration {
	return &domainrecommendation.SessionSemanticPolicyConfiguration{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1,
		ContractKey:    contractKey, LookbackSeconds: 2 * 60 * 60,
		MaxSeeds:           domainrecommendation.MaxSessionSemanticSeeds,
		MinPositiveSignals: minSignals, MinConfidence: 0.1,
	}
}
