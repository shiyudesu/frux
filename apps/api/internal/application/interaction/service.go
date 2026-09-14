package applicationinteraction

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainmessage "github.com/shiyudesu/frux/internal/domain/message"
)

const defaultCommentLimit = 20
const hotScoreLikeWeight = 3
const hotScoreFavoriteWeight = 4
const hotScoreCommentWeight = 5
const actionRecoveryTimeout = 3 * time.Second

var ErrLoadInteractionFailed = errors.New("failed to load interaction")
var ErrSaveInteractionFailed = errors.New("failed to save interaction")
var ErrUpdateInteractionFailed = errors.New("failed to update interaction")

type Service struct {
	repo             domaininteraction.Repository
	hotScoreRecorder HotScoreRecorder
	statCache        StatCache
	actionStateStore ActionStateStore
	actionPublisher  ActionEventPublisher
	actionObserver   ActionDeliveryObserver
	messageWriter    MessageWriter
	commentModerator CommentModeratorReader
}

// HotScoreRecorder 把互动变化投递到热榜分钟桶。
type HotScoreRecorder interface {
	AddHotScore(ctx context.Context, videoID int64, scoreDelta int, at time.Time) error
}

// StatCache 同步 Feed 展示所需的视频互动计数缓存。
type StatCache interface {
	SetVideoStat(ctx context.Context, stat *domaininteraction.VideoStat) error
}

type Option func(*Service)

type CommentModeratorReader interface {
	IsCommentModerator(ctx context.Context, userID int64) (bool, error)
}

type ActionStateResult struct {
	UserID                   int64
	VideoID                  int64
	ActionType               string
	Active                   bool
	LikeCount                int
	FavoriteCount            int
	Delta                    int
	IdempotencyKey           string
	RecommendationRequestID  string
	Version                  int64
	EventID                  string
	OccurredAt               time.Time
	ShouldPublish            bool
	CanRollback              bool
	Previous                 domaininteraction.ActionStateSnapshot
	PreviousHandoffConfirmed bool
	PreviousHasDependency    bool
}

type ActionMutation struct {
	EventID                 string
	RecommendationRequestID string
	OccurredAt              time.Time
}

// ActionStateStore 保存点赞收藏的快速状态和计数。
type ActionStateStore interface {
	SetActionState(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, initialStat *domaininteraction.VideoStat, initialState *domaininteraction.ActionStateSnapshot, mutation ActionMutation) (*ActionStateResult, error)
	RollbackActionState(ctx context.Context, state *ActionStateResult) (bool, error)
}

// ActionStateHandoffConfirmer records that an asynchronous action event has
// reached a durable handoff. Older ActionStateStore implementations may omit it.
type ActionStateHandoffConfirmer interface {
	ConfirmActionStateHandoff(ctx context.Context, state *ActionStateResult) error
}

// ActionEventPublisher 投递点赞收藏变更事件。
type ActionEventPublisher interface {
	PublishActionChanged(ctx context.Context, event *ActionChangedEvent) error
}

type ActionDeliveryObserver interface {
	ObserveActionFallback(result string)
	ObserveActionRollback(result string)
}

// MessageWriter 写入互动触发的站内消息。
type MessageWriter interface {
	CreateFromEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string) (any, error)
}

// ActorMessageWriter 可在消息里携带触发互动的用户资料。
type ActorMessageWriter interface {
	CreateFromActorEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string, actorID int64, actorNickname string, actorAvatarURL string) (any, error)
}

type TargetedActorMessageWriter interface {
	CreateFromTargetedActorEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string, actorID int64, actorNickname string, actorAvatarURL string, videoID int64, commentID int64, rootCommentID int64) (any, error)
}

type ActionChangedEvent struct {
	EventID                 string    `json:"event_id"`
	UserID                  int64     `json:"user_id"`
	VideoID                 int64     `json:"video_id"`
	ActionType              string    `json:"action_type"`
	Active                  bool      `json:"active"`
	IdempotencyKey          string    `json:"idempotency_key"`
	RecommendationRequestID string    `json:"recommendation_request_id,omitempty"`
	Version                 int64     `json:"version"`
	OccurredAt              time.Time `json:"occurred_at"`
}

type ActionResult struct {
	VideoID       int64
	ActionType    string
	Active        bool
	LikeCount     int
	FavoriteCount int
}

