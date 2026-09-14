package infrainteraction

import (
	"context"
	"errors"
	"strings"

	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	"gorm.io/gorm"
)

func (r *Repository) FindActionRequest(ctx context.Context, userID, videoID int64, actionType string, active bool, key string) (*domaininteraction.ActionRequestReceipt, error) {
	if strings.TrimSpace(key) == "" {
		return nil, nil
	}
	// Replays must not bypass the current public-video boundary.
	if _, err := r.GetVideoStat(ctx, videoID); err != nil {
		return nil, err
	}
	return findActionRequest(r.db.WithContext(ctx), userID, videoID, actionType, active, key)
}

func findActionRequest(tx *gorm.DB, userID, videoID int64, actionType string, active bool, key string) (*domaininteraction.ActionRequestReceipt, error) {
	var stored ActionIdempotencyReceiptModel
	err := tx.Where("user_id = ? AND video_id = ? AND action_type = ? AND idempotency_key = ?", userID, videoID, actionType, strings.TrimSpace(key)).Take(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if stored.Active != active {
		return nil, domaininteraction.ErrActionIdempotencyConflict
	}
	return &domaininteraction.ActionRequestReceipt{VideoID: videoID, ActionType: actionType, Active: active, Count: stored.ActionCount, Replayed: true}, nil
}

func (r *Repository) PersistActionRequest(ctx context.Context, event *domaininteraction.AcceptedActionEvent, requestKey string) (*domaininteraction.ActionRequestReceipt, error) {
	if event == nil {
		return nil, domaininteraction.ErrInvalidActionEvent
	}
	requestKey = strings.TrimSpace(requestKey)
	var reply *domaininteraction.ActionRequestReceipt
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Match the lock order of normal action writers and event consumers.
		if _, err := lockAcceptedActionVideo(tx, event.VideoID); err != nil {
			return err
		}
		if requestKey != "" {
			previous, err := findActionRequest(tx, event.UserID, event.VideoID, event.ActionType, event.Active, requestKey)
			if err != nil {
				return err
			}
			if previous != nil {
				reply = previous
				return nil
			}
		}
		if err := New(tx).PersistAcceptedActionEvent(ctx, event); err != nil {
			return err
		}
		count, err := currentActionCount(tx, event.VideoID, event.ActionType)
		if err != nil {
			return err
		}
		reply = &domaininteraction.ActionRequestReceipt{VideoID: event.VideoID, ActionType: event.ActionType, Active: event.Active, Count: count}
		if requestKey == "" {
			return nil
		}
		var action ActionModel
		if err := tx.Where("user_id = ? AND video_id = ? AND action_type = ?", event.UserID, event.VideoID, event.ActionType).Take(&action).Error; err != nil {
			return err
		}
		return tx.Create(&ActionIdempotencyReceiptModel{UserID: event.UserID, VideoID: event.VideoID, ActionType: event.ActionType, IdempotencyKey: requestKey, Active: event.Active, ActionID: action.ID, ActionCount: count}).Error
	})
	return reply, err
}
