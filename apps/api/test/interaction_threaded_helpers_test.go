package test

import (
	"context"
	"sort"
	"strings"
	"time"

	domainaccount "GCFeed/internal/domain/account"
	domaininteraction "GCFeed/internal/domain/interaction"
)

func (r *memoryInteractionRepo) CreateThreadedComment(_ context.Context, input *domaininteraction.Comment) (*domaininteraction.CommentMutationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.videoPublished(input.VideoID) {
		return nil, domaininteraction.ErrVideoNotFound
	}

	rootID := int64(0)
	if input.ReplyToCommentID > 0 {
		target := r.comments[input.ReplyToCommentID]
		if target == nil || target.VideoID != input.VideoID || target.Status != domaininteraction.CommentStatusNormal {
			return nil, domaininteraction.ErrReplyTargetUnavailable
		}
		rootID = target.EffectiveRootCommentID()
		root := r.comments[rootID]
		if root == nil || !root.IsRoot() || root.Status != domaininteraction.CommentStatusNormal {
			return nil, domaininteraction.ErrReplyTargetUnavailable
		}
	}
	fingerprint := domaininteraction.CommentRequestFingerprint(input.VideoID, rootID, input.ReplyToCommentID, input.Content)
	if input.IdempotencyKey != "" {
		key := memoryInteractionCommentIdemKey(input.UserID, input.IdempotencyKey)
		if id, exists := r.commentIdem[key]; exists {
			existing := r.comments[id]
			if existing.RequestFingerprint != "" && existing.RequestFingerprint != fingerprint {
				return nil, domaininteraction.ErrCommentIdempotencyConflict
			}
			return &domaininteraction.CommentMutationResult{
				Comment: cloneInteractionComment(existing), CommentCount: r.stats[existing.VideoID].CommentCount,
			}, nil
		}
	}

	now := time.Now().UTC()
	comment := cloneInteractionComment(input)
	comment.ID = r.nextCommentID
	r.nextCommentID++
	comment.RootCommentID = rootID
	comment.RequestFingerprint = fingerprint
	comment.UserNickname = memoryInteractionNickname(comment.UserID)
	comment.UserAvatarURL = memoryInteractionAvatar(comment.UserID)
	comment.CanDelete = true
	comment.CreatedAt = now
	comment.UpdatedAt = now
	if comment.ReplyToCommentID > 0 {
		target := r.comments[comment.ReplyToCommentID]
		comment.ReplyToUserID = target.UserID
		comment.ReplyToUserNickname = target.UserNickname
		comment.ReplyToUserAvatarURL = target.UserAvatarURL
	}
	r.comments[comment.ID] = cloneInteractionComment(comment)
	if comment.IdempotencyKey != "" {
		r.commentIdem[memoryInteractionCommentIdemKey(comment.UserID, comment.IdempotencyKey)] = comment.ID
	}
	stat := r.stats[comment.VideoID]
	stat.CommentCount++
	r.stats[comment.VideoID] = stat
	if rootID > 0 {
		root := r.comments[rootID]
		root.ReplyCount++
		root.HotScore = int64(root.LikeCount*3 + root.ReplyCount*5)
	}
	return &domaininteraction.CommentMutationResult{
		Comment: cloneInteractionComment(comment), CommentCount: stat.CommentCount,
		VideoDelta: 1, RootReplyDelta: boolInt(rootID > 0),
	}, nil
}