type CreateCommentResult struct {
	Comment      *domaininteraction.Comment
	CommentCount int
}

type DeleteCommentResult struct {
	CommentID      int64
	Status         int
	CommentCount   int
	RootReplyCount int
	DeletedCount   int
	ThreadHidden   bool
	Tombstone      bool
}

type CommentListResult struct {
	Items        []*domaininteraction.Comment
	NextCursor   string
	HasMore      bool
	CommentCount int
	Sort         string
}

type commentCursorPayload struct {
	Version   int    `json:"v,omitempty"`
	Sort      string `json:"sort,omitempty"`
	HotScore  int64  `json:"hot_score,omitempty"`
	CreatedAt string `json:"created_at"`
	CommentID int64  `json:"comment_id"`
}

func New(repo domaininteraction.Repository, options ...Option) *Service {
	service := &Service{repo: repo}
	for _, option := range options {
		option(service)
	}
	return service
}

// WithHotScoreRecorder 为互动服务启用热榜增量写入。
func WithHotScoreRecorder(recorder HotScoreRecorder) Option {
	return func(s *Service) {
		s.hotScoreRecorder = recorder
	}
}

// WithStatCache 为评论写入后的 Feed 计数展示启用缓存同步。
func WithStatCache(cache StatCache) Option {
	return func(s *Service) {
		s.statCache = cache
	}
}

// WithAsyncActionPipeline retains Redis action receipts and Kafka delivery.
// Accepted actions are persisted before success; Redis counts are not authoritative.
func WithAsyncActionPipeline(store ActionStateStore, publisher ActionEventPublisher) Option {
	return func(s *Service) {
		s.actionStateStore = store
		s.actionPublisher = publisher
	}
}

func WithActionDeliveryObserver(observer ActionDeliveryObserver) Option {
	return func(s *Service) {
		s.actionObserver = observer
	}
}

// WithMessageWriter 为点赞和评论成功后的通知写入启用消息中心。
func WithMessageWriter(writer MessageWriter) Option {
	return func(s *Service) {
		s.messageWriter = writer
	}
}

func WithCommentModeratorReader(reader CommentModeratorReader) Option {
	return func(s *Service) {
		s.commentModerator = reader
	}
}

// Like 设置用户对视频的点赞状态为有效。
func (s *Service) Like(ctx context.Context, userID int64, videoID int64, idempotencyKey string) (*ActionResult, error) {
	return s.LikeWithRecommendation(ctx, userID, videoID, idempotencyKey, "")
}

func (s *Service) LikeWithRecommendation(ctx context.Context, userID int64, videoID int64, idempotencyKey string, recommendationRequestID string) (*ActionResult, error) {
	return s.setAction(ctx, userID, videoID, domaininteraction.ActionTypeLike, true, idempotencyKey, recommendationRequestID)
}

// Unlike 设置用户对视频的点赞状态为取消。
func (s *Service) Unlike(ctx context.Context, userID int64, videoID int64, idempotencyKey string) (*ActionResult, error) {
	return s.UnlikeWithRecommendation(ctx, userID, videoID, idempotencyKey, "")
}

func (s *Service) UnlikeWithRecommendation(ctx context.Context, userID int64, videoID int64, idempotencyKey string, recommendationRequestID string) (*ActionResult, error) {
	return s.setAction(ctx, userID, videoID, domaininteraction.ActionTypeLike, false, idempotencyKey, recommendationRequestID)
}

// Favorite 设置用户对视频的收藏状态为有效。
func (s *Service) Favorite(ctx context.Context, userID int64, videoID int64, idempotencyKey string) (*ActionResult, error) {
	return s.FavoriteWithRecommendation(ctx, userID, videoID, idempotencyKey, "")
}

func (s *Service) FavoriteWithRecommendation(ctx context.Context, userID int64, videoID int64, idempotencyKey string, recommendationRequestID string) (*ActionResult, error) {
	return s.setAction(ctx, userID, videoID, domaininteraction.ActionTypeFavorite, true, idempotencyKey, recommendationRequestID)
}

// Unfavorite 设置用户对视频的收藏状态为取消。
func (s *Service) Unfavorite(ctx context.Context, userID int64, videoID int64, idempotencyKey string) (*ActionResult, error) {
	return s.UnfavoriteWithRecommendation(ctx, userID, videoID, idempotencyKey, "")
}

