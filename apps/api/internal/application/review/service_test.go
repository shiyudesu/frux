package applicationreview

import (
	"context"
	"errors"
	"testing"
	"time"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type reviewServiceRepo struct {
	cases      map[int64]*domainreview.ReviewCase
	ids        []int64
	results    map[string]*domainreview.ProcessingResult
	policy     *domainreview.Policy
	processErr error
	intakeErr  error
}

func newReviewServiceRepo(t *testing.T, outcome string) *reviewServiceRepo {
	t.Helper()
	config := domainreview.PolicyConfiguration{DefaultOutcome: outcome, Rules: []domainreview.LabelRule{{Label: domainreview.LabelSafe}}}
	policy, err := domainreview.NewPolicy(1, true, config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &reviewServiceRepo{cases: map[int64]*domainreview.ReviewCase{}, results: map[string]*domainreview.ProcessingResult{}, policy: policy}
}

func (r *reviewServiceRepo) CreateOrGetCase(_ context.Context, videoID int64) (*domainreview.ReviewCase, bool, error) {
	if r.intakeErr != nil {
		return nil, false, r.intakeErr
	}
	if existing := r.cases[videoID]; existing != nil {
		return existing, false, nil
	}
	reviewCase, _ := domainreview.NewCase(videoID, 1, 1, time.Now())
	reviewCase.ID = int64(len(r.cases) + 1)
	r.cases[videoID] = reviewCase
	return reviewCase, true, nil
}

func (r *reviewServiceRepo) ProcessMachineResult(_ context.Context, result *domainreview.MachineResult) (*domainreview.ProcessingResult, error) {
	if r.processErr != nil {
		return nil, r.processErr
	}
	key := result.Provider + "|" + result.ResultID
	if existing := r.results[key]; existing != nil {
		duplicate := *existing
		duplicate.Duplicate = true
		return &duplicate, nil
	}
	reviewCase := r.cases[result.VideoID]
	outcome, priority, err := r.policy.RouteWithPriority(result.Signals)
	if err != nil {
		return nil, err
	}
	switch outcome {
	case domainreview.OutcomeApprove:
		reviewCase.Status = domainreview.CaseStatusApproved
	case domainreview.OutcomeReject:
		reviewCase.Status = domainreview.CaseStatusRejected
	default:
		reviewCase.Status = domainreview.CaseStatusPendingHuman
		reviewCase.Priority = priority
	}
	processed := &domainreview.ProcessingResult{
		Case: reviewCase,
		Decision: &domainreview.AutomatedDecision{
			ID: 1, CaseID: reviewCase.ID, ResultID: result.ResultID,
			Outcome: outcome, PolicyVersion: 1, CreatedAt: time.Now(),
		},
		ApplySideEffects: true,
	}
	r.results[key] = processed
	return processed, nil
}

func (r *reviewServiceRepo) ListReviewableVideoIDsWithoutCase(_ context.Context, _ int) ([]int64, error) {
	return r.ids, nil
}

type reviewObserverRecord struct{ events []string }

func (o *reviewObserverRecord) Observe(stage, result string) {
	o.events = append(o.events, stage+":"+result)
}

type failingOutcomeApplier struct{ err error }

func (a failingOutcomeApplier) ApplyReviewOutcome(context.Context, *domainreview.ProcessingResult) error {
	return a.err
}

type countingOutcomeApplier struct{ calls int }

func (a *countingOutcomeApplier) ApplyReviewOutcome(context.Context, *domainreview.ProcessingResult) error {
	a.calls++
	return nil
}

func TestServiceIntakeResultAndReconciliation(t *testing.T) {
	repo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	observer := &reviewObserverRecord{}
	service := New(repo, WithObserver(observer))
	reviewCase, created, err := service.EnsureCase(context.Background(), 11)
	if err != nil || !created {
		t.Fatalf("EnsureCase() = %#v created=%v err=%v", reviewCase, created, err)
	}
	_, created, err = service.EnsureCase(context.Background(), 11)
	if err != nil || created {
		t.Fatalf("duplicate EnsureCase() created=%v err=%v", created, err)
	}
	processed, err := service.SubmitMachineResult(context.Background(), domainreview.MachineResultInput{
		CaseID: reviewCase.ID, VideoID: 11, ReviewVersion: 1, ResultID: "result-1",
		Provider: "provider", ModelVersion: "v1", PolicyVersion: 1,
		Signals: []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 1}},
	})
	if err != nil || processed.Decision.Outcome != domainreview.OutcomeApprove {
		t.Fatalf("SubmitMachineResult() = %#v err=%v", processed, err)
	}
	replayed, err := service.SubmitMachineResult(context.Background(), domainreview.MachineResultInput{
		CaseID: reviewCase.ID, VideoID: 11, ReviewVersion: 1, ResultID: "result-1",
		Provider: "provider", ModelVersion: "v1", PolicyVersion: 1,
		Signals: []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 1}},
	})
	if err != nil || !replayed.Duplicate {
		t.Fatalf("duplicate result = %#v err=%v", replayed, err)
	}
	repo.ids = []int64{12, 13}
	stats, err := service.Reconcile(context.Background(), 100)
	if err != nil || stats.Created != 2 || stats.Scanned != 2 {
		t.Fatalf("Reconcile() = %#v err=%v", stats, err)
	}
	if len(observer.events) == 0 {
		t.Fatal("expected bounded observations")
	}
}

