package applicationvideo

import (
	"context"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	"reflect"
	"testing"
	"time"
)

type managementRepoStub struct {
	lastIDs          []int64
	lastFingerprint  string
	assetReferences  []domainvideo.AssetReference
	localAssets      map[string]*domainvideo.LocalAsset
	mediaRefs        []MediaAssetRef
	replayed         bool
	videos           map[int64]*domainvideo.Video
	publicationReady bool
	publicationMarks int
}

type managementMediaPublisherStub struct {
	readyCalls   int
	protectCalls int
}

type managementPublishedPublisherStub struct {
	events []*PublishedEvent
}

func (p *managementPublishedPublisherStub) PublishVideoPublished(_ context.Context, event *PublishedEvent) error {
	p.events = append(p.events, event)
	return nil
}

func (p *managementMediaPublisherStub) MediaReady(context.Context, int64) error {
	p.readyCalls++
	return nil
}

func (p *managementMediaPublisherStub) ProtectVideo(context.Context, int64, int64, int64) error {
	p.protectCalls++
	return nil
}

func (r *managementRepoStub) QueryCreatorVideos(context.Context, domainvideo.CreatorVideoFilter) ([]*domainvideo.Video, error) {
	return nil, nil
}
func (r *managementRepoStub) ApplyBatch(_ context.Context, userID int64, action string, videoIDs []int64, key, fingerprint string) (*domainvideo.BatchOperation, bool, error) {
	r.lastIDs = append([]int64(nil), videoIDs...)
	r.lastFingerprint = fingerprint
	return &domainvideo.BatchOperation{UserID: userID, Key: key, Action: action, VideoIDs: videoIDs}, r.replayed, nil
}
func (r *managementRepoStub) ListMediaAssetRefs(context.Context, []int64) ([]MediaAssetRef, error) {
	return append([]MediaAssetRef(nil), r.mediaRefs...), nil
}
func (r *managementRepoStub) FindByIDAnyStatus(_ context.Context, videoID int64) (*domainvideo.Video, error) {
	video := r.videos[videoID]
	if video == nil {
		return nil, domainvideo.ErrVideoNotFound
	}
	cloned := *video
	return &cloned, nil
}
func (r *managementRepoStub) LifecyclePublicationReady(context.Context, string) (bool, error) {
	return r.publicationReady, nil
}
func (r *managementRepoStub) MarkLifecyclePublicationReady(context.Context, string, time.Time) error {
	r.publicationReady = true
	r.publicationMarks++
	return nil
}
func (r *managementRepoStub) BatchGetReadable(context.Context, int64, []int64, bool) (map[int64]*domainvideo.Video, error) {
	return nil, nil
}
func (r *managementRepoStub) ListAssetReferences(context.Context, string) ([]domainvideo.AssetReference, error) {
	return append([]domainvideo.AssetReference(nil), r.assetReferences...), nil
}
func (r *managementRepoStub) CreateLocalAsset(_ context.Context, asset *domainvideo.LocalAsset) error {
	if r.localAssets == nil {
		r.localAssets = map[string]*domainvideo.LocalAsset{}
	}
	cloned := *asset
	r.localAssets[asset.AssetURL] = &cloned
	return nil
}
func (r *managementRepoStub) FindLocalAsset(_ context.Context, assetURL string) (*domainvideo.LocalAsset, error) {
	asset := r.localAssets[assetURL]
	if asset == nil {
		return nil, domainvideo.ErrLocalAssetNotFound
	}
	cloned := *asset
	return &cloned, nil
}

func TestBatchNormalizationAndFingerprint(t *testing.T) {
	repo := &managementRepoStub{}
	service := NewManagement(repo, nil)
	result, err := service.ApplyBatch(context.Background(), 7, domainvideo.BatchActionMakePrivate, []int64{3, 1, 3, 2}, "batch-key")
	if err != nil {
		t.Fatalf("apply batch: %v", err)
	}

	if !reflect.DeepEqual(result.VideoIDs, []int64{1, 2, 3}) {
		t.Fatalf("unexpected normalized ids: %v", result.VideoIDs)
	}
	firstFingerprint := repo.lastFingerprint
	if _, err := service.ApplyBatch(context.Background(), 7, domainvideo.BatchActionMakePrivate, []int64{2, 3, 1}, "other-key"); err != nil {
		t.Fatalf("repeat payload: %v", err)
	}
	if repo.lastFingerprint != firstFingerprint {
		t.Fatalf("fingerprint changed for equivalent payload")
	}
	if _, err := service.ApplyBatch(context.Background(), 7, "archive", []int64{1}, "key"); err != domainvideo.ErrInvalidBatchAction {
		t.Fatalf("expected invalid action, got %v", err)
	}
}