func (s *Service) UnfavoriteWithRecommendation(ctx context.Context, userID int64, videoID int64, idempotencyKey string, recommendationRequestID string) (*ActionResult, error) {
	return s.setAction(ctx, userID, videoID, domaininteraction.ActionTypeFavorite, false, idempotencyKey, recommendationRequestID)
}

// CreateComment 创建评论，并通过幂等键防止客户端重试生成重复评论。
func (s *Service) CreateComment(ctx context.Context, userID int64, videoID int64, content string, idempotencyKey string) (*CreateCommentResult, error) {
	comment, err := domaininteraction.NewRootComment(videoID, userID, content, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if threaded, ok := s.repo.(domaininteraction.ThreadedCommentRepository); ok {
		mutation, err := threaded.CreateThreadedComment(ctx, comment)
		if err != nil {
			if errors.Is(err, domaininteraction.ErrVideoNotFound) ||
				errors.Is(err, domaininteraction.ErrCommentIdempotencyConflict) {
				return nil, err
			}
			return nil, ErrSaveInteractionFailed
		}
		s.recordHotScore(ctx, mutation.Comment.VideoID, mutation.VideoDelta*hotScoreCommentWeight)
		s.syncCommentCount(ctx, mutation.Comment.VideoID, mutation.CommentCount)
		return &CreateCommentResult{Comment: mutation.Comment, CommentCount: mutation.CommentCount}, nil
	}

	created, count, delta, err := s.repo.CreateComment(ctx, comment)
	if err != nil {
		if errors.Is(err, domaininteraction.ErrVideoNotFound) {
			return nil, domaininteraction.ErrVideoNotFound
		}
		return nil, ErrSaveInteractionFailed
	}
	s.recordHotScore(ctx, created.VideoID, delta*hotScoreCommentWeight)
	s.syncCommentCount(ctx, created.VideoID, count)
	if delta > 0 {
		s.notifyComment(ctx, created)
	}

	return &CreateCommentResult{Comment: created, CommentCount: count}, nil
}

// ListComments 使用游标分页查询评论，返回下一页游标和 has_more。
func (s *Service) ListComments(ctx context.Context, videoID int64, cursor string, limit int) (*CommentListResult, error) {
	return s.ListCommentRoots(ctx, videoID, 0, "", domaininteraction.CommentSortLatest, cursor, limit)
}

func (s *Service) ListCommentRoots(ctx context.Context, videoID int64, viewerID int64, viewerRole string, sortMode string, cursor string, limit int) (*CommentListResult, error) {
	if videoID <= 0 {
		return nil, domaininteraction.ErrInvalidVideoID
	}
	sortMode, err := domaininteraction.NormalizeCommentSort(sortMode)
	if err != nil {
		return nil, err
	}

	parsedCursor, err := parseRootCommentCursor(cursor, sortMode)
	if err != nil {
		return nil, err
	}
	limit = normalizeCommentLimit(limit)

	threaded, ok := s.repo.(domaininteraction.ThreadedCommentRepository)
	if !ok {
		if sortMode != domaininteraction.CommentSortLatest || viewerID > 0 {
			return nil, ErrLoadInteractionFailed
		}
		items, err := s.repo.ListComments(ctx, videoID, parsedCursor, limit+1)
		if err != nil {
			if errors.Is(err, domaininteraction.ErrVideoNotFound) {
				return nil, domaininteraction.ErrVideoNotFound
			}
			return nil, ErrLoadInteractionFailed
		}
		return buildRootCommentListResult(items, 0, sortMode, limit), nil
	}
	viewer, err := s.resolveCommentViewer(ctx, viewerID, viewerRole)
	if err != nil {
		return nil, ErrLoadInteractionFailed
	}
	page, err := threaded.ListCommentRoots(ctx, domaininteraction.CommentRootQuery{
		VideoID:      videoID,
		Viewer:       viewer,
		Sort:         sortMode,
		Cursor:       parsedCursor,
		Limit:        limit + 1,
		PreviewLimit: domaininteraction.ReplyPreviewLimit,
	})
	if err != nil {
		if errors.Is(err, domaininteraction.ErrVideoNotFound) {
			return nil, domaininteraction.ErrVideoNotFound
		}
		if errors.Is(err, domaininteraction.ErrInvalidCursor) || errors.Is(err, domaininteraction.ErrInvalidCommentSort) {
			return nil, err
		}
		return nil, ErrLoadInteractionFailed
	}
	return buildRootCommentListResult(page.Items, page.CommentCount, sortMode, limit), nil
}

// DeleteComment 删除评论并返回删除后的评论状态和视频评论数。
func (s *Service) DeleteComment(ctx context.Context, commentID int64, userID int64, role string) (*DeleteCommentResult, error) {
	if commentID <= 0 {
		return nil, domaininteraction.ErrInvalidCommentID
	}
	if userID <= 0 {
		return nil, domaininteraction.ErrInvalidUserID
	}
	viewer, err := s.resolveCommentViewer(ctx, userID, role)
	if err != nil {
		return nil, ErrLoadInteractionFailed
	}
	role = viewer.Role

	if threaded, ok := s.repo.(domaininteraction.ThreadedCommentRepository); ok {
		deletion, err := threaded.DeleteThreadedComment(ctx, commentID, userID, role)
		if err != nil {
			if errors.Is(err, domaininteraction.ErrCommentNotFound) ||
				errors.Is(err, domaininteraction.ErrCommentPermissionDenied) {
				return nil, err
			}

			return nil, ErrUpdateInteractionFailed
		}
		s.recordHotScore(ctx, deletion.Comment.VideoID, deletion.VideoDelta*hotScoreCommentWeight)
		s.syncCommentCount(ctx, deletion.Comment.VideoID, deletion.CommentCount)
		return &DeleteCommentResult{
			CommentID:      deletion.Comment.ID,
			Status:         deletion.Comment.Status,
			CommentCount:   deletion.CommentCount,
			RootReplyCount: deletion.RootReplyCount,
			DeletedCount:   deletion.DeletedCount,
			ThreadHidden:   deletion.ThreadHidden,
			Tombstone:      deletion.Tombstone,
		}, nil
	}

	comment, count, delta, err := s.repo.DeleteComment(ctx, commentID, userID, role)
	if err != nil {
		if errors.Is(err, domaininteraction.ErrCommentNotFound) ||
			errors.Is(err, domaininteraction.ErrCommentPermissionDenied) {
			return nil, err
		}
		return nil, ErrUpdateInteractionFailed
	}
	s.recordHotScore(ctx, comment.VideoID, delta*hotScoreCommentWeight)
	s.syncCommentCount(ctx, comment.VideoID, count)

	return &DeleteCommentResult{
		CommentID:    comment.ID,
		Status:       comment.Status,
		CommentCount: count,
	}, nil
}

func (s *Service) resolveCommentViewer(
	ctx context.Context,
	userID int64,
	role string,
) (domaininteraction.CommentViewer, error) {
	viewer := domaininteraction.CommentViewer{UserID: userID, Role: role}
	if s.commentModerator == nil || userID <= 0 {
		return viewer, nil
	}
	moderator, err := s.commentModerator.IsCommentModerator(ctx, userID)
	if err != nil {
		return domaininteraction.CommentViewer{}, err
	}
	viewer.Role = ""
	if moderator {
		viewer.Role = "admin"
	}
	return viewer, nil
}

// setAction 统一处理点赞和收藏状态变更，actionType 区分点赞或收藏，active 表示目标状态。
func (s *Service) setAction(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, recommendationRequestID string) (*ActionResult, error) {
	if userID <= 0 {
		return nil, domaininteraction.ErrInvalidUserID
	}
	if videoID <= 0 {
		return nil, domaininteraction.ErrInvalidVideoID
	}
	if len(strings.TrimSpace(idempotencyKey)) > domaininteraction.MaxIdempotencyKeyLength {
		return nil, domaininteraction.ErrIdempotencyKeyTooLong
	}
	if len(strings.TrimSpace(recommendationRequestID)) > domaininteraction.MaxRecommendationRequestIDLength {
		return nil, domaininteraction.ErrRecommendationRequestIDTooLong
	}

	actionType, err := domaininteraction.NormalizeActionType(actionType)
	if err != nil {
		return nil, err
	}
	if durable, ok := s.repo.(domaininteraction.DurableActionRequestRepository); ok && strings.TrimSpace(idempotencyKey) != "" {
		receipt, err := durable.FindActionRequest(ctx, userID, videoID, actionType, active, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			return actionResultFromReceipt(receipt), nil
		}
	}
	if s.actionStateStore != nil && s.actionPublisher != nil {
		return s.setActionAsync(ctx, userID, videoID, actionType, active, idempotencyKey, recommendationRequestID)
	}

	return s.setActionSync(ctx, userID, videoID, actionType, active, idempotencyKey, recommendationRequestID)
}

func (s *Service) setActionAsync(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, recommendationRequestID string) (*ActionResult, error) {
	initialStat, err := s.repo.GetVideoStat(ctx, videoID)
	if err != nil {
		if errors.Is(err, domaininteraction.ErrVideoNotFound) {
			return nil, domaininteraction.ErrVideoNotFound
		}
		return nil, ErrUpdateInteractionFailed
	}
	initialState, err := s.repo.GetActionState(ctx, userID, videoID, actionType)
	if err != nil {
		return nil, ErrUpdateInteractionFailed
	}

	mutation := newActionMutation(recommendationRequestID)
	state, err := s.actionStateStore.SetActionState(ctx, userID, videoID, actionType, active, idempotencyKey, initialStat, initialState, mutation)
	if err != nil {
		if errors.Is(err, domaininteraction.ErrActionIdempotencyConflict) {
			return nil, err
		}
		if state != nil {
			// Preserve the existing conditional recovery of a mutation that the
			// store knows it committed; do not invent a different accepted event.
			return nil, s.handleActionStateStoreFailure(ctx, state, err)
		}
		// Redis is recoverable state, not the authority for accepted actions.
		// A Redis outage must not prevent the durable repository from accepting
		// a request. Its receipt and version checks remain the final authority.
		return s.setActionSync(ctx, userID, videoID, actionType, active, idempotencyKey, recommendationRequestID)
	}

	if state.ShouldPublish || state.EventID != "" {
		event := actionChangedEventFromState(userID, state)
		accepted, acceptedErr := acceptedActionEvent(event)
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), actionRecoveryTimeout)
		var receipt *domaininteraction.ActionRequestReceipt
		if acceptedErr == nil {
			if durable, ok := s.repo.(domaininteraction.DurableActionRequestRepository); ok {
				receipt, acceptedErr = durable.PersistActionRequest(recoveryCtx, accepted, idempotencyKey)
			} else {
				acceptedErr = s.repo.PersistAcceptedActionEvent(recoveryCtx, accepted)
			}
		}
		if acceptedErr != nil {
			_, rollbackErr := s.rollbackActionState(recoveryCtx, state)
			cancel()
			s.observeActionFallback("failure")
			return nil, actionUpdateError(acceptedErr, rollbackErr)
		}
		if receipt != nil && receipt.Replayed {
			// Another request may have committed this key after the initial
			// lookup. Discard only our still-owned provisional Redis mutation.
			_, _ = s.rollbackActionState(recoveryCtx, state)
			cancel()
			return actionResultFromReceipt(receipt), nil
		}
		cancel()

		// Success is now backed by PostgreSQL before any Kafka publication.
		// The accepted receipt already owns durable profile/outcome handoffs,
		// so a transport failure cannot lose this mutation or justify rollback.
		if state.ShouldPublish {
			if publishErr := s.actionPublisher.PublishActionChanged(ctx, event); publishErr != nil {
				s.observeActionFallback("success")
			}
		}
		_ = s.confirmActionStateHandoff(ctx, state)
	} else if _, durable := s.repo.(domaininteraction.DurableActionRequestRepository); durable {
		// A no-op without an event still needs a durable HTTP request receipt.
		return s.setActionSync(ctx, userID, videoID, actionType, active, idempotencyKey, recommendationRequestID)
	}
	if snapshot := s.syncVideoStat(ctx, videoID); snapshot != nil {
		state.LikeCount = snapshot.LikeCount
		state.FavoriteCount = snapshot.FavoriteCount
	}
	s.applyActionSideEffects(ctx, state, userID)
	return &ActionResult{
		VideoID: state.VideoID, ActionType: state.ActionType, Active: state.Active,
		LikeCount: state.LikeCount, FavoriteCount: state.FavoriteCount,
	}, nil
}

