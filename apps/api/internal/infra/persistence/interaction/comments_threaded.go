package infrainteraction

import (
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infrapersistence "github.com/shiyudesu/frux/internal/infra/persistence"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type threadedCommentRow struct {
	ID                   int64
	VideoID              int64
	UserID               int64
	UserNickname         string
	UserAvatarURL        string
	RootCommentID        *int64
	ReplyToCommentID     *int64
	ReplyToUserID        *int64
	ReplyToUserNickname  string
	ReplyToUserAvatarURL string
	Content              string
	Status               int
	ReplyCount           int
	LikeCount            int
	HotScore             int64
	RequestFingerprint   string
	IdempotencyKey       *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (r *Repository) CreateThreadedComment(ctx context.Context, comment *domaininteraction.Comment) (*domaininteraction.CommentMutationResult, error) {
	if comment == nil {
		return nil, domaininteraction.ErrCommentNotFound
	}
	if comment.IdempotencyKey != "" {
		existing, err := r.loadThreadedCommentByIdempotency(ctx, comment.UserID, comment.IdempotencyKey)
		if err == nil {
			return r.replayThreadedComment(ctx, existing, comment)
		}
		if !errors.Is(err, domaininteraction.ErrCommentNotFound) {
			return nil, err
		}
	}

	var createdID int64
	var count, videoDelta, rootReplyDelta int
	var resolvedFingerprint string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		video, err := lockPublishedVideo(tx, comment.VideoID)
		if err != nil {
			return err
		}

		rootCommentID := int64(0)
		replyToCommentID := comment.ReplyToCommentID
		recipientID := video.AuthorID
		messageType := domaininteraction.CommentNotificationTypeRoot
		title := "收到评论"
		if replyToCommentID > 0 {
			var target CommentModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", replyToCommentID).Take(&target).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domaininteraction.ErrReplyTargetUnavailable
				}
				return err
			}
			if target.VideoID != comment.VideoID || target.Status != domaininteraction.CommentStatusNormal {
				return domaininteraction.ErrReplyTargetUnavailable
			}
			recipientID = target.UserID
			messageType = domaininteraction.CommentNotificationTypeReply
			title = "收到回复"
			if target.RootCommentID == nil {
				rootCommentID = target.ID
			} else {
				rootCommentID = *target.RootCommentID
			}

			var root CommentModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", rootCommentID).Take(&root).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domaininteraction.ErrReplyTargetUnavailable
				}
				return err
			}
			if root.RootCommentID != nil || root.VideoID != comment.VideoID || root.Status != domaininteraction.CommentStatusNormal {
				return domaininteraction.ErrReplyTargetUnavailable
			}
		}
		resolvedFingerprint = domaininteraction.CommentRequestFingerprint(comment.VideoID, rootCommentID, replyToCommentID, comment.Content)

		if comment.IdempotencyKey != "" {
			var existing CommentModel
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ? AND idempotency_key = ?", comment.UserID, comment.IdempotencyKey).
				Take(&existing).Error
			if err == nil {
				if existing.RequestFingerprint != "" && existing.RequestFingerprint != resolvedFingerprint {
					return domaininteraction.ErrCommentIdempotencyConflict
				}
				createdID = existing.ID
				currentCount, countErr := currentVideoStatCounter(tx, existing.VideoID, "comment_count")
				if countErr != nil {
					return countErr
				}
				count = currentCount
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		model := CommentModel{
			VideoID:            comment.VideoID,
			UserID:             comment.UserID,
			Content:            comment.Content,
			Status:             domaininteraction.CommentStatusNormal,
			RequestFingerprint: resolvedFingerprint,
			IdempotencyKey:     idempotencyKeyPtr(comment.IdempotencyKey),
		}
		if rootCommentID > 0 {
			model.RootCommentID = &rootCommentID
			model.ReplyToCommentID = &replyToCommentID
		}
		if err := tx.Create(&model).Error; err != nil {
			if infrapersistence.IsDuplicatedKeyError(err) && comment.IdempotencyKey != "" {
				return domaininteraction.ErrCommentNotFound
			}
			return err
		}
		createdID = model.ID

		nextCount, err := updateVideoStatCounter(tx, model.VideoID, "comment_count", 1)
		if err != nil {
			return err
		}
		count = nextCount
		videoDelta = 1
		if rootCommentID > 0 {
			if err := tx.Model(&CommentModel{}).Where("id = ?", rootCommentID).Updates(map[string]any{
				"reply_count": gorm.Expr("reply_count + 1"),
				"hot_score":   gorm.Expr("like_count * 3 + (reply_count + 1) * 5"),
			}).Error; err != nil {
				return err
			}
			rootReplyDelta = 1
		}
		notificationRootID := model.ID
		if rootCommentID > 0 {
			notificationRootID = rootCommentID
		}
		notification, err := domaininteraction.NewCommentNotification(
			fmt.Sprintf("interaction:comment:%d", model.ID),
			recipientID,
			model.UserID,
			messageType,
			title,
			model.Content,
			model.VideoID,
			notificationRootID,
			model.ID,
			model.CreatedAt,
		)
		if err != nil {
			return err
		}
		if err := createCommentNotificationOutbox(tx, notification); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, domaininteraction.ErrCommentNotFound) && comment.IdempotencyKey != "" {
		existing, loadErr := r.loadThreadedCommentByIdempotency(ctx, comment.UserID, comment.IdempotencyKey)
		if loadErr != nil {
			return nil, loadErr
		}
		if existing.RequestFingerprint != "" && existing.RequestFingerprint != resolvedFingerprint {
			return nil, domaininteraction.ErrCommentIdempotencyConflict
		}
		count, loadErr = r.commentCount(ctx, existing.VideoID)
		if loadErr != nil {
			return nil, loadErr
		}
		return &domaininteraction.CommentMutationResult{Comment: existing, CommentCount: count}, nil
	}
	if err != nil {
		return nil, mapVideoError(err)
	}

	created, err := r.loadThreadedCommentByID(ctx, createdID)
	if err != nil {
		return nil, err
	}
	created.CanDelete = created.UserID == comment.UserID
	return &domaininteraction.CommentMutationResult{
		Comment: created, CommentCount: count, VideoDelta: videoDelta, RootReplyDelta: rootReplyDelta,
	}, nil
}

