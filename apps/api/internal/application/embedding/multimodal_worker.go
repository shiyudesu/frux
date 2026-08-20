package applicationembedding

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type MultimodalWorkerRepository interface {
	HandoffMultimodalJob(context.Context, *domainembedding.MultimodalEmbeddingJob) (*domainembedding.MultimodalEmbeddingJob, bool, bool, error)
	ClaimMultimodalJobs(context.Context, string, time.Duration, int) ([]*domainembedding.MultimodalEmbeddingJob, error)
	HeartbeatMultimodalJob(context.Context, int64, string, time.Duration) (bool, error)
	RetryMultimodalJob(context.Context, int64, string, string, time.Duration) (bool, error)
	CompleteMultimodalJob(context.Context, int64, string, *domainembedding.MultimodalVectorFact) (bool, error)
	TerminalMultimodalJob(context.Context, int64, string, string) (bool, error)
}

type MultimodalVideoReader interface {
	FindByIDAnyStatus(context.Context, int64) (*domainvideo.Video, error)
}

type MultimodalMediaAssetReader interface {
	FindAssetByID(context.Context, int64) (*domainmedia.MediaAsset, error)
}

type MultimodalJobWorkerConfig struct {
	Contract           domainembedding.MultimodalContractIdentity
	MaxAttempts        int
	MaxVideoTextRunes  int
	LeaseTTL           time.Duration
	HeartbeatInterval  time.Duration
	PollInterval       time.Duration
	ProviderDeadline   time.Duration
	AdmissionLimit     int
	RetryBase          time.Duration
	RetryMax           time.Duration
	ShutdownTimeout    time.Duration
	MaxImages          int
	MaxImageBytes      int
	MaxTotalImageBytes int
	MaxImagePixels     int64
	AllowedMIMETypes   []string
}

type MultimodalProviderError struct {
	Retryable  bool
	RetryAfter time.Duration
	Err        error
}

func (e *MultimodalProviderError) Error() string {
	if e == nil || e.Err == nil {
		return "multimodal provider failed"
	}
	return "multimodal provider failed: " + e.Err.Error()
}

func (e *MultimodalProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type MultimodalJobWorker struct {
	repository MultimodalWorkerRepository
	videos     MultimodalVideoReader
	assets     MultimodalMediaAssetReader
	preparer   MultimodalMediaPreparer
	provider   MultimodalEmbeddingProvider
	config     MultimodalJobWorkerConfig
	slots      chan struct{}
	now        func() time.Time
}

func NewMultimodalJobWorker(
	repository MultimodalWorkerRepository,
	videos MultimodalVideoReader,
	assets MultimodalMediaAssetReader,
	preparer MultimodalMediaPreparer,
	provider MultimodalEmbeddingProvider,
	config MultimodalJobWorkerConfig,
) (*MultimodalJobWorker, error) {
	if repository == nil || videos == nil || assets == nil || preparer == nil || provider == nil ||
		!validMultimodalWorkerConfig(config) {
		return nil, ErrInvalidMultimodalHandoff
	}
	return &MultimodalJobWorker{
		repository: repository, videos: videos, assets: assets,
		preparer: preparer, provider: provider, config: config,
		slots: make(chan struct{}, config.AdmissionLimit),
		now:   func() time.Time { return time.Now().UTC() },
	}, nil
}

func (w *MultimodalJobWorker) Run(ctx context.Context, owner string) error {
	owner = strings.TrimSpace(owner)
	if w == nil || owner == "" {
		return ErrInvalidMultimodalHandoff
	}
	var active sync.WaitGroup
	claim := func() error {
		jobs, err := w.repository.ClaimMultimodalJobs(ctx, owner, w.config.LeaseTTL, w.config.AdmissionLimit)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			job := job
			active.Add(1)
			go func() {
				defer active.Done()
				_ = w.processClaimedJob(ctx, job)
			}()
		}
		return nil
	}
	if err := claim(); err != nil {
		return err
	}
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done := make(chan struct{})
			go func() {
				active.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(w.config.ShutdownTimeout):
			}
			return nil
		case <-ticker.C:
			if err := claim(); err != nil {
				return err
			}
		}
	}
}