func actionResultFromReceipt(receipt *domaininteraction.ActionRequestReceipt) *ActionResult {
	result := &ActionResult{VideoID: receipt.VideoID, ActionType: receipt.ActionType, Active: receipt.Active}
	if receipt.ActionType == domaininteraction.ActionTypeLike {
		result.LikeCount = receipt.Count
	} else {
		result.FavoriteCount = receipt.Count
	}
	return result
}

func (s *Service) applyActionSideEffects(
	ctx context.Context,
	state *ActionStateResult,
	userID int64,
) {
	if state == nil || state.Delta == 0 {
		return
	}
	s.recordActionHotScore(ctx, state.VideoID, state.ActionType, state.Delta)
	if state.ActionType == domaininteraction.ActionTypeLike &&
		state.Active && state.Delta > 0 {
		s.notifyLike(ctx, &domaininteraction.Action{
			UserID:     userID,
			VideoID:    state.VideoID,
			ActionType: state.ActionType,
			Status:     domaininteraction.ActionStatusActive,
		})
	}
}

func (s *Service) handleActionStateStoreFailure(ctx context.Context, state *ActionStateResult, stateErr error) error {
	if state == nil {
		return actionUpdateError(stateErr)
	}
	if !state.CanRollback {
		if state.ShouldPublish {
			return actionUpdateError(stateErr, s.ensureActionEventDurable(ctx, actionChangedEventFromState(state.UserID, state)))
		}
		return actionUpdateError(stateErr)
	}

	recoveryCtx, cancelRecovery := context.WithTimeout(context.WithoutCancel(ctx), actionRecoveryTimeout)
	rolledBack, rollbackErr := s.rollbackActionState(recoveryCtx, state)
	cancelRecovery()
	if rollbackErr != nil {
		recoveryErr := s.ensureActionEventDurable(ctx, actionChangedEventFromState(state.UserID, state))
		return actionUpdateError(stateErr, rollbackErr, recoveryErr)
	}
	if !rolledBack {
		// A newer mutation owns the action now and must not be undone or replaced by this event.
		return actionUpdateError(stateErr)
	}
	return actionUpdateError(stateErr)
}

