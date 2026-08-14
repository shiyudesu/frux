package interfaceshttpsearch

import (
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"time"
)

type videoResultResponse struct {
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
	PublishedAt     time.Time                    `json:"published_at"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
	MediaStatus     string                       `json:"media_status"`
	PlaybackSources []domainmedia.PlaybackSource `json:"playback_sources,omitempty"`
}

type userResultResponse struct {
	ID        int64  `json:"id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Bio       string `json:"bio"`
}

type videoPageResponse struct {
	Items      []videoResultResponse `json:"items"`
	NextCursor string                `json:"next_cursor"`
	HasMore    bool                  `json:"has_more"`
}

type userPageResponse struct {
	Items      []userResultResponse `json:"items"`
	NextCursor string               `json:"next_cursor"`
	HasMore    bool                 `json:"has_more"`
}
