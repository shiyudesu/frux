package applicationvideo

import (
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	"strings"
	"time"
)

type PublishedEvent struct {
	EventID             string    `json:"event_id"`
	VideoID             int64     `json:"video_id"`
	AuthorID            int64     `json:"author_id"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	MediaURL            string    `json:"media_url"`
	CoverURL            string    `json:"cover_url"`
	MediaAssetID        int64     `json:"media_asset_id,omitempty"`
	CoverAssetID        int64     `json:"cover_asset_id,omitempty"`
	MediaProfileVersion string    `json:"media_profile_version,omitempty"`
	VideoVersion        int       `json:"video_version,omitempty"`
	PublishedAt         time.Time `json:"published_at"`
	OccurredAt          time.Time `json:"occurred_at"`
}

func NewPublishedEvent(video *domainvideo.Video) *PublishedEvent {
	if video == nil || video.PublishedAt == nil || !video.IsPubliclyReadable() {
		return nil
	}
	return &PublishedEvent{
		EventID:             publishedEventID(video),
		VideoID:             video.ID,
		AuthorID:            video.AuthorID,
		Title:               strings.TrimSpace(video.Title),
		Description:         strings.TrimSpace(video.Description),
		MediaURL:            strings.TrimSpace(video.MediaURL),
		CoverURL:            strings.TrimSpace(video.CoverURL),
		MediaAssetID:        video.MediaAssetID,
		CoverAssetID:        video.CoverAssetID,
		MediaProfileVersion: strings.TrimSpace(video.MediaProfileVersion),
		VideoVersion:        video.Version,
		PublishedAt:         video.PublishedAt.UTC(),
		OccurredAt:          time.Now().UTC(),
	}
}

func publishedEventID(video *domainvideo.Video) string {
	return domainmessage.PublicationEventID(video.ID, video.ReviewVersion)
}
