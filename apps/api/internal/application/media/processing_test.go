package applicationmedia

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestMediaProcessingWorkerCompletesAndDeduplicates(t *testing.T) {
	now := time.Date(2026, 7, 26, 7, 0, 0, 0, time.UTC)
	repo := &processingRepositoryStub{
		asset: &domainmedia.MediaAsset{ID: 11, OwnerID: 4, Kind: domainmedia.AssetKindVideo, State: domainmedia.AssetStateUploaded},
		job: &domainmedia.MediaProcessingJob{
			ID: 7, AssetID: 11, ProfileVersion: "v1", State: domainmedia.JobStatePending,
			MaxAttempts: 5, NextAttemptAt: now,
		},
	}
	processor := &processorStub{result: &ProcessResult{
		Width: 1920, Height: 1080, DurationMS: 5000, VideoCodec: "h264", AudioCodec: "aac",
		Variants: []*domainmedia.MediaVariant{{
			AssetID: 11, ProfileVersion: "v1", SourceType: domainmedia.SourceTypeMP4,
			Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady,
		}},
	}}
	worker := NewMediaProcessingWorker(repo, processor, nil, time.Minute, 1)
	worker.now = func() time.Time { return now }

	if err := worker.HandleRequested(context.Background(), NewProcessingRequestedEvent(11, "v1", now)); err != nil {
		t.Fatalf("process media: %v", err)
	}
	if repo.asset.State != domainmedia.AssetStateReady || repo.job.State != domainmedia.JobStateCompleted || len(repo.variants) != 1 {
		t.Fatalf("unexpected processing result: asset=%+v job=%+v variants=%+v", repo.asset, repo.job, repo.variants)
	}
	if processor.calls != 1 {
		t.Fatalf("expected one processor call, got %d", processor.calls)
	}
	if err := worker.HandleRequested(context.Background(), NewProcessingRequestedEvent(11, "v1", now)); err != nil {
		t.Fatalf("replay media event: %v", err)
	}
	if processor.calls != 1 {
		t.Fatalf("duplicate event reprocessed output: %d", processor.calls)
	}
}

func TestMediaProcessingWorkerRecordsTerminalFailure(t *testing.T) {
	now := time.Date(2026, 7, 26, 7, 0, 0, 0, time.UTC)
	repo := &processingRepositoryStub{
		asset: &domainmedia.MediaAsset{ID: 12, OwnerID: 4, Kind: domainmedia.AssetKindVideo, State: domainmedia.AssetStateUploaded},
		job: &domainmedia.MediaProcessingJob{
			ID: 8, AssetID: 12, ProfileVersion: "v1", State: domainmedia.JobStatePending,
			MaxAttempts: 5, NextAttemptAt: now,
		},
	}

	processor := &processorStub{err: &ProcessError{Code: "probe_invalid", Terminal: true, Err: errors.New("bad media")}}
	worker := NewMediaProcessingWorker(repo, processor, nil, time.Minute, 1)
	worker.now = func() time.Time { return now }

	if err := worker.HandleRequested(context.Background(), NewProcessingRequestedEvent(12, "v1", now)); err != nil {
		t.Fatalf("record terminal failure: %v", err)
	}
	if repo.asset.State != domainmedia.AssetStateFailed || repo.asset.ErrorCode != "probe_invalid" ||
		repo.job.State != domainmedia.JobStateFailed {
		t.Fatalf("unexpected failure state: asset=%+v job=%+v", repo.asset, repo.job)
	}
}

func TestTruncateProcessingErrorKeepsDiagnosticTail(t *testing.T) {
	message := strings.Repeat("x", 600) + "actionable-terminal-error"
	got := truncateProcessingError(errors.New(message))
	if len(got) != 512 || !strings.HasSuffix(got, "actionable-terminal-error") {
		t.Fatalf("truncated error = %q", got)
	}
}

