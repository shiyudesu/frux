package domaininteraction

import (
	"context"
	"time"
)

type ActionIndex interface {
	ListActiveActionVideoIDs(ctx context.Context, userID int64, actionType string, cursor *ActionCursor, limit int) ([]ActionVideo, error)
}

type ViewerActionStateReader interface {
	BatchGetViewerActionStates(ctx context.Context, viewerID int64, videoIDs []int64) (map[int64]*ViewerActionState, error)
}

type AcceptedActionEventRepository interface {
	// PersistAcceptedActionEvent durably applies an interaction that was validated before enqueueing.
	PersistAcceptedActionEvent(ctx context.Context, event *AcceptedActionEvent) error
}

type ThreadedCommentRepository interface {
	CreateThreadedComment(ctx context.Context, comment *Comment) (*CommentMutationResult, error)
	ListCommentRoots(ctx context.Context, query CommentRootQuery) (*CommentPage, error)
	ListCommentReplies(ctx context.Context, query CommentReplyQuery) (*CommentPage, error)
	GetCommentThreadContext(ctx context.Context, targetCommentID int64, viewer CommentViewer, replyLimit int) (*CommentThreadContext, error)
	SetCommentLike(ctx context.Context, commentID int64, userID int64, active bool, idempotencyKey string) (*CommentLikeResult, error)
	DeleteThreadedComment(ctx context.Context, commentID int64, userID int64, role string) (*CommentDeletionResult, error)
	ReconcileCommentCounters(ctx context.Context) error
}

type CommentNotificationOutboxRepository interface {
	ClaimCommentNotifications(ctx context.Context, leaseOwner string, limit int, now time.Time, leaseUntil time.Time) ([]*CommentNotification, error)
	MarkCommentNotificationDelivered(ctx context.Context, eventID string, leaseOwner string, deliveredAt time.Time) error
	MarkCommentNotificationFailed(ctx context.Context, eventID string, leaseOwner string, availableAt time.Time, reason string, terminal bool) error
}

// Repository 定义互动领域需要的持久化能力。
type Repository interface {
	AcceptedActionEventRepository
	// GetVideoStat 读取公开视频当前互动计数。
	GetVideoStat(ctx context.Context, videoID int64) (*VideoStat, error)
	// GetActionState reads the durable baseline used to seed an absent Redis action key.
	GetActionState(ctx context.Context, userID int64, videoID int64, actionType string) (*ActionStateSnapshot, error)
	// GetVideoAuthorID 读取公开视频作者，用于互动通知。
	GetVideoAuthorID(ctx context.Context, videoID int64) (int64, error)
	// GetUserProfile 读取触发互动的用户展示资料。
	GetUserProfile(ctx context.Context, userID int64) (*UserProfile, error)
	// SetAction 设置点赞或收藏状态，并返回最新统计值。
	SetAction(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string) (*Action, int, int, error)
	// SetActionWithAcceptedEvent synchronously validates a public video and,
	// in the same transaction, persists the action receipt plus its profile
	// and recommendation-outcome handoffs.
	SetActionWithAcceptedEvent(ctx context.Context, event *AcceptedActionEvent) (*Action, int, int, error)
	// CreateComment 创建评论并返回视频最新评论数。
	CreateComment(ctx context.Context, comment *Comment) (*Comment, int, int, error)
	// FindCommentByUserAndIdempotencyKey 用于评论创建接口的幂等重放。
	FindCommentByUserAndIdempotencyKey(ctx context.Context, userID int64, idempotencyKey string) (*Comment, int, error)
	// ListComments 仅为已发布公开视频按创建时间倒序读取评论列表。
	ListComments(ctx context.Context, videoID int64, cursor *CommentCursor, limit int) ([]*Comment, error)
	// DeleteComment 软删除评论，并根据操作者身份判断权限。
	DeleteComment(ctx context.Context, commentID int64, userID int64, role string) (*Comment, int, int, error)
}