func (r *Repository) ListCommentRoots(ctx context.Context, query domaininteraction.CommentRootQuery) (*domaininteraction.CommentPage, error) {
	sortMode, err := domaininteraction.NormalizeCommentSort(query.Sort)
	if err != nil {
		return nil, err
	}
	video, err := r.publicVideo(ctx, query.VideoID)
	if err != nil {
		return nil, err
	}

	var rows []threadedCommentRow
	dbQuery := r.commentRows(r.db.WithContext(ctx)).
		Joins("JOIN video AS visible_video ON visible_video.id = c.video_id").
		Where("c.video_id = ? AND c.root_comment_id IS NULL AND (c.status = ? OR (c.status = ? AND c.reply_count > 0))",
			query.VideoID, domaininteraction.CommentStatusNormal, domaininteraction.CommentStatusSelfDeleted)
	dbQuery = requireVisibleVideoQuery(dbQuery, "visible_video")
	if query.Cursor != nil {
		if query.Cursor.Version != domaininteraction.CommentCursorVersion || query.Cursor.Sort != sortMode {
			return nil, domaininteraction.ErrInvalidCursor
		}
		if sortMode == domaininteraction.CommentSortHot {
			dbQuery = dbQuery.Where(
				"(c.hot_score < ? OR (c.hot_score = ? AND (c.created_at < ? OR (c.created_at = ? AND c.id < ?))))",
				query.Cursor.HotScore, query.Cursor.HotScore, query.Cursor.CreatedAt, query.Cursor.CreatedAt, query.Cursor.CommentID,
			)
		} else {
			dbQuery = dbQuery.Where(
				"(c.created_at < ? OR (c.created_at = ? AND c.id < ?))",
				query.Cursor.CreatedAt, query.Cursor.CreatedAt, query.Cursor.CommentID,
			)
		}
	}
	if sortMode == domaininteraction.CommentSortHot {
		dbQuery = dbQuery.Order("c.hot_score DESC")
	}
	if err := dbQuery.Order("c.created_at DESC").Order("c.id DESC").Limit(query.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*domaininteraction.Comment, 0, len(rows))
	rootIDs := make([]int64, 0, len(rows))
	byRoot := make(map[int64]*domaininteraction.Comment, len(rows))
	for _, row := range rows {
		comment := restoreThreadedComment(row)
		items = append(items, comment)
		rootIDs = append(rootIDs, comment.ID)
		byRoot[comment.ID] = comment
	}
	previewLimit := query.PreviewLimit
	if previewLimit <= 0 || previewLimit > domaininteraction.ReplyPreviewLimit {
		previewLimit = domaininteraction.ReplyPreviewLimit
	}
	if len(rootIDs) > 0 && previewLimit > 0 {
		var previews []threadedCommentRow
		previewSQL := `
			WITH ranked AS (
				SELECT c.id, ROW_NUMBER() OVER (
					PARTITION BY c.root_comment_id ORDER BY c.created_at ASC, c.id ASC
				) AS row_number
				FROM interaction_comment AS c
				WHERE c.root_comment_id IN ? AND c.status = ?
			)
		`
		if err := r.commentRows(r.db.WithContext(ctx)).
			Joins("JOIN ("+previewSQL+" SELECT id FROM ranked WHERE row_number <= ?) AS preview ON preview.id = c.id",
				rootIDs, domaininteraction.CommentStatusNormal, previewLimit).
			Order("c.root_comment_id ASC").Order("c.created_at ASC").Order("c.id ASC").
			Scan(&previews).Error; err != nil {
			return nil, err
		}
		for _, row := range previews {
			reply := restoreThreadedComment(row)
			if root := byRoot[reply.RootCommentID]; root != nil {
				root.ReplyPreviews = append(root.ReplyPreviews, reply)
			}
		}
	}
	allPreviews := make([]*domaininteraction.Comment, 0)
	for _, root := range items {
		allPreviews = append(allPreviews, root.ReplyPreviews...)
	}
	if err := r.applyCommentViewerState(ctx, items, query.Viewer, video.AuthorID); err != nil {
		return nil, err
	}
	if err := r.applyCommentViewerState(ctx, allPreviews, query.Viewer, video.AuthorID); err != nil {
		return nil, err
	}
	for _, root := range items {
		root.ApplyPublicProjection()
	}
	count, err := r.commentCount(ctx, query.VideoID)
	if err != nil {
		return nil, err
	}
	return &domaininteraction.CommentPage{Items: items, CommentCount: count}, nil
}

