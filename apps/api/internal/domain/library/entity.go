package domainlibrary

import (
	domainmedia "GCFeed/internal/domain/media"
	"context"
	"errors"
	"strings"
	"time"
)

const (
	WatchLaterStatusActive  = 1
	WatchLaterStatusRemoved = 2
	MaxLimit                = 100
)

var ErrInvalidUserID = errors.New("user id must be positive")
var ErrInvalidVideoID = errors.New("video id must be positive")
var ErrInvalidCursor = errors.New("invalid cursor")
var ErrInvalidLimit = errors.New("invalid limit")
var ErrVideoNotFound = errors.New("video not found")
var ErrLikedVideosPrivate = errors.New("liked videos are private")

type WatchLater struct {
	UserID    int64
	VideoID   int64
	Status    int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Cursor struct {
	UpdatedAt time.Time
	VideoID   int64
}

type VideoCard struct {
	ID              int64
	AuthorID        int64
	Title           string
	Description     string
	MediaURL        string
	CoverURL        string
	Status          int
	Visibility      string
	LikeCount       int
	CommentCount    int
	FavoriteCount   int
	PublishedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	MediaStatus     string
	PlaybackSources []domainmedia.PlaybackSource
}

type VideoCandidate struct {
	VideoID   int64
	UpdatedAt time.Time
}

type HistoryCandidate struct {
	VideoID        int64
	UpdatedAt      time.Time
	LastScene      string
	LastEventType  string
	LastPositionMs int
	LastWatchMs    int
	Completed      bool
}

type VideoItem struct {
	Video     *VideoCard
	UpdatedAt time.Time
	History   *HistoryCandidate
}

func NewWatchLater(userID, videoID int64, active bool) (*WatchLater, error) {
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	status := WatchLaterStatusRemoved
	if active {
		status = WatchLaterStatusActive
	}
	return &WatchLater{UserID: userID, VideoID: videoID, Status: status}, nil
}

func RestoreWatchLater(userID, videoID int64, status int, createdAt, updatedAt time.Time) *WatchLater {
	return &WatchLater{UserID: userID, VideoID: videoID, Status: status, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func (w *WatchLater) Active() bool {
	return w != nil && w.Status == WatchLaterStatusActive
}

func NormalizeVisibility(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

type WatchLaterRepository interface {
	SetWatchLater(ctx context.Context, fact *WatchLater) (*WatchLater, error)
	ListWatchLater(ctx context.Context, userID int64, cursor *Cursor, limit int) ([]VideoCandidate, error)
}

type ActionIndex interface {
	ListActionVideos(ctx context.Context, userID int64, actionType string, cursor *Cursor, limit int) ([]VideoCandidate, error)
}

type HistoryIndex interface {
	ListHistoryVideos(ctx context.Context, userID int64, cursor *Cursor, limit int) ([]HistoryCandidate, error)
	DeleteHistory(ctx context.Context, userID, videoID int64) error
	ClearHistory(ctx context.Context, userID int64) error
}

type VideoCatalog interface {
	BatchGetReadable(ctx context.Context, viewerID int64, videoIDs []int64, publicOnly bool) (map[int64]*VideoCard, error)
}

type PrivacyReader interface {
	LikedVideosPublic(ctx context.Context, userID int64) (bool, error)
}
