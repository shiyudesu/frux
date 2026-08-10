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

type hashIntakeRepository struct {
	mutex     sync.Mutex
	embedding *domainembedding.VideoEmbedding
	saveErr   error
}

func (r *hashIntakeRepository) SaveVideoEmbedding(
	_ context.Context,
	embedding *domainembedding.VideoEmbedding,
) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.embedding = embedding
	return nil
}

func (r *hashIntakeRepository) FindVideoEmbedding(
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

func TestPublicationIntakePersistsOnlyHashEmbedding(t *testing.T) {
	now := time.Now().UTC()
	repository := &hashIntakeRepository{}
	service := New(repository, nil)
	event := &applicationvideo.PublishedEvent{
		EventID: "video-published:8:1", VideoID: 8, AuthorID: 2,
		Title: " title ", Description: " description ",
		PublishedAt: now, OccurredAt: now,
	}
	result, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if repository.embedding == nil ||
		repository.embedding.Model != domainembedding.HashNgramModel ||
		repository.embedding.TextHash != domainembedding.TextHash("title\ndescription") {
		t.Fatalf("hash embedding=%+v", repository.embedding)
	}
	if !result.CreatedOrUpdated {
		t.Fatal("new hash fact reported skipped")
	}

	result, err = service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedOrUpdated {
		t.Fatal("duplicate publication reported a changed hash fact")
	}
}

func TestPublicationIntakeUpdatesChangedHashText(t *testing.T) {
	now := time.Now().UTC()
	repository := &hashIntakeRepository{}
	service := New(repository, nil)
	event := &applicationvideo.PublishedEvent{
		EventID: "video-published:9:1", VideoID: 9, AuthorID: 2,
		Title: "first", PublishedAt: now, OccurredAt: now,
	}
	first, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	event.Title = "second"
	second, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if !second.CreatedOrUpdated || second.Embedding.TextHash == first.Embedding.TextHash {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestPublicationIntakeSurfacesHashPersistenceFailure(t *testing.T) {
	repository := &hashIntakeRepository{saveErr: errors.New("database unavailable")}
	service := New(repository, nil)
	_, err := service.GenerateForPublishedVideo(
		context.Background(),
		&applicationvideo.PublishedEvent{VideoID: 10, Title: "title"},
	)
	if !errors.Is(err, ErrSaveVideoEmbeddingFailed) {
		t.Fatalf("hash failure=%v", err)
	}
}

func TestPublicationIntakeRejectsInvalidHashText(t *testing.T) {
	repository := &hashIntakeRepository{}
	service := New(repository, nil)
	_, err := service.GenerateForPublishedVideo(
		context.Background(),
		&applicationvideo.PublishedEvent{VideoID: 11, Title: "title\x00"},
	)
	if !errors.Is(err, domainembedding.ErrInvalidHashText) {
		t.Fatalf("invalid hash text error=%v", err)
	}
	if repository.embedding != nil {
		t.Fatalf("invalid hash text persisted=%+v", repository.embedding)
	}
}
