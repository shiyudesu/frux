package interfaceshttplibrary

import "time"

type videoResponse struct {
	ID            int64      `json:"id"`
	AuthorID      int64      `json:"author_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	MediaURL      string     `json:"media_url"`
	CoverURL      string     `json:"cover_url"`
	Status        int        `json:"status"`
	Visibility    string     `json:"visibility"`
	LikeCount     int        `json:"like_count"`
	CommentCount  int        `json:"comment_count"`
	FavoriteCount int        `json:"favorite_count"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type historyMetadataResponse struct {
	LastScene     string    `json:"last_scene"`
	LastEventType string    `json:"last_event_type"`
	LastWatchMs   int       `json:"last_watch_ms"`
	Completed     bool      `json:"completed"`
	LastWatchedAt time.Time `json:"last_watched_at"`
}

type videoItemResponse struct {
	Video     videoResponse            `json:"video"`
	UpdatedAt time.Time                `json:"updated_at"`
	History   *historyMetadataResponse `json:"history,omitempty"`
}

type videoPageResponse struct {
	Items      []videoItemResponse `json:"items"`
	NextCursor string              `json:"next_cursor"`
	HasMore    bool                `json:"has_more"`
}

type watchLaterStateResponse struct {
	VideoID   int64     `json:"video_id"`
	Active    bool      `json:"active"`
	UpdatedAt time.Time `json:"updated_at"`
}