func (r *Repository) ListCommentReplies(ctx context.Context, query domaininteraction.CommentReplyQuery) (*domaininteraction.CommentPage, error) {
	if query.RootCommentID <= 0 {
		return nil, domaininteraction.ErrInvalidRootCommentID
	}
	root, err := r.loadThreadedCommentByID(ctx, query.RootCommentID)
	if err != nil {
		return nil, err
	}
	if !root.IsRoot() || !root.EligibleForPublicProjection() {
		return nil, domaininteraction.ErrCommentNotFound
	}
	video, err := r.publicVideo(ctx, root.VideoID)
	if err != nil {
		return nil, err
	}

	var rows []threadedCommentRow
	dbQuery := r.commentRows(r.db.WithContext(ctx)).
		Joins("JOIN video AS visible_video ON visible_video.id = c.video_id").
		Where("c.root_comment_id = ? AND c.status = ?", query.RootCommentID, domaininteraction.CommentStatusNormal)
	dbQuery = requireVisibleVideoQuery(dbQuery, "visible_video")
	if query.Cursor != nil {
		if query.Cursor.Version != domaininteraction.CommentCursorVersion {
			return nil, domaininteraction.ErrInvalidCursor
		}
		dbQuery = dbQuery.Where(
			"(c.created_at > ? OR (c.created_at = ? AND c.id > ?))",
			query.Cursor.CreatedAt, query.Cursor.CreatedAt, query.Cursor.CommentID,
		)
	}
	if err := dbQuery.Order("c.created_at ASC").Order("c.id ASC").Limit(query.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := restoreThreadedComments(rows)
	if err := r.applyCommentViewerState(ctx, items, query.Viewer, video.AuthorID); err != nil {
		return nil, err
	}
	count, err := r.commentCount(ctx, root.VideoID)
	if err != nil {
		return nil, err
	}
	return &domaininteraction.CommentPage{Items: items, CommentCount: count}, nil
}

func (r *Repository) GetCommentThreadContext(ctx context.Context, targetCommentID int64, viewer domaininteraction.CommentViewer, replyLimit int) (*domaininteraction.CommentThreadContext, error) {
	target, err := r.loadThreadedCommentByID(ctx, targetCommentID)
	if err != nil {
		return nil, err
	}
	if target.Status != domaininteraction.CommentStatusNormal && !(target.IsRoot() && target.EligibleForPublicProjection()) {
		if target.Status == domaininteraction.CommentStatusModerated {
			return nil, domaininteraction.ErrCommentModerated
		}
		return nil, domaininteraction.ErrCommentNotFound
	}
	rootID := target.EffectiveRootCommentID()
	root, err := r.loadThreadedCommentByID(ctx, rootID)
	if err != nil {
		return nil, err
	}
	if !root.IsRoot() || !root.EligibleForPublicProjection() {
		return nil, domaininteraction.ErrCommentNotFound
	}
	video, err := r.publicVideo(ctx, root.VideoID)
	if err != nil {
		return nil, err
	}
	page, err := r.ListCommentReplies(ctx, domaininteraction.CommentReplyQuery{
		RootCommentID: rootID, Viewer: viewer, Limit: replyLimit,
	})
	if err != nil {
		return nil, err
	}
	if err := r.applyCommentViewerState(ctx, []*domaininteraction.Comment{root, target}, viewer, video.AuthorID); err != nil {
		return nil, err
	}
	root.ApplyPublicProjection()
	if target.ID == root.ID {
		target = root
	}
	return &domaininteraction.CommentThreadContext{
		Root: root, Replies: page.Items, Target: target, CommentCount: page.CommentCount,
	}, nil
}

func (r *Repository) SetCommentLike(ctx context.Context, commentID int64, userID int64, active bool, idempotencyKey string) (*domaininteraction.CommentLikeResult, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	var result domaininteraction.CommentLikeResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, ?))", idempotencyKey, userID).Error; err != nil {
				return err
			}
		}
		var identity CommentModel
		if err := tx.Select("id", "video_id").Where("id = ?", commentID).Take(&identity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domaininteraction.ErrCommentNotFound
			}
			return err
		}
		if _, err := lockPublishedVideo(tx, identity.VideoID); err != nil {
			return err
		}
		var comment CommentModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", commentID).Take(&comment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domaininteraction.ErrCommentNotFound
			}
			return err
		}
		if comment.Status != domaininteraction.CommentStatusNormal {
			if comment.Status == domaininteraction.CommentStatusModerated {
				return domaininteraction.ErrCommentModerated
			}
			return domaininteraction.ErrCommentUnavailable
		}
		rootID := comment.ID
		if comment.RootCommentID != nil {
			rootID = *comment.RootCommentID
		}
		if idempotencyKey != "" {
			var receipt CommentLikeIdempotencyReceiptModel
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).
				Take(&receipt).Error
			if err == nil {
				if receipt.CommentID != commentID || receipt.Active != active {
					return domaininteraction.ErrCommentLikeIdempotencyConflict
				}
				result = domaininteraction.CommentLikeResult{
					CommentID: commentID, RootCommentID: rootID, Liked: active, LikeCount: receipt.LikeCount,
				}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		var like CommentLikeModel
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND comment_id = ?", userID, commentID).
			Take(&like).Error
		wasActive := findErr == nil && like.Status == domaininteraction.ActionStatusActive
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		delta := 0
		if wasActive != active {
			if active {
				delta = 1
			} else {
				delta = -1
			}
		}
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			like = CommentLikeModel{UserID: userID, CommentID: commentID, Status: actionStatusFromActive(active)}
			if err := tx.Create(&like).Error; err != nil {
				return err
			}
		} else if like.Status != actionStatusFromActive(active) {
			like.Status = actionStatusFromActive(active)
			if err := tx.Save(&like).Error; err != nil {
				return err
			}
		}
		if delta != 0 {
			comment.LikeCount = clampCount(comment.LikeCount + delta)
			updates := map[string]any{"like_count": comment.LikeCount}
			if comment.RootCommentID == nil {
				comment.HotScore = int64(comment.LikeCount*3 + comment.ReplyCount*5)
				updates["hot_score"] = comment.HotScore
			}
			if err := tx.Model(&CommentModel{}).Where("id = ?", comment.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if delta > 0 && active && comment.UserID != userID {
			notification, err := domaininteraction.NewCommentNotification(
				fmt.Sprintf("interaction:comment-like:%d:%d", comment.ID, userID),
				comment.UserID,
				userID,
				domaininteraction.CommentNotificationTypeLike,
				"评论获赞",
				"点赞了你的评论",
				comment.VideoID,
				rootID,
				comment.ID,
				time.Now().UTC(),
			)
			if err != nil {
				return err
			}
			if err := createCommentNotificationOutbox(tx, notification); err != nil {
				return err
			}
		}
		result = domaininteraction.CommentLikeResult{
			CommentID: comment.ID, RootCommentID: rootID, Liked: active, LikeCount: comment.LikeCount,
		}
		if idempotencyKey != "" {
			if err := tx.Create(&CommentLikeIdempotencyReceiptModel{
				UserID: userID, IdempotencyKey: idempotencyKey, CommentID: commentID,
				Active: active, LikeCount: comment.LikeCount,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapVideoError(err)
	}
	return &result, nil
}

func (r *Repository) DeleteThreadedComment(ctx context.Context, commentID int64, userID int64, role string) (*domaininteraction.CommentDeletionResult, error) {
	role = strings.TrimSpace(role)
	var result domaininteraction.CommentDeletionResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity CommentModel
		if err := tx.Select("id", "video_id").Where("id = ?", commentID).Take(&identity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domaininteraction.ErrCommentNotFound
			}
			return err
		}
		var video infravideo.VideoModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", identity.VideoID).Take(&video).Error; err != nil {
			return mapVideoError(err)
		}
		var comment CommentModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", commentID).Take(&comment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domaininteraction.ErrCommentNotFound
			}
			return err
		}
		isModerator := video.AuthorID == userID || role == domainaccount.RoleAdmin
		if comment.UserID != userID && !isModerator {
			return domaininteraction.ErrCommentPermissionDenied
		}
		count, err := currentVideoStatCounter(tx, comment.VideoID, "comment_count")
		if err != nil {
			return err
		}
		result = domaininteraction.CommentDeletionResult{CommentCount: count}
		if comment.Status == domaininteraction.CommentStatusSelfDeleted && comment.RootCommentID == nil && isModerator {
			var activeReplies int64
			if err := tx.Model(&CommentModel{}).
				Where("root_comment_id = ? AND status = ?", comment.ID, domaininteraction.CommentStatusNormal).
				Count(&activeReplies).Error; err != nil {
				return err
			}
			if err := tx.Model(&CommentModel{}).
				Where("root_comment_id = ? AND status = ?", comment.ID, domaininteraction.CommentStatusNormal).
				Update("status", domaininteraction.CommentStatusModerated).Error; err != nil {
				return err
			}
			comment.Status = domaininteraction.CommentStatusModerated
			comment.ReplyCount = 0
			comment.HotScore = 0
			if err := tx.Model(&CommentModel{}).Where("id = ?", comment.ID).Updates(map[string]any{
				"status": comment.Status, "reply_count": 0, "hot_score": 0,
			}).Error; err != nil {
				return err
			}
			result.DeletedCount = int(activeReplies)
			result.VideoDelta = -int(activeReplies)
			result.ThreadHidden = true
			if result.VideoDelta != 0 {
				nextCount, err := updateVideoStatCounter(tx, comment.VideoID, "comment_count", result.VideoDelta)
				if err != nil {
					return err
				}
				result.CommentCount = nextCount
			}
			result.Comment = restoreThreadedCommentModel(comment)
			return nil
		}
		if comment.Status != domaininteraction.CommentStatusNormal {
			result.Comment = restoreThreadedCommentModel(comment)
			if comment.RootCommentID == nil {
				result.RootReplyCount = comment.ReplyCount
				result.Tombstone = comment.Status == domaininteraction.CommentStatusSelfDeleted && comment.ReplyCount > 0
				result.ThreadHidden = comment.Status == domaininteraction.CommentStatusModerated
			}
			return nil
		}

		if comment.RootCommentID == nil {
			if isModerator {
				var activeReplies int64
				if err := tx.Model(&CommentModel{}).
					Where("root_comment_id = ? AND status = ?", comment.ID, domaininteraction.CommentStatusNormal).
					Count(&activeReplies).Error; err != nil {
					return err
				}
				if err := tx.Model(&CommentModel{}).
					Where("root_comment_id = ? AND status = ?", comment.ID, domaininteraction.CommentStatusNormal).
					Updates(map[string]any{"status": domaininteraction.CommentStatusModerated}).Error; err != nil {
					return err
				}
				comment.Status = domaininteraction.CommentStatusModerated
				comment.ReplyCount = 0
				comment.HotScore = 0
				if err := tx.Model(&CommentModel{}).Where("id = ?", comment.ID).Updates(map[string]any{
					"status": comment.Status, "reply_count": 0, "hot_score": 0,
				}).Error; err != nil {
					return err
				}
				result.DeletedCount = int(activeReplies) + 1
				result.VideoDelta = -result.DeletedCount
				result.ThreadHidden = true
			} else {
				comment.Status = domaininteraction.CommentStatusSelfDeleted
				if err := tx.Model(&CommentModel{}).Where("id = ?", comment.ID).Update("status", comment.Status).Error; err != nil {
					return err
				}
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
			if err := tx.Model(&CommentModel{}).Where("id = ?", comment.ID).Update("status", comment.Status).Error; err != nil {
				return err
			}
			if err := tx.Model(&CommentModel{}).Where("id = ?", *comment.RootCommentID).Updates(map[string]any{
				"reply_count": gorm.Expr("GREATEST(reply_count - 1, 0)"),
				"hot_score":   gorm.Expr("like_count * 3 + GREATEST(reply_count - 1, 0) * 5"),
			}).Error; err != nil {
				return err
			}
			var root CommentModel
			if err := tx.Where("id = ?", *comment.RootCommentID).Take(&root).Error; err != nil {
				return err
			}
			result.RootReplyCount = root.ReplyCount
			result.DeletedCount = 1
			result.VideoDelta = -1
		}
		if result.VideoDelta != 0 {
			nextCount, err := updateVideoStatCounter(tx, comment.VideoID, "comment_count", result.VideoDelta)
			if err != nil {
				return err
			}
			result.CommentCount = nextCount
		}
		result.Comment = restoreThreadedCommentModel(comment)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) ReconcileCommentCounters(ctx context.Context) error {
	return ReconcileCommentCounters(r.db.WithContext(ctx))
}

