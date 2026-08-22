package interfaceshttprouter

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

type routerSessionSemanticShadowProviderStub struct{}

func (routerSessionSemanticShadowProviderStub) RecallWithSessionSemanticEvidence(
	context.Context,
	applicationrecommendation.RecallRequest,
) ([]*domainrecommendation.Candidate, *domainrecommendation.SessionSemanticEvidence, error) {
	return []*domainrecommendation.Candidate{}, nil, nil
}

type routerSessionSemanticShadowSimulatorStub struct{}

func (routerSessionSemanticShadowSimulatorStub) SimulateSessionSemanticShadow(
	context.Context,
	applicationrecommendation.SessionSemanticShadowSimulationRequest,
) (*applicationrecommendation.SessionSemanticShadowSimulationResult, error) {
	return &applicationrecommendation.SessionSemanticShadowSimulationResult{}, nil
}

func TestNewSessionSemanticShadowEvaluatorScopesComposition(t *testing.T) {
	disabled, err := newSessionSemanticShadowEvaluator(infraconfig.MultimodalConfig{}, nil, nil)
	if err != nil || disabled != nil {
		t.Fatalf("disabled evaluator=%v error=%v", disabled, err)
	}
	cfg := routerSessionSemanticShadowConfig(t)
	for _, test := range []struct {
		name      string
		provider  applicationrecommendation.SessionSemanticEvidenceProvider
		simulator applicationrecommendation.SessionSemanticShadowSimulator
	}{
		{name: "missing provider", simulator: routerSessionSemanticShadowSimulatorStub{}},
		{name: "missing simulator", provider: routerSessionSemanticShadowProviderStub{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluator, createErr := newSessionSemanticShadowEvaluator(cfg, test.provider, test.simulator)
			if evaluator != nil || !errors.Is(createErr, infraconfig.ErrMissingMultimodalDependency) {
				t.Fatalf("evaluator=%v error=%v", evaluator, createErr)
			}
		})
	}
	evaluator, err := newSessionSemanticShadowEvaluator(
		cfg, routerSessionSemanticShadowProviderStub{}, routerSessionSemanticShadowSimulatorStub{},
	)
	if err != nil || evaluator == nil {
		t.Fatalf("evaluator=%v error=%v", evaluator, err)
	}
	if evaluator.ShutdownTimeout() != time.Second {
		t.Fatalf("shutdown timeout=%v", evaluator.ShutdownTimeout())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := evaluator.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func routerSessionSemanticShadowConfig(t testing.TB) infraconfig.MultimodalConfig {
	t.Helper()
	contract := routerMultimodalContract(t)
	return infraconfig.MultimodalConfig{
		Enabled: true, SessionRecommendationEnabled: true,
		Contract: routerMultimodalContractConfig(contract),
		Session:  infraconfig.MultimodalSessionConfig{MaxSeeds: 21, MaxLookback: "24h"},
		SessionShadow: infraconfig.MultimodalSessionShadowConfig{
			Enabled: true, SamplePPM: 10_000, Budget: 25, Deadline: "250ms",
			MaxInFlight: 2, ComparisonLimit: 100, SimulatedPoolLimit: 100,
			SimulatedTopK: 20, SemanticReservation: 10, SemanticWeight: 0.25,
			ShutdownTimeout: "1s",
		},
	}
}
