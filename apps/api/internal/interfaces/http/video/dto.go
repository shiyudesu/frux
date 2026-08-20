package interfaceshttpvideo

import (
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"time"
)

// CreateVideoRequest 是发布视频的 JSON 请求体。
type CreateVideoRequest struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	MediaURL     string `json:"media_url"`
	CoverURL     string `json:"cover_url"`
	MediaAssetID int64  `json:"media_asset_id,omitempty"`
	CoverAssetID int64  `json:"cover_asset_id,omitempty"`
}

type CreatorVideoQueryRequest struct {
	VideoID     int64  `json:"video_id"`
	Visibility  string `json:"visibility"`
	Statuses    []int  `json:"statuses"`
	Query       string `json:"query"`
	CreatedFrom string `json:"created_from"`
	CreatedTo   string `json:"created_to"`
	Cursor      string `json:"cursor"`
	Limit       int    `json:"limit"`
}

type BatchVideoActionRequest struct {
	VideoIDs []int64 `json:"video_ids"`
	Action   string  `json:"action"`
}

// videoResponse 是视频详情响应，包含视频主体字段和互动计数。
type videoResponse struct {
	ID              int64                        `json:"id"`
	AuthorID        int64                        `json:"author_id"`
	Title           string                       `json:"title"`
	Description     string                       `json:"description"`
	MediaURL        string                       `json:"media_url"`
	CoverURL        string                       `json:"cover_url"`
	Status          int                          `json:"status"`
	Visibility      string                       `json:"visibility"`
	LikeCount       int                          `json:"like_count"`
	CommentCount    int                          `json:"comment_count"`
	FavoriteCount   int                          `json:"favorite_count"`
	PublishedAt     *time.Time                   `json:"published_at,omitempty"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
	MediaAssetID    int64                        `json:"media_asset_id,omitempty"`
	CoverAssetID    int64                        `json:"cover_asset_id,omitempty"`
	MediaStatus     string                       `json:"media_status"`
	ReviewVersion   int                          `json:"review_version"`
	MediaErrorCode  string                       `json:"media_error_code,omitempty"`
	PlaybackSources []domainmedia.PlaybackSource `json:"playback_sources,omitempty"`
}

type cursorVideoListResponse struct {
	Items      []videoResponse `json:"items"`
	NextCursor string          `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

type creatorArchiveMonthResponse struct {
	Months []string `json:"months"`
}

type batchVideoActionResponse struct {
	Action   string  `json:"action"`
	VideoIDs []int64 `json:"video_ids"`
	Replayed bool    `json:"replayed"`
}

// videoListResponse 是 offset 分页列表响应。
type videoListResponse struct {
	Items  []videoResponse `json:"items"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}