func (w *MultimodalJobWorker) processClaimedJob(ctx context.Context, job *domainembedding.MultimodalEmbeddingJob) error {
	if w == nil || job == nil || job.State != domainembedding.MultimodalJobStateLeased || job.ClaimToken == "" {
		return domainembedding.ErrInvalidMultimodalJob
	}
	attemptCtx, cancelAttempt := context.WithCancel(ctx)
	heartbeatStop := make(chan struct{})
	heartbeatLost := make(chan error, 1)
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		ticker := time.NewTicker(w.config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatStop:
				return
			case <-attemptCtx.Done():
				return
			case <-ticker.C:
				owned, err := w.repository.HeartbeatMultimodalJob(
					attemptCtx, job.ID, job.ClaimToken, w.config.LeaseTTL,
				)
				if err != nil || !owned {
					select {
					case heartbeatLost <- errors.Join(domainembedding.ErrMultimodalLeaseLost, err):
					default:
					}
					cancelAttempt()
					return
				}
			}
		}
	}()
	defer func() {
		close(heartbeatStop)
		cancelAttempt()
		heartbeat.Wait()
	}()

	source, err := w.loadCurrentSource(attemptCtx, job)
	if err != nil {
		if attemptCtx.Err() != nil || receiveHeartbeatLoss(heartbeatLost) != nil {
			return nil
		}
		return w.finishSourceFailure(attemptCtx, job, err)
	}
	if source.sourceHash != job.SourceHash {
		return w.refreshSource(attemptCtx, job, source.sourceHash)
	}
	prepared, err := w.preparer.PrepareMultimodalMedia(attemptCtx, applicationMediaRequest(w.config, source))
	if err != nil {
		if attemptCtx.Err() != nil || receiveHeartbeatLoss(heartbeatLost) != nil {
			return nil
		}
		if errors.Is(err, ErrInvalidMultimodalMediaPreparation) {
			return w.terminal(attemptCtx, job, domainembedding.MultimodalFailureInvalidInput)
		}
		return w.retry(attemptCtx, job, domainembedding.MultimodalFailureProviderRetryable, 0)
	}
	if lost := receiveHeartbeatLoss(heartbeatLost); lost != nil {
		return nil
	}
	current, err := w.loadCurrentSource(attemptCtx, job)
	if err != nil {
		if attemptCtx.Err() != nil || receiveHeartbeatLoss(heartbeatLost) != nil {
			return nil
		}
		return w.finishSourceFailure(attemptCtx, job, err)
	}
	if current.sourceHash != job.SourceHash {
		return w.refreshSource(attemptCtx, job, current.sourceHash)
	}
	request, err := NewMultimodalVideoEmbeddingRequestForSource(
		job.Contract, job.SourceHash, current.content,
		w.config.MaxVideoTextRunes, prepared.Images,
	)
	if err != nil {
		return w.terminal(attemptCtx, job, domainembedding.MultimodalFailureInvalidInput)
	}
	select {
	case w.slots <- struct{}{}:
	case <-attemptCtx.Done():
		return nil
	default:
		return w.retry(attemptCtx, job, domainembedding.MultimodalFailureAdmission, 0)
	}
	type providerResult struct {
		result *MultimodalEmbeddingResult
		err    error
	}
	providerDone := make(chan providerResult, 1)
	providerCtx, cancelProvider := context.WithTimeout(attemptCtx, w.config.ProviderDeadline)
	defer cancelProvider()
	go func() {
		defer func() { <-w.slots }()
		result, err := w.provider.EmbedVideoContent(providerCtx, request)
		providerDone <- providerResult{result: result, err: err}
	}()
	var result *MultimodalEmbeddingResult
	select {
	case lost := <-heartbeatLost:
		_ = lost
		return nil
	case completed := <-providerDone:
		if completed.err != nil {
			if attemptCtx.Err() != nil || receiveHeartbeatLoss(heartbeatLost) != nil {
				return nil
			}
			return w.finishProviderFailure(attemptCtx, job, completed.err)
		}
		result = completed.result
	case <-providerCtx.Done():
		if attemptCtx.Err() != nil {
			return nil
		}
		return w.retry(attemptCtx, job, domainembedding.MultimodalFailureTimeout, 0)
	}
	validated, err := ValidateMultimodalEmbeddingResult(job.Contract, job.SourceHash, result)
	if err != nil {
		return w.terminal(attemptCtx, job, domainembedding.MultimodalFailureInvalidVector)
	}
	current, err = w.loadCurrentSource(attemptCtx, job)
	if err != nil {
		if attemptCtx.Err() != nil || receiveHeartbeatLoss(heartbeatLost) != nil {
			return nil
		}
		return w.finishSourceFailure(attemptCtx, job, err)
	}
	if current.sourceHash != job.SourceHash {
		return w.refreshSource(attemptCtx, job, current.sourceHash)
	}
	fact, err := domainembedding.NewMultimodalVectorFact(job.VideoID, validated, w.now())
	if err != nil {
		return w.terminal(attemptCtx, job, domainembedding.MultimodalFailureInvalidVector)
	}
	_, err = w.repository.CompleteMultimodalJob(attemptCtx, job.ID, job.ClaimToken, fact)
	return err
}

