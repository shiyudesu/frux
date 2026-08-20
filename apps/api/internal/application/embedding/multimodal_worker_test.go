package applicationembedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type multimodalWorkerRepositoryStub struct {
	mutex          sync.Mutex
	claimJobs      []*domainembedding.MultimodalEmbeddingJob
	heartbeatOwned bool
	heartbeatCalls int
	retries        map[int64]string
	retryAfter     map[int64]time.Duration
	terminals      map[int64]string
	completed      map[int64]*domainembedding.MultimodalVectorFact
	handoffs       map[int64]*domainembedding.MultimodalEmbeddingJob
}

func (r *multimodalWorkerRepositoryStub) HandoffMultimodalJob(_ context.Context, job *domainembedding.MultimodalEmbeddingJob) (*domainembedding.MultimodalEmbeddingJob, bool, bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.handoffs == nil {
		r.handoffs = map[int64]*domainembedding.MultimodalEmbeddingJob{}
	}
	r.handoffs[job.VideoID] = job.Clone()
	return job.Clone(), false, true, nil
}

func (r *multimodalWorkerRepositoryStub) ClaimMultimodalJobs(context.Context, string, time.Duration, int) ([]*domainembedding.MultimodalEmbeddingJob, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	jobs := r.claimJobs
	r.claimJobs = nil
	return jobs, nil
}

func (r *multimodalWorkerRepositoryStub) HeartbeatMultimodalJob(context.Context, int64, string, time.Duration) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.heartbeatCalls++
	return r.heartbeatOwned, nil
}

func (r *multimodalWorkerRepositoryStub) RetryMultimodalJob(_ context.Context, jobID int64, _ string, failure string, retryAfter time.Duration) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.retries == nil {
		r.retries = map[int64]string{}
		r.retryAfter = map[int64]time.Duration{}
	}
	r.retries[jobID] = failure
	r.retryAfter[jobID] = retryAfter
	return true, nil
}

func (r *multimodalWorkerRepositoryStub) CompleteMultimodalJob(_ context.Context, jobID int64, _ string, fact *domainembedding.MultimodalVectorFact) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.completed == nil {
		r.completed = map[int64]*domainembedding.MultimodalVectorFact{}
	}
	r.completed[jobID] = fact.Clone()
	return true, nil
}

func (r *multimodalWorkerRepositoryStub) TerminalMultimodalJob(_ context.Context, jobID int64, _ string, failure string) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.terminals == nil {
		r.terminals = map[int64]string{}
	}
	r.terminals[jobID] = failure
	return true, nil
}

type multimodalVideoReaderStub struct {
	mutex    sync.Mutex
	video    *domainvideo.Video
	err      error
	calls    int
	mutateAt int
	mutate   func(*domainvideo.Video)
}

func (r *multimodalVideoReaderStub) FindByIDAnyStatus(context.Context, int64) (*domainvideo.Video, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	if r.video == nil {
		return nil, domainvideo.ErrVideoNotFound
	}
	cloned := *r.video
	if cloned.PublishedAt != nil {
		publishedAt := *cloned.PublishedAt
		cloned.PublishedAt = &publishedAt
	}
	if r.mutate != nil && r.calls >= r.mutateAt {
		r.mutate(&cloned)
	}
	return &cloned, nil
}

type multimodalAssetReaderStub struct {
	assets map[int64]*domainmedia.MediaAsset
	err    error
}

func (r multimodalAssetReaderStub) FindAssetByID(_ context.Context, id int64) (*domainmedia.MediaAsset, error) {
	if r.err != nil {
		return nil, r.err
	}
	asset := r.assets[id]
	if asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	cloned := *asset
	return &cloned, nil
}

type multimodalPreparerStub struct {
	mutex  sync.Mutex
	calls  int
	result *PreparedMultimodalMedia
	err    error
}

func (p *multimodalPreparerStub) PrepareMultimodalMedia(context.Context, MultimodalMediaPreparationRequest) (*PreparedMultimodalMedia, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.calls++
	return p.result.Clone(), p.err
}

