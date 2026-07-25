package infravideo

import (
	domainvideo "GCFeed/internal/domain/video"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func AdjustContentStat(tx *gorm.DB, userID int64, publicWorkDelta, privateWorkDelta, receivedLikeDelta, collectionDelta int) error {
	if userID <= 0 {
		return nil
	}
	base := UserContentStatModel{UserID: userID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&base).Error; err != nil {
		return err
	}
	return tx.Model(&UserContentStatModel{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"public_work_count":   gorm.Expr("GREATEST(public_work_count + ?, 0)", publicWorkDelta),
			"private_work_count":  gorm.Expr("GREATEST(private_work_count + ?, 0)", privateWorkDelta),
			"received_like_count": gorm.Expr("GREATEST(received_like_count + ?, 0)", receivedLikeDelta),
			"collection_count":    gorm.Expr("GREATEST(collection_count + ?, 0)", collectionDelta),
		}).Error
}

func ReconcileContentStats(db *gorm.DB) error {
	if err := db.Exec(`
		INSERT INTO user_content_stat (
			user_id, public_work_count, private_work_count,
			received_like_count, collection_count, created_at, updated_at
		)
		SELECT account.id, 0, 0, 0, 0, NOW(), NOW()
		FROM account
		LEFT JOIN user_content_stat ON user_content_stat.user_id = account.id
		WHERE user_content_stat.user_id IS NULL
		ON CONFLICT (user_id) DO NOTHING
	`).Error; err != nil {
		return err
	}
	return db.Exec(`
		WITH desired_video AS (
			SELECT
				video.author_id AS user_id,
				COUNT(*) FILTER (
					WHERE video.status = 2 AND video.visibility = 'public'
				) AS public_work_count,
				COUNT(*) FILTER (
					WHERE video.status <> 4 AND video.visibility = 'private'
				) AS private_work_count,
				COALESCE(SUM(
					CASE WHEN video.status <> 4 THEN video_stat.like_count ELSE 0 END
				), 0) AS received_like_count
			FROM video
			LEFT JOIN video_stat ON video_stat.video_id = video.id
			GROUP BY video.author_id
		),
		desired_collection AS (
			SELECT owner_id AS user_id, COUNT(*) AS collection_count
			FROM video_collection
			WHERE status = 1
			GROUP BY owner_id
		),
		snapshot AS MATERIALIZED (
			SELECT
				current.user_id,
				current.public_work_count AS baseline_public_work_count,
				current.private_work_count AS baseline_private_work_count,
				current.received_like_count AS baseline_received_like_count,
				current.collection_count AS baseline_collection_count,
				COALESCE(video.public_work_count, 0) AS desired_public_work_count,
				COALESCE(video.private_work_count, 0) AS desired_private_work_count,
				COALESCE(video.received_like_count, 0) AS desired_received_like_count,
				COALESCE(collection.collection_count, 0) AS desired_collection_count
			FROM user_content_stat AS current
			LEFT JOIN desired_video AS video ON video.user_id = current.user_id
			LEFT JOIN desired_collection AS collection ON collection.user_id = current.user_id
		)
		UPDATE user_content_stat AS current
		SET
			public_work_count = GREATEST(
				current.public_work_count
				+ snapshot.desired_public_work_count
				- snapshot.baseline_public_work_count,
				0
			),
			private_work_count = GREATEST(
				current.private_work_count
				+ snapshot.desired_private_work_count
				- snapshot.baseline_private_work_count,
				0
			),
			received_like_count = GREATEST(
				current.received_like_count
				+ snapshot.desired_received_like_count
				- snapshot.baseline_received_like_count,
				0
			),
			collection_count = GREATEST(
				current.collection_count
				+ snapshot.desired_collection_count
				- snapshot.baseline_collection_count,
				0
			),
			updated_at = NOW()
		FROM snapshot
		WHERE current.user_id = snapshot.user_id
			AND (
				snapshot.desired_public_work_count <> snapshot.baseline_public_work_count
				OR snapshot.desired_private_work_count <> snapshot.baseline_private_work_count
				OR snapshot.desired_received_like_count <> snapshot.baseline_received_like_count
				OR snapshot.desired_collection_count <> snapshot.baseline_collection_count
			)
	`).Error
}

func contentWorkCounts(status int, visibility string) (publicWork, privateWork int) {
	if status == domainvideo.StatusPublished && visibility == domainvideo.VisibilityPublic {
		publicWork = 1
	}
	if status != domainvideo.StatusDeleted && visibility == domainvideo.VisibilityPrivate {
		privateWork = 1
	}
	return publicWork, privateWork
}

func contentWorkDeltas(oldStatus int, oldVisibility string, newStatus int, newVisibility string) (publicDelta, privateDelta int) {
	oldPublic, oldPrivate := contentWorkCounts(oldStatus, oldVisibility)
	newPublic, newPrivate := contentWorkCounts(newStatus, newVisibility)
	return newPublic - oldPublic, newPrivate - oldPrivate
}
