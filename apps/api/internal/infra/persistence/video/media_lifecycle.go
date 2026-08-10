package infravideo

import (
	"fmt"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	inframedia "github.com/shiyudesu/frux/internal/infra/persistence/media"

	"gorm.io/gorm"
)

const mediaLifecycleMaxAttempts = 10

func AppendMediaLifecycleTask(
	tx *gorm.DB,
	dedupeKey string,
	video VideoModel,
	action string,
	requiredStatus int,
	requiredVisibility string,
	at time.Time,
) error {
	mediaAssetID := int64(0)
	if video.MediaAssetID != nil {
		mediaAssetID = *video.MediaAssetID
	}
	coverAssetID := int64(0)
	if video.CoverAssetID != nil {
		coverAssetID = *video.CoverAssetID
	}
	task, err := domainmedia.NewVideoLifecycleTask(
		fmt.Sprintf("%s:%d", dedupeKey, video.ID),
		video.ID,
		mediaAssetID,
		coverAssetID,
		action,
		requiredStatus,
		requiredVisibility,
		mediaLifecycleMaxAttempts,
		at.UTC(),
	)
	if err != nil {
		return err
	}
	return inframedia.AppendVideoLifecycleTask(tx, task)
}

func EnsureMediaLifecycleTasks(tx *gorm.DB) error {
	if tx == nil {
		return domainmedia.ErrInvalidLifecycleTask
	}
	return tx.Exec(`
		INSERT INTO media_video_lifecycle_task (
			dedupe_key,
			video_id,
			media_asset_id,
			cover_asset_id,
			action,
			required_status,
			required_visibility,
			state,
			attempts,
			max_attempts,
			error_code,
			lease_owner,
			lease_until,
			next_attempt_at,
			completed_at,
			created_at,
			updated_at
		)
		SELECT
			'video-lifecycle-reconcile:' || video.id::text || ':' ||
				video.status::text || ':' || video.visibility || ':' ||
				((EXTRACT(EPOCH FROM video.updated_at) * 1000000)::bigint)::text,
			video.id,
			COALESCE(video.media_asset_id, 0),
			COALESCE(video.cover_asset_id, 0),
			CASE WHEN video.status = ? THEN ? ELSE ? END,
			CASE WHEN video.status IN (?, ?, ?) THEN video.status ELSE 0 END,
			CASE
				WHEN video.status NOT IN (?, ?, ?) AND video.visibility = ?
					THEN ?
				ELSE ''
			END,
			?,
			0,
			?,
			'',
			'',
			NULL,
			clock_timestamp(),
			NULL,
			clock_timestamp(),
			clock_timestamp()
		FROM video
		WHERE video.status IN (?, ?, ?)
		   OR video.visibility = ?
		ON CONFLICT (dedupe_key) DO NOTHING
	`,
		domainvideo.StatusDeleted,
		domainmedia.LifecycleActionDelete,
		domainmedia.LifecycleActionProtect,
		domainvideo.StatusDeleted,
		domainvideo.StatusOffline,
		domainvideo.StatusRejected,
		domainvideo.StatusDeleted,
		domainvideo.StatusOffline,
		domainvideo.StatusRejected,
		domainvideo.VisibilityPrivate,
		domainvideo.VisibilityPrivate,
		domainmedia.JobStatePending,
		mediaLifecycleMaxAttempts,
		domainvideo.StatusDeleted,
		domainvideo.StatusOffline,
		domainvideo.StatusRejected,
		domainvideo.VisibilityPrivate,
	).Error
}
