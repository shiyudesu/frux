package applicationvideo

import (
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	"context"
	"reflect"
	"testing"
	"time"
)

type managementRepoStub struct {
	lastIDs         []int64
	lastFingerprint string
	assetReferences []domainvideo.AssetReference
	localAssets     map[string]*domainvideo.LocalAsset
	collection      *domainvideo.Collection
	collectionItems []*domainvideo.CollectionItem
	lastUpdate      domainvideo.CollectionUpdate
}

func (r *managementRepoStub) QueryCreatorVideos(context.Context, domainvideo.CreatorVideoFilter) ([]*domainvideo.Video, error) {
	return nil, nil
}
func (r *managementRepoStub) ApplyBatch(_ context.Context, userID int64, action string, videoIDs []int64, key, fingerprint string) (*domainvideo.BatchOperation, bool, error) {
	r.lastIDs = append([]int64(nil), videoIDs...)
	r.lastFingerprint = fingerprint
	return &domainvideo.BatchOperation{UserID: userID, Key: key, Action: action, VideoIDs: videoIDs}, false, nil
}
func (r *managementRepoStub) CreateCollection(context.Context, *domainvideo.Collection) (*domainvideo.Collection, bool, error) {
	return nil, false, nil
}
func (r *managementRepoStub) ListCollections(context.Context, int64, bool, *domainvideo.CollectionCursor, int) ([]*domainvideo.Collection, error) {
	return nil, nil
}
func (r *managementRepoStub) GetCollection(_ context.Context, collectionID int64) (*domainvideo.Collection, error) {
	if r.collection == nil || r.collection.ID != collectionID {
		return nil, domainvideo.ErrCollectionNotFound
	}
	cloned := *r.collection
	return &cloned, nil
}
func (r *managementRepoStub) UpdateCollection(_ context.Context, collection *domainvideo.Collection, update domainvideo.CollectionUpdate) error {
	r.lastUpdate = update
	if update.Title != nil {
		r.collection.Title = collection.Title
	}
	if update.Description != nil {
		r.collection.Description = collection.Description
	}
	if update.Visibility != nil {
		r.collection.Visibility = collection.Visibility
	}
	return nil
}
func (r *managementRepoStub) DeleteCollection(context.Context, *domainvideo.Collection) error {
	return nil
}
func (r *managementRepoStub) SetCollectionItem(context.Context, int64, int64, int64, bool) error {
	return nil
}
func (r *managementRepoStub) ListCollectionItems(context.Context, int64, bool) ([]*domainvideo.CollectionItem, error) {
	return append([]*domainvideo.CollectionItem(nil), r.collectionItems...), nil
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

func TestUpdateCollectionPersistsOnlySuppliedFieldsAndHydratesMembers(t *testing.T) {
	repo := &managementRepoStub{
		collection: &domainvideo.Collection{
			ID: 11, OwnerID: 7, Title: "original", Description: "preserve",
			Visibility: domainvideo.VisibilityPublic, Status: domainvideo.CollectionStatusActive,
		},
		collectionItems: []*domainvideo.CollectionItem{{CollectionID: 11, VideoID: 99}},
	}
	service := NewManagement(repo, nil)
	title := " renamed "

	updated, err := service.UpdateCollection(context.Background(), 7, 11, &title, nil, nil)
	if err != nil {
		t.Fatalf("update collection: %v", err)
	}
	if updated.Title != "renamed" || updated.Description != "preserve" || updated.Visibility != domainvideo.VisibilityPublic {
		t.Fatalf("partial update overwrote unrelated fields: %+v", updated)
	}
	if repo.lastUpdate.Title == nil || repo.lastUpdate.Description != nil || repo.lastUpdate.Visibility != nil {
		t.Fatalf("repository received wrong update mask: %+v", repo.lastUpdate)
	}
	if len(updated.Items) != 1 || updated.Items[0].VideoID != 99 {
		t.Fatalf("updated collection did not hydrate members: %+v", updated.Items)
	}
	if updated.MemberCount != 1 {
		t.Fatalf("updated collection member count = %d, want 1", updated.MemberCount)
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