func ReconcileCommentCounters(db *gorm.DB) error {
	if err := db.Exec(`
		WITH desired_replies AS (
			SELECT root_comment_id AS id, COUNT(*) AS reply_count
			FROM interaction_comment
			WHERE root_comment_id IS NOT NULL AND status = 1
			GROUP BY root_comment_id
		),
		desired_likes AS (
			SELECT comment_id AS id, COUNT(*) AS like_count
			FROM interaction_comment_like
			WHERE status = 1
			GROUP BY comment_id
		),
		desired AS (
			SELECT
				c.id,
				COALESCE(replies.reply_count, 0) AS reply_count,
				COALESCE(likes.like_count, 0) AS like_count
			FROM interaction_comment AS c
			LEFT JOIN desired_replies AS replies ON replies.id = c.id
			LEFT JOIN desired_likes AS likes ON likes.id = c.id
		),
		snapshot AS MATERIALIZED (
			SELECT
				current.id,
				current.reply_count AS baseline_reply_count,
				current.like_count AS baseline_like_count,
				current.hot_score AS baseline_hot_score,
				desired.reply_count AS desired_reply_count,
				desired.like_count AS desired_like_count,
				CASE
					WHEN current.root_comment_id IS NULL
					THEN desired.like_count * 3 + desired.reply_count * 5
					ELSE 0
				END AS desired_hot_score
			FROM interaction_comment AS current
			JOIN desired ON desired.id = current.id
		)
		UPDATE interaction_comment AS current
		SET
			reply_count = GREATEST(current.reply_count + snapshot.desired_reply_count - snapshot.baseline_reply_count, 0),
			like_count = GREATEST(current.like_count + snapshot.desired_like_count - snapshot.baseline_like_count, 0),
			hot_score = GREATEST(current.hot_score + snapshot.desired_hot_score - snapshot.baseline_hot_score, 0),
			updated_at = NOW()
		FROM snapshot
		WHERE current.id = snapshot.id
			AND (
				snapshot.desired_reply_count <> snapshot.baseline_reply_count
				OR snapshot.desired_like_count <> snapshot.baseline_like_count
				OR snapshot.desired_hot_score <> snapshot.baseline_hot_score
			)
	`).Error; err != nil {
		return err
	}
	return db.Exec(`
		WITH desired AS (
			SELECT video_id, COUNT(*) FILTER (WHERE status = 1) AS comment_count
			FROM interaction_comment
			GROUP BY video_id
		),
		snapshot AS MATERIALIZED (
			SELECT
				current.video_id,
				current.comment_count AS baseline_comment_count,
				COALESCE(desired.comment_count, 0) AS desired_comment_count
			FROM video_stat AS current
			LEFT JOIN desired ON desired.video_id = current.video_id
		)
		UPDATE video_stat AS current
		SET
			comment_count = GREATEST(current.comment_count + snapshot.desired_comment_count - snapshot.baseline_comment_count, 0),
			updated_at = NOW()
		FROM snapshot
		WHERE current.video_id = snapshot.video_id
			AND snapshot.desired_comment_count <> snapshot.baseline_comment_count
	`).Error
}