func (s *Service) ensureActionEventDurable(ctx context.Context, event *ActionChangedEvent) error {
	accepted, err := acceptedActionEvent(event)
	if err != nil {
		return err
	}

	publishCtx, cancelPublish := context.WithTimeout(context.WithoutCancel(ctx), actionRecoveryTimeout)
	publishErr := s.actionPublisher.PublishActionChanged(publishCtx, event)
	cancelPublish()
	if publishErr == nil {
		return nil
	}

	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), actionRecoveryTimeout)
	persistErr := s.repo.PersistAcceptedActionEvent(persistCtx, accepted)
	cancelPersist()
	if persistErr == nil {
		return nil
	}
	return errors.Join(publishErr, persistErr)
}

func actionUpdateError(causes ...error) error {
	joined := make([]error, 0, len(causes)+1)
	joined = append(joined, ErrUpdateInteractionFailed)
	for _, cause := range causes {
		if cause != nil {
			joined = append(joined, cause)
		}
	}
	return errors.Join(joined...)
}

func (s *Service) rollbackActionState(ctx context.Context, state *ActionStateResult) (bool, error) {
	if s.actionStateStore == nil || state == nil || !state.CanRollback {
		s.observeActionRollback("not_applicable")
		return false, nil
	}
	rolledBack, err := s.actionStateStore.RollbackActionState(ctx, state)
	switch {
	case err != nil:
		s.observeActionRollback("failure")
	case rolledBack:
		s.observeActionRollback("success")
	default:
		s.observeActionRollback("superseded")
	}
	return rolledBack, err
}

