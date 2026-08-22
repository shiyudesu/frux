package applicationrecommendation

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

type sessionSemanticShadowProviderStub struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	result  sessionSemanticShadowProviderResult
	panic   bool
}

func (s *sessionSemanticShadowProviderStub) RecallWithSessionSemanticEvidence(
	context.Context,
	RecallRequest,
) ([]*domainrecommendation.Candidate, *domainrecommendation.SessionSemanticEvidence, error) {
	s.calls.Add(1)
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.release != nil {
		<-s.release
	}
	if s.panic {
		panic("shadow provider panic")
	}
	return cloneCandidates(s.result.candidates), s.result.evidence.Clone(), s.result.err
}

type sessionSemanticShadowSimulatorStub struct {
	calls  atomic.Int32
	result *SessionSemanticShadowSimulationResult
	err    error
	panic  bool
}

func (s *sessionSemanticShadowSimulatorStub) SimulateSessionSemanticShadow(
	context.Context,
	SessionSemanticShadowSimulationRequest,
) (*SessionSemanticShadowSimulationResult, error) {
	s.calls.Add(1)
	if s.panic {
		panic("shadow simulator panic")
	}
	return s.result.clone(), s.err
}

type sessionSemanticShadowObserverStub struct {
	mu         sync.Mutex
	selections []string
	admissions []string
	inFlight   int
	terminals  []SessionSemanticShadowObservation
	notify     chan struct{}
}

func (s *sessionSemanticShadowObserverStub) ObserveSelection(value string) {
	s.mu.Lock()
	s.selections = append(s.selections, value)
	s.mu.Unlock()
}

func (s *sessionSemanticShadowObserverStub) ObserveAdmission(value string) {
	s.mu.Lock()
	s.admissions = append(s.admissions, value)
	s.mu.Unlock()
}

func (s *sessionSemanticShadowObserverStub) ObserveInFlight(delta int) {
	s.mu.Lock()
	s.inFlight += delta
	s.mu.Unlock()
}

func (s *sessionSemanticShadowObserverStub) ObserveTerminal(value SessionSemanticShadowObservation) {
	s.mu.Lock()
	s.terminals = append(s.terminals, value)
	s.mu.Unlock()
	if s.notify != nil {
		select {
		case s.notify <- struct{}{}:
		default:
		}
	}
}

func (s *sessionSemanticShadowObserverStub) terminal(t testing.TB, timeout time.Duration) SessionSemanticShadowObservation {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		if len(s.terminals) > 0 {
			value := s.terminals[len(s.terminals)-1]
			s.mu.Unlock()
			return value
		}
		s.mu.Unlock()
		select {
		case <-s.notify:
		case <-deadline.C:
			t.Fatal("timed out waiting for shadow terminal observation")
		}
	}
}

