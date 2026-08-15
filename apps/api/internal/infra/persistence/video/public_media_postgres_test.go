package infravideo_test

import (
	"context"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	inframedia "github.com/shiyudesu/frux/internal/infra/persistence/media"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
)

func TestResolvePublicMediaObjectPostgreSQL(t *testing.T) {
	db := openSearchPostgres(t)
	now := time.Now().UTC()
	assetID := int64(700)
	video := searchVideoModel(
		701,
		"public media",
		"",
		domainvideo.StatusPublished,
		domainvideo.VisibilityPublic,
		domainmedia.MediaStatusReady,
		now,
	)
	video.MediaAssetID = &assetID
	if err := db.Create(&video).Error; err != nil {
		t.Fatal(err)
	}
	variants := []inframedia.VariantModel{
		{
			ID: 711, AssetID: assetID, SourceType: domainmedia.SourceTypeDASH,
			Format: "mpd", ObjectKey: "processed/700/v1/dash/manifest.mpd",
			ExposureGeneration: testStringPointer("generation-a"), Role: domainmedia.VariantRoleManifest,
			State: domainmedia.VariantStateReady, ChecksumSHA256: testSHA256("a"),
			SizeBytes: 100, Public: true,
		},
		{
			ID: 712, AssetID: assetID, SourceType: domainmedia.SourceTypeDASH,
			Format: "m4s", ObjectKey: "processed/700/v1/dash/chunk-1.m4s",
			ExposureGeneration: testStringPointer("generation-a"), Role: domainmedia.VariantRoleSegment,
			State: domainmedia.VariantStateReady, ChecksumSHA256: testSHA256("b"),
			SizeBytes: 200, Public: true,
		},
	}
	if err := db.Create(&variants).Error; err != nil {
		t.Fatal(err)
	}

	repository := infravideo.New(db)
	object, err := repository.ResolvePublicMediaObject(
		context.Background(),
		"media/v3/generation-a/711/manifest.mpd",
	)
	if err != nil || object == nil || object.StorageKey != variants[0].ObjectKey {
		t.Fatalf("manifest resolution object=%+v err=%v", object, err)
	}
	object, err = repository.ResolvePublicMediaObject(
		context.Background(),
		"media/v3/generation-a/711/chunk-1.m4s",
	)
	if err != nil || object == nil || object.StorageKey != variants[1].ObjectKey {
		t.Fatalf("relative segment resolution object=%+v err=%v", object, err)
	}
	object, err = repository.ResolvePublicMediaObject(
		context.Background(),
		"media/v2/legacy-generation/700/v1/dash/manifest.mpd",
	)
	if err != nil || object == nil {
		t.Fatalf("legacy compatibility object=%+v err=%v", object, err)
	}
	object, err = repository.ResolvePublicMediaObject(
		context.Background(),
		"media/700/v1/dash/manifest.mpd",
	)
	if err != nil || object == nil {
		t.Fatalf("pre-v2 compatibility object=%+v err=%v", object, err)
	}
	object, err = repository.ResolvePublicMediaObject(
		context.Background(),
		"media/v3/stale-generation/711/manifest.mpd",
	)
	if err != nil || object != nil {
		t.Fatalf("stale generation object=%+v err=%v", object, err)
	}

	if err := db.Model(&infravideo.VideoModel{}).
		Where("id = ?", video.ID).
		Update("status", domainvideo.StatusOffline).Error; err != nil {
		t.Fatal(err)
	}
	object, err = repository.ResolvePublicMediaObject(
		context.Background(),
		"media/v3/generation-a/711/manifest.mpd",
	)
	if err != nil || object != nil {
		t.Fatalf("offline resolution object=%+v err=%v", object, err)
	}
}

func testSHA256(character string) string {
	result := ""
	for len(result) < 64 {
		result += character
	}
	return result[:64]
}

func testStringPointer(value string) *string {
	return &value
}
