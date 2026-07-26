package domainmedia

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildUploadObjectKeyBindsOwnerAndSession(t *testing.T) {
	key, err := BuildUploadObjectKey(42, "session-7", AssetKindVideo, ".mp4")
	if err != nil {
		t.Fatalf("build upload key: %v", err)
	}
	if key != "uploads/42/session-7/video/source.mp4" {
		t.Fatalf("unexpected upload key %q", key)
	}
	if _, err := BuildUploadObjectKey(0, "session-7", AssetKindVideo, ".mp4"); !errors.Is(err, ErrInvalidOwnerID) {
		t.Fatalf("expected invalid owner, got %v", err)
	}
	if _, err := NewMediaAsset(42, AssetKindVideo, StorageBackendS3, key, "video/mp4", 10, strings.Repeat("z", 64)); !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("expected invalid checksum, got %v", err)
	}
}

func TestSortPlaybackSourcesIsDeterministic(t *testing.T) {
	sources := []PlaybackSource{
		{Type: SourceTypeDASH, URL: "manifest.mpd", SortOrder: 30},
		{Type: SourceTypeMP4, URL: "high.mp4", SortOrder: 20, Bitrate: 2_000_000},
		{Type: SourceTypeMP4, URL: "baseline.mp4", SortOrder: 10, Bitrate: 1_000_000},
	}
	sorted := SortPlaybackSources(sources)
	if sorted[0].URL != "baseline.mp4" || sorted[1].URL != "high.mp4" || sorted[2].URL != "manifest.mpd" {
		t.Fatalf("unexpected order: %+v", sorted)
	}
}