func TestMediaProcessingWorkerDoesNotResurrectDeletedAsset(t *testing.T) {
	now := time.Date(2026, 7, 26, 7, 0, 0, 0, time.UTC)
	repo := &processingRepositoryStub{
		asset: &domainmedia.MediaAsset{
			ID: 13, OwnerID: 4, Kind: domainmedia.AssetKindVideo,
			State: domainmedia.AssetStateDeleted,
		},
		job: &domainmedia.MediaProcessingJob{
			ID: 9, AssetID: 13, ProfileVersion: "v1",
			State: domainmedia.JobStatePending, MaxAttempts: 5, NextAttemptAt: now,
		},
	}
	notifier := &mediaProcessingNotifierStub{}
	worker := NewMediaProcessingWorker(
		repo, &processorStub{}, nil, time.Minute, 1,
		WithMediaStateNotifier(notifier),
	)
	worker.now = func() time.Time { return now }
	if err := worker.HandleRequested(
		context.Background(),
		NewProcessingRequestedEvent(13, "v1", now),
	); err != nil {
		t.Fatal(err)
	}
	if repo.asset.State != domainmedia.AssetStateDeleted ||
		repo.job.State != domainmedia.JobStateFailed ||
		notifier.failed != 0 || notifier.ready != 0 {
		t.Fatalf("deleted asset was resurrected or notified: asset=%+v job=%+v notifier=%+v",
			repo.asset, repo.job, notifier)
	}
}

type mediaProcessingNotifierStub struct {
	ready  int
	failed int
}

func (s *mediaProcessingNotifierStub) MediaReady(context.Context, int64) error {
	s.ready++
	return nil
}

func (*mediaProcessingNotifierStub) MediaRepairing(
	context.Context, int64, string,
) error {
	return nil
}

func (s *mediaProcessingNotifierStub) MediaFailed(
	context.Context, int64, string, string,
) error {
	s.failed++
	return nil
}

