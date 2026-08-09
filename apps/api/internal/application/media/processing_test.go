package applicationmedia

import (
	"context"
	"errors"
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
	if processor.calls != 0 || len(worker.wakeups) != 1 {
		t.Fatalf("processor calls=%d queued=%d", processor.calls, len(worker.wakeups))
	}
	if err := worker.SignalRequested(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(worker.wakeups) != 1 {
		t.Fatalf("full scheduler changed queued wakeups: %d", len(worker.wakeups))
	}
	stale := NewProcessingRequestedEvent(20, "v2", now)
	if err := worker.SignalRequested(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	if len(worker.wakeups) != 1 {
		t.Fatalf("stale wakeup was queued: %d", len(worker.wakeups))
	}
}

type processorStub struct {
	result *ProcessResult
	err    error
	calls  int
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
	return r.job, nil
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

func (*processingRepositoryStub) ExtendProcessingLease(context.Context, int64, string, time.Time) error {
	return nil
}

func (*processingRepositoryStub) CreateCleanupTasks(context.Context, []*domainmedia.CleanupTask) error {
	return nil
}