func (s *Service) observeActionFallback(result string) {
	if s.actionObserver != nil {
		s.actionObserver.ObserveActionFallback(result)
	}
}

func (s *Service) observeActionRollback(result string) {
	if s.actionObserver != nil {
		s.actionObserver.ObserveActionRollback(result)
	}
}

func (s *Service) confirmActionStateHandoff(ctx context.Context, state *ActionStateResult) error {
	confirmer, ok := s.actionStateStore.(ActionStateHandoffConfirmer)
	if !ok || state == nil {
		return nil
	}
	return confirmer.ConfirmActionStateHandoff(ctx, state)
}

func (s *Service) setActionSync(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, recommendationRequestID string) (*ActionResult, error) {
	mutation := newActionMutation(recommendationRequestID)
	accepted, err := domaininteraction.NewAcceptedActionEventWithRecommendation(
		mutation.EventID,
		userID,
		videoID,
		actionType,
		active,
		idempotencyKey,
		mutation.RecommendationRequestID,
		0,
		mutation.OccurredAt,
	)
	if err != nil {
		return nil, err
	}
	action, count, delta, err := s.repo.SetActionWithAcceptedEvent(ctx, accepted)
	if err != nil {
		if errors.Is(err, domaininteraction.ErrVideoNotFound) {
			return nil, domaininteraction.ErrVideoNotFound
		}
		if errors.Is(err, domaininteraction.ErrActionIdempotencyConflict) {
			return nil, err
		}
		return nil, ErrUpdateInteractionFailed
	}

	result := &ActionResult{
		VideoID:    action.VideoID,
		ActionType: action.ActionType,
		Active:     action.Active(),
	}
	if action.ActionType == domaininteraction.ActionTypeLike {
		result.LikeCount = count
	} else {
		result.FavoriteCount = count
	}
	s.recordActionHotScore(ctx, action.VideoID, action.ActionType, delta)
	if action.ActionType == domaininteraction.ActionTypeLike && action.Active() && delta > 0 {
		s.notifyLike(ctx, action)
	}
	s.syncVideoStat(ctx, videoID)
	return result, nil
}

