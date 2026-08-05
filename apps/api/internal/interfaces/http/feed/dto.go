package interfaceshttpfeed

import (
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"time"
)

// feedQueryRequest 是复杂 Feed 查询入口的请求体。
type feedQueryRequest struct {
	Scene   string                        `json:"scene"`
	Cursor  string                        `json:"cursor"`
	Limit   *int                          `json:"limit"`
	Context *recommendationContextRequest `json:"context"`
}

type recommendationContextRequest struct {
	RequestID            string   `json:"request_id"`
	SessionID            string   `json:"session_id"`
	RefreshIndex         int      `json:"refresh_index"`
	RecentVideoIDs       []int64  `json:"recent_video_ids"`
	CurrentVideoID       int64    `json:"current_video_id"`
	NetworkClass         string   `json:"network_class"`
	SaveData             bool     `json:"save_data"`
	ViewportClass        string   `json:"viewport_class"`
	PlaybackCapabilities []string `json:"playback_capabilities"`
}

// feedItemsResponse 是 Feed 游标分页响应。
type feedItemsResponse struct {
	Scene      string             `json:"scene"`
	RequestID  string             `json:"request_id,omitempty"`
	Items      []feedItemResponse `json:"items"`
	NextCursor string             `json:"next_cursor"`
	HasMore    bool               `json:"has_more"`
}

// feedItemResponse 是 Feed 中单条视频卡片的响应结构。
type feedItemResponse struct {
	VideoID         int64                        `json:"video_id"`
	AuthorID        int64                        `json:"author_id"`
	AuthorNickname  string                       `json:"author_nickname"`
	AuthorAvatarURL string                       `json:"author_avatar_url"`
	Title           string                       `json:"title"`
	Description     string                       `json:"description"`
	MediaURL        string                       `json:"media_url"`
	CoverURL        string                       `json:"cover_url"`
	LikeCount       int                          `json:"like_count"`
	CommentCount    int                          `json:"comment_count"`
	FavoriteCount   int                          `json:"favorite_count"`
	Liked           bool                         `json:"liked"`
	Favorited       bool                         `json:"favorited"`
	PublishedAt     time.Time                    `json:"published_at"`
	MediaStatus     string                       `json:"media_status"`
	PlaybackSources []domainmedia.PlaybackSource `json:"playback_sources,omitempty"`
}