func TestSessionSemanticShadowEvaluatorCompletesSelectedRequest(t *testing.T) {
	config := sessionSemanticShadowTestConfig(t)
	evidence := sessionSemanticShadowEvidence(t, config)
	provider := &sessionSemanticShadowProviderStub{result: sessionSemanticShadowProviderResult{
		candidates: []*domainrecommendation.Candidate{
			annotateCandidate(shadowCandidate(9, 0, time.Unix(100, 0)), domainrecommendation.RecallProviderSemanticSession, 0.8),
		},
		evidence: evidence,
	}}
	comparison := SessionSemanticShadowComparison{
		ActiveCount: 1, SemanticCount: 1, UniqueSemanticCount: 1,
		MixedCount: 2, SemanticPoolSurvival: 1, SimulatedTopKCount: 2,
		SemanticRankSurvival: 1, UniqueRankSurvival: 1,
		UniqueContributionRatio: 1, PoolSurvivalRatio: 1,
		RankSurvivalRatio: 1, UniqueRankSurvivalRatio: 1,
	}
	simulator := &sessionSemanticShadowSimulatorStub{result: &SessionSemanticShadowSimulationResult{Comparison: comparison}}
	observer := &sessionSemanticShadowObserverStub{notify: make(chan struct{}, 4)}
	evaluator, err := NewSessionSemanticShadowEvaluator(config, provider, simulator, observer)
	if err != nil {
		t.Fatal(err)
	}
	if !evaluator.TryEvaluate(sessionSemanticShadowRequest(t)) {
		t.Fatal("selected request was not admitted")
	}
	terminal := observer.terminal(t, time.Second)
	if terminal.Result != SessionSemanticShadowResultSuccess || terminal.ConfidenceBand != evidence.ConfidenceBand ||
		terminal.Comparison.UniqueSemanticCount != 1 || provider.calls.Load() != 1 || simulator.calls.Load() != 1 {
		t.Fatalf("terminal=%#v provider=%d simulator=%d", terminal, provider.calls.Load(), simulator.calls.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := evaluator.Close(ctx); err != nil {
		t.Fatal(err)
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.inFlight != 0 || len(observer.terminals) != 1 {
		t.Fatalf("in_flight=%d terminals=%d", observer.inFlight, len(observer.terminals))
	}
}

func TestSessionSemanticShadowEvaluatorKeepsPermitForIgnoringCall(t *testing.T) {
	config := sessionSemanticShadowTestConfig(t)
	config.MaxInFlight = 1
	config.Deadline = 25 * time.Millisecond
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	provider := &sessionSemanticShadowProviderStub{started: started, release: release}
	observer := &sessionSemanticShadowObserverStub{notify: make(chan struct{}, 8)}
	evaluator, err := NewSessionSemanticShadowEvaluator(
		config, provider, &sessionSemanticShadowSimulatorStub{}, observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := sessionSemanticShadowRequest(t)
	if !evaluator.TryEvaluate(request) {
		t.Fatal("first request was not admitted")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	terminal := observer.terminal(t, time.Second)
	if terminal.Result != SessionSemanticShadowResultTimeout {
		t.Fatalf("terminal=%#v", terminal)
	}
	request.RequestID = "second-request"
	if evaluator.TryEvaluate(request) {
		t.Fatal("second request entered full no-queue admission")
	}
	observer.mu.Lock()
	last := observer.terminals[len(observer.terminals)-1]
	inFlight := observer.inFlight
	observer.mu.Unlock()
	if last.Result != SessionSemanticShadowResultCapacity || inFlight != 1 || provider.calls.Load() != 1 {
		t.Fatalf("last=%#v in_flight=%d calls=%d", last, inFlight, provider.calls.Load())
	}
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shortCancel()
	if err := evaluator.Close(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error=%v", err)
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := evaluator.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSemanticShadowEvaluatorContainsPanics(t *testing.T) {
	config := sessionSemanticShadowTestConfig(t)
	provider := &sessionSemanticShadowProviderStub{panic: true}
	observer := &sessionSemanticShadowObserverStub{notify: make(chan struct{}, 4)}
	evaluator, err := NewSessionSemanticShadowEvaluator(
		config, provider, &sessionSemanticShadowSimulatorStub{}, observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !evaluator.TryEvaluate(sessionSemanticShadowRequest(t)) {
		t.Fatal("request was not admitted")
	}
	if terminal := observer.terminal(t, time.Second); terminal.Result != SessionSemanticShadowResultPanic {
		t.Fatalf("terminal=%#v", terminal)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := evaluator.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSemanticShadowEvaluatorClosedEmptyErrorAndSimulationPanic(t *testing.T) {
	for _, test := range []struct {
		name      string
		provider  sessionSemanticShadowProviderResult
		simulator *sessionSemanticShadowSimulatorStub
		want      SessionSemanticShadowTerminalResult
	}{
		{name: "empty", provider: sessionSemanticShadowProviderResult{}, simulator: &sessionSemanticShadowSimulatorStub{}, want: SessionSemanticShadowResultEmpty},
		{name: "provider error", provider: sessionSemanticShadowProviderResult{err: errors.New("provider unavailable")}, simulator: &sessionSemanticShadowSimulatorStub{}, want: SessionSemanticShadowResultError},
		{name: "simulation panic", provider: sessionSemanticShadowProviderResult{
			candidates: []*domainrecommendation.Candidate{
				annotateCandidate(shadowCandidate(9, 0, time.Unix(100, 0)), domainrecommendation.RecallProviderSemanticSession, 0.8),
			},
		}, simulator: &sessionSemanticShadowSimulatorStub{panic: true}, want: SessionSemanticShadowResultPanic},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := sessionSemanticShadowTestConfig(t)
			if len(test.provider.candidates) > 0 {
				test.provider.evidence = sessionSemanticShadowEvidence(t, config)
			}
			observer := &sessionSemanticShadowObserverStub{notify: make(chan struct{}, 4)}
			evaluator, err := NewSessionSemanticShadowEvaluator(
				config, &sessionSemanticShadowProviderStub{result: test.provider}, test.simulator, observer,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !evaluator.TryEvaluate(sessionSemanticShadowRequest(t)) {
				t.Fatal("request was not admitted")
			}
			if terminal := observer.terminal(t, time.Second); terminal.Result != test.want {
				t.Fatalf("terminal=%#v", terminal)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := evaluator.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}

	config := sessionSemanticShadowTestConfig(t)
	observer := &sessionSemanticShadowObserverStub{notify: make(chan struct{}, 4)}
	evaluator, err := NewSessionSemanticShadowEvaluator(
		config, &sessionSemanticShadowProviderStub{}, &sessionSemanticShadowSimulatorStub{}, observer,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := evaluator.Close(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	if evaluator.TryEvaluate(sessionSemanticShadowRequest(t)) {
		t.Fatal("closed evaluator admitted work")
	}
	if terminal := observer.terminal(t, time.Second); terminal.Result != SessionSemanticShadowResultClosed {
		t.Fatalf("terminal=%#v", terminal)
	}
}

func TestSessionSemanticShadowHookDoesNotChangeRecommendationResultOrCursorPages(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	pool := []*domainrecommendation.Candidate{
		shadowCandidate(1, 10, now.Add(-time.Minute)),
		shadowCandidate(2, 20, now.Add(-2*time.Minute)),
		shadowCandidate(3, 30, now.Add(-3*time.Minute)),
	}
	newRepo := func() *rankerTestRepo {
		return &rankerTestRepo{
			pool: cloneCandidates(pool), vectors: map[int64][]float64{
				1: {1, 0}, 2: {0.9, 0.1}, 3: {0.8, 0.2}, 4: {0.95, 0.05},
			},
			features: &domainrecommendation.RankingFeatures{
				FollowedAuthors: map[int64]bool{}, RecentExposures: map[int64]*domainrecommendation.Exposure{},
				NegativeVideos: map[int64]bool{}, NegativeAuthors: map[int64]bool{},
				SuppressedVideos: map[int64]bool{}, SuppressedAuthors: map[int64]bool{},
			},
		}
	}
	baseline := New(newRepo(), WithNow(func() time.Time { return now }))
	shadowed := New(newRepo(), WithNow(func() time.Time { return now }))
	config := sessionSemanticShadowTestConfig(t)
	config.SimulatedTopK = 3
	provider := &sessionSemanticShadowProviderStub{result: sessionSemanticShadowProviderResult{
		candidates: []*domainrecommendation.Candidate{
			annotateCandidate(shadowCandidate(4, 40, now), domainrecommendation.RecallProviderSemanticSession, 0.9),
		},
		evidence: sessionSemanticShadowEvidence(t, config),
	}}
	observer := &sessionSemanticShadowObserverStub{notify: make(chan struct{}, 8)}
	evaluator, err := NewSessionSemanticShadowEvaluator(config, provider, shadowed, observer)
	if err != nil {
		t.Fatal(err)
	}
	shadowed.SetSessionSemanticShadowEvaluator(evaluator)
	contextValue := sessionSemanticContext(t, 1, []int64{2})
	contextValue.RequestID = "invariance-request"
	input := CandidateRequest{
		UserID: 42, Scene: "recommend", RequestID: "invariance-request", Limit: 2, Context: contextValue,
	}
	baselineResult, err := baseline.Recommend(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	shadowResult, err := shadowed.Recommend(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baselineResult, shadowResult) {
		t.Fatalf("baseline=%#v shadow=%#v", baselineResult, shadowResult)
	}
	if terminal := observer.terminal(t, time.Second); terminal.Result != SessionSemanticShadowResultSuccess {
		t.Fatalf("terminal=%#v", terminal)
	}
	if shadowResult.NextCursor == "" {
		t.Fatal("expected a degraded score cursor")
	}
	secondInput := input
	secondInput.Cursor = shadowResult.NextCursor
	if _, err := shadowed.Recommend(context.Background(), secondInput); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	observer.mu.Lock()
	selections := len(observer.selections)
	observer.mu.Unlock()
	if selections != 1 {
		t.Fatalf("cursor page launched another Shadow selection: %d", selections)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := evaluator.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSemanticShadowDoesNotRerunForSnapshotRetryOrPage(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	visible := &visibilityCatalog{visible: map[int64]*domainrecommendation.Candidate{}}
	for id := int64(1); id <= 4; id++ {
		visible.visible[id] = rankerCandidate(id, id, int(id), now, domainrecommendation.RecallProviderHot)
	}
	service, _ := snapshotService(
		t, &now, &memorySnapshotStore{}, visible,
		&mutablePolicySelector{policy: snapshotPolicy(t, 1)},
	)
	config := sessionSemanticShadowTestConfig(t)
	provider := &sessionSemanticShadowProviderStub{result: sessionSemanticShadowProviderResult{
		candidates: []*domainrecommendation.Candidate{
			annotateCandidate(shadowCandidate(4, 4, now), domainrecommendation.RecallProviderSemanticSession, 0.8),
		},
		evidence: sessionSemanticShadowEvidence(t, config),
	}}
	observer := &sessionSemanticShadowObserverStub{notify: make(chan struct{}, 8)}
	evaluator, err := NewSessionSemanticShadowEvaluator(config, provider, service, observer)
	if err != nil {
		t.Fatal(err)
	}
	service.SetSessionSemanticShadowEvaluator(evaluator)
	input := CandidateRequest{UserID: 7, Scene: "recommend", RequestID: "snapshot-shadow", Limit: 2}
	first, err := service.Recommend(context.Background(), input)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first=%#v error=%v", first, err)
	}
	if terminal := observer.terminal(t, time.Second); terminal.Result != SessionSemanticShadowResultSuccess {
		t.Fatalf("terminal=%#v", terminal)
	}
	if _, err := service.Recommend(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	page := input
	page.Cursor = first.NextCursor
	if _, err := service.Recommend(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	observer.mu.Lock()
	selections := len(observer.selections)
	observer.mu.Unlock()
	if selections != 1 || provider.calls.Load() != 1 {
		t.Fatalf("selections=%d calls=%d", selections, provider.calls.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := evaluator.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func sessionSemanticShadowRequest(t testing.TB) SessionSemanticShadowRequest {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	active := annotateCandidate(shadowCandidate(1, 10, now), domainrecommendation.RecallProviderFresh, 1)
	return SessionSemanticShadowRequest{
		UserID: 42, Scene: "recommend", RequestID: "shadow-request", Context: sessionSemanticContext(t, 1, nil),
		ActivePolicy: defaultRecommendationPolicy(), ActiveCandidates: []*domainrecommendation.Candidate{active},
		BaselineState: "providers", Now: now,
	}
}

func sessionSemanticShadowEvidence(
	t testing.TB,
	config SessionSemanticShadowRuntimeConfig,
) *domainrecommendation.SessionSemanticEvidence {
	t.Helper()
	evidence, err := domainrecommendation.NewSessionSemanticEvidence(domainrecommendation.SessionSemanticEvidence{
		BuilderVersion: domainrecommendation.SessionSemanticBuilderV1,
		ContractKey:    config.Contract.Key(), Result: domainrecommendation.SessionSemanticResultSuccess,
		Confidence: 0.75, ConfidenceBand: domainrecommendation.SessionSemanticConfidenceMedium,
		EligibleCount: 1, PositiveCount: 2, CompatibleCount: 1, InputDigest: config.Contract.Key(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