func TestKafkaWakeupValidatesAndSignalsWithoutProcessing(t *testing.T) {
	now := time.Now().UTC()
	repo := &processingRepositoryStub{
		job: &domainmedia.MediaProcessingJob{
			ID: 1, AssetID: 20, ProfileVersion: "v1", State: domainmedia.JobStatePending,
		},
	}

	processor := &processorStub{}
	worker := NewMediaProcessingWorker(repo, processor, nil, time.Minute, 1)
	event := NewProcessingRequestedEvent(20, "v1", now)
	if err := worker.SignalRequested(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if processor.calls != 0 || len(worker.schedule) != 1 {
		t.Fatalf("processor calls=%d queued=%d", processor.calls, len(worker.schedule))
	}
	if err := worker.SignalRequested(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(worker.schedule) != 1 {
		t.Fatalf("full scheduler changed queued wakeups: %d", len(worker.schedule))
	}
	stale := NewProcessingRequestedEvent(20, "v2", now)
	if err := worker.SignalRequested(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	if len(worker.schedule) != 1 {
		t.Fatalf("stale wakeup was queued: %d", len(worker.schedule))
	}
}

func TestMediaSchedulerNeverClaimsBeyondAvailableSlots(t *testing.T) {
	now := time.Now().UTC()
	repo := newConcurrentProcessingRepository(now, 4)
	release := make(chan struct{})
	processor := &blockingProcessor{release: release, started: make(chan struct{}, 4)}
	worker := NewMediaProcessingWorker(repo, processor, nil, time.Minute, 2)
	worker.pollInterval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 2; id++ {
		if err := worker.SignalRequested(
			context.Background(),
			NewProcessingRequestedEvent(id, "v1", now),
		); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		select {
		case <-processor.started:
		case <-time.After(time.Second):
			t.Fatal("processing slots did not start")
		}
	}
	time.Sleep(30 * time.Millisecond)
	repo.mu.Lock()
	claims := repo.claims
	tokens := append([]string(nil), repo.tokens...)
	repo.mu.Unlock()
	if claims != 2 {
		t.Fatalf("claimed %d jobs with only two slots", claims)
	}
	if len(tokens) != 2 || tokens[0] == tokens[1] {
		t.Fatalf("claim tokens are not unique: %v", tokens)
	}
	close(release)
}

func TestMediaHeartbeatStallIsBoundedAndPreventsStaleCompletion(t *testing.T) {
	now := time.Now().UTC()
	repo := newConcurrentProcessingRepository(now, 1)
	repo.blockHeartbeat = true
	processor := &cancelingProcessor{}
	worker := NewMediaProcessingWorker(repo, processor, nil, 300*time.Millisecond, 1)
	started := time.Now()
	err := worker.HandleRequested(
		context.Background(),
		NewProcessingRequestedEvent(1, "v1", now),
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("heartbeat error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled heartbeat blocked processing for %v", elapsed)
	}
	repo.mu.Lock()
	completions := repo.ownedUpdates
	repo.mu.Unlock()
	if completions != 0 {
		t.Fatalf("stale claim performed %d fenced updates", completions)
	}
}

func TestMediaFinalizationFencesStaleClaimAfterReclaim(t *testing.T) {
	now := time.Now().UTC()
	repo := newConcurrentProcessingRepository(now, 1)
	stale, err := repo.LeaseProcessingJob(
		context.Background(), 1, "v1", "stale-token", now, now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	repo.jobs[1].LeaseUntil = timePointer(now.Add(-time.Second))
	repo.jobs[1].State = domainmedia.JobStateRetryable
	repo.mu.Unlock()
	reclaimed, err := repo.LeaseProcessingJob(
		context.Background(), 1, "v1", "reclaimed-token", now, now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	stale.State = domainmedia.JobStateCompleted
	stale.LeaseOwner = ""
	stale.LeaseUntil = nil
	if err := repo.FinalizeProcessingJob(
		context.Background(),
		&domainmedia.ProcessingFinalization{
			Asset: &domainmedia.MediaAsset{
				ID: 1, State: domainmedia.AssetStateReady, Width: 111,
			},
			Variants: []*domainmedia.MediaVariant{{
				AssetID: 1, ObjectKey: "variants/stale.mp4",
			}},
			Job: stale, LeaseOwner: "stale-token", CommittedAt: now,
		},
	); !errors.Is(err, domainmedia.ErrProcessingJobLeaseLost) {
		t.Fatalf("stale finalization error = %v", err)
	}
	repo.mu.Lock()
	if repo.assets[1].Width != 0 || len(repo.variants) != 0 {
		t.Fatalf("stale claim mutated asset or variants: asset=%+v variants=%+v",
			repo.assets[1], repo.variants)
	}
	repo.mu.Unlock()

	reclaimed.State = domainmedia.JobStateCompleted
	reclaimed.LeaseOwner = ""
	reclaimed.LeaseUntil = nil
	if err := repo.FinalizeProcessingJob(
		context.Background(),
		&domainmedia.ProcessingFinalization{
			Asset: &domainmedia.MediaAsset{
				ID: 1, State: domainmedia.AssetStateReady, Width: 1920,
			},
			Variants: []*domainmedia.MediaVariant{{
				AssetID: 1, ObjectKey: "variants/reclaimed.mp4",
			}},
			Job: reclaimed, LeaseOwner: "reclaimed-token", CommittedAt: now,
		},
	); err != nil {
		t.Fatalf("reclaimed finalization: %v", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.assets[1].Width != 1920 || len(repo.variants) != 1 ||
		repo.jobs[1].State != domainmedia.JobStateCompleted {
		t.Fatalf("reclaimed finalization did not commit atomically: asset=%+v variants=%+v job=%+v",
			repo.assets[1], repo.variants, repo.jobs[1])
	}
}

type processorStub struct {
	result *ProcessResult
	err    error
	calls  int
}

type blockingProcessor struct {
	release <-chan struct{}
	started chan struct{}
}

func (p *blockingProcessor) Process(
	ctx context.Context,
	_ *domainmedia.MediaAsset,
	job *domainmedia.MediaProcessingJob,
) (*ProcessResult, error) {
	p.started <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
		return &ProcessResult{Variants: []*domainmedia.MediaVariant{{
			AssetID: job.AssetID, ProfileVersion: job.ProfileVersion,
			Role: domainmedia.VariantRoleBaseline, State: domainmedia.VariantStateReady,
		}}}, nil
	}
}

type cancelingProcessor struct{}

func (*cancelingProcessor) Process(
	ctx context.Context,
	_ *domainmedia.MediaAsset,
	_ *domainmedia.MediaProcessingJob,
) (*ProcessResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type concurrentProcessingRepository struct {
	mu             sync.Mutex
	assets         map[int64]*domainmedia.MediaAsset
	jobs           map[int64]*domainmedia.MediaProcessingJob
	claims         int
	tokens         []string
	ownedUpdates   int
	blockHeartbeat bool
	extendCalls    int
	variants       []*domainmedia.MediaVariant
}

func newConcurrentProcessingRepository(
	now time.Time,
	count int,
) *concurrentProcessingRepository {
	repository := &concurrentProcessingRepository{
		assets: make(map[int64]*domainmedia.MediaAsset, count),
		jobs:   make(map[int64]*domainmedia.MediaProcessingJob, count),
	}
	for index := 1; index <= count; index++ {
		id := int64(index)
		repository.assets[id] = &domainmedia.MediaAsset{
			ID: id, Kind: domainmedia.AssetKindVideo, State: domainmedia.AssetStateUploaded,
		}
		repository.jobs[id] = &domainmedia.MediaProcessingJob{
			ID: id, AssetID: id, ProfileVersion: "v1",
			State: domainmedia.JobStatePending, MaxAttempts: 5, NextAttemptAt: now,
		}
	}
	return repository
}

func (r *concurrentProcessingRepository) FindAssetByID(
	_ context.Context,
	assetID int64,
) (*domainmedia.MediaAsset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	asset := r.assets[assetID]
	if asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	copy := *asset
	return &copy, nil
}

func (r *concurrentProcessingRepository) FindProcessingJobByAsset(
	_ context.Context,
	assetID int64,
) (*domainmedia.MediaProcessingJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[assetID]
	if job == nil {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	copy := *job
	return &copy, nil
}

func (r *concurrentProcessingRepository) UpdateAsset(
	_ context.Context,
	asset *domainmedia.MediaAsset,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *asset
	r.assets[asset.ID] = &copy
	return nil
}

func (*concurrentProcessingRepository) UpsertVariants(
	context.Context,
	[]*domainmedia.MediaVariant,
) error {
	return nil
}

func (r *concurrentProcessingRepository) LeaseProcessingJob(
	_ context.Context,
	assetID int64,
	profileVersion string,
	token string,
	_ time.Time,
	leaseUntil time.Time,
) (*domainmedia.MediaProcessingJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[assetID]
	if job == nil || job.ProfileVersion != profileVersion ||
		(job.State != domainmedia.JobStatePending &&
			job.State != domainmedia.JobStateRetryable) {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	job.State = domainmedia.JobStateProcessing
	job.Attempts++
	job.LeaseOwner = token
	job.LeaseUntil = &leaseUntil
	r.claims++
	r.tokens = append(r.tokens, token)
	copy := *job
	return &copy, nil
}

func (r *concurrentProcessingRepository) LeaseProcessingJobs(
	ctx context.Context,
	token string,
	now time.Time,
	leaseUntil time.Time,
	limit int,
) ([]*domainmedia.MediaProcessingJob, error) {
	r.mu.Lock()
	var assetID int64
	for id, job := range r.jobs {
		if job.State == domainmedia.JobStatePending ||
			job.State == domainmedia.JobStateRetryable {
			assetID = id
			break
		}
	}
	r.mu.Unlock()
	if assetID == 0 || limit <= 0 {
		return nil, nil
	}
	job, err := r.LeaseProcessingJob(
		ctx, assetID, "v1", token, now, leaseUntil,
	)
	if err != nil {
		return nil, err
	}
	return []*domainmedia.MediaProcessingJob{job}, nil
}

func (*concurrentProcessingRepository) UpdateProcessingJob(
	context.Context,
	*domainmedia.MediaProcessingJob,
) error {
	return nil
}

func (r *concurrentProcessingRepository) UpdateProcessingJobOwned(
	_ context.Context,
	job *domainmedia.MediaProcessingJob,
	token string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.jobs[job.AssetID]
	if current == nil || current.LeaseOwner != token ||
		current.State != domainmedia.JobStateProcessing {
		return domainmedia.ErrProcessingJobLeaseLost
	}
	copy := *job
	r.jobs[job.AssetID] = &copy
	r.ownedUpdates++
	return nil
}

func (r *concurrentProcessingRepository) FinalizeProcessingJob(
	_ context.Context,
	finalization *domainmedia.ProcessingFinalization,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if finalization == nil || finalization.Job == nil {
		return domainmedia.ErrProcessingJobLeaseLost
	}
	current := r.jobs[finalization.Job.AssetID]
	if current == nil || current.State != domainmedia.JobStateProcessing ||
		current.LeaseOwner != finalization.LeaseOwner ||
		current.LeaseUntil == nil || !current.LeaseUntil.After(finalization.CommittedAt) {
		return domainmedia.ErrProcessingJobLeaseLost
	}
	if finalization.Asset != nil {
		copyAsset := *finalization.Asset
		r.assets[copyAsset.ID] = &copyAsset
	}
	r.variants = append(r.variants, finalization.Variants...)
	copyJob := *finalization.Job
	r.jobs[copyJob.AssetID] = &copyJob
	r.ownedUpdates++
	return nil
}

func (r *concurrentProcessingRepository) ExtendProcessingLease(
	ctx context.Context,
	_ int64,
	_ string,
	_ time.Duration,
) error {
	r.mu.Lock()
	r.extendCalls++
	shouldBlock := r.blockHeartbeat && r.extendCalls > 1
	r.mu.Unlock()
	if shouldBlock {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (*concurrentProcessingRepository) CreateCleanupTasks(
	context.Context,
	[]*domainmedia.CleanupTask,
) error {
	return nil
}

func (p *processorStub) Process(context.Context, *domainmedia.MediaAsset, *domainmedia.MediaProcessingJob) (*ProcessResult, error) {
	p.calls++
	return p.result, p.err
}

type processingRepositoryStub struct {
	asset    *domainmedia.MediaAsset
	job      *domainmedia.MediaProcessingJob
	variants []*domainmedia.MediaVariant
}

func (r *processingRepositoryStub) FindAssetByID(context.Context, int64) (*domainmedia.MediaAsset, error) {
	if r.asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}

	return r.asset, nil
}

func (r *processingRepositoryStub) FindProcessingJobByAsset(
	context.Context,
	int64,
) (*domainmedia.MediaProcessingJob, error) {
	if r.job == nil {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	return r.job, nil
}

func (r *processingRepositoryStub) UpdateAsset(_ context.Context, asset *domainmedia.MediaAsset) error {
	r.asset = asset
	return nil
}

func (r *processingRepositoryStub) UpsertVariants(_ context.Context, variants []*domainmedia.MediaVariant) error {
	r.variants = variants
	return nil
}

func (r *processingRepositoryStub) LeaseProcessingJob(_ context.Context, assetID int64, profileVersion, owner string, now time.Time, leaseUntil time.Time) (*domainmedia.MediaProcessingJob, error) {
	if r.job == nil || r.job.AssetID != assetID || r.job.ProfileVersion != profileVersion ||
		(r.job.State != domainmedia.JobStatePending && r.job.State != domainmedia.JobStateRetryable) {
		return nil, domainmedia.ErrProcessingJobNotFound
	}
	r.job.State = domainmedia.JobStateProcessing
	r.job.Attempts++
	r.job.LeaseOwner = owner
	r.job.LeaseUntil = &leaseUntil
	copyJob := *r.job
	return &copyJob, nil
}

func (r *processingRepositoryStub) LeaseProcessingJobs(context.Context, string, time.Time, time.Time, int) ([]*domainmedia.MediaProcessingJob, error) {
	return nil, nil
}

func (r *processingRepositoryStub) UpdateProcessingJob(_ context.Context, job *domainmedia.MediaProcessingJob) error {
	r.job = job
	return nil
}

func (r *processingRepositoryStub) UpdateProcessingJobOwned(_ context.Context, job *domainmedia.MediaProcessingJob, _ string) error {
	r.job = job
	return nil
}

func (r *processingRepositoryStub) FinalizeProcessingJob(
	_ context.Context,
	finalization *domainmedia.ProcessingFinalization,
) error {
	if finalization == nil || finalization.Job == nil || r.job == nil ||
		r.job.State != domainmedia.JobStateProcessing ||
		r.job.LeaseOwner != finalization.LeaseOwner ||
		r.job.LeaseUntil == nil || !r.job.LeaseUntil.After(finalization.CommittedAt) {
		return domainmedia.ErrProcessingJobLeaseLost
	}
	if finalization.Asset != nil {
		r.asset = finalization.Asset
	}
	r.variants = append(r.variants, finalization.Variants...)
	copyJob := *finalization.Job
	r.job = &copyJob
	return nil
}

func (*processingRepositoryStub) ExtendProcessingLease(context.Context, int64, string, time.Duration) error {
	return nil
}

func (*processingRepositoryStub) CreateCleanupTasks(context.Context, []*domainmedia.CleanupTask) error {
	return nil
}

func timePointer(value time.Time) *time.Time {
	return &value
}