type multimodalCurrentSource struct {
	content        MultimodalPublicVideoContent
	videoObjectKey string
	coverObjectKey string
	sourceHash     string
}

func (w *MultimodalJobWorker) loadCurrentSource(
	ctx context.Context,
	job *domainembedding.MultimodalEmbeddingJob,
) (*multimodalCurrentSource, error) {
	video, err := w.videos.FindByIDAnyStatus(ctx, job.VideoID)
	if err != nil {
		if errors.Is(err, domainvideo.ErrVideoNotFound) {
			return nil, ErrIneligibleMultimodalContent
		}
		return nil, err
	}
	if video == nil || !video.IsPubliclyReadable() || video.PublishedAt == nil ||
		video.MediaAssetID <= 0 || strings.TrimSpace(video.MediaURL) == "" {
		return nil, ErrIneligibleMultimodalContent
	}
	mediaAsset, err := w.assets.FindAssetByID(ctx, video.MediaAssetID)
	if err != nil {
		if errors.Is(err, domainmedia.ErrMediaAssetNotFound) {
			return nil, ErrIneligibleMultimodalContent
		}
		return nil, err
	}
	if mediaAsset == nil || mediaAsset.State != domainmedia.AssetStateReady || strings.TrimSpace(mediaAsset.ObjectKey) == "" {
		return nil, ErrIneligibleMultimodalContent
	}
	coverObjectKey := ""
	if video.CoverAssetID > 0 {
		coverAsset, err := w.assets.FindAssetByID(ctx, video.CoverAssetID)
		if err != nil {
			if errors.Is(err, domainmedia.ErrMediaAssetNotFound) {
				return nil, ErrIneligibleMultimodalContent
			}
			return nil, err
		}
		if coverAsset == nil || coverAsset.State != domainmedia.AssetStateReady || strings.TrimSpace(coverAsset.ObjectKey) == "" {
			return nil, ErrIneligibleMultimodalContent
		}
		coverObjectKey = coverAsset.ObjectKey
	}
	text, err := domainembedding.CanonicalizePublicVideoText(video.Title, video.Description, w.config.MaxVideoTextRunes)
	if err != nil {
		return nil, ErrInvalidMultimodalHandoff
	}
	content := MultimodalPublicVideoContent{
		Title: video.Title, Description: video.Description,
		Published: true, Public: true, MediaReady: true, SourceCurrent: true,
	}
	return &multimodalCurrentSource{
		content: content, videoObjectKey: mediaAsset.ObjectKey, coverObjectKey: coverObjectKey,
		sourceHash: MultimodalVideoSourceHash(
			job.Contract, text, video.MediaURL, video.CoverURL,
			video.MediaAssetID, video.CoverAssetID, video.MediaProfileVersion, video.Version,
		),
	}, nil
}

func applicationMediaRequest(config MultimodalJobWorkerConfig, source *multimodalCurrentSource) MultimodalMediaPreparationRequest {
	return MultimodalMediaPreparationRequest{
		VideoObjectKey: source.videoObjectKey, CoverObjectKey: source.coverObjectKey,
		FrameSamplingPolicy:      config.Contract.FrameSamplingPolicy,
		ImagePreprocessingPolicy: config.Contract.ImagePreprocessingPolicy,
		MaxImages:                config.MaxImages, MaxBytesEach: config.MaxImageBytes,
		MaxTotalBytes: config.MaxTotalImageBytes, MaxPixelsEach: config.MaxImagePixels,
		AllowedMIMETypes: append([]string(nil), config.AllowedMIMETypes...),
	}
}

func (w *MultimodalJobWorker) finishSourceFailure(ctx context.Context, job *domainembedding.MultimodalEmbeddingJob, err error) error {
	if errors.Is(err, ErrIneligibleMultimodalContent) || errors.Is(err, ErrInvalidMultimodalHandoff) {
		return w.terminal(ctx, job, domainembedding.MultimodalFailureStaleSource)
	}
	return w.retry(ctx, job, domainembedding.MultimodalFailureProviderRetryable, 0)
}

