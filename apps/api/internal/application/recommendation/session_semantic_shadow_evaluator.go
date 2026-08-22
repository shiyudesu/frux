package applicationrecommendation

import (
	"context"
	"errors"
	"sync"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

var errSessionSemanticShadowProviderPanic = errors.New("session semantic shadow provider panic")

type sessionSemanticShadowProviderResult struct {
	candidates []*domainrecommendation.Candidate
	evidence   *domainrecommendation.SessionSemanticEvidence
	err        error
}

type SessionSemanticShadowEvaluator struct {
	config    SessionSemanticShadowRuntimeConfig
	provider  SessionSemanticEvidenceProvider
	simulator SessionSemanticShadowSimulator
	observer  SessionSemanticShadowObserver

	rootCtx context.Context
	cancel  context.CancelFunc
	permits chan struct{}

	mu       sync.Mutex
	closed   bool
	workWG   sync.WaitGroup
	actualWG sync.WaitGroup
}

func NewSessionSemanticShadowEvaluator(
	config SessionSemanticShadowRuntimeConfig,
	provider SessionSemanticEvidenceProvider,
	simulator SessionSemanticShadowSimulator,
	observer SessionSemanticShadowObserver,
) (*SessionSemanticShadowEvaluator, error) {
	if !config.Enabled || config.Validate() != nil || provider == nil || simulator == nil {
		return nil, ErrSessionSemanticShadowConfiguration
	}
	if observer == nil {
		observer = noopSessionSemanticShadowObserver{}
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	return &SessionSemanticShadowEvaluator{
		config: config, provider: provider, simulator: simulator, observer: observer,
		rootCtx: rootCtx, cancel: cancel, permits: make(chan struct{}, config.MaxInFlight),
	}, nil
}

func (e *SessionSemanticShadowEvaluator) ShutdownTimeout() time.Duration {
	if e == nil {
		return 0
	}
	return e.config.ShutdownTimeout
}

// TryEvaluate returns true only when one selected request was admitted. Every
// path is diagnostic-only and intentionally has no error return into delivery.
func (e *SessionSemanticShadowEvaluator) TryEvaluate(request SessionSemanticShadowRequest) bool {
	if e == nil || !e.config.Enabled {
		return false
	}
	if !ShouldSampleSessionSemanticShadow(
		e.config.SamplePPM, request.UserID, request.Scene, request.RequestID, true,
	) {
		e.observer.ObserveSelection("not_selected")
		return false
	}
	e.observer.ObserveSelection("selected")
	request = request.clone(e.config.ComparisonLimit)
	if !request.valid() {
		e.observer.ObserveAdmission("invalid")
		e.observeImmediate(request, SessionSemanticShadowResultError)
		return false
	}
	shadowPolicy, err := BuildSessionSemanticShadowPolicy(request.ActivePolicy, e.config, request.Now)
	if err != nil {
		e.observer.ObserveAdmission("invalid")
		e.observeImmediate(request, SessionSemanticShadowResultError)
		return false
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		e.observer.ObserveAdmission("closed")
		e.observeImmediate(request, SessionSemanticShadowResultClosed)
		return false
	}
	select {
	case e.permits <- struct{}{}:
		e.workWG.Add(1)
		e.actualWG.Add(1)
	default:
		e.mu.Unlock()
		e.observer.ObserveAdmission("capacity")
		e.observeImmediate(request, SessionSemanticShadowResultCapacity)
		return false
	}
	e.mu.Unlock()
	e.observer.ObserveAdmission("admitted")
	e.observer.ObserveInFlight(1)

	go e.evaluate(request, shadowPolicy)
	return true
}

func (e *SessionSemanticShadowEvaluator) evaluate(
	request SessionSemanticShadowRequest,
	shadowPolicy *domainrecommendation.Policy,
) {
	started := time.Now()
	terminalObserved := false
	observe := func(result SessionSemanticShadowTerminalResult, evidence *domainrecommendation.SessionSemanticEvidence, comparison SessionSemanticShadowComparison) {
		if terminalObserved {
			return
		}
		terminalObserved = true
		band := domainrecommendation.SessionSemanticConfidenceNone
		if evidence != nil && domainrecommendation.ValidSessionSemanticConfidenceBand(evidence.ConfidenceBand) {
			band = evidence.ConfidenceBand
		}
		observation := SessionSemanticShadowObservation{
			Result: result, BaselineState: normalizeSessionSemanticShadowBaseline(request.BaselineState),
			ConfidenceBand: band, Comparison: comparison, Duration: time.Since(started),
		}
		if observation.valid() {
			e.observer.ObserveTerminal(observation)
		}
	}
	defer func() {
		e.workWG.Done()
		if recover() != nil {
			observe(SessionSemanticShadowResultPanic, nil, SessionSemanticShadowComparison{})
		}
	}()

	workCtx, cancel := context.WithTimeout(e.rootCtx, e.config.Deadline)
	defer cancel()
	completed := make(chan sessionSemanticShadowProviderResult, 1)
	go e.callProvider(workCtx, request, shadowPolicy, completed)

	var providerResult sessionSemanticShadowProviderResult
	select {
	case providerResult = <-completed:
	case <-workCtx.Done():
		result := SessionSemanticShadowResultTimeout
		if errors.Is(e.rootCtx.Err(), context.Canceled) {
			result = SessionSemanticShadowResultClosed
		}
		observe(result, nil, SessionSemanticShadowComparison{})
		return
	}
	if providerResult.err != nil {
		result := SessionSemanticShadowResultError
		if errors.Is(providerResult.err, context.DeadlineExceeded) || errors.Is(workCtx.Err(), context.DeadlineExceeded) {
			result = SessionSemanticShadowResultTimeout
		} else if errors.Is(e.rootCtx.Err(), context.Canceled) {
			result = SessionSemanticShadowResultClosed
		} else if errors.Is(providerResult.err, errSessionSemanticShadowProviderPanic) {
			result = SessionSemanticShadowResultPanic
		}
		observe(result, providerResult.evidence, SessionSemanticShadowComparison{})
		return
	}
	if len(providerResult.candidates) == 0 {
		observe(SessionSemanticShadowResultEmpty, providerResult.evidence, SessionSemanticShadowComparison{})
		return
	}

	simulation, err := e.simulator.SimulateSessionSemanticShadow(workCtx, SessionSemanticShadowSimulationRequest{
		Request: request, ShadowPolicy: shadowPolicy,
		SemanticCandidates: cloneCandidates(providerResult.candidates),
		ComparisonLimit:    e.config.ComparisonLimit, TopK: e.config.SimulatedTopK,
	})
	if err != nil || simulation == nil || !simulation.Comparison.valid() {
		result := SessionSemanticShadowResultError
		if errors.Is(workCtx.Err(), context.DeadlineExceeded) {
			result = SessionSemanticShadowResultTimeout
		} else if errors.Is(e.rootCtx.Err(), context.Canceled) {
			result = SessionSemanticShadowResultClosed
		}
		observe(result, providerResult.evidence, SessionSemanticShadowComparison{})
		return
	}
	observe(SessionSemanticShadowResultSuccess, providerResult.evidence, simulation.Comparison)
}

func (e *SessionSemanticShadowEvaluator) callProvider(
	ctx context.Context,
	request SessionSemanticShadowRequest,
	shadowPolicy *domainrecommendation.Policy,
	completed chan<- sessionSemanticShadowProviderResult,
) {
	defer func() {
		<-e.permits
		e.observer.ObserveInFlight(-1)
		e.actualWG.Done()
		if recover() != nil {
			completed <- sessionSemanticShadowProviderResult{err: errSessionSemanticShadowProviderPanic}
		}
	}()
	candidates, evidence, err := e.provider.RecallWithSessionSemanticEvidence(ctx, RecallRequest{
		UserID: request.UserID, Scene: request.Scene, Context: request.Context,
		Budget: e.config.Budget, Now: request.Now, Policy: shadowPolicy.Clone(),
	})
	completed <- sessionSemanticShadowProviderResult{
		candidates: cloneCandidates(candidates), evidence: evidence.Clone(), err: err,
	}
}

func (e *SessionSemanticShadowEvaluator) observeImmediate(
	request SessionSemanticShadowRequest,
	result SessionSemanticShadowTerminalResult,
) {
	observation := SessionSemanticShadowObservation{
		Result: result, BaselineState: normalizeSessionSemanticShadowBaseline(request.BaselineState),
		ConfidenceBand: domainrecommendation.SessionSemanticConfidenceNone,
		Comparison:     SessionSemanticShadowComparison{}, Duration: 0,
	}
	if observation.valid() {
		e.observer.ObserveTerminal(observation)
	}
}

func (e *SessionSemanticShadowEvaluator) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	if !e.closed {
		e.closed = true
		e.cancel()
	}
	e.mu.Unlock()

	done := make(chan struct{})
	go func() {
		e.workWG.Wait()
		e.actualWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
