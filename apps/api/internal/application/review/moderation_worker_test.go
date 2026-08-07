package applicationreview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type moderationWorkerRepo struct {
	job            *domainreview.ModerationJob
	subject        *domainreview.ModerationSubject
	current        bool
	accepted       bool
	acceptedResult *domainreview.ProcessingResult
	retryCode      string
	submitted      bool
	cancelled      bool
	manifest       string
}

func (r *moderationWorkerRepo) ClaimModerationJobs(
	context.Context, string, int, time.Duration,
) ([]*domainreview.ModerationJob, error) {
	if r.job == nil || r.submitted || r.cancelled {
		return nil, nil
	}
	copyJob := *r.job
	copyJob.Attempts++
	r.job.Attempts = copyJob.Attempts
	return []*domainreview.ModerationJob{&copyJob}, nil
}

func (r *moderationWorkerRepo) LoadModerationSubject(
	context.Context, *domainreview.ModerationJob,
) (*domainreview.ModerationSubject, error) {
	return r.subject, nil
}

func (r *moderationWorkerRepo) ModerationJobCurrent(
	context.Context, *domainreview.ModerationJob,
) (bool, error) {
	return r.current, nil
}

func (r *moderationWorkerRepo) ModerationResultAccepted(
	context.Context, string,
) (bool, error) {
	return r.accepted, nil
}

func (r *moderationWorkerRepo) LoadModerationProcessingResult(
	context.Context, string,
) (*domainreview.ProcessingResult, error) {
	return r.acceptedResult, nil
}

func (r *moderationWorkerRepo) RenewModerationJobLease(
	context.Context, int64, string, time.Duration,
) error {
	return nil
}

func (r *moderationWorkerRepo) SaveModerationInputManifest(
	_ context.Context, _ int64, _ string, manifestJSON string,
) error {
	r.manifest = manifestJSON
	r.job.InputManifestJSON = manifestJSON
	return nil
}

func (r *moderationWorkerRepo) MarkModerationJobRetry(
	_ context.Context, _ int64, _ string, _ time.Time, code string,
) error {
	r.retryCode = code
	return nil
}

func (r *moderationWorkerRepo) MarkModerationJobSubmitted(
	context.Context, int64, string, time.Time,
) error {
	r.submitted = true
	return nil
}

func (r *moderationWorkerRepo) MarkModerationJobTerminal(
	context.Context, int64, string, string,
) error {
	return nil
}

func (r *moderationWorkerRepo) CancelModerationJob(
	context.Context, int64, string, string,
) error {
	r.cancelled = true
	return nil
}

func (r *moderationWorkerRepo) ReconcileModerationJobs(
	context.Context, domainreview.ModerationJobConfig, int,
) (domainreview.ModerationReconciliationStats, error) {
	return domainreview.ModerationReconciliationStats{}, nil
}

type moderationPreparer struct {
	prepareCalls int
	manifest     *domainreview.ModerationInputManifest
	err          error
}

func (p *moderationPreparer) Prepare(
	context.Context,
	*domainreview.ModerationSubject,
	*domainreview.ModerationJob,
) (*domainreview.ModerationInputManifest, error) {
	p.prepareCalls++
	if p.err != nil {
		return nil, p.err
	}
	return p.manifest, nil
}

func (p *moderationPreparer) ResolveAccess(
	context.Context,
	*domainreview.ModerationInputManifest,
	time.Duration,
) ([]domainreview.ModerationFrameAccess, error) {
	return []domainreview.ModerationFrameAccess{{
		TimestampMS: 500, SHA256: strings.Repeat("a", 64),
		URL: "https://media.example/frame.jpg", ExpiresAt: time.Now().Add(time.Minute),
	}}, nil
}

type moderationProvider struct {
	calls int
	err   error
}

