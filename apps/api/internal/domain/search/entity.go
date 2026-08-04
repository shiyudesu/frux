package domainsearch

import (
	domainmedia "GCFeed/internal/domain/media"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	CategoryVideos = "videos"
	CategoryUsers  = "users"

	MaxQueryRunes = 64
	MaxLimit      = 50

	VideoRelevanceExactTitle      = 1
	VideoRelevanceTitlePrefix     = 2
	VideoRelevanceTitleContains   = 3
	VideoRelevanceDescriptionOnly = 4

	UserRelevanceExactAccount     = 1
	UserRelevanceAccountPrefix    = 2
	UserRelevanceNicknamePrefix   = 3
	UserRelevanceAccountContains  = 4
	UserRelevanceNicknameContains = 5
)

type VideoCursor struct {
	Relevance   int
	PublishedAt time.Time
	VideoID     int64
}

type UserCursor struct {
	Relevance int
	UpdatedAt time.Time
	UserID    int64
}

type VideoIndexItem struct {
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
	PublishedAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	MediaStatus     string
	PlaybackSources []domainmedia.PlaybackSource
	Relevance       int
}

type UserIndexItem struct {
	ID        int64
	Account   string
	Nickname  string
	AvatarURL string
	Bio       string
	UpdatedAt time.Time
	Relevance int
}

func NormalizeQuery(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalidQuery
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrEmptyQuery
	}
	if utf8.RuneCountInString(value) > MaxQueryRunes {
		return "", ErrQueryTooLong
	}
	return value, nil
}

func EscapeLikeLiteral(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func ValidVideoRelevance(value int) bool {
	return value >= VideoRelevanceExactTitle && value <= VideoRelevanceDescriptionOnly
}

func ValidUserRelevance(value int) bool {
	return value >= UserRelevanceExactAccount && value <= UserRelevanceNicknameContains
}
