package domainvideo

import (
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"strings"
	"time"
)

const (
	StatusDraft     = 1
	StatusPublished = 2
	StatusOffline   = 3
	StatusDeleted   = 4

	VisibilityPublic  = "public"
	VisibilityPrivate = "private"

	MaxTitleLength          = 128
	MaxDescriptionLength    = 512
	MaxIdempotencyKeyLength = 128
)

// Video 是视频聚合根，包含内容信息、发布状态和统计快照。
type Video struct {
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
	IdempotencyKey  string
	MediaAssetID    int64
	CoverAssetID    int64
	MediaStatus     string
	MediaErrorCode  string
	PlaybackSources []domainmedia.PlaybackSource
}

func NewProcessing(authorID int64, title, description string, mediaAssetID, coverAssetID int64, idempotencyKey string) (*Video, error) {
	if authorID <= 0 {
		return nil, ErrInvalidAuthorID
	}
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if title == "" {
		return nil, ErrEmptyTitle
	}
	if len(title) > MaxTitleLength {
		return nil, ErrTitleTooLong
	}
	if len(description) > MaxDescriptionLength {
		return nil, ErrDescriptionTooLong
	}
	if mediaAssetID <= 0 || coverAssetID <= 0 {
		return nil, domainmedia.ErrInvalidAssetID
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}
	now := time.Now()
	return &Video{
		AuthorID: authorID, Title: title, Description: description,
		Status: StatusPublished, Visibility: VisibilityPublic, PublishedAt: &now,
		IdempotencyKey: idempotencyKey, MediaAssetID: mediaAssetID, CoverAssetID: coverAssetID,
		MediaStatus: domainmedia.MediaStatusProcessing,
	}, nil
}

// NewPublished 创建一个直接发布的视频，适合当前项目的发布流程。
func NewPublished(authorID int64, title, description, mediaURL, coverURL, idempotencyKey string) (*Video, error) {
	if authorID <= 0 {
		return nil, ErrInvalidAuthorID
	}

	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	mediaURL = strings.TrimSpace(mediaURL)
	coverURL = strings.TrimSpace(coverURL)
	idempotencyKey = strings.TrimSpace(idempotencyKey)

	if title == "" {
		return nil, ErrEmptyTitle
	}
	if len(title) > MaxTitleLength {
		return nil, ErrTitleTooLong
	}
	if len(description) > MaxDescriptionLength {
		return nil, ErrDescriptionTooLong
	}
	if mediaURL == "" {
		return nil, ErrEmptyMediaURL
	}
	if coverURL == "" {
		return nil, ErrEmptyCoverURL
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}

	now := time.Now()
	// 新建视频直接进入 Published 状态，同时记录发布时间用于 Feed 排序。
	return &Video{
		AuthorID:       authorID,
		Title:          title,
		Description:    description,
		MediaURL:       mediaURL,
		CoverURL:       coverURL,
		Status:         StatusPublished,
		Visibility:     VisibilityPublic,
		PublishedAt:    &now,
		IdempotencyKey: idempotencyKey,
		MediaStatus:    domainmedia.MediaStatusLegacyReady,
	}, nil
}

// RestoreVideo 从数据库查询结果恢复领域对象，统计字段来自 video_stat 表。
func RestoreVideo(
	id int64,
	authorID int64,
	title string,
	description string,
	mediaURL string,
	coverURL string,
	status int,
	likeCount int,
	commentCount int,
	favoriteCount int,
	publishedAt *time.Time,
	createdAt time.Time,
	updatedAt time.Time,
	idempotencyKey string,
) *Video {
	return RestoreVideoWithVisibility(id, authorID, title, description, mediaURL, coverURL, status, VisibilityPublic, likeCount, commentCount, favoriteCount, publishedAt, createdAt, updatedAt, idempotencyKey)
}

func RestoreVideoWithVisibility(
	id int64,
	authorID int64,
	title string,
	description string,
	mediaURL string,
	coverURL string,
	status int,
	visibility string,
	likeCount int,
	commentCount int,
	favoriteCount int,
	publishedAt *time.Time,
	createdAt time.Time,
	updatedAt time.Time,
	idempotencyKey string,
) *Video {
	return RestoreVideoWithMedia(
		id, authorID, title, description, mediaURL, coverURL, status, visibility,
		likeCount, commentCount, favoriteCount, publishedAt, createdAt, updatedAt,
		idempotencyKey, 0, domainmedia.MediaStatusLegacyReady, "", nil,
		0,
	)
}

func RestoreVideoWithMedia(
	id int64,
	authorID int64,
	title string,
	description string,
	mediaURL string,
	coverURL string,
	status int,
	visibility string,
	likeCount int,
	commentCount int,
	favoriteCount int,
	publishedAt *time.Time,
	createdAt time.Time,
	updatedAt time.Time,
	idempotencyKey string,
	mediaAssetID int64,
	mediaStatus string,
	mediaErrorCode string,
	playbackSources []domainmedia.PlaybackSource,
	coverAssetID int64,
) *Video {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	mediaURL = strings.TrimSpace(mediaURL)
	coverURL = strings.TrimSpace(coverURL)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if status == 0 {
		status = StatusPublished
	}
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if !ValidVisibility(visibility) {
		visibility = VisibilityPublic
	}
	mediaStatus = strings.ToLower(strings.TrimSpace(mediaStatus))
	if !domainmedia.ValidMediaStatus(mediaStatus) {
		mediaStatus = domainmedia.MediaStatusLegacyReady
	}

	return &Video{
		ID:              id,
		AuthorID:        authorID,
		Title:           title,
		Description:     description,
		MediaURL:        mediaURL,
		CoverURL:        coverURL,
		Status:          status,
		Visibility:      visibility,
		LikeCount:       likeCount,
		CommentCount:    commentCount,
		FavoriteCount:   favoriteCount,
		PublishedAt:     publishedAt,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		IdempotencyKey:  idempotencyKey,
		MediaAssetID:    mediaAssetID,
		CoverAssetID:    coverAssetID,
		MediaStatus:     mediaStatus,
		MediaErrorCode:  strings.TrimSpace(mediaErrorCode),
		PlaybackSources: domainmedia.SortPlaybackSources(playbackSources),
	}
}

func (v *Video) SetVisibilityBy(authorID int64, visibility string) error {
	if authorID <= 0 {
		return ErrInvalidAuthorID
	}
	if v.AuthorID != authorID {
		return ErrVideoPermissionDenied
	}
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if !ValidVisibility(visibility) {
		return ErrInvalidVisibility
	}
	if v.Status == StatusDeleted || v.Status == StatusOffline {
		return ErrVideoStateNotAllowed
	}
	v.Visibility = visibility
	return nil
}

func (v *Video) IsPubliclyReadable() bool {
	return v != nil &&
		v.Status == StatusPublished &&
		v.Visibility == VisibilityPublic &&
		domainmedia.IsPublicReadyStatus(v.MediaStatus)
}

func ValidVisibility(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VisibilityPublic, VisibilityPrivate:
		return true
	default:
		return false
	}
}

// DeleteBy 执行作者权限校验并把视频置为删除状态。
func (v *Video) DeleteBy(authorID int64) error {
	if authorID <= 0 {
		return ErrInvalidAuthorID
	}
	if v.AuthorID != authorID {
		return ErrVideoPermissionDenied
	}
	// 删除采用软删除，保留原始记录用于审计、统计或后续恢复。
	if v.Status == StatusDeleted {
		return nil
	}
	v.Status = StatusDeleted
	return nil
}
