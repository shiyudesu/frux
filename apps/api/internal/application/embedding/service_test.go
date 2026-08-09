package applicationembedding

import (
	"context"
	"testing"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type intakeRepositoryStub struct {
	embedding *domainembedding.VideoEmbedding
	job       *domainembedding.SemanticJob
	persisted int
}

func (r *intakeRepositoryStub) SaveVideoEmbedding(
	context.Context,
	*domainembedding.VideoEmbedding,
) error {
	return nil
}
func (r *intakeRepositoryStub) FindVideoEmbedding(
	context.Context,
	int64,
	string,
) (*domainembedding.VideoEmbedding, error) {
	if r.embedding == nil {
		return nil, domainembedding.ErrVideoEmbeddingNotFound
	}
	return r.embedding, nil
}
func (r *intakeRepositoryStub) PersistHashAndSemanticJob(
	_ context.Context,
	embedding *domainembedding.VideoEmbedding,
	job *domainembedding.SemanticJob,
) error {
	r.embedding = embedding
	r.job = job
	r.persisted++
	return nil
}

func TestPublicationIntakePersistsHashAndSemanticJobTogether(t *testing.T) {
	now := time.Now().UTC()
	repository := &intakeRepositoryStub{}
	service := New(repository, nil)
	service.now = func() time.Time { return now }
	event := &applicationvideo.PublishedEvent{
		EventID: "video-published:8:1", VideoID: 8, AuthorID: 2,
		Title: "  Ｆｒｕｘ  ", Description: " 语义\t内容 ",
		PublishedAt: now, OccurredAt: now,
	}
	result, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if repository.persisted != 1 || repository.embedding == nil || repository.job == nil {
		t.Fatalf("intake boundary = %+v", repository)
	}
	if repository.job.State != domainembedding.SemanticJobSuspended ||
		repository.job.TextHash != repository.embedding.TextHash ||
		repository.job.Title != "Frux" ||
		repository.job.Description != "语义 内容" {
		t.Fatalf("semantic job = %+v", repository.job)
	}
	if !result.CreatedOrUpdated {
		t.Fatal("new hash fact reported skipped")
	}
	service.SetSemanticEnabled(true)
	result, err = service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedOrUpdated {
		t.Fatal("duplicate publication churned the hash fact")
	}
}