func BackfillThreadedComments(db *gorm.DB) error {
	return db.Exec(`
		UPDATE interaction_comment
		SET
			reply_count = COALESCE(reply_count, 0),
			like_count = COALESCE(like_count, 0),
			hot_score = COALESCE(hot_score, 0),
			request_fingerprint = COALESCE(request_fingerprint, '')
		WHERE
			reply_count IS NULL OR like_count IS NULL OR hot_score IS NULL OR request_fingerprint IS NULL
	`).Error
}

func (r *Repository) publicVideo(ctx context.Context, videoID int64) (*infravideo.VideoModel, error) {
	var video infravideo.VideoModel
	err := r.db.WithContext(ctx).
		Where("id = ? AND status = ? AND visibility = ? AND media_status IN ?",
			videoID, domainvideo.StatusPublished, domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Take(&video).Error
	if err != nil {
		return nil, mapVideoError(err)
	}
	return &video, nil
}

func requireVisibleVideoQuery(query *gorm.DB, alias string) *gorm.DB {
	return query.Where(
		alias+".status = ? AND "+alias+".visibility = ? AND "+alias+".media_status IN ?",
		domainvideo.StatusPublished,
		domainvideo.VisibilityPublic,
		[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady},
	)
}

func (r *Repository) commentRows(db *gorm.DB) *gorm.DB {
	return db.Table("interaction_comment AS c").
		Select(threadedCommentSelect()).
		Joins("LEFT JOIN account AS author ON author.id = c.user_id").
		Joins("LEFT JOIN interaction_comment AS target ON target.id = c.reply_to_comment_id").
		Joins("LEFT JOIN account AS target_author ON target_author.id = target.user_id")
}

func threadedCommentSelect() string {
	return `c.id, c.video_id, c.user_id,
		author.nickname AS user_nickname, author.avatar_url AS user_avatar_url,
		c.root_comment_id, c.reply_to_comment_id,
		CASE WHEN target.status = 1 THEN target.user_id END AS reply_to_user_id,
		CASE WHEN target.status = 1 THEN target_author.nickname ELSE '' END AS reply_to_user_nickname,
		CASE WHEN target.status = 1 THEN target_author.avatar_url ELSE '' END AS reply_to_user_avatar_url,
		c.content, c.status, c.reply_count, c.like_count, c.hot_score,
		c.request_fingerprint, c.idempotency_key, c.created_at, c.updated_at`
}

func (r *Repository) loadThreadedCommentByID(ctx context.Context, commentID int64) (*domaininteraction.Comment, error) {
	var row threadedCommentRow
	err := r.commentRows(r.db.WithContext(ctx)).Where("c.id = ?", commentID).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domaininteraction.ErrCommentNotFound
		}
		return nil, err
	}
	return restoreThreadedComment(row), nil
}

