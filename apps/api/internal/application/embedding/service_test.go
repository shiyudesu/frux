package applicationembedding

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type intakeRepositoryStub struct {
	mutex      sync.Mutex
	embedding  *domainembedding.VideoEmbedding
	job        *domainembedding.SemanticJob
	persisted  int
	persistErr error
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
	r.mutex.Lock()
	defer r.mutex.Unlock()
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
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.persistErr != nil {
		return r.persistErr
	}
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

func TestPublicationIntakeChangedTextResetsOneSemanticJob(t *testing.T) {
	now := time.Now().UTC()
	repository := &intakeRepositoryStub{}
	service := New(repository, nil)
	service.SetSemanticEnabled(true)
	service.now = func() time.Time { return now }
	event := &applicationvideo.PublishedEvent{
		EventID: "video-published:8:1", VideoID: 8, AuthorID: 2,
		Title: "first", PublishedAt: now, OccurredAt: now,
	}
	first, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	firstHash := first.Embedding.TextHash
	event.Title = "second"
	second, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if !second.CreatedOrUpdated || second.Embedding.TextHash == firstHash ||
		repository.job.TextHash != second.Embedding.TextHash ||
		repository.job.State != domainembedding.SemanticJobPending {
		t.Fatalf("changed intake result=%+v job=%+v", second, repository.job)
	}
}

type hashFailureRepository struct {
	err error
}

func (r hashFailureRepository) SaveVideoEmbedding(
	context.Context,
	*domainembedding.VideoEmbedding,
) error {
	return r.err
}

func (hashFailureRepository) FindVideoEmbedding(
	context.Context,
	int64,
	string,
) (*domainembedding.VideoEmbedding, error) {
	return nil, domainembedding.ErrVideoEmbeddingNotFound
}

func TestPublicationIntakeStopsAtHashOrSemanticHandoffFailure(t *testing.T) {
	now := time.Now().UTC()
	event := &applicationvideo.PublishedEvent{
		EventID: "video-published:9:1", VideoID: 9, AuthorID: 2,
		Title: "title", PublishedAt: now, OccurredAt: now,
	}
	service := New(hashFailureRepository{err: errors.New("hash down")}, nil)
	if _, err := service.GenerateForPublishedVideo(
		context.Background(),
		event,
	); !errors.Is(err, ErrSaveVideoEmbeddingFailed) {
		t.Fatalf("hash failure = %v", err)
	}
	repository := &intakeRepositoryStub{persistErr: errors.New("handoff down")}
	service = New(repository, nil)
	service.SetSemanticEnabled(true)
	if _, err := service.GenerateForPublishedVideo(
		context.Background(),
		event,
	); !errors.Is(err, ErrSaveVideoEmbeddingFailed) {
		t.Fatalf("handoff failure = %v", err)
	}
	if repository.embedding != nil || repository.job != nil {
		t.Fatal("failed atomic handoff exposed partial durable state")
	}
}

func TestPublicationIntakeConcurrentDuplicatesRemainOneFact(t *testing.T) {
	now := time.Now().UTC()
	repository := &intakeRepositoryStub{}
	service := New(repository, nil)
	service.SetSemanticEnabled(true)
	service.now = func() time.Time { return now }
	event := &applicationvideo.PublishedEvent{
		EventID: "video-published:10:1", VideoID: 10, AuthorID: 2,
		Title: "title", PublishedAt: now, OccurredAt: now,
	}
	var wait sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := service.GenerateForPublishedVideo(context.Background(), event)
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if repository.embedding == nil || repository.job == nil ||
		repository.embedding.VideoID != event.VideoID ||
		repository.job.VideoID != event.VideoID {
		t.Fatalf("concurrent fact=%+v job=%+v", repository.embedding, repository.job)
	}
}