type multimodalProviderStub struct {
	mutex   sync.Mutex
	calls   int
	run     func(context.Context, MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error)
	started chan struct{}
}

func (p *multimodalProviderStub) EmbedVideoContent(ctx context.Context, request MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	p.mutex.Lock()
	p.calls++
	started := p.started
	p.mutex.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	return p.run(ctx, request)
}

func (p *multimodalProviderStub) EmbedQueryText(context.Context, MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	return nil, errors.New("unexpected query embedding")
}

func TestMultimodalJobWorkerCompletesCurrentValidatedSource(t *testing.T) {
	fixture := newMultimodalWorkerFixture(t)
	if err := fixture.worker.processClaimedJob(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	if fixture.repository.completed[fixture.job.ID] == nil || len(fixture.repository.retries) != 0 ||
		len(fixture.repository.terminals) != 0 || fixture.videos.calls != 3 || fixture.preparer.calls != 1 ||
		fixture.provider.calls != 1 {
		t.Fatalf("unexpected success state: completed=%#v retries=%v terminals=%v video_calls=%d prepare=%d provider=%d",
			fixture.repository.completed, fixture.repository.retries, fixture.repository.terminals,
			fixture.videos.calls, fixture.preparer.calls, fixture.provider.calls)
	}
}

func TestMultimodalJobWorkerRejectsIneligibleBeforePreparation(t *testing.T) {
	fixture := newMultimodalWorkerFixture(t)
	fixture.videos.video.Visibility = domainvideo.VisibilityPrivate
	if err := fixture.worker.processClaimedJob(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	if fixture.repository.terminals[fixture.job.ID] != domainembedding.MultimodalFailureStaleSource ||
		fixture.preparer.calls != 0 || fixture.provider.calls != 0 {
		t.Fatalf("private source crossed boundary: terminals=%v prepare=%d provider=%d",
			fixture.repository.terminals, fixture.preparer.calls, fixture.provider.calls)
	}
}

func TestMultimodalJobWorkerRefreshesSourceChangedDuringInference(t *testing.T) {
	fixture := newMultimodalWorkerFixture(t)
	fixture.videos.mutateAt = 3
	fixture.videos.mutate = func(video *domainvideo.Video) {
		video.Title = "changed"
		video.Version++
	}
	if err := fixture.worker.processClaimedJob(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	refreshed := fixture.repository.handoffs[fixture.job.VideoID]
	if refreshed == nil || refreshed.SourceHash == fixture.job.SourceHash ||
		len(fixture.repository.completed) != 0 || len(fixture.repository.terminals) != 0 {
		t.Fatalf("stale result was not refreshed: handoff=%#v completed=%v terminals=%v",
			refreshed, fixture.repository.completed, fixture.repository.terminals)
	}
}

func TestMultimodalJobWorkerClassifiesInvalidVectorAndProviderFailure(t *testing.T) {
	invalid := newMultimodalWorkerFixture(t)
	invalid.provider.run = func(_ context.Context, request MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		values := make([]float64, request.Contract.Dimension)
		return &MultimodalEmbeddingResult{Identity: domainembedding.MultimodalVectorIdentity{
			Contract: request.Contract, SourceHash: request.SourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(values),
		}, Vector: values}, nil
	}
	if err := invalid.worker.processClaimedJob(context.Background(), invalid.job); err != nil {
		t.Fatal(err)
	}
	if invalid.repository.terminals[invalid.job.ID] != domainembedding.MultimodalFailureInvalidVector {
		t.Fatalf("invalid vector transition=%v", invalid.repository.terminals)
	}

	retryable := newMultimodalWorkerFixture(t)
	retryable.provider.run = func(context.Context, MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		return nil, &MultimodalProviderError{Retryable: true, RetryAfter: 2 * time.Second, Err: errors.New("secret provider detail")}
	}
	if err := retryable.worker.processClaimedJob(context.Background(), retryable.job); err != nil {
		t.Fatal(err)
	}
	if retryable.repository.retries[retryable.job.ID] != domainembedding.MultimodalFailureProviderRetryable ||
		retryable.repository.retryAfter[retryable.job.ID] != 2*time.Second {
		t.Fatalf("retry classification=%v retry_after=%v", retryable.repository.retries, retryable.repository.retryAfter)
	}

	terminal := newMultimodalWorkerFixture(t)
	terminal.provider.run = func(context.Context, MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		return nil, &MultimodalProviderError{Retryable: false, Err: errors.New("raw provider detail")}
	}
	if err := terminal.worker.processClaimedJob(context.Background(), terminal.job); err != nil {
		t.Fatal(err)
	}
	if terminal.repository.terminals[terminal.job.ID] != domainembedding.MultimodalFailureProviderTerminal {
		t.Fatalf("terminal classification=%v", terminal.repository.terminals)
	}
}

func TestMultimodalJobWorkerBoundsCancellationIgnoringProvider(t *testing.T) {
	fixture := newMultimodalWorkerFixture(t)
	fixture.worker.config.ProviderDeadline = 100 * time.Millisecond
	fixture.worker.slots = make(chan struct{}, 1)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fixture.provider.started = started
	fixture.provider.run = func(context.Context, MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		<-release
		return nil, errors.New("released")
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- fixture.worker.processClaimedJob(context.Background(), fixture.job) }()
	<-started
	second := leasedMultimodalJob(t, 2, fixture.job.VideoID, fixture.job.Contract, fixture.job.SourceHash, fixture.worker.now())
	if err := fixture.worker.processClaimedJob(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	fixture.repository.mutex.Lock()
	secondFailure := fixture.repository.retries[second.ID]
	fixture.repository.mutex.Unlock()
	if secondFailure != domainembedding.MultimodalFailureAdmission {
		t.Fatalf("admission transition=%s", secondFailure)
	}
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider deadline did not bound processing")
	}
	fixture.repository.mutex.Lock()
	firstFailure := fixture.repository.retries[fixture.job.ID]
	fixture.repository.mutex.Unlock()
	if firstFailure != domainembedding.MultimodalFailureTimeout {
		t.Fatalf("timeout transition=%s", firstFailure)
	}
	close(release)
}

func TestMultimodalJobWorkerStopsOnHeartbeatLossAndShutsDown(t *testing.T) {
	fixture := newMultimodalWorkerFixture(t)
	fixture.repository.heartbeatOwned = false
	fixture.worker.config.ProviderDeadline = 3 * time.Second
	fixture.worker.config.HeartbeatInterval = time.Second
	fixture.worker.config.LeaseTTL = 4 * time.Second
	fixture.provider.run = func(ctx context.Context, _ MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	started := time.Now()
	if err := fixture.worker.processClaimedJob(context.Background(), fixture.job); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < time.Second || elapsed > 2*time.Second {
		t.Fatalf("heartbeat loss elapsed=%s", elapsed)
	}
	if len(fixture.repository.retries) != 0 || len(fixture.repository.terminals) != 0 ||
		len(fixture.repository.completed) != 0 {
		t.Fatalf("lease-lost worker wrote a transition: retry=%v terminal=%v complete=%v",
			fixture.repository.retries, fixture.repository.terminals, fixture.repository.completed)
	}

	shutdown := newMultimodalWorkerFixture(t)
	shutdown.repository.claimJobs = []*domainembedding.MultimodalEmbeddingJob{shutdown.job}
	providerStarted := make(chan struct{}, 1)
	shutdown.provider.started = providerStarted
	shutdown.provider.run = func(ctx context.Context, _ MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- shutdown.worker.Run(runCtx, "worker") }()
	<-providerStarted
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not shut down within its bound")
	}
}

type multimodalWorkerFixture struct {
	worker     *MultimodalJobWorker
	job        *domainembedding.MultimodalEmbeddingJob
	repository *multimodalWorkerRepositoryStub
	videos     *multimodalVideoReaderStub
	preparer   *multimodalPreparerStub
	provider   *multimodalProviderStub
}

func newMultimodalWorkerFixture(t testing.TB) multimodalWorkerFixture {
	t.Helper()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	contract := testWorkerMultimodalContract(t)
	video := readableMultimodalVideo(now)
	text, err := domainembedding.CanonicalizePublicVideoText(video.Title, video.Description, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := MultimodalVideoSourceHash(
		contract, text, video.MediaURL, video.CoverURL,
		video.MediaAssetID, video.CoverAssetID, video.MediaProfileVersion, video.Version,
	)
	job := leasedMultimodalJob(t, 1, video.ID, contract, sourceHash, now)
	imageContent := []byte("prepared-image")
	imageDigest := sha256.Sum256(imageContent)
	repository := &multimodalWorkerRepositoryStub{heartbeatOwned: true}
	videos := &multimodalVideoReaderStub{video: video}
	preparer := &multimodalPreparerStub{result: &PreparedMultimodalMedia{Images: []PreparedMultimodalImage{{
		MIMEType: "image/jpeg", Width: 2, Height: 2,
		Digest: hex.EncodeToString(imageDigest[:]), Content: imageContent,
	}}}}
	provider := &multimodalProviderStub{}
	provider.run = func(_ context.Context, request MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
		values := make([]float64, request.Contract.Dimension)
		values[0] = 1
		return &MultimodalEmbeddingResult{
			Identity: domainembedding.MultimodalVectorIdentity{
				Contract: request.Contract, SourceHash: request.SourceHash,
				VectorDigest: domainembedding.MultimodalVectorDigest(values),
			},
			Vector: values,
		}, nil
	}
	config := MultimodalJobWorkerConfig{
		Contract: contract, MaxAttempts: 5, MaxVideoTextRunes: 2048,
		LeaseTTL: 4 * time.Second, HeartbeatInterval: time.Second,
		PollInterval: 100 * time.Millisecond, ProviderDeadline: 500 * time.Millisecond,
		AdmissionLimit: 1, RetryBase: time.Second, RetryMax: time.Minute,
		ShutdownTimeout: time.Second, MaxImages: 4, MaxImageBytes: 2 * 1024 * 1024,
		MaxTotalImageBytes: 6 * 1024 * 1024, MaxImagePixels: 512 * 512,
		AllowedMIMETypes: []string{"image/jpeg"},
	}
	worker, err := NewMultimodalJobWorker(
		repository, videos,
		multimodalAssetReaderStub{assets: map[int64]*domainmedia.MediaAsset{
			video.MediaAssetID: {ID: video.MediaAssetID, State: domainmedia.AssetStateReady, ObjectKey: "media/source.mp4"},
			video.CoverAssetID: {ID: video.CoverAssetID, State: domainmedia.AssetStateReady, ObjectKey: "media/cover.jpg"},
		}},
		preparer, provider, config,
	)
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return now }
	return multimodalWorkerFixture{
		worker: worker, job: job, repository: repository,
		videos: videos, preparer: preparer, provider: provider,
	}
}

func readableMultimodalVideo(now time.Time) *domainvideo.Video {
	video := domainvideo.RestoreVideoWithMedia(
		91, 7, "title", "description",
		"https://media.example/video.mp4", "https://media.example/cover.jpg",
		domainvideo.StatusPublished, domainvideo.VisibilityPublic,
		0, 0, 0, &now, now.Add(-time.Hour), now, "key",
		101, domainmedia.MediaStatusReady, "", nil, 102,
	)
	video.MediaProfileVersion = "v2"
	video.Version = 3
	return video
}

func testWorkerMultimodalContract(t testing.TB) domainembedding.MultimodalContractIdentity {
	t.Helper()
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", "revision", domainembedding.MinMultimodalDimension,
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

func leasedMultimodalJob(
	t testing.TB,
	id int64,
	videoID int64,
	contract domainembedding.MultimodalContractIdentity,
	sourceHash string,
	now time.Time,
) *domainembedding.MultimodalEmbeddingJob {
	t.Helper()
	leaseUntil := now.Add(time.Hour)
	job := domainembedding.RestoreMultimodalEmbeddingJob(
		id, videoID, contract, sourceHash, domainembedding.MultimodalJobStateLeased,
		1, 5, "claim-token", &leaseUntil, now, "", now, now, nil,
	)
	if job == nil {
		t.Fatal("failed to restore leased multimodal job")
	}
	return job
}