func (r *memoryInteractionRepo) ListCommentRoots(_ context.Context, query domaininteraction.CommentRootQuery) (*domaininteraction.CommentPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.videoPublished(query.VideoID) {
		return nil, domaininteraction.ErrVideoNotFound
	}
	sortMode, err := domaininteraction.NormalizeCommentSort(query.Sort)
	if err != nil {
		return nil, err
	}
	items := make([]*domaininteraction.Comment, 0)
	for _, stored := range r.comments {
		if stored.VideoID != query.VideoID || !stored.IsRoot() || !stored.EligibleForPublicProjection() {
			continue
		}
		if query.Cursor != nil {
			if query.Cursor.Version != domaininteraction.CommentCursorVersion || query.Cursor.Sort != sortMode {
				return nil, domaininteraction.ErrInvalidCursor
			}
			if !memoryRootAfterCursor(stored, query.Cursor, sortMode) {
				continue
			}
		}
		item := cloneInteractionComment(stored)
		r.applyMemoryViewer(item, query.Viewer, r.videos[item.VideoID].AuthorID)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if sortMode == domaininteraction.CommentSortHot && items[i].HotScore != items[j].HotScore {
			return items[i].HotScore > items[j].HotScore
		}
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	for _, root := range items {
		previews := r.activeRepliesLocked(root.ID)
		if len(previews) > domaininteraction.ReplyPreviewLimit {
			previews = previews[:domaininteraction.ReplyPreviewLimit]
		}
		for _, preview := range previews {
			r.applyMemoryViewer(preview, query.Viewer, r.videos[root.VideoID].AuthorID)
		}
		root.ReplyPreviews = previews
		root.ApplyPublicProjection()
	}
	return &domaininteraction.CommentPage{Items: items, CommentCount: r.stats[query.VideoID].CommentCount}, nil
}

func (r *memoryInteractionRepo) ListCommentReplies(_ context.Context, query domaininteraction.CommentReplyQuery) (*domaininteraction.CommentPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	root := r.comments[query.RootCommentID]
	if root == nil || !root.IsRoot() || !root.EligibleForPublicProjection() {
		return nil, domaininteraction.ErrCommentNotFound
	}
	if !r.videoPublished(root.VideoID) {
		return nil, domaininteraction.ErrVideoNotFound
	}
	items := r.activeRepliesLocked(root.ID)
	filtered := make([]*domaininteraction.Comment, 0, len(items))
	for _, item := range items {
		if query.Cursor != nil {
			if query.Cursor.Version != domaininteraction.CommentCursorVersion {
				return nil, domaininteraction.ErrInvalidCursor
			}
			if item.CreatedAt.Before(query.Cursor.CreatedAt) ||
				(item.CreatedAt.Equal(query.Cursor.CreatedAt) && item.ID <= query.Cursor.CommentID) {
				continue
			}
		}
		r.applyMemoryViewer(item, query.Viewer, r.videos[root.VideoID].AuthorID)
		filtered = append(filtered, item)
	}
	if len(filtered) > query.Limit {
		filtered = filtered[:query.Limit]
	}
	return &domaininteraction.CommentPage{Items: filtered, CommentCount: r.stats[root.VideoID].CommentCount}, nil
}

func (r *memoryInteractionRepo) GetCommentThreadContext(ctx context.Context, targetCommentID int64, viewer domaininteraction.CommentViewer, replyLimit int) (*domaininteraction.CommentThreadContext, error) {
	r.mu.Lock()
	target := r.comments[targetCommentID]
	if target == nil || (target.Status != domaininteraction.CommentStatusNormal && !(target.IsRoot() && target.EligibleForPublicProjection())) {
		r.mu.Unlock()
		return nil, domaininteraction.ErrCommentNotFound
	}
	rootID := target.EffectiveRootCommentID()
	root := cloneInteractionComment(r.comments[rootID])
	targetCopy := cloneInteractionComment(target)
	video := r.videos[root.VideoID]
	r.applyMemoryViewer(root, viewer, video.AuthorID)
	r.applyMemoryViewer(targetCopy, viewer, video.AuthorID)
	root.ApplyPublicProjection()
	if targetCopy.ID == root.ID {
		targetCopy = root
	}
	r.mu.Unlock()
	page, err := r.ListCommentReplies(ctx, domaininteraction.CommentReplyQuery{
		RootCommentID: rootID, Viewer: viewer, Limit: replyLimit,
	})
	if err != nil {
		return nil, err
	}
	return &domaininteraction.CommentThreadContext{
		Root: root, Replies: page.Items, Target: targetCopy, CommentCount: page.CommentCount,
	}, nil
}

func (r *memoryInteractionRepo) SetCommentLike(_ context.Context, commentID int64, userID int64, active bool, idempotencyKey string) (*domaininteraction.CommentLikeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	comment := r.comments[commentID]
	if comment == nil {
		return nil, domaininteraction.ErrCommentNotFound
	}
	if !r.videoPublished(comment.VideoID) {
		return nil, domaininteraction.ErrVideoNotFound
	}
	if comment.Status != domaininteraction.CommentStatusNormal {
		return nil, domaininteraction.ErrCommentUnavailable
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" {
		receiptKey := int64String(userID) + ":" + idempotencyKey
		if receipt, exists := r.commentLikeIdem[receiptKey]; exists {
			if receipt.CommentID != commentID || receipt.Active != active {
				return nil, domaininteraction.ErrCommentLikeIdempotencyConflict
			}
			return &domaininteraction.CommentLikeResult{
				CommentID: commentID, RootCommentID: comment.EffectiveRootCommentID(),
				Liked: active, LikeCount: receipt.LikeCount,
			}, nil
		}
	}
	likeKey := int64String(userID) + ":" + int64String(commentID)
	if r.commentLikes[likeKey] != active {
		if active {
			comment.LikeCount++
		} else if comment.LikeCount > 0 {
			comment.LikeCount--
		}
		r.commentLikes[likeKey] = active
		if comment.IsRoot() {
			comment.HotScore = int64(comment.LikeCount*3 + comment.ReplyCount*5)
		}
	}
	if idempotencyKey != "" {
		r.commentLikeIdem[int64String(userID)+":"+idempotencyKey] = memoryCommentLikeReceipt{
			CommentID: commentID, Active: active, LikeCount: comment.LikeCount,
		}
	}
	return &domaininteraction.CommentLikeResult{
		CommentID: commentID, RootCommentID: comment.EffectiveRootCommentID(),
		Liked: active, LikeCount: comment.LikeCount,
	}, nil
}

func (r *memoryInteractionRepo) DeleteThreadedComment(_ context.Context, commentID int64, userID int64, role string) (*domaininteraction.CommentDeletionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	comment := r.comments[commentID]
	if comment == nil {
		return nil, domaininteraction.ErrCommentNotFound
	}
	video := r.videos[comment.VideoID]
	isModerator := video.AuthorID == userID || role == domainaccount.RoleAdmin
	if comment.UserID != userID && !isModerator {
		return nil, domaininteraction.ErrCommentPermissionDenied
	}
	result := &domaininteraction.CommentDeletionResult{
		Comment: cloneInteractionComment(comment), CommentCount: r.stats[comment.VideoID].CommentCount,
	}
	if comment.Status != domaininteraction.CommentStatusNormal {
		result.RootReplyCount = comment.ReplyCount
		result.ThreadHidden = comment.Status == domaininteraction.CommentStatusModerated
		result.Tombstone = comment.IsRoot() && comment.Status == domaininteraction.CommentStatusSelfDeleted && comment.ReplyCount > 0
		return result, nil
	}
	if comment.IsRoot() {
		if isModerator {
			deleted := 1
			for _, reply := range r.comments {
				if reply.RootCommentID == comment.ID && reply.Status == domaininteraction.CommentStatusNormal {
					reply.Status = domaininteraction.CommentStatusModerated
					deleted++
				}
			}
			comment.Status = domaininteraction.CommentStatusModerated
			comment.ReplyCount = 0
			comment.HotScore = 0
			result.ThreadHidden = true
			result.DeletedCount = deleted
			result.VideoDelta = -deleted
		} else {
			comment.Status = domaininteraction.CommentStatusSelfDeleted
			result.DeletedCount = 1
			result.VideoDelta = -1
			result.RootReplyCount = comment.ReplyCount
			result.Tombstone = comment.ReplyCount > 0
		}
	} else {
		if isModerator {
			comment.Status = domaininteraction.CommentStatusModerated
		} else {
			comment.Status = domaininteraction.CommentStatusSelfDeleted
		}
		root := r.comments[comment.RootCommentID]
		if root.ReplyCount > 0 {
			root.ReplyCount--
		}
		root.HotScore = int64(root.LikeCount*3 + root.ReplyCount*5)
		result.RootReplyCount = root.ReplyCount
		result.DeletedCount = 1
		result.VideoDelta = -1
	}
	stat := r.stats[comment.VideoID]
	stat.CommentCount = clampMemoryCount(stat.CommentCount + result.VideoDelta)
	r.stats[comment.VideoID] = stat
	result.CommentCount = stat.CommentCount
	result.Comment = cloneInteractionComment(comment)
	return result, nil
}

func (*memoryInteractionRepo) ReconcileCommentCounters(context.Context) error {
	return nil
}

func (r *memoryInteractionRepo) activeRepliesLocked(rootID int64) []*domaininteraction.Comment {
	items := make([]*domaininteraction.Comment, 0)
	for _, reply := range r.comments {
		if reply.RootCommentID == rootID && reply.Status == domaininteraction.CommentStatusNormal {
			items = append(items, cloneInteractionComment(reply))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (r *memoryInteractionRepo) applyMemoryViewer(comment *domaininteraction.Comment, viewer domaininteraction.CommentViewer, videoAuthorID int64) {
	if comment == nil {
		return
	}
	comment.CanDelete = viewer.UserID > 0 &&
		(comment.UserID == viewer.UserID || videoAuthorID == viewer.UserID || viewer.Role == domainaccount.RoleAdmin)
	comment.Liked = viewer.UserID > 0 && r.commentLikes[int64String(viewer.UserID)+":"+int64String(comment.ID)]
}

func memoryRootAfterCursor(comment *domaininteraction.Comment, cursor *domaininteraction.CommentCursor, sortMode string) bool {
	if sortMode == domaininteraction.CommentSortHot && comment.HotScore != cursor.HotScore {
		return comment.HotScore < cursor.HotScore
	}
	return comment.CreatedAt.Before(cursor.CreatedAt) ||
		(comment.CreatedAt.Equal(cursor.CreatedAt) && comment.ID < cursor.CommentID)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

var _ domaininteraction.ThreadedCommentRepository = (*memoryInteractionRepo)(nil)