func (w *MultimodalJobWorker) finishProviderFailure(ctx context.Context, job *domainembedding.MultimodalEmbeddingJob, err error) error {
	var providerError *MultimodalProviderError
	if errors.As(err, &providerError) {
		if !providerError.Retryable {
			return w.terminal(ctx, job, domainembedding.MultimodalFailureProviderTerminal)
		}
		return w.retry(ctx, job, domainembedding.MultimodalFailureProviderRetryable, providerError.RetryAfter)
	}
	return w.retry(ctx, job, domainembedding.MultimodalFailureProviderRetryable, 0)
}

func (w *MultimodalJobWorker) retry(
	ctx context.Context,
	job *domainembedding.MultimodalEmbeddingJob,
	failureCode string,
	retryAfter time.Duration,
) error {
	if job.Attempts >= job.MaxAttempts {
		return w.terminal(ctx, job, failureCode)
	}
	if retryAfter < w.config.RetryBase {
		retryAfter = w.retryDelay(job.Attempts)
	}
	if retryAfter > w.config.RetryMax {
		retryAfter = w.config.RetryMax
	}
	_, err := w.repository.RetryMultimodalJob(ctx, job.ID, job.ClaimToken, failureCode, retryAfter)
	return err
}

func (w *MultimodalJobWorker) terminal(ctx context.Context, job *domainembedding.MultimodalEmbeddingJob, failureCode string) error {
	_, err := w.repository.TerminalMultimodalJob(ctx, job.ID, job.ClaimToken, failureCode)
	return err
}

func (w *MultimodalJobWorker) refreshSource(ctx context.Context, job *domainembedding.MultimodalEmbeddingJob, sourceHash string) error {
	refreshed, err := domainembedding.NewMultimodalEmbeddingJob(
		job.VideoID, job.Contract, sourceHash, job.MaxAttempts, w.now(),
	)
	if err != nil {
		return err
	}
	_, _, _, err = w.repository.HandoffMultimodalJob(ctx, refreshed)
	return err
}

func (w *MultimodalJobWorker) retryDelay(attempt int) time.Duration {
	delay := w.config.RetryBase
	for index := 1; index < attempt && delay < w.config.RetryMax; index++ {
		if delay > w.config.RetryMax/2 {
			return w.config.RetryMax
		}
		delay *= 2
	}
	return min(delay, w.config.RetryMax)
}

func receiveHeartbeatLoss(channel <-chan error) error {
	select {
	case err := <-channel:
		return err
	default:
		return nil
	}
}

func validMultimodalWorkerConfig(config MultimodalJobWorkerConfig) bool {
	contract, err := domainembedding.NewMultimodalContractIdentity(
		config.Contract.ProviderAlias, config.Contract.ModelAlias,
		config.Contract.RevisionAlias, config.Contract.Dimension,
		config.Contract.TextCanonicalizer, config.Contract.FrameSamplingPolicy,
		config.Contract.ImagePreprocessingPolicy, config.Contract.FusionPolicy,
	)
	return err == nil && contract.Equal(config.Contract) &&
		config.MaxAttempts >= 1 && config.MaxAttempts <= domainembedding.MaxMultimodalJobAttempts &&
		config.MaxVideoTextRunes >= 1 && config.MaxVideoTextRunes <= 8192 &&
		config.LeaseTTL > config.ProviderDeadline && config.LeaseTTL <= 10*time.Minute &&
		config.HeartbeatInterval >= time.Second && config.HeartbeatInterval*2 < config.LeaseTTL &&
		config.PollInterval >= 100*time.Millisecond && config.PollInterval <= time.Minute &&
		config.ProviderDeadline >= 100*time.Millisecond && config.ProviderDeadline <= 2*time.Minute &&
		config.AdmissionLimit >= 1 && config.AdmissionLimit <= 64 &&
		config.RetryBase >= time.Second && config.RetryMax >= config.RetryBase && config.RetryMax <= 24*time.Hour &&
		config.ShutdownTimeout >= time.Second && config.ShutdownTimeout <= time.Minute &&
		config.MaxImages >= 1 && config.MaxImages <= 16 &&
		config.MaxImageBytes >= 64*1024 && config.MaxTotalImageBytes >= config.MaxImageBytes &&
		config.MaxImagePixels >= 10_000 && len(config.AllowedMIMETypes) > 0
}
