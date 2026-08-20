package applicationembedding

import (
	"context"
	"errors"
	"fmt"
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

type multimodalHandoffStub struct {
	mutex          sync.Mutex
	jobs           map[string]*domainembedding.MultimodalEmbeddingJob
	err            error
	hashRepository *hashIntakeRepository
}

func (r *multimodalHandoffStub) HandoffMultimodalJob(
	_ context.Context,
	job *domainembedding.MultimodalEmbeddingJob,
) (*domainembedding.MultimodalEmbeddingJob, bool, bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.err != nil {
		return nil, false, false, r.err
	}
	if r.hashRepository == nil {
		return nil, false, false, errors.New("hash fact was not durable before handoff")
	}
	r.hashRepository.mutex.Lock()
	hashReady := r.hashRepository.embedding != nil
	r.hashRepository.mutex.Unlock()
	if !hashReady {
		return nil, false, false, errors.New("hash fact was not durable before handoff")
	}
	if r.jobs == nil {
		r.jobs = map[string]*domainembedding.MultimodalEmbeddingJob{}
	}
	key := fmt.Sprintf("%d:%s", job.VideoID, job.Contract.Key())
	existing := r.jobs[key]
	if existing == nil {
		stored := job.Clone()
		stored.ID = int64(len(r.jobs) + 1)
		r.jobs[key] = stored
		return stored.Clone(), true, false, nil
	}
	if existing.SourceHash == job.SourceHash {
		return existing.Clone(), false, false, nil
	}
	stored := job.Clone()
	stored.ID = existing.ID
	r.jobs[key] = stored
	return stored.Clone(), false, true, nil
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

func TestPublicationIntakeHandsOffMultimodalOnlyAfterHashPersistence(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	repository := &hashIntakeRepository{}
	handoff := &multimodalHandoffStub{hashRepository: repository}
	config := testMultimodalHandoffConfig(t)
	service := New(
		repository,
		nil,
		WithMultimodalJobHandoff(handoff, config),
		WithEmbeddingNow(func() time.Time { return now }),
	)
	event := &applicationvideo.PublishedEvent{
		VideoID: 12, Title: " title ", Description: " description ",
		MediaURL: "https://media.example/video.mp4", CoverURL: "https://media.example/cover.jpg",
		PublishedAt: now, OccurredAt: now,
	}
	first, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil || first.MultimodalHandoff != MultimodalHandoffCreated || len(handoff.jobs) != 1 {
		t.Fatalf("first handoff=%#v jobs=%d err=%v", first, len(handoff.jobs), err)
	}
	second, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil || second.MultimodalHandoff != MultimodalHandoffExisting || len(handoff.jobs) != 1 {
		t.Fatalf("duplicate handoff=%#v jobs=%d err=%v", second, len(handoff.jobs), err)
	}
	event.Title = "changed"
	third, err := service.GenerateForPublishedVideo(context.Background(), event)
	if err != nil || third.MultimodalHandoff != MultimodalHandoffRefreshed || len(handoff.jobs) != 1 {
		t.Fatalf("changed handoff=%#v jobs=%d err=%v", third, len(handoff.jobs), err)
	}
}

func TestPublicationIntakeMultimodalNoopAndRetryBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	config := testMultimodalHandoffConfig(t)
	repository := &hashIntakeRepository{}
	handoff := &multimodalHandoffStub{hashRepository: repository}
	service := New(repository, nil, WithMultimodalJobHandoff(handoff, config), WithEmbeddingNow(func() time.Time { return now }))
	result, err := service.GenerateForPublishedVideo(context.Background(), &applicationvideo.PublishedEvent{
		VideoID: 13, Title: "title", PublishedAt: now,
	})
	if err != nil || result.MultimodalHandoff != MultimodalHandoffNoop || len(handoff.jobs) != 0 || repository.embedding == nil {
		t.Fatalf("ineligible no-op=%#v jobs=%d hash=%#v err=%v", result, len(handoff.jobs), repository.embedding, err)
	}

	repository = &hashIntakeRepository{}
	handoff = &multimodalHandoffStub{hashRepository: repository, err: errors.New("database unavailable")}
	service = New(repository, nil, WithMultimodalJobHandoff(handoff, config), WithEmbeddingNow(func() time.Time { return now }))
	_, err = service.GenerateForPublishedVideo(context.Background(), &applicationvideo.PublishedEvent{
		VideoID: 14, Title: "title", MediaURL: "https://media.example/video.mp4", PublishedAt: now,
	})
	if !errors.Is(err, ErrHandoffMultimodalEmbeddingFailed) || repository.embedding == nil {
		t.Fatalf("handoff failure=%v hash=%#v", err, repository.embedding)
	}
}

func TestPublicationIntakeRejectsOversizedMultimodalTextAfterHashSafety(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	repository := &hashIntakeRepository{}
	handoff := &multimodalHandoffStub{hashRepository: repository}
	config := testMultimodalHandoffConfig(t)
	config.MaxVideoTextRunes = 4
	service := New(repository, nil, WithMultimodalJobHandoff(handoff, config), WithEmbeddingNow(func() time.Time { return now }))
	_, err := service.GenerateForPublishedVideo(context.Background(), &applicationvideo.PublishedEvent{
		VideoID: 15, Title: "too long", MediaURL: "https://media.example/video.mp4", PublishedAt: now,
	})
	if !errors.Is(err, ErrInvalidMultimodalHandoff) || repository.embedding == nil || len(handoff.jobs) != 0 {
		t.Fatalf("oversized handoff=%v hash=%#v jobs=%d", err, repository.embedding, len(handoff.jobs))
	}
}

func testMultimodalHandoffConfig(t testing.TB) MultimodalHandoffConfig {
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
	config, err := NewMultimodalHandoffConfig(true, contract, 5, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return config
}