func (r *Repository) loadThreadedCommentByIdempotency(ctx context.Context, userID int64, idempotencyKey string) (*domaininteraction.Comment, error) {
	var row threadedCommentRow
	err := r.commentRows(r.db.WithContext(ctx)).
		Where("c.user_id = ? AND c.idempotency_key = ?", userID, idempotencyKey).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domaininteraction.ErrCommentNotFound
		}
		return nil, err
	}
	return restoreThreadedComment(row), nil
}

func (r *Repository) applyCommentViewerState(ctx context.Context, comments []*domaininteraction.Comment, viewer domaininteraction.CommentViewer, videoAuthorID int64) error {
	if len(comments) == 0 {
		return nil
	}
	isAdmin := strings.TrimSpace(viewer.Role) == domainaccount.RoleAdmin
	ids := make([]int64, 0, len(comments))
	for _, comment := range comments {
		if comment == nil {
			continue
		}
		comment.CanDelete = viewer.UserID > 0 &&
			(comment.UserID == viewer.UserID || videoAuthorID == viewer.UserID || isAdmin)
		ids = append(ids, comment.ID)
	}
	if viewer.UserID <= 0 || len(ids) == 0 {
		return nil
	}
	var likedIDs []int64
	if err := r.db.WithContext(ctx).Model(&CommentLikeModel{}).
		Where("user_id = ? AND comment_id IN ? AND status = ?", viewer.UserID, ids, domaininteraction.ActionStatusActive).
		Pluck("comment_id", &likedIDs).Error; err != nil {
		return err
	}
	liked := make(map[int64]struct{}, len(likedIDs))
	for _, id := range likedIDs {
		liked[id] = struct{}{}
	}
	for _, comment := range comments {
		_, comment.Liked = liked[comment.ID]
	}
	return nil
}

