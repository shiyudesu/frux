package applicationembedding

import (
	"context"
	"encoding/json"
	"errors"
	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	"time"

	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

var ErrSaveVideoEmbeddingFailed = errors.New("failed to save video embedding")
var ErrMarshalEmbeddingFailed = errors.New("failed to marshal embedding")

type Service struct {
	repo            domainembedding.Repository
	vectorizer      domainembedding.Vectorizer
	semanticEnabled bool
	now             func() time.Time
}

type GenerateVideoEmbeddingResult struct {
	Embedding        *domainembedding.VideoEmbedding
	CreatedOrUpdated bool
}

func New(repo domainembedding.Repository, vectorizer domainembedding.Vectorizer) *Service {
	if vectorizer == nil {
		vectorizer = domainembedding.NewHashNgramVectorizer()
	}
	return &Service{
		repo:       repo,
		vectorizer: vectorizer,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetSemanticEnabled(enabled bool) {
	if s != nil {
		s.semanticEnabled = enabled
	}
}

// GenerateForPublishedVideo 根据视频发布事件生成并保存视频内容向量。
func (s *Service) GenerateForPublishedVideo(ctx context.Context, event *applicationvideo.PublishedEvent) (*GenerateVideoEmbeddingResult, error) {
	if event == nil || event.VideoID <= 0 {
		return &GenerateVideoEmbeddingResult{}, nil
	}

	title, description, text, err := domainembedding.CanonicalVideoText(
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
	jobState := domainembedding.SemanticJobPending
	if !s.semanticEnabled {
		jobState = domainembedding.SemanticJobSuspended
	}
	now := s.now().UTC()
	job := &domainembedding.SemanticJob{
		VideoID: event.VideoID, Model: domainembedding.SemanticModelKey,
		TextHash: embedding.TextHash, Title: title, Description: description,
		State: jobState, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if repository, ok := s.repo.(interface {
		PersistHashAndSemanticJob(
			context.Context,
			*domainembedding.VideoEmbedding,
			*domainembedding.SemanticJob,
		) error
	}); ok {
		if err := repository.PersistHashAndSemanticJob(ctx, embedding, job); err != nil {
			inframetrics.ObserveHashVector("failed")
			return nil, ErrSaveVideoEmbeddingFailed
		}
	} else if err := s.repo.SaveVideoEmbedding(ctx, embedding); err != nil {
		inframetrics.ObserveHashVector("failed")
		return nil, ErrSaveVideoEmbeddingFailed
	}
	if createdOrUpdated {
		inframetrics.ObserveHashVector("generated")
	} else {
		inframetrics.ObserveHashVector("skipped")
	}

	return &GenerateVideoEmbeddingResult{
		Embedding:        embedding,
		CreatedOrUpdated: createdOrUpdated,
	}, nil
}