func (s *Service) recordActionHotScore(ctx context.Context, videoID int64, actionType string, delta int) {
	recordActionHotScore(ctx, s.hotScoreRecorder, videoID, actionType, delta)
}

func (s *Service) recordHotScore(ctx context.Context, videoID int64, scoreDelta int) {
	recordHotScore(ctx, s.hotScoreRecorder, videoID, scoreDelta)
}

func (s *Service) syncCommentCount(ctx context.Context, videoID int64, commentCount int) {
	// commentCount belongs to the earlier mutation result, not necessarily to
	// the newer snapshot read below. Never mix that value with a newer revision.
	s.syncVideoStat(ctx, videoID)
}

func (s *Service) syncVideoStat(ctx context.Context, videoID int64) *domaininteraction.VideoStat {
	stat, err := s.repo.GetVideoStat(ctx, videoID)
	if err != nil || stat == nil {
		// Do not manufacture zero counts on a read failure.
		return nil
	}
	if s.statCache != nil {
		_ = s.statCache.SetVideoStat(ctx, stat)
	}
	return stat
}

func recordActionHotScore(ctx context.Context, recorder HotScoreRecorder, videoID int64, actionType string, delta int) {
	if actionType == domaininteraction.ActionTypeLike {
		recordHotScore(ctx, recorder, videoID, delta*hotScoreLikeWeight)
		return
	}
	recordHotScore(ctx, recorder, videoID, delta*hotScoreFavoriteWeight)
}

func recordHotScore(ctx context.Context, recorder HotScoreRecorder, videoID int64, scoreDelta int) {
	if recorder == nil || scoreDelta == 0 {
		return
	}
	_ = recorder.AddHotScore(ctx, videoID, scoreDelta, time.Now())
}

func newActionMutation(recommendationRequestID string) ActionMutation {
	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	return ActionMutation{
		EventID:                 newEventID(occurredAt),
		RecommendationRequestID: strings.TrimSpace(recommendationRequestID),
		OccurredAt:              occurredAt,
	}
}

func actionChangedEventFromState(userID int64, state *ActionStateResult) *ActionChangedEvent {
	return &ActionChangedEvent{
		EventID:                 state.EventID,
		UserID:                  userID,
		VideoID:                 state.VideoID,
		ActionType:              state.ActionType,
		Active:                  state.Active,
		IdempotencyKey:          state.IdempotencyKey,
		RecommendationRequestID: state.RecommendationRequestID,
		Version:                 state.Version,
		OccurredAt:              state.OccurredAt,
	}
}

func (s *Service) notifyLike(ctx context.Context, action *domaininteraction.Action) {
	if s.messageWriter == nil || action == nil {
		return
	}
	authorID, err := s.repo.GetVideoAuthorID(ctx, action.VideoID)
	if err != nil || authorID == action.UserID {
		return
	}
	eventID := fmt.Sprintf("interaction:like:%d:%d", action.VideoID, action.UserID)
	s.createInteractionMessage(ctx, authorID, domainmessage.TypeLike, "收到点赞", "点赞了你的视频", eventID, action.UserID)
}

