package domainvideo

import (
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"strings"
	"time"
)

const (
	StatusDraft         = 1
	StatusPublished     = 2
	StatusOffline       = 3
	StatusDeleted       = 4
	StatusPendingReview = 5
	StatusRejected      = 6

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
	ReviewVersion   int
	Version         int
	PlaybackSources []domainmedia.PlaybackSource
}

type LifecycleTransition string

const (
	LifecycleApprove     LifecycleTransition = "approve"
	LifecycleReject      LifecycleTransition = "reject"
	LifecycleTakeOffline LifecycleTransition = "take_offline"
	LifecycleRestore     LifecycleTransition = "restore"
)

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
	return &Video{
		AuthorID: authorID, Title: title, Description: description,
		Status: StatusPendingReview, Visibility: VisibilityPublic,
		IdempotencyKey: idempotencyKey, MediaAssetID: mediaAssetID, CoverAssetID: coverAssetID,
		MediaStatus: domainmedia.MediaStatusProcessing, ReviewVersion: 1,
		Version: 1,
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

	// 兼容媒体路径与生产媒体路径使用相同审核生命周期。
	return &Video{
		AuthorID:       authorID,
		Title:          title,
		Description:    description,
		MediaURL:       mediaURL,
		CoverURL:       coverURL,
		Status:         StatusPendingReview,
		Visibility:     VisibilityPublic,
		IdempotencyKey: idempotencyKey,
		MediaStatus:    domainmedia.MediaStatusLegacyReady,
		ReviewVersion:  1,
		Version:        1,
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
	return RestoreVideoWithReviewVersion(
		id, authorID, title, description, mediaURL, coverURL, status, visibility,
		likeCount, commentCount, favoriteCount, publishedAt, createdAt, updatedAt,
		idempotencyKey, mediaAssetID, mediaStatus, mediaErrorCode, playbackSources,
		coverAssetID, 1,
	)
}

func RestoreVideoWithReviewVersion(
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
	reviewVersion int,
) *Video {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	mediaURL = strings.TrimSpace(mediaURL)
	coverURL = strings.TrimSpace(coverURL)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if status == 0 {
		status = StatusPublished
	} else if !ValidStatus(status) {
		status = StatusDraft
	}
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if !ValidVisibility(visibility) {
		visibility = VisibilityPublic
	}
	mediaStatus = strings.ToLower(strings.TrimSpace(mediaStatus))
	if !domainmedia.ValidMediaStatus(mediaStatus) {
		mediaStatus = domainmedia.MediaStatusLegacyReady
	}
	if reviewVersion <= 0 {
		reviewVersion = 1
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
		ReviewVersion:   reviewVersion,
		Version:         1,
		PlaybackSources: domainmedia.SortPlaybackSources(playbackSources),
	}
}

func (v *Video) HasCurrentReviewVersion(version int) bool {
	return v != nil && v.ReviewVersion > 0 && version == v.ReviewVersion
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

func (v *Video) Approve(approvedAt time.Time) error {
	if v == nil || approvedAt.IsZero() {
		return ErrVideoStateNotAllowed
	}
	switch v.Status {
	case StatusPublished:
		return nil
	case StatusPendingReview:
		approvedAt = approvedAt.UTC().Truncate(time.Microsecond)
		v.Status = StatusPublished
		if v.PublishedAt == nil {
			v.PublishedAt = &approvedAt
		}
		return nil
	default:
		return ErrVideoStateNotAllowed
	}
}

func (v *Video) ApplyLifecycleTransition(transition LifecycleTransition, at time.Time) error {
	switch transition {
	case LifecycleApprove:
		return v.Approve(at)
	case LifecycleReject:
		return v.Reject()
	case LifecycleTakeOffline:
		return v.TakeOffline()
	case LifecycleRestore:
		return v.Restore()
	default:
		return ErrVideoStateNotAllowed
	}
}

func (v *Video) Reject() error {
	if v == nil {
		return ErrVideoStateNotAllowed
	}
	switch v.Status {
	case StatusRejected:
		return nil
	case StatusPendingReview:
		v.Status = StatusRejected
		return nil
	default:
		return ErrVideoStateNotAllowed
	}
}

func (v *Video) TakeOffline() error {
	if v == nil {
		return ErrVideoStateNotAllowed
	}
	switch v.Status {
	case StatusOffline:
		return nil
	case StatusPublished:
		v.Status = StatusOffline
		return nil
	default:
		return ErrVideoStateNotAllowed
	}
}

func (v *Video) Restore() error {
	if v == nil {
		return ErrVideoStateNotAllowed
	}
	switch v.Status {
	case StatusPublished:
		return nil
	case StatusOffline:
		if v.PublishedAt == nil {
			return ErrVideoStateNotAllowed
		}
		v.Status = StatusPublished
		return nil
	default:
		return ErrVideoStateNotAllowed
	}
}

func ValidStatus(status int) bool {
	return status >= StatusDraft && status <= StatusRejected
}

func ValidLifecycleTransition(from, to int) bool {
	if !ValidStatus(from) || !ValidStatus(to) {
		return false
	}
	if from == to {
		return true
	}
	if to == StatusDeleted {
		return from != StatusDeleted
	}
	switch {
	case from == StatusPendingReview && (to == StatusPublished || to == StatusRejected):
		return true
	case from == StatusPublished && to == StatusOffline:
		return true
	case from == StatusOffline && to == StatusPublished:
		return true
	default:
		return false
	}
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
