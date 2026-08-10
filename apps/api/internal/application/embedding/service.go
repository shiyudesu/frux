package applicationembedding

import (
	"context"
	"encoding/json"
	"errors"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"

	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

var ErrSaveVideoEmbeddingFailed = errors.New("failed to save video embedding")
var ErrMarshalEmbeddingFailed = errors.New("failed to marshal embedding")

type Service struct {
	repo       domainembedding.Repository
	vectorizer domainembedding.Vectorizer
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
	}
}

// GenerateForPublishedVideo 根据视频发布事件生成并保存视频内容向量。
func (s *Service) GenerateForPublishedVideo(ctx context.Context, event *applicationvideo.PublishedEvent) (*GenerateVideoEmbeddingResult, error) {
	if event == nil || event.VideoID <= 0 {
		return &GenerateVideoEmbeddingResult{}, nil
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

	return &GenerateVideoEmbeddingResult{
		Embedding:        embedding,
		CreatedOrUpdated: createdOrUpdated,
	}, nil
}
