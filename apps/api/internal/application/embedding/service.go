package applicationembedding

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"

	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

var ErrSaveVideoEmbeddingFailed = errors.New("failed to save video embedding")
var ErrMarshalEmbeddingFailed = errors.New("failed to marshal embedding")
var ErrHandoffMultimodalEmbeddingFailed = errors.New("failed to hand off multimodal embedding")
var ErrInvalidMultimodalHandoff = errors.New("invalid multimodal embedding handoff")

const (
	MultimodalHandoffDisabled  = "disabled"
	MultimodalHandoffNoop      = "no_op"
	MultimodalHandoffCreated   = "created"
	MultimodalHandoffExisting  = "existing"
	MultimodalHandoffRefreshed = "refreshed"
)

type MultimodalJobHandoff interface {
	HandoffMultimodalJob(context.Context, *domainembedding.MultimodalEmbeddingJob) (*domainembedding.MultimodalEmbeddingJob, bool, bool, error)
}

type MultimodalHandoffConfig struct {
	Enabled           bool
	Contract          domainembedding.MultimodalContractIdentity
	MaxAttempts       int
	MaxVideoTextRunes int
}

func NewMultimodalHandoffConfig(
	enabled bool,
	contract domainembedding.MultimodalContractIdentity,
	maxAttempts int,
	maxVideoTextRunes int,
) (MultimodalHandoffConfig, error) {
	if !enabled {
		return MultimodalHandoffConfig{}, nil
	}
	normalized, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	if err != nil || maxAttempts < 1 || maxAttempts > domainembedding.MaxMultimodalJobAttempts ||
		maxVideoTextRunes < 1 || maxVideoTextRunes > 8192 {
		return MultimodalHandoffConfig{}, ErrInvalidMultimodalHandoff
	}
	return MultimodalHandoffConfig{
		Enabled: true, Contract: normalized, MaxAttempts: maxAttempts,
		MaxVideoTextRunes: maxVideoTextRunes,
	}, nil
}

type Option func(*Service)

func WithMultimodalJobHandoff(repository MultimodalJobHandoff, config MultimodalHandoffConfig) Option {
	return func(service *Service) {
		if service == nil || !config.Enabled {
			return
		}
		service.multimodalJobs = repository
		service.multimodalConfig = config
	}
}

func WithEmbeddingNow(now func() time.Time) Option {
	return func(service *Service) {
		if service != nil && now != nil {
			service.now = now
		}
	}
}

type Service struct {
	repo             domainembedding.Repository
	vectorizer       domainembedding.Vectorizer
	multimodalJobs   MultimodalJobHandoff
	multimodalConfig MultimodalHandoffConfig
	now              func() time.Time
}

type GenerateVideoEmbeddingResult struct {
	Embedding         *domainembedding.VideoEmbedding
	CreatedOrUpdated  bool
	MultimodalHandoff string
}

func New(repo domainembedding.Repository, vectorizer domainembedding.Vectorizer, options ...Option) *Service {
	if vectorizer == nil {
		vectorizer = domainembedding.NewHashNgramVectorizer()
	}
	service := &Service{
		repo:       repo,
		vectorizer: vectorizer,
		now:        func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// GenerateForPublishedVideo 根据视频发布事件生成并保存视频内容向量。
func (s *Service) GenerateForPublishedVideo(ctx context.Context, event *applicationvideo.PublishedEvent) (*GenerateVideoEmbeddingResult, error) {
	if event == nil || event.VideoID <= 0 {
		return &GenerateVideoEmbeddingResult{MultimodalHandoff: MultimodalHandoffNoop}, nil
	}

	text, err := domainembedding.BuildValidatedVideoText(
		event.Title, event.Description,
	)
	if err != nil {
		return nil, err
	}
	vector := s.vectorizer.Vectorize(text)
	content, err := json.Marshal(vector)
	if err != nil {
		return nil, ErrMarshalEmbeddingFailed
	}

	embedding := domainembedding.NewVideoEmbedding(
		event.VideoID,
		s.vectorizer.Model(),
		vector,
		domainembedding.TextHash(text),
		string(content),
	)
	createdOrUpdated := true
	if finder, ok := s.repo.(interface {
		FindVideoEmbedding(context.Context, int64, string) (*domainembedding.VideoEmbedding, error)
	}); ok {
		existing, findErr := finder.FindVideoEmbedding(ctx, event.VideoID, embedding.Model)
		if findErr == nil && existing != nil && existing.TextHash == embedding.TextHash {
			createdOrUpdated = false
		}
	}
	if err := s.repo.SaveVideoEmbedding(ctx, embedding); err != nil {
		inframetrics.ObserveHashVector("failed")
		return nil, ErrSaveVideoEmbeddingFailed
	}
	if createdOrUpdated {
		inframetrics.ObserveHashVector("generated")
	} else {
		inframetrics.ObserveHashVector("skipped")
	}
	handoff, err := s.handoffMultimodalJob(ctx, event)
	if err != nil {
		return nil, err
	}

	return &GenerateVideoEmbeddingResult{
		Embedding:         embedding,
		CreatedOrUpdated:  createdOrUpdated,
		MultimodalHandoff: handoff,
	}, nil
}

func (s *Service) handoffMultimodalJob(ctx context.Context, event *applicationvideo.PublishedEvent) (string, error) {
	if s == nil || !s.multimodalConfig.Enabled {
		return MultimodalHandoffDisabled, nil
	}
	if s.multimodalJobs == nil {
		return "", ErrHandoffMultimodalEmbeddingFailed
	}
	if event == nil || event.VideoID <= 0 || event.PublishedAt.IsZero() ||
		event.MediaAssetID <= 0 || strings.TrimSpace(event.MediaURL) == "" {
		return MultimodalHandoffNoop, nil
	}
	text, err := domainembedding.CanonicalizePublicVideoText(
		event.Title, event.Description, s.multimodalConfig.MaxVideoTextRunes,
	)
	if err != nil {
		return "", ErrInvalidMultimodalHandoff
	}
	sourceHash := MultimodalVideoSourceHash(
		s.multimodalConfig.Contract, text, event.MediaURL, event.CoverURL,
		event.MediaAssetID, event.CoverAssetID, event.MediaProfileVersion, event.VideoVersion,
	)
	job, err := domainembedding.NewMultimodalEmbeddingJob(
		event.VideoID, s.multimodalConfig.Contract, sourceHash,
		s.multimodalConfig.MaxAttempts, s.now(),
	)
	if err != nil {
		return "", ErrInvalidMultimodalHandoff
	}
	_, created, refreshed, err := s.multimodalJobs.HandoffMultimodalJob(ctx, job)
	if err != nil {
		return "", ErrHandoffMultimodalEmbeddingFailed
	}
	if created {
		return MultimodalHandoffCreated, nil
	}
	if refreshed {
		return MultimodalHandoffRefreshed, nil
	}
	return MultimodalHandoffExisting, nil
}