func (s *Service) notifyComment(ctx context.Context, comment *domaininteraction.Comment) {
	if s.messageWriter == nil || comment == nil {
		return
	}
	authorID, err := s.repo.GetVideoAuthorID(ctx, comment.VideoID)
	if err != nil || authorID == comment.UserID {
		return
	}
	eventID := fmt.Sprintf("interaction:comment:%d", comment.ID)
	if writer, ok := s.messageWriter.(TargetedActorMessageWriter); ok {
		actor, _ := s.repo.GetUserProfile(ctx, comment.UserID)
		actorNickname := ""
		actorAvatarURL := ""
		if actor != nil {
			actorNickname = actor.Nickname
			actorAvatarURL = actor.AvatarURL
		}
		_, _ = writer.CreateFromTargetedActorEvent(
			ctx, authorID, domainmessage.TypeComment, "收到评论", comment.Content,
			eventID, "", comment.UserID, actorNickname, actorAvatarURL,
			comment.VideoID, comment.ID, comment.ID,
		)
		return
	}
	s.createInteractionMessage(ctx, authorID, domainmessage.TypeComment, "收到评论", comment.Content, eventID, comment.UserID)
}

func (s *Service) createInteractionMessage(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, actorID int64) {
	actor, _ := s.repo.GetUserProfile(ctx, actorID)
	if writer, ok := s.messageWriter.(ActorMessageWriter); ok {
		actorNickname := ""
		actorAvatarURL := ""
		if actor != nil {
			actorNickname = actor.Nickname
			actorAvatarURL = actor.AvatarURL
		}
		_, _ = writer.CreateFromActorEvent(ctx, userID, messageType, title, content, eventID, eventID, actorID, actorNickname, actorAvatarURL)
		return
	}
	actorName := fmt.Sprintf("用户 %d", actorID)
	if actor != nil && actor.Nickname != "" {
		actorName = actor.Nickname
	}
	_, _ = s.messageWriter.CreateFromEvent(ctx, userID, messageType, title, fmt.Sprintf("%s %s", actorName, content), eventID, eventID)
}

func newEventID(occurredAt time.Time) string {
	content := make([]byte, 12)
	if _, err := rand.Read(content); err == nil {
		return fmt.Sprintf("%020d-%s", occurredAt.UnixNano(), hex.EncodeToString(content))
	}
	return fmt.Sprintf("%020d", occurredAt.UnixNano())
}

// normalizeCommentLimit 统一评论分页默认值和最大值。
func normalizeCommentLimit(limit int) int {
	if limit <= 0 {
		return defaultCommentLimit
	}
	if limit > domaininteraction.MaxLimit {
		return domaininteraction.MaxLimit
	}
	return limit
}

// parseCommentCursor 解析上一页返回的游标，游标内保存最后一条评论的排序字段。
func parseCommentCursor(raw string) (*domaininteraction.CommentCursor, error) {
	return parseRootCommentCursor(raw, domaininteraction.CommentSortLatest)
}

func parseRootCommentCursor(raw string, sortMode string) (*domaininteraction.CommentCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	content, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		content, err = base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, domaininteraction.ErrInvalidCursor
		}
	}

	var payload commentCursorPayload
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, domaininteraction.ErrInvalidCursor
	}

	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.CreatedAt))
	if err != nil || payload.CommentID <= 0 {
		return nil, domaininteraction.ErrInvalidCursor
	}
	if payload.Version == 0 && payload.Sort == "" {
		payload.Version = domaininteraction.CommentCursorVersion
		payload.Sort = domaininteraction.CommentSortLatest
	}
	if payload.Version != domaininteraction.CommentCursorVersion || payload.Sort != sortMode {
		return nil, domaininteraction.ErrInvalidCursor
	}

	return &domaininteraction.CommentCursor{
		Version:   payload.Version,
		Sort:      payload.Sort,
		HotScore:  payload.HotScore,
		CreatedAt: createdAt,
		CommentID: payload.CommentID,
	}, nil
}

// encodeCommentCursor 把当前页最后一条评论的排序字段编码成下一页游标。
func encodeCommentCursor(cursor *domaininteraction.CommentCursor) string {
	if cursor == nil || cursor.CommentID <= 0 || cursor.CreatedAt.IsZero() {
		return ""
	}

	content, err := json.Marshal(commentCursorPayload{
		Version:   cursor.Version,
		Sort:      cursor.Sort,
		HotScore:  cursor.HotScore,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		CommentID: cursor.CommentID,
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(content)
}
