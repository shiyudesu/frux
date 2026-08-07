package applicationinteraction

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	"strings"
	"time"
)

type CreateReplyResult = CreateCommentResult

type ReplyListResult struct {
	RootCommentID int64
	Items         []*domaininteraction.Comment
	NextCursor    string
	HasMore       bool
	CommentCount  int
}

type ThreadContextResult struct {
	Root         *domaininteraction.Comment
	Replies      []*domaininteraction.Comment
	Target       *domaininteraction.Comment
	NextCursor   string
	HasMore      bool
	CommentCount int
}

type CommentLikeResult struct {
	CommentID          int64
	RootCommentID      int64
	Liked              bool
	LikeCount          int
	LikedByVideoAuthor bool
}

type replyCursorPayload struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"created_at"`
	CommentID int64  `json:"comment_id"`
}

func (s *Service) CreateReply(ctx context.Context, userID int64, videoID int64, targetCommentID int64, content string, idempotencyKey string) (*CreateReplyResult, error) {
	comment, err := domaininteraction.NewReplyComment(videoID, userID, targetCommentID, content, idempotencyKey)
	if err != nil {
		return nil, err
	}
	threaded, ok := s.repo.(domaininteraction.ThreadedCommentRepository)
	if !ok {
		return nil, ErrSaveInteractionFailed
	}
	mutation, err := threaded.CreateThreadedComment(ctx, comment)
	if err != nil {
		if errors.Is(err, domaininteraction.ErrVideoNotFound) ||
			errors.Is(err, domaininteraction.ErrReplyTargetUnavailable) ||
			errors.Is(err, domaininteraction.ErrCommentIdempotencyConflict) {
			return nil, err
		}
		return nil, ErrSaveInteractionFailed
	}
	s.recordHotScore(ctx, mutation.Comment.VideoID, mutation.VideoDelta*hotScoreCommentWeight)
	s.syncCommentCount(ctx, mutation.Comment.VideoID, mutation.CommentCount)
	return &CreateReplyResult{Comment: mutation.Comment, CommentCount: mutation.CommentCount}, nil
}

func (s *Service) ListCommentReplies(ctx context.Context, rootCommentID int64, viewerID int64, viewerRole string, cursor string, limit int) (*ReplyListResult, error) {
	if rootCommentID <= 0 {
		return nil, domaininteraction.ErrInvalidRootCommentID
	}
	parsedCursor, err := parseReplyCursor(cursor)
	if err != nil {
		return nil, err
	}
	limit = normalizeCommentLimit(limit)
	threaded, ok := s.repo.(domaininteraction.ThreadedCommentRepository)
	if !ok {
		return nil, ErrLoadInteractionFailed
	}
	page, err := threaded.ListCommentReplies(ctx, domaininteraction.CommentReplyQuery{
		RootCommentID: rootCommentID,
		Viewer:        domaininteraction.CommentViewer{UserID: viewerID, Role: viewerRole},
		Cursor:        parsedCursor,
		Limit:         limit + 1,
	})
	if err != nil {
		if isCommentReadDomainError(err) {
			return nil, err
		}
		return nil, ErrLoadInteractionFailed
	}
	hasMore := len(page.Items) > limit
	if hasMore {
		page.Items = page.Items[:limit]
	}
	return &ReplyListResult{
		RootCommentID: rootCommentID,
		Items:         page.Items,
		NextCursor:    nextReplyCursor(page.Items),
		HasMore:       hasMore,
		CommentCount:  page.CommentCount,
	}, nil
}

func (s *Service) GetCommentThreadContext(ctx context.Context, targetCommentID int64, viewerID int64, viewerRole string, replyLimit int) (*ThreadContextResult, error) {
	if targetCommentID <= 0 {
		return nil, domaininteraction.ErrInvalidCommentID
	}
	replyLimit = normalizeCommentLimit(replyLimit)
	threaded, ok := s.repo.(domaininteraction.ThreadedCommentRepository)
	if !ok {
		return nil, ErrLoadInteractionFailed
	}
	contextResult, err := threaded.GetCommentThreadContext(
		ctx,
		targetCommentID,
		domaininteraction.CommentViewer{UserID: viewerID, Role: viewerRole},
		replyLimit+1,
	)
	if err != nil {
		if isCommentReadDomainError(err) {
			return nil, err
		}
		return nil, ErrLoadInteractionFailed
	}
	hasMore := len(contextResult.Replies) > replyLimit
	if hasMore {
		contextResult.Replies = contextResult.Replies[:replyLimit]
	}
	return &ThreadContextResult{
		Root:         contextResult.Root,
		Replies:      contextResult.Replies,
		Target:       contextResult.Target,
		NextCursor:   nextReplyCursor(contextResult.Replies),
		HasMore:      hasMore,
		CommentCount: contextResult.CommentCount,
	}, nil
}

func (s *Service) LikeComment(ctx context.Context, commentID int64, userID int64, idempotencyKey string) (*CommentLikeResult, error) {
	return s.setCommentLike(ctx, commentID, userID, true, idempotencyKey)
}

func (s *Service) UnlikeComment(ctx context.Context, commentID int64, userID int64, idempotencyKey string) (*CommentLikeResult, error) {
	return s.setCommentLike(ctx, commentID, userID, false, idempotencyKey)
}

func (s *Service) setCommentLike(ctx context.Context, commentID int64, userID int64, active bool, idempotencyKey string) (*CommentLikeResult, error) {
	if commentID <= 0 {
		return nil, domaininteraction.ErrInvalidCommentID
	}
	if userID <= 0 {
		return nil, domaininteraction.ErrInvalidUserID
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) > domaininteraction.MaxIdempotencyKeyLength {
		return nil, domaininteraction.ErrIdempotencyKeyTooLong
	}
	threaded, ok := s.repo.(domaininteraction.ThreadedCommentRepository)
	if !ok {
		return nil, ErrUpdateInteractionFailed
	}
	result, err := threaded.SetCommentLike(ctx, commentID, userID, active, idempotencyKey)
	if err != nil {
		if errors.Is(err, domaininteraction.ErrVideoNotFound) ||
			errors.Is(err, domaininteraction.ErrCommentNotFound) ||
			errors.Is(err, domaininteraction.ErrCommentUnavailable) ||
			errors.Is(err, domaininteraction.ErrCommentModerated) ||
			errors.Is(err, domaininteraction.ErrCommentLikeIdempotencyConflict) {
			return nil, err
		}
		return nil, ErrUpdateInteractionFailed
	}
	return &CommentLikeResult{
		CommentID:          result.CommentID,
		RootCommentID:      result.RootCommentID,
		Liked:              result.Liked,
		LikeCount:          result.LikeCount,
		LikedByVideoAuthor: result.LikedByVideoAuthor,
	}, nil
}

func buildRootCommentListResult(items []*domaininteraction.Comment, commentCount int, sortMode string, limit int) *CommentListResult {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeCommentCursor(&domaininteraction.CommentCursor{
			Version:   domaininteraction.CommentCursorVersion,
			Sort:      sortMode,
			HotScore:  last.HotScore,
			CreatedAt: last.CreatedAt,
			CommentID: last.ID,
		})
	}
	return &CommentListResult{
		Items: items, NextCursor: nextCursor, HasMore: hasMore,
		CommentCount: commentCount, Sort: sortMode,
	}
}

func parseReplyCursor(raw string) (*domaininteraction.ReplyCursor, error) {
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
	var payload replyCursorPayload
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, domaininteraction.ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.CreatedAt))
	if err != nil || payload.Version != domaininteraction.CommentCursorVersion || payload.CommentID <= 0 {
		return nil, domaininteraction.ErrInvalidCursor
	}
	return &domaininteraction.ReplyCursor{
		Version: payload.Version, CreatedAt: createdAt, CommentID: payload.CommentID,
	}, nil
}

func nextReplyCursor(items []*domaininteraction.Comment) string {
	if len(items) == 0 {
		return ""
	}
	last := items[len(items)-1]
	content, err := json.Marshal(replyCursorPayload{
		Version:   domaininteraction.CommentCursorVersion,
		CreatedAt: last.CreatedAt.UTC().Format(time.RFC3339Nano),
		CommentID: last.ID,
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(content)
}

func isCommentReadDomainError(err error) bool {
	return errors.Is(err, domaininteraction.ErrVideoNotFound) ||
		errors.Is(err, domaininteraction.ErrCommentNotFound) ||
		errors.Is(err, domaininteraction.ErrCommentModerated) ||
		errors.Is(err, domaininteraction.ErrInvalidCursor)
}
