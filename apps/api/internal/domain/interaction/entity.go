package domaininteraction

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ActionTypeLike     = "LIKE"
	ActionTypeFavorite = "FAVORITE"

	ActionStatusActive   = 1
	ActionStatusCanceled = 2

	CommentStatusNormal      = 1
	CommentStatusSelfDeleted = 2
	CommentStatusModerated   = 3
	// CommentStatusDeleted is retained for source compatibility with flat-comment callers.
	CommentStatusDeleted = CommentStatusSelfDeleted

	CommentSortLatest = "latest"
	CommentSortHot    = "hot"

	CommentCursorVersion = 1
	ReplyPreviewLimit    = 3

	CommentNotificationTypeRoot  = "COMMENT"
	CommentNotificationTypeReply = "COMMENT_REPLY"
	CommentNotificationTypeLike  = "COMMENT_LIKE"

	CommentNotificationStatePending   = "pending"
	CommentNotificationStateDelivered = "delivered"
	CommentNotificationStateTerminal  = "terminal"

	MaxCommentContentLength          = 1000
	MaxIdempotencyKeyLength          = 128
	MaxActionEventIDLength           = 128
	MaxRecommendationRequestIDLength = 64
	MaxLimit                         = 100
)

// Action 表示用户对视频的一类互动状态，例如点赞或收藏。
type Action struct {
	ID             int64
	UserID         int64
	VideoID        int64
	ActionType     string
	Status         int
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ActionCursor struct {
	UpdatedAt time.Time
	VideoID   int64
}

type ActionVideo struct {
	VideoID   int64
	UpdatedAt time.Time
}

type ViewerActionState struct {
	VideoID   int64
	Liked     bool
	Favorited bool
}

// ActionStateSnapshot is the durable baseline used when Redis has no action state.
type ActionStateSnapshot struct {
	Exists                  bool
	Active                  bool
	IdempotencyKey          string
	RecommendationRequestID string
	Version                 int64
	EventID                 string
	OccurredAt              time.Time
	UpdatedAt               time.Time
}

// AcceptedActionEvent represents an interaction that passed public request validation before enqueueing.
type AcceptedActionEvent struct {
	EventID                 string
	UserID                  int64
	VideoID                 int64
	ActionType              string
	Active                  bool
	IdempotencyKey          string
	RecommendationRequestID string
	Version                 int64
	OccurredAt              time.Time
}

// ActionEventComesAfter compares the durable order tuple used for one action fact.
func ActionEventComesAfter(version int64, occurredAt time.Time, eventID string, latestVersion int64, latestOccurredAt time.Time, latestEventID string) bool {
	if version != latestVersion {
		return version > latestVersion
	}
	occurredAt = occurredAt.UTC().Truncate(time.Microsecond)
	latestOccurredAt = latestOccurredAt.UTC().Truncate(time.Microsecond)
	if latestOccurredAt.IsZero() || occurredAt.After(latestOccurredAt) {
		return true
	}
	if occurredAt.Before(latestOccurredAt) {
		return false
	}
	return eventID > latestEventID
}

// Comment 表示视频评论，包含评论者展示信息和软删除状态。
type Comment struct {
	ID                   int64
	VideoID              int64
	UserID               int64
	UserNickname         string
	UserAvatarURL        string
	RootCommentID        int64
	ReplyToCommentID     int64
	ReplyToUserID        int64
	ReplyToUserNickname  string
	ReplyToUserAvatarURL string
	Content              string
	Status               int
	ReplyCount           int
	LikeCount            int
	HotScore             int64
	RequestFingerprint   string
	IdempotencyKey       string
	Liked                bool
	CanDelete            bool
	ReplyPreviews        []*Comment
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// CommentCursor stores a sort-bound root page position.
type CommentCursor struct {
	Version   int
	Sort      string
	HotScore  int64
	CreatedAt time.Time
	CommentID int64
}

type ReplyCursor struct {
	Version   int
	CreatedAt time.Time
	CommentID int64
}

type CommentViewer struct {
	UserID int64
	Role   string
}

type CommentRootQuery struct {
	VideoID      int64
	Viewer       CommentViewer
	Sort         string
	Cursor       *CommentCursor
	Limit        int
	PreviewLimit int
}

type CommentReplyQuery struct {
	RootCommentID int64
	Viewer        CommentViewer
	Cursor        *ReplyCursor
	Limit         int
}

type CommentPage struct {
	Items        []*Comment
	CommentCount int
}

type CommentThreadContext struct {
	Root         *Comment
	Replies      []*Comment
	Target       *Comment
	CommentCount int
}

type CommentLikeResult struct {
	CommentID     int64
	RootCommentID int64
	Liked         bool
	LikeCount     int
}

type CommentMutationResult struct {
	Comment        *Comment
	CommentCount   int
	VideoDelta     int
	RootReplyDelta int
}

type CommentDeletionResult struct {
	Comment        *Comment
	CommentCount   int
	RootReplyCount int
	VideoDelta     int
	DeletedCount   int
	ThreadHidden   bool
	Tombstone      bool
}

type CommentNotification struct {
	EventID       string
	RecipientID   int64
	ActorID       int64
	MessageType   string
	Title         string
	Content       string
	VideoID       int64
	RootCommentID int64
	CommentID     int64
	State         string
	Attempts      int
	AvailableAt   time.Time
	LeaseOwner    string
	LeaseUntil    *time.Time
	LastError     string
	DeliveredAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// VideoStat 保存互动模块需要的视频统计快照。
type VideoStat struct {
	VideoID       int64
	LikeCount     int
	CommentCount  int
	FavoriteCount int
}

// UserProfile 保存互动消息需要展示的用户资料。
type UserProfile struct {
	ID        int64
	Nickname  string
	AvatarURL string
}

// NormalizeActionType 统一行为类型大小写，避免外层传入 like、LIKE 等不同写法。
func NormalizeActionType(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value != ActionTypeLike && value != ActionTypeFavorite {
		return "", ErrInvalidActionType
	}
	return value, nil
}

func NewAcceptedActionEvent(eventID string, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, version int64, occurredAt time.Time) (*AcceptedActionEvent, error) {
	return NewAcceptedActionEventWithRecommendation(eventID, userID, videoID, actionType, active, idempotencyKey, "", version, occurredAt)
}

func NewAcceptedActionEventWithRecommendation(eventID string, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, recommendationRequestID string, version int64, occurredAt time.Time) (*AcceptedActionEvent, error) {
	eventID = strings.TrimSpace(eventID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	recommendationRequestID = strings.TrimSpace(recommendationRequestID)
	if eventID == "" || len(eventID) > MaxActionEventIDLength {
		return nil, ErrInvalidActionEventID
	}
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	normalizedActionType, err := NormalizeActionType(actionType)
	if err != nil {
		return nil, err
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}
	if len(recommendationRequestID) > MaxRecommendationRequestIDLength {
		return nil, ErrRecommendationRequestIDTooLong
	}
	if version < 0 {
		return nil, ErrInvalidActionEvent
	}
	if occurredAt.IsZero() {
		return nil, ErrInvalidActionEventTime
	}
	return &AcceptedActionEvent{
		EventID:                 eventID,
		UserID:                  userID,
		VideoID:                 videoID,
		ActionType:              normalizedActionType,
		Active:                  active,
		IdempotencyKey:          idempotencyKey,
		RecommendationRequestID: recommendationRequestID,
		Version:                 version,
		OccurredAt:              occurredAt.UTC().Truncate(time.Microsecond),
	}, nil
}

// NewComment 创建评论领域对象，负责校验视频、用户、内容和幂等键。
func NewComment(videoID int64, userID int64, content string, idempotencyKey string) (*Comment, error) {
	return NewRootComment(videoID, userID, content, idempotencyKey)
}

func NewRootComment(videoID int64, userID int64, content string, idempotencyKey string) (*Comment, error) {
	return newComment(videoID, userID, 0, 0, content, idempotencyKey)
}

func NewReplyComment(videoID int64, userID int64, targetCommentID int64, content string, idempotencyKey string) (*Comment, error) {
	if targetCommentID <= 0 {
		return nil, ErrInvalidReplyTargetID
	}
	return newComment(videoID, userID, 0, targetCommentID, content, idempotencyKey)
}

func newComment(videoID int64, userID int64, rootCommentID int64, replyToCommentID int64, content string, idempotencyKey string) (*Comment, error) {
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}

	content = strings.TrimSpace(content)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if content == "" {
		return nil, ErrEmptyCommentContent
	}
	if utf8.RuneCountInString(content) > MaxCommentContentLength {
		return nil, ErrCommentContentTooLong
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}

	comment := &Comment{
		VideoID:          videoID,
		UserID:           userID,
		RootCommentID:    rootCommentID,
		ReplyToCommentID: replyToCommentID,
		Content:          content,
		Status:           CommentStatusNormal,
		IdempotencyKey:   idempotencyKey,
	}
	comment.RequestFingerprint = CommentRequestFingerprint(videoID, rootCommentID, replyToCommentID, content)
	return comment, nil
}

func CommentRequestFingerprint(videoID int64, rootCommentID int64, replyToCommentID int64, content string) string {
	canonical := strconv.FormatInt(videoID, 10) + "\n" +
		strconv.FormatInt(rootCommentID, 10) + "\n" +
		strconv.FormatInt(replyToCommentID, 10) + "\n" +
		strings.TrimSpace(content)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func NormalizeCommentSort(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return CommentSortLatest, nil
	}
	if value != CommentSortLatest && value != CommentSortHot {
		return "", ErrInvalidCommentSort
	}
	return value, nil
}

func NewCommentNotification(eventID string, recipientID int64, actorID int64, messageType string, title string, content string, videoID int64, rootCommentID int64, commentID int64, availableAt time.Time) (*CommentNotification, error) {
	eventID = strings.TrimSpace(eventID)
	messageType = strings.ToUpper(strings.TrimSpace(messageType))
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if eventID == "" || len(eventID) > 64 || recipientID <= 0 || actorID <= 0 ||
		videoID <= 0 || rootCommentID <= 0 || commentID <= 0 || title == "" || content == "" ||
		(messageType != CommentNotificationTypeRoot &&
			messageType != CommentNotificationTypeReply &&
			messageType != CommentNotificationTypeLike) {
		return nil, ErrInvalidCommentNotification
	}
	if availableAt.IsZero() {
		availableAt = time.Now().UTC()
	}
	return &CommentNotification{
		EventID: eventID, RecipientID: recipientID, ActorID: actorID, MessageType: messageType,
		Title: title, Content: content, VideoID: videoID, RootCommentID: rootCommentID,
		CommentID: commentID, State: CommentNotificationStatePending, AvailableAt: availableAt.UTC(),
	}, nil
}

func RestoreCommentNotification(eventID string, recipientID int64, actorID int64, messageType string, title string, content string, videoID int64, rootCommentID int64, commentID int64, state string, attempts int, availableAt time.Time, leaseOwner string, leaseUntil *time.Time, lastError string, deliveredAt *time.Time, createdAt time.Time, updatedAt time.Time) *CommentNotification {
	return &CommentNotification{
		EventID: strings.TrimSpace(eventID), RecipientID: recipientID, ActorID: actorID,
		MessageType: strings.ToUpper(strings.TrimSpace(messageType)), Title: strings.TrimSpace(title),
		Content: strings.TrimSpace(content), VideoID: videoID, RootCommentID: rootCommentID,
		CommentID: commentID, State: strings.TrimSpace(state), Attempts: attempts,
		AvailableAt: availableAt, LeaseOwner: strings.TrimSpace(leaseOwner), LeaseUntil: leaseUntil,
		LastError: strings.TrimSpace(lastError), DeliveredAt: deliveredAt, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

// RestoreAction 从数据库记录恢复互动行为，供仓储层返回领域对象。
func RestoreAction(id int64, userID int64, videoID int64, actionType string, status int, idempotencyKey string, createdAt time.Time, updatedAt time.Time) *Action {
	actionType, _ = NormalizeActionType(actionType)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if status == 0 {
		status = ActionStatusActive
	}

	return &Action{
		ID:             id,
		UserID:         userID,
		VideoID:        videoID,
		ActionType:     actionType,
		Status:         status,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}

// RestoreComment 从数据库查询结果恢复评论对象，并清洗展示字段。
func RestoreComment(id int64, videoID int64, userID int64, userNickname string, userAvatarURL string, content string, status int, idempotencyKey string, createdAt time.Time, updatedAt time.Time) *Comment {
	return RestoreThreadedComment(
		id, videoID, userID, userNickname, userAvatarURL, 0, 0, 0, "", "",
		content, status, 0, 0, 0, "", idempotencyKey, false, false, createdAt, updatedAt,
	)
}

func RestoreThreadedComment(
	id int64,
	videoID int64,
	userID int64,
	userNickname string,
	userAvatarURL string,
	rootCommentID int64,
	replyToCommentID int64,
	replyToUserID int64,
	replyToUserNickname string,
	replyToUserAvatarURL string,
	content string,
	status int,
	replyCount int,
	likeCount int,
	hotScore int64,
	requestFingerprint string,
	idempotencyKey string,
	liked bool,
	canDelete bool,
	createdAt time.Time,
	updatedAt time.Time,
) *Comment {
	content = strings.TrimSpace(content)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if status == 0 {
		status = CommentStatusNormal
	}

	return &Comment{
		ID:                   id,
		VideoID:              videoID,
		UserID:               userID,
		UserNickname:         strings.TrimSpace(userNickname),
		UserAvatarURL:        strings.TrimSpace(userAvatarURL),
		RootCommentID:        rootCommentID,
		ReplyToCommentID:     replyToCommentID,
		ReplyToUserID:        replyToUserID,
		ReplyToUserNickname:  strings.TrimSpace(replyToUserNickname),
		ReplyToUserAvatarURL: strings.TrimSpace(replyToUserAvatarURL),
		Content:              content,
		Status:               status,
		ReplyCount:           clampDomainCount(replyCount),
		LikeCount:            clampDomainCount(likeCount),
		HotScore:             maxInt64(hotScore, 0),
		RequestFingerprint:   strings.TrimSpace(requestFingerprint),
		IdempotencyKey:       idempotencyKey,
		Liked:                liked,
		CanDelete:            canDelete,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
	}
}

// Active 判断点赞或收藏当前是否处于有效状态。
func (a *Action) Active() bool {
	return a.Status == ActionStatusActive
}

// Deleted 判断评论是否已经被软删除。
func (c *Comment) Deleted() bool {
	return c.Status != CommentStatusNormal
}

func (c *Comment) IsRoot() bool {
	return c != nil && c.RootCommentID == 0
}

func (c *Comment) EffectiveRootCommentID() int64 {
	if c == nil {
		return 0
	}
	if c.RootCommentID > 0 {
		return c.RootCommentID
	}
	return c.ID
}

func (c *Comment) EligibleForPublicProjection() bool {
	if c == nil {
		return false
	}
	if c.Status == CommentStatusNormal {
		return true
	}
	return c.IsRoot() && c.Status == CommentStatusSelfDeleted && c.ReplyCount > 0
}

func (c *Comment) ApplyPublicProjection() {
	if c == nil || c.Status != CommentStatusSelfDeleted || !c.IsRoot() {
		return
	}
	c.UserID = 0
	c.UserNickname = ""
	c.UserAvatarURL = ""
	c.Content = ""
	c.Liked = false
	c.CanDelete = false
	c.LikeCount = 0
}

func clampDomainCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
