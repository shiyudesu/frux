package applicationembedding

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

type SemanticResult string

const (
	SemanticSuccess      SemanticResult = "success"
	SemanticCanceled     SemanticResult = "canceled"
	SemanticTimeout      SemanticResult = "timeout"
	SemanticOverCapacity SemanticResult = "over_capacity"
	SemanticAuth         SemanticResult = "auth"
	SemanticUnavailable  SemanticResult = "unavailable"
	SemanticContract     SemanticResult = "contract"
	SemanticInternal     SemanticResult = "internal"
)

type SemanticError struct {
	Result   SemanticResult
	Terminal bool
	Err      error
}

func (e *SemanticError) Error() string {
	if e == nil || e.Err == nil {
		return "semantic embedding failed"
	}
	return e.Err.Error()
}

func (e *SemanticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type SemanticGenerator interface {
	ValidateMetadata(context.Context) error
	Generate(context.Context, []SemanticInput) ([][]float64, error)
}

type SemanticInput struct {
	ID          string
	Title       string
	Description string
}

type SemanticJobRepository interface {
	FindVideoEmbedding(context.Context, int64, string) (*domainembedding.VideoEmbedding, error)
	ClaimSemanticJobs(context.Context, string, time.Time, time.Time, int) ([]*domainembedding.SemanticJob, error)
	CompleteSemanticJob(
		context.Context,
		*domainembedding.SemanticJob,
		*domainembedding.VideoEmbedding,
		time.Time,
	) error
	RetrySemanticJob(context.Context, *domainembedding.SemanticJob, time.Time, string, bool) error
	SuspendSemanticJobs(context.Context, time.Time) (int64, error)
	ResumeSemanticJobs(context.Context, time.Time) (int64, error)
	SemanticBacklog(context.Context) ([]domainembedding.SemanticBacklog, error)
	CleanupSemanticJobs(context.Context, time.Time, int) (int64, error)
}

type SemanticWorker struct {
	repo         SemanticJobRepository
	generator    SemanticGenerator
	enabled      bool
	owner        string
	concurrency  int
	leaseTTL     time.Duration
	pollInterval time.Duration
	now          func() time.Time
	startOnce    sync.Once
}

func NewSemanticWorker(
	repo SemanticJobRepository,
	generator SemanticGenerator,
	enabled bool,
	concurrency int,
	leaseTTL time.Duration,
	pollInterval time.Duration,
) *SemanticWorker {
	owner, _ := os.Hostname()
	if strings.TrimSpace(owner) == "" {
		owner = "frux-worker"
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if leaseTTL <= 0 {
		leaseTTL = 30 * time.Second
	}
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	return &SemanticWorker{
		repo: repo, generator: generator, enabled: enabled,
		owner: "semantic:" + owner, concurrency: concurrency,
		leaseTTL: leaseTTL, pollInterval: pollInterval,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (w *SemanticWorker) Start(ctx context.Context) error {
	if w == nil || w.repo == nil {
		return nil
	}
	now := w.now().UTC()
	if !w.enabled || w.generator == nil {
		_, err := w.repo.SuspendSemanticJobs(ctx, now)
		return err
	}
	if err := w.generator.ValidateMetadata(ctx); err != nil {
		if _, suspendErr := w.repo.SuspendSemanticJobs(ctx, now); suspendErr != nil {
			return suspendErr
		}
		go w.runMetadataValidator(ctx)
		return nil
	}
	_, _ = w.repo.ResumeSemanticJobs(ctx, now)
	w.startProcessors(ctx)
	return nil
}

func (w *SemanticWorker) startProcessors(ctx context.Context) {
	w.startOnce.Do(func() {
		for range w.concurrency {
			go w.run(ctx)
		}
	})
}

func (w *SemanticWorker) run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = w.ProcessPending(ctx)
		}
	}
}

func (w *SemanticWorker) ProcessPending(ctx context.Context) (int, error) {
	if w == nil || w.repo == nil || !w.enabled || w.generator == nil {
		return 0, nil
	}
	now := w.now().UTC()
	jobs, err := w.repo.ClaimSemanticJobs(
		ctx, w.owner, now, now.Add(w.leaseTTL), w.concurrency,
	)
	if err != nil {
		return 0, err
	}
	processed := 0
	var processErr error
	for _, job := range jobs {
		if err := w.process(ctx, job); err != nil {
			processErr = errors.Join(processErr, err)
			continue
		}
		processed++
	}
	return processed, processErr
}

func (w *SemanticWorker) process(
	ctx context.Context,
	job *domainembedding.SemanticJob,
) error {
	if job == nil {
		return nil
	}
	existing, err := w.repo.FindVideoEmbedding(ctx, job.VideoID, job.Model)
	if err == nil && existing != nil && existing.TextHash == job.TextHash {
		return w.repo.CompleteSemanticJob(ctx, job, existing, w.now().UTC())
	}
	if err != nil && !errors.Is(err, domainembedding.ErrVideoEmbeddingNotFound) {
		return w.retry(ctx, job, SemanticInternal, false, err)
	}
	vectors, err := w.generator.Generate(ctx, []SemanticInput{{
		ID: "video:" + formatVideoID(job.VideoID), Title: job.Title, Description: job.Description,
	}})
	if err != nil {
		result, terminal := classifySemanticFailure(err)
		return w.retry(ctx, job, result, terminal, err)
	}
	if len(vectors) != 1 {
		return w.retry(ctx, job, SemanticContract, true, domainembedding.ErrInvalidSemanticVector)
	}
	vector, err := domainembedding.NormalizeSemanticVector(vectors[0])
	if err != nil {
		return w.retry(ctx, job, SemanticContract, true, err)
	}
	content, err := json.Marshal(vector)
	if err != nil {
		return w.retry(ctx, job, SemanticInternal, false, err)
	}
	embedding := domainembedding.NewVideoEmbedding(
		job.VideoID, domainembedding.SemanticModelKey, vector, job.TextHash, string(content),
	)
	if err := w.repo.CompleteSemanticJob(ctx, job, embedding, w.now().UTC()); err != nil {
		return err
	}
	inframetrics.ObserveSemanticVector("generated")
	return nil
}

func (w *SemanticWorker) retry(
	ctx context.Context,
	job *domainembedding.SemanticJob,
	result SemanticResult,
	terminal bool,
	cause error,
) error {
	availableAt := w.now().UTC().Add(domainembedding.SemanticRetryDelay(job.Attempts))
	if err := w.repo.RetrySemanticJob(
		ctx, job, availableAt, string(result), terminal,
	); err != nil {
		return errors.Join(cause, err)
	}
	if terminal {
		inframetrics.ObserveSemanticVector("failed")
	} else {
		inframetrics.ObserveSemanticVector("retried")
	}
	return nil
}

func (w *SemanticWorker) runMetadataValidator(ctx context.Context) {
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}
	index := 0
	for {
		delay := 5 * time.Minute
		if index < len(delays) {
			delay = delays[index]
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if err := w.generator.ValidateMetadata(ctx); err == nil {
			_, _ = w.repo.ResumeSemanticJobs(ctx, w.now().UTC())
			w.startProcessors(ctx)
			return
		}
		_, _ = w.repo.SuspendSemanticJobs(ctx, w.now().UTC())
		index++
	}
}

func classifySemanticFailure(err error) (SemanticResult, bool) {
	var semanticErr *SemanticError
	if errors.As(err, &semanticErr) {
		return semanticErr.Result, semanticErr.Terminal
	}
	if errors.Is(err, context.Canceled) {
		return SemanticCanceled, false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return SemanticTimeout, false
	}
	return SemanticInternal, false
}

func formatVideoID(value int64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