func (r *Repository) replayThreadedComment(ctx context.Context, existing *domaininteraction.Comment, requested *domaininteraction.Comment) (*domaininteraction.CommentMutationResult, error) {
	if existing == nil || requested == nil {
		return nil, domaininteraction.ErrCommentNotFound
	}
	if existing.RequestFingerprint != "" {
		if existing.VideoID != requested.VideoID ||
			existing.ReplyToCommentID != requested.ReplyToCommentID ||
			existing.Content != strings.TrimSpace(requested.Content) {
			return nil, domaininteraction.ErrCommentIdempotencyConflict
		}
		expected := domaininteraction.CommentRequestFingerprint(
			existing.VideoID, existing.RootCommentID, existing.ReplyToCommentID, existing.Content,
		)
		if existing.RequestFingerprint != expected {
			return nil, domaininteraction.ErrCommentIdempotencyConflict
		}
	}
	if _, err := r.publicVideo(ctx, existing.VideoID); err != nil {
		return nil, err
	}
	if !existing.EligibleForPublicProjection() {
		return nil, domaininteraction.ErrCommentNotFound
	}
	count, err := r.commentCount(ctx, existing.VideoID)
	if err != nil {
		return nil, err
	}
	existing.CanDelete = existing.UserID == requested.UserID
	existing.ApplyPublicProjection()
	return &domaininteraction.CommentMutationResult{Comment: existing, CommentCount: count}, nil
}