func TestServiceInvalidAndRetryPaths(t *testing.T) {
	repo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	observer := &reviewObserverRecord{}
	retryErr := errors.New("media publication unavailable")
	service := New(repo, WithObserver(observer), WithOutcomeApplier(failingOutcomeApplier{err: retryErr}))
	reviewCase, _, _ := service.EnsureCase(context.Background(), 21)
	_, err := service.SubmitMachineResult(context.Background(), domainreview.MachineResultInput{
		CaseID: reviewCase.ID, VideoID: 21, ReviewVersion: 1, ResultID: "invalid",
		Provider: "provider", ModelVersion: "v1", PolicyVersion: 1,
		Signals: []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 2}},
	})
	if !errors.Is(err, domainreview.ErrInvalidConfidence) {
		t.Fatalf("invalid result error = %v", err)
	}

	_, err = service.SubmitMachineResult(context.Background(), domainreview.MachineResultInput{
		CaseID: reviewCase.ID, VideoID: 21, ReviewVersion: 1, ResultID: "retry",
		Provider: "provider", ModelVersion: "v1", PolicyVersion: 1,
		Signals: []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 1}},
	})
	if !errors.Is(err, retryErr) {
		t.Fatalf("side effect error = %v", err)
	}
}

func TestServiceSkipsStaleDuplicateSideEffects(t *testing.T) {
	repo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	reviewCase, _, _ := repo.CreateOrGetCase(context.Background(), 31)
	result := &domainreview.ProcessingResult{
		Case: reviewCase,
		Decision: &domainreview.AutomatedDecision{
			ID: 1, CaseID: reviewCase.ID, ResultID: "stale-duplicate",
			Outcome: domainreview.OutcomeApprove, PolicyVersion: 1, CreatedAt: time.Now(),
		},
		Duplicate: true, ApplySideEffects: false,
	}
	repo.results["provider|stale-duplicate"] = result
	applier := &countingOutcomeApplier{}
	service := New(repo, WithOutcomeApplier(applier))
	processed, err := service.SubmitMachineResult(context.Background(), domainreview.MachineResultInput{
		CaseID: reviewCase.ID, VideoID: 31, ReviewVersion: 1, ResultID: "stale-duplicate",
		Provider: "provider", ModelVersion: "v1", PolicyVersion: 1,
		Signals: []domainreview.MachineSignal{{Label: domainreview.LabelSafe, Confidence: 1}},
	})
	if err != nil || !processed.Duplicate {
		t.Fatalf("stale duplicate = %#v err=%v", processed, err)
	}
	if applier.calls != 0 {
		t.Fatalf("stale duplicate reapplied side effects: %d", applier.calls)
	}
}

func TestReconciliationWorkerRunOnce(t *testing.T) {
	repo := newReviewServiceRepo(t, domainreview.OutcomeHuman)
	repo.ids = []int64{31}
	worker := NewReconciliationWorker(New(repo))
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if repo.cases[31] == nil {
		t.Fatal("worker did not create missing review case")
	}
}