func TestPrivateBatchNeverPerformsPostCommitMediaMutation(t *testing.T) {
	publisher := &managementMediaPublisherStub{}
	repo := &managementRepoStub{
		replayed: true,
		mediaRefs: []MediaAssetRef{{
			VideoID: 1, MediaAssetID: 11, CoverAssetID: 12,
			Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic,
		}},
	}

	service := NewManagement(repo, nil, WithManagementMediaPublisher(publisher))
	if _, err := service.ApplyBatch(
		context.Background(),
		7,
		domainvideo.BatchActionMakePrivate,
		[]int64{1},
		"old-private-key",
	); err != nil {
		t.Fatalf("replay old private batch: %v", err)
	}
	if publisher.protectCalls != 0 {
		t.Fatalf("old replay demoted current public video: %d", publisher.protectCalls)
	}

	repo.replayed = false
	repo.mediaRefs[0].Visibility = domainvideo.VisibilityPrivate
	if _, err := service.ApplyBatch(
		context.Background(),
		7,
		domainvideo.BatchActionMakePrivate,
		[]int64{1},
		"current-private-key",
	); err != nil {
		t.Fatalf("retry current private batch: %v", err)
	}
	if publisher.protectCalls != 0 {
		t.Fatalf("private batch performed post-commit protection: %d", publisher.protectCalls)
	}
}

func TestMakePublicReliesOnAtomicLegacyPublicationHandoff(t *testing.T) {
	publishedAt := time.Now().UTC()
	publisher := &managementPublishedPublisherStub{}
	repo := &managementRepoStub{
		mediaRefs: []MediaAssetRef{{
			VideoID: 1, Status: domainvideo.StatusPublished,
			Visibility: domainvideo.VisibilityPublic, MediaStatus: domainmedia.MediaStatusLegacyReady,
		}},
		videos: map[int64]*domainvideo.Video{
			1: domainvideo.RestoreVideoWithVisibility(
				1, 7, "legacy", "", "media", "cover",
				domainvideo.StatusPublished, domainvideo.VisibilityPublic,
				0, 0, 0, &publishedAt, publishedAt, publishedAt, "",
			),
		},
	}

	repo.videos[1].ReviewVersion = 1
	service := NewManagement(repo, nil, WithManagementPublishedPublisher(publisher))
	if _, err := service.ApplyBatch(
		context.Background(),
		7,
		domainvideo.BatchActionMakePublic,
		[]int64{1},
		"legacy-public",
	); err != nil {
		t.Fatalf("make legacy video public: %v", err)
	}
	if len(publisher.events) != 0 || repo.publicationMarks != 0 {
		t.Fatalf("post-commit publication attempted events=%d marks=%d",
			len(publisher.events), repo.publicationMarks)
	}
	if _, err := service.ApplyBatch(
		context.Background(), 7, domainvideo.BatchActionMakePublic,
		[]int64{1}, "legacy-public-retry",
	); err != nil {
		t.Fatal(err)
	}
	if len(publisher.events) != 0 || repo.publicationMarks != 0 {
		t.Fatalf(
			"legacy event replayed events=%d marks=%d",
			len(publisher.events), repo.publicationMarks,
		)
	}
}

func TestMakePublicWithoutPublisherUsesRepositoryHandoff(t *testing.T) {
	publishedAt := time.Now().UTC()
	repo := &managementRepoStub{
		mediaRefs: []MediaAssetRef{{
			VideoID: 2, Status: domainvideo.StatusPublished,
			Visibility:  domainvideo.VisibilityPublic,
			MediaStatus: domainmedia.MediaStatusLegacyReady,
		}},
		videos: map[int64]*domainvideo.Video{
			2: domainvideo.RestoreVideoWithVisibility(
				2, 7, "legacy", "", "media", "cover",
				domainvideo.StatusPublished, domainvideo.VisibilityPublic,
				0, 0, 0, &publishedAt, publishedAt, publishedAt, "",
			),
		},
	}
	repo.videos[2].ReviewVersion = 1
	service := NewManagement(repo, nil)
	if _, err := service.ApplyBatch(
		context.Background(), 7, domainvideo.BatchActionMakePublic,
		[]int64{2}, "legacy-no-publisher",
	); err != nil {
		t.Fatal(err)
	}
	if repo.publicationMarks != 0 || repo.publicationReady {
		t.Fatalf(
			"legacy readiness marks=%d ready=%v",
			repo.publicationMarks, repo.publicationReady,
		)
	}
}

func TestManagementCursorRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	encoded := encodeCreatorCursor(now, 99)
	decoded, err := decodeCreatorCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded.VideoID != 99 || !decoded.CreatedAt.Equal(now) {
		t.Fatalf("unexpected cursor: %+v", decoded)
	}
	if _, err := decodeCreatorCursor("bad"); err != domainvideo.ErrInvalidCursor {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

func TestAuthorizeLocalAsset(t *testing.T) {
	repo := &managementRepoStub{
		localAssets: map[string]*domainvideo.LocalAsset{
			"/uploads/video/private.mp4": {
				AssetURL: "/uploads/video/private.mp4", OwnerID: 42, Kind: domainvideo.LocalAssetKindVideo,
			},
			"/uploads/video/public.mp4": {
				AssetURL: "/uploads/video/public.mp4", OwnerID: 42, Kind: domainvideo.LocalAssetKindVideo,
			},
		},
		assetReferences: []domainvideo.AssetReference{{
			AuthorID: 42, Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPrivate,
		}},
	}
	service := NewManagement(repo, nil)

	referenced, public, allowed, err := service.AuthorizeLocalAsset(context.Background(), "/uploads/video/private.mp4", 0)
	if err != nil || !referenced || public || allowed {
		t.Fatalf("anonymous private authorization: referenced=%v public=%v allowed=%v err=%v", referenced, public, allowed, err)
	}
	_, _, allowed, err = service.AuthorizeLocalAsset(context.Background(), "/uploads/video/private.mp4", 42)
	if err != nil || !allowed {
		t.Fatalf("owner private authorization: allowed=%v err=%v", allowed, err)
	}
	for _, status := range []int{domainvideo.StatusPendingReview, domainvideo.StatusRejected} {
		repo.assetReferences[0] = domainvideo.AssetReference{
			AuthorID: 42, Status: status, Visibility: domainvideo.VisibilityPublic,
		}
		_, public, allowed, err = service.AuthorizeLocalAsset(context.Background(), "/uploads/video/private.mp4", 0)
		if err != nil || public || allowed {
			t.Fatalf("status %d anonymous authorization: public=%v allowed=%v err=%v", status, public, allowed, err)
		}
		_, public, allowed, err = service.AuthorizeLocalAsset(context.Background(), "/uploads/video/private.mp4", 42)
		if err != nil || public || !allowed {
			t.Fatalf("status %d owner authorization: public=%v allowed=%v err=%v", status, public, allowed, err)
		}
	}

	repo.assetReferences[0].Status = domainvideo.StatusDeleted
	_, _, allowed, err = service.AuthorizeLocalAsset(context.Background(), "/uploads/video/private.mp4", 42)
	if err != nil || allowed {
		t.Fatalf("deleted asset authorization: allowed=%v err=%v", allowed, err)
	}

	repo.assetReferences[0] = domainvideo.AssetReference{
		AuthorID: 77, Status: domainvideo.StatusPublished, Visibility: domainvideo.VisibilityPublic,
	}
	_, public, allowed, err = service.AuthorizeLocalAsset(context.Background(), "/uploads/video/public.mp4", 0)
	if err != nil || public || allowed {
		t.Fatalf("cross-owner public reference authorized asset: public=%v allowed=%v err=%v", public, allowed, err)
	}
	repo.assetReferences[0].AuthorID = 42
	_, public, allowed, err = service.AuthorizeLocalAsset(context.Background(), "/uploads/video/public.mp4", 0)
	if err != nil || !public || !allowed {
		t.Fatalf("public asset authorization: public=%v allowed=%v err=%v", public, allowed, err)
	}
}

func TestLocalUploadOwnershipValidation(t *testing.T) {
	repo := &managementRepoStub{}
	service := NewManagement(repo, nil)
	ctx := context.Background()

	if err := service.RecordLocalUpload(ctx, 42, "/uploads/video/owned.mp4", domainvideo.LocalAssetKindVideo); err != nil {
		t.Fatalf("record local upload: %v", err)
	}
	if err := service.ValidateLocalAssetOwner(ctx, 42, "/uploads/video/owned.mp4", domainvideo.LocalAssetKindVideo); err != nil {
		t.Fatalf("owner validation: %v", err)
	}
	if err := service.ValidateLocalAssetOwner(ctx, 77, "/uploads/video/owned.mp4", domainvideo.LocalAssetKindVideo); err != domainvideo.ErrLocalAssetPermissionDenied {
		t.Fatalf("expected attacker rejection, got %v", err)
	}
}