func restoreThreadedComments(rows []threadedCommentRow) []*domaininteraction.Comment {
	items := make([]*domaininteraction.Comment, 0, len(rows))
	for _, row := range rows {
		items = append(items, restoreThreadedComment(row))
	}
	return items
}

func restoreThreadedComment(row threadedCommentRow) *domaininteraction.Comment {
	return domaininteraction.RestoreThreadedComment(
		row.ID, row.VideoID, row.UserID, row.UserNickname, row.UserAvatarURL,
		nullableInt64Value(row.RootCommentID), nullableInt64Value(row.ReplyToCommentID),
		nullableInt64Value(row.ReplyToUserID), row.ReplyToUserNickname, row.ReplyToUserAvatarURL,
		row.Content, row.Status, row.ReplyCount, row.LikeCount, row.HotScore,
		row.RequestFingerprint, idempotencyKeyValue(row.IdempotencyKey), false, false,
		row.CreatedAt, row.UpdatedAt,
	)
}

func restoreThreadedCommentModel(model CommentModel) *domaininteraction.Comment {
	return domaininteraction.RestoreThreadedComment(
		model.ID, model.VideoID, model.UserID, "", "",
		nullableInt64Value(model.RootCommentID), nullableInt64Value(model.ReplyToCommentID),
		0, "", "", model.Content, model.Status, model.ReplyCount, model.LikeCount,
		model.HotScore, model.RequestFingerprint, idempotencyKeyValue(model.IdempotencyKey),
		false, false, model.CreatedAt, model.UpdatedAt,
	)
}

func nullableInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

var _ domaininteraction.ThreadedCommentRepository = (*Repository)(nil)