func (p *moderationProvider) Evaluate(
	context.Context,
	ModerationProviderRequest,
) (*ModerationProviderResult, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &ModerationProviderResult{
		Provider: "fixture", ModelVersion: "v1", GeneratedAt: time.Now().UTC(),
		Signals: []ModerationProviderSignal{{
			Label: domainreview.LabelSafe, Confidence: 0.99,
			FrameTimestampsMS: []int64{500},
		}},
	}, nil
}

type moderationCleanup struct {
	keys  []string
	calls int
	times []time.Time
}

func (c *moderationCleanup) ScheduleModerationSampleCleanup(
	_ context.Context,
	keys []string,
	notBefore time.Time,
) error {
	c.keys = append([]string(nil), keys...)
	c.calls++
	c.times = append(c.times, notBefore)
	return nil
}

func TestModerationWorkerObservePersistsProductionEvidenceAndRoutesHuman(t *testing.T) {
	reviewRepo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	reviewCase, _, _ := reviewRepo.CreateOrGetCase(context.Background(), 10)
	job, subject, manifest := moderationWorkerFixture(
		t, reviewCase, domainreview.ModerationModeObserve,
	)
	jobRepo := &moderationWorkerRepo{job: job, subject: subject, current: true}
	preparer := &moderationPreparer{manifest: manifest}
	provider := &moderationProvider{}
	cleanup := &moderationCleanup{}
	worker, err := NewModerationWorker(
		jobRepo, preparer, provider, New(reviewRepo), cleanup, nil,
		moderationWorkerConfig(domainreview.ModerationModeObserve),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	if !jobRepo.submitted || provider.calls != 1 ||
		reviewRepo.lastResult.SourceKind != domainreview.MachineSourceProductionProvider ||
		reviewRepo.lastResult.RolloutMode != domainreview.ModerationModeObserve ||
		reviewRepo.results["fixture|"+job.ResultID].Decision.Outcome != domainreview.OutcomeHuman ||
		len(cleanup.keys) != 1 || cleanup.calls != 2 ||
		!cleanup.times[1].Before(cleanup.times[0]) {
		t.Fatalf(
			"submitted=%v provider=%d result=%#v cleanup=%v",
			jobRepo.submitted, provider.calls, reviewRepo.lastResult, cleanup.keys,
		)
	}
}

func TestModerationWorkerCleansPartialSamplesBeforeRecovery(t *testing.T) {
	reviewRepo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	reviewCase, _, _ := reviewRepo.CreateOrGetCase(context.Background(), 13)
	job, subject, _ := moderationWorkerFixture(
		t, reviewCase, domainreview.ModerationModeEnforce,
	)
	job.MaxAttempts = 1
	partialKey := "moderation/1/1/1/frames-v1/partial.jpg"
	jobRepo := &moderationWorkerRepo{job: job, subject: subject, current: true}
	cleanup := &moderationCleanup{}
	worker, err := NewModerationWorker(
		jobRepo,
		&moderationPreparer{err: &ModerationInputError{
			Code: "frame_store", Terminal: true,
			ObjectKeys: []string{partialKey}, Err: errors.New("store failed"),
		}},
		&moderationProvider{},
		New(reviewRepo),
		cleanup,
		nil,
		moderationWorkerConfig(domainreview.ModerationModeEnforce),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	if !jobRepo.submitted || cleanup.calls != 1 ||
		len(cleanup.keys) != 1 || cleanup.keys[0] != partialKey ||
		reviewRepo.lastResult.SourceKind != domainreview.MachineSourceRecovery {
		t.Fatalf(
			"submitted=%v cleanup=%#v result=%#v",
			jobRepo.submitted, cleanup, reviewRepo.lastResult,
		)
	}
}

func TestModerationWorkerDisabledUsesStableRecoveryWithoutProvider(t *testing.T) {
	reviewRepo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	reviewCase, _, _ := reviewRepo.CreateOrGetCase(context.Background(), 11)
	job, subject, _ := moderationWorkerFixture(
		t, reviewCase, domainreview.ModerationModeDisabled,
	)
	jobRepo := &moderationWorkerRepo{job: job, subject: subject, current: true}
	worker, err := NewModerationWorker(
		jobRepo, nil, nil, New(reviewRepo), nil, nil,
		moderationWorkerConfig(domainreview.ModerationModeDisabled),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	if !jobRepo.submitted ||
		reviewRepo.lastResult.SourceKind != domainreview.MachineSourceRecovery ||
		reviewRepo.lastResult.Provider != "frux-moderation-recovery" ||
		reviewRepo.lastResult.Signals[0].Label != "moderation_unavailable" ||
		reviewRepo.results["frux-moderation-recovery|"+job.ResultID].Decision.Outcome != domainreview.OutcomeHuman {
		t.Fatalf("recovery result = %#v", reviewRepo.lastResult)
	}
}

func TestModerationWorkerRecoversIncompatibleHistoricalJobWithoutProvider(t *testing.T) {
	reviewRepo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	reviewCase, _, _ := reviewRepo.CreateOrGetCase(context.Background(), 14)
	job, subject, _ := moderationWorkerFixture(
		t, reviewCase, domainreview.ModerationModeObserve,
	)
	job.ProviderConfigVersion = 1
	jobRepo := &moderationWorkerRepo{job: job, subject: subject, current: true}
	config := moderationWorkerConfig(domainreview.ModerationModeDisabled)
	config.JobConfig.ProviderConfigVersion = 2
	worker, err := NewModerationWorker(
		jobRepo, nil, nil, New(reviewRepo), nil, nil, config,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	if !jobRepo.submitted ||
		reviewRepo.lastResult.SourceKind != domainreview.MachineSourceRecovery {
		t.Fatalf("historical fallback = %#v", reviewRepo.lastResult)
	}
}

func TestModerationWorkerEnforceAppliesAutomatedApprovalAndRejection(t *testing.T) {
	for _, test := range []struct {
		name          string
		policyOutcome string
		want          string
		videoID       int64
	}{
		{name: "approve", policyOutcome: domainreview.OutcomeApprove, want: domainreview.OutcomeApprove, videoID: 21},
		{name: "reject", policyOutcome: domainreview.OutcomeReject, want: domainreview.OutcomeReject, videoID: 22},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewRepo := newReviewServiceRepo(t, test.policyOutcome)
			reviewCase, _, _ := reviewRepo.CreateOrGetCase(context.Background(), test.videoID)
			job, subject, manifest := moderationWorkerFixture(
				t, reviewCase, domainreview.ModerationModeEnforce,
			)
			jobRepo := &moderationWorkerRepo{job: job, subject: subject, current: true}
			worker, err := NewModerationWorker(
				jobRepo,
				&moderationPreparer{manifest: manifest},
				&moderationProvider{},
				New(reviewRepo),
				&moderationCleanup{},
				nil,
				moderationWorkerConfig(domainreview.ModerationModeEnforce),
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := worker.RunOnce(context.Background(), "worker"); err != nil {
				t.Fatal(err)
			}
			decision := reviewRepo.results["fixture|"+job.ResultID].Decision
			if decision.Outcome != test.want || !jobRepo.submitted {
				t.Fatalf("decision=%#v submitted=%v", decision, jobRepo.submitted)
			}
		})
	}
}

func TestModerationWorkerRetriesProviderFailureThenFallsBack(t *testing.T) {
	reviewRepo := newReviewServiceRepo(t, domainreview.OutcomeApprove)
	reviewCase, _, _ := reviewRepo.CreateOrGetCase(context.Background(), 12)
	job, subject, manifest := moderationWorkerFixture(
		t, reviewCase, domainreview.ModerationModeEnforce,
	)
	job.MaxAttempts = 2
	jobRepo := &moderationWorkerRepo{job: job, subject: subject, current: true}
	preparer := &moderationPreparer{manifest: manifest}
	provider := &moderationProvider{err: &ModerationProviderError{
		Code: "timeout", Retryable: true, Err: errors.New("timeout"),
	}}
	worker, err := NewModerationWorker(
		jobRepo, preparer, provider, New(reviewRepo), nil, nil,
		moderationWorkerConfig(domainreview.ModerationModeEnforce),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), "worker"); err == nil ||
		jobRepo.retryCode != "timeout" {
		t.Fatalf("first retry error=%v code=%q", err, jobRepo.retryCode)
	}
	jobRepo.retryCode = ""
	if err := worker.RunOnce(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	if !jobRepo.submitted ||
		reviewRepo.lastResult.SourceKind != domainreview.MachineSourceRecovery {
		t.Fatalf("fallback result = %#v", reviewRepo.lastResult)
	}
}

func TestModerationWorkerCompletesAcceptedUncertainResult(t *testing.T) {
	job := &domainreview.ModerationJob{
		ID: 1, ResultID: "moderation-result:1:1:1",
		Status: domainreview.ModerationJobLeased,
	}
	accepted := &domainreview.ProcessingResult{
		Case: &domainreview.ReviewCase{ID: 1, VideoID: 2, ReviewVersion: 1},
		Decision: &domainreview.AutomatedDecision{
			ID: 1, CaseID: 1, ResultID: job.ResultID,
			Outcome: domainreview.OutcomeReject,
		},
		ApplySideEffects: true,
	}
	repo := &moderationWorkerRepo{
		job: job, current: false, accepted: true, acceptedResult: accepted,
	}
	reviewRepo := newReviewServiceRepo(t, domainreview.OutcomeHuman)
	applier := &countingOutcomeApplier{}
	worker, err := NewModerationWorker(
		repo, nil, nil, New(reviewRepo, WithOutcomeApplier(applier)), nil, nil,
		moderationWorkerConfig(domainreview.ModerationModeDisabled),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	if !repo.submitted || repo.cancelled || applier.calls != 1 {
		t.Fatalf(
			"submitted=%v cancelled=%v side_effects=%d",
			repo.submitted, repo.cancelled, applier.calls,
		)
	}
}

func moderationWorkerFixture(
	t *testing.T,
	reviewCase *domainreview.ReviewCase,
	mode string,
) (*domainreview.ModerationJob, *domainreview.ModerationSubject, *domainreview.ModerationInputManifest) {
	t.Helper()
	job, err := domainreview.NewModerationJob(
		reviewCase.ID, reviewCase.VideoID, reviewCase.ReviewVersion,
		domainreview.ModerationJobConfig{
			Mode: mode, ProviderConfigVersion: 1,
			InputProfileVersion: "frames-v1", MaxAttempts: 3,
		},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	job.ID = 1
	manifest := &domainreview.ModerationInputManifest{
		ProfileVersion: "frames-v1", DurationMS: 1000, PreparedAt: time.Now().UTC(),
		Frames: []domainreview.ModerationFrameSample{{
			TimestampMS: 500, SHA256: strings.Repeat("a", 64),
			ObjectKey: "moderation/1/1/frames-v1/frame.jpg",
			SizeBytes: 100, Width: 100, Height: 100,
		}},
	}
	return job, &domainreview.ModerationSubject{
		CaseID: reviewCase.ID, VideoID: reviewCase.VideoID,
		ReviewVersion: reviewCase.ReviewVersion, PolicyVersion: reviewCase.PolicyVersion,
		Title: "title", Description: "description", SourceObjectKey: "video/source.mp4",
	}, manifest
}

func moderationWorkerConfig(mode string) ModerationWorkerConfig {
	return ModerationWorkerConfig{
		JobConfig: domainreview.ModerationJobConfig{
			Mode: mode, ProviderConfigVersion: 1,
			InputProfileVersion: "frames-v1", MaxAttempts: 3,
		},
		LeaseTTL: time.Minute, PollInterval: time.Second,
		SampleURLTTL: time.Minute, SampleRetention: time.Hour, Concurrency: 1,
	}
}
