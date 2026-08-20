package applicationembedding

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

type multimodalProjectionRepositoryStub struct {
	ids         []int64
	facts       map[int64]*domainembedding.MultimodalVectorFact
	projections map[int64]*domainembedding.MultimodalProjection
}

func (r *multimodalProjectionRepositoryStub) ListMultimodalReconciliationVideoIDs(_ context.Context, _ domainembedding.MultimodalContractIdentity, after int64, limit int) ([]int64, error) {
	ids := make([]int64, 0, limit)
	for _, videoID := range r.ids {
		if videoID > after {
			ids = append(ids, videoID)
		}
		if len(ids) == limit {
			break
		}
	}
	return ids, nil
}

func (r *multimodalProjectionRepositoryStub) FindMultimodalVectorFact(_ context.Context, videoID int64, _ domainembedding.MultimodalContractIdentity) (*domainembedding.MultimodalVectorFact, error) {
	fact := r.facts[videoID]
	if fact == nil {
		return nil, domainembedding.ErrMultimodalVectorFactNotFound
	}
	return fact.Clone(), nil
}

func (r *multimodalProjectionRepositoryStub) UpsertMultimodalProjection(_ context.Context, projection *domainembedding.MultimodalProjection) (bool, error) {
	if r.projections == nil {
		r.projections = map[int64]*domainembedding.MultimodalProjection{}
	}
	existing := r.projections[projection.VideoID]
	changed := existing == nil || existing.Identity != projection.Identity ||
		!existing.PublishedAt.Equal(projection.PublishedAt)
	cloned := *projection
	cloned.Values = append([]float64(nil), projection.Values...)
	r.projections[projection.VideoID] = &cloned
	return changed, nil
}

func (r *multimodalProjectionRepositoryStub) DeleteMultimodalProjection(_ context.Context, videoID int64, _ string) (bool, error) {
	if r.projections[videoID] == nil {
		return false, nil
	}
	delete(r.projections, videoID)
	return true, nil
}

type multimodalVideoMapReader struct {
	videos map[int64]*domainvideo.Video
	err    error
}

func (r multimodalVideoMapReader) FindByIDAnyStatus(_ context.Context, videoID int64) (*domainvideo.Video, error) {
	if r.err != nil {
		return nil, r.err
	}
	video := r.videos[videoID]
	if video == nil {
		return nil, domainvideo.ErrVideoNotFound
	}
	cloned := *video
	if video.PublishedAt != nil {
		publishedAt := *video.PublishedAt
		cloned.PublishedAt = &publishedAt
	}
	return &cloned, nil
}

func TestMultimodalProjectionReconcilerUpsertsCurrentAndDeletesStale(t *testing.T) {
	now := time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
	contract := testWorkerMultimodalContract(t)
	videos := map[int64]*domainvideo.Video{}
	assets := map[int64]*domainmedia.MediaAsset{}
	facts := map[int64]*domainembedding.MultimodalVectorFact{}
	projections := map[int64]*domainembedding.MultimodalProjection{}
	for videoID := int64(1); videoID <= 4; videoID++ {
		video := readableMultimodalVideo(now)
		video.ID = videoID
		video.MediaAssetID = 100 + videoID
		video.CoverAssetID = 200 + videoID
		videos[videoID] = video
		assets[video.MediaAssetID] = &domainmedia.MediaAsset{ID: video.MediaAssetID, State: domainmedia.AssetStateReady, ObjectKey: "media/video"}
		assets[video.CoverAssetID] = &domainmedia.MediaAsset{ID: video.CoverAssetID, State: domainmedia.AssetStateReady, ObjectKey: "media/cover"}
		text, err := domainembedding.CanonicalizePublicVideoText(video.Title, video.Description, 2048)
		if err != nil {
			t.Fatal(err)
		}
		sourceHash := MultimodalVideoSourceHash(
			contract, text, video.MediaURL, video.CoverURL,
			video.MediaAssetID, video.CoverAssetID, video.MediaProfileVersion, video.Version,
		)
		facts[videoID] = multimodalProjectionFact(t, videoID, contract, sourceHash, now)
	}
	facts[2] = multimodalProjectionFact(t, 2, contract, domainembedding.MultimodalSourceHash([]byte("stale")), now)
	delete(facts, 3)
	videos[4].Visibility = domainvideo.VisibilityPrivate
	for _, videoID := range []int64{2, 3, 4} {
		fact := facts[videoID]
		if fact == nil {
			fact = multimodalProjectionFact(t, videoID, contract, domainembedding.MultimodalSourceHash([]byte("orphan")), now)
		}
		projection, err := domainembedding.NewMultimodalProjection(fact, now.Add(-time.Hour), now)
		if err != nil {
			t.Fatal(err)
		}
		projections[videoID] = projection
	}
	repository := &multimodalProjectionRepositoryStub{
		ids: []int64{1, 2, 3, 4}, facts: facts, projections: projections,
	}
	reconciler, err := NewMultimodalProjectionReconciler(
		repository,
		multimodalVideoMapReader{videos: videos},
		multimodalAssetReaderStub{assets: assets},
		contract,
		2048,
	)
	if err != nil {
		t.Fatal(err)
	}
	reconciler.now = func() time.Time { return now }
	result, err := reconciler.ReconcileBatch(context.Background(), 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if result.Examined != 4 || result.Upserted != 1 || result.Deleted != 3 ||
		result.NextVideoID != 4 || result.Complete || len(repository.projections) != 1 ||
		repository.projections[1] == nil {
		t.Fatalf("reconcile result=%#v projections=%v", result, sortedProjectionIDs(repository.projections))
	}
	next, err := reconciler.ReconcileBatch(context.Background(), result.NextVideoID, 4)
	if err != nil || !next.Complete || next.Examined != 0 {
		t.Fatalf("next reconcile=%#v err=%v", next, err)
	}
}

func TestMultimodalProjectionReconcilerPreservesProjectionOnTransientLoadFailure(t *testing.T) {
	contract := testWorkerMultimodalContract(t)
	repository := &multimodalProjectionRepositoryStub{ids: []int64{1}, facts: map[int64]*domainembedding.MultimodalVectorFact{
		1: multimodalProjectionFact(t, 1, contract, domainembedding.MultimodalSourceHash([]byte("source")), time.Now()),
	}}
	reconciler, err := NewMultimodalProjectionReconciler(
		repository,
		multimodalVideoMapReader{err: errors.New("database unavailable")},
		multimodalAssetReaderStub{}, contract, 2048,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.ReconcileBatch(context.Background(), 0, 10); err == nil {
		t.Fatal("transient source failure was treated as stale deletion")
	}
}

func multimodalProjectionFact(
	t testing.TB,
	videoID int64,
	contract domainembedding.MultimodalContractIdentity,
	sourceHash string,
	now time.Time,
) *domainembedding.MultimodalVectorFact {
	t.Helper()
	values := make([]float64, contract.Dimension)
	values[0] = 1
	fact, err := domainembedding.NewMultimodalVectorFact(videoID, &domainembedding.MultimodalVector{
		Identity: domainembedding.MultimodalVectorIdentity{
			Contract: contract, SourceHash: sourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(values),
		},
		Values: values,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

func sortedProjectionIDs(values map[int64]*domainembedding.MultimodalProjection) []int64 {
	ids := make([]int64, 0, len(values))
	for videoID := range values {
		ids = append(ids, videoID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
