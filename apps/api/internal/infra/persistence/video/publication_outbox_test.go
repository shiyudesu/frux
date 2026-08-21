package infravideo

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPublicationEventFromModelPreservesImmutableMediaIdentity(t *testing.T) {
	publishedAt := time.Date(2026, 8, 21, 3, 0, 0, 0, time.UTC)
	mediaAssetID := int64(21)
	coverAssetID := int64(22)
	event := publicationEventFromModel(VideoModel{
		ID: 12, AuthorID: 15, Title: " title ", Description: " description ",
		MediaURL: " https://media.example/video.mp4 ", CoverURL: " https://media.example/cover.jpg ",
		MediaAssetID: &mediaAssetID, CoverAssetID: &coverAssetID,
		ReviewVersion: 1, Version: 3, PublishedAt: &publishedAt,
	})
	if event.MediaAssetID != mediaAssetID || event.CoverAssetID != coverAssetID ||
		event.VideoVersion != 3 || event.Title != "title" || event.Description != "description" {
		t.Fatalf("event=%#v", event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["media_asset_id"] != float64(mediaAssetID) ||
		decoded["cover_asset_id"] != float64(coverAssetID) ||
		decoded["video_version"] != float64(3) {
		t.Fatalf("payload=%s", payload)
	}
}
