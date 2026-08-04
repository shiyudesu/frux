package infravideo

import (
	domainmedia "GCFeed/internal/domain/media"
	domainsearch "GCFeed/internal/domain/search"
	domainvideo "GCFeed/internal/domain/video"
	"context"
	"time"

	"gorm.io/gorm"
)

const videoSearchRelevanceSQL = `
	CASE
		WHEN LOWER(v.title) = LOWER(?) THEN ?
		WHEN v.title ILIKE ? ESCAPE '\' THEN ?
		WHEN v.title ILIKE ? ESCAPE '\' THEN ?
		WHEN v.description ILIKE ? ESCAPE '\' THEN ?
		ELSE ?
	END
`

type videoSearchModel struct {
	ID             int64
	AuthorID       int64
	Title          string
	Description    string
	MediaURL       string
	CoverURL       string
	MediaAssetID   *int64
	CoverAssetID   *int64
	MediaStatus    string
	MediaErrorCode string
	Status         int
	Visibility     string
	LikeCount      int
	CommentCount   int
	FavoriteCount  int
	PublishedAt    time.Time
	IdempotencyKey *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Relevance      int
}

func (r *Repository) SearchVideos(ctx context.Context, query string, cursor *domainsearch.VideoCursor, limit int) ([]*domainsearch.VideoIndexItem, error) {
	var models []videoSearchModel
	if err := buildVideoSearchQuery(r.db.WithContext(ctx), query, cursor, limit).Scan(&models).Error; err != nil {
		return nil, err
	}

	videos := make([]*domainvideo.Video, 0, len(models))
	for _, model := range models {
		videos = append(videos, restoreVideo(model.videoWithStatModel()))
	}
	if err := r.hydrateMediaDelivery(ctx, videos); err != nil {
		return nil, err
	}

	items := make([]*domainsearch.VideoIndexItem, 0, len(models))
	for index, model := range models {
		video := videos[index]
		items = append(items, &domainsearch.VideoIndexItem{
			ID: video.ID, AuthorID: video.AuthorID, Title: video.Title, Description: video.Description,
			MediaURL: video.MediaURL, CoverURL: video.CoverURL, Status: video.Status,
			Visibility: video.Visibility, LikeCount: video.LikeCount, CommentCount: video.CommentCount,
			FavoriteCount: video.FavoriteCount, PublishedAt: model.PublishedAt,
			CreatedAt: video.CreatedAt, UpdatedAt: video.UpdatedAt, MediaStatus: video.MediaStatus,
			PlaybackSources: video.PlaybackSources, Relevance: model.Relevance,
		})
	}
	return items, nil
}

func buildVideoSearchQuery(db *gorm.DB, query string, cursor *domainsearch.VideoCursor, limit int) *gorm.DB {
	escaped := domainsearch.EscapeLikeLiteral(query)
	prefixPattern := escaped + "%"
	containsPattern := "%" + escaped + "%"
	base := db.
		Table("video AS v").
		Select(
			videoWithStatSelect()+`,
				`+videoSearchRelevanceSQL+` AS relevance`,
			query, domainsearch.VideoRelevanceExactTitle,
			prefixPattern, domainsearch.VideoRelevanceTitlePrefix,
			containsPattern, domainsearch.VideoRelevanceTitleContains,
			containsPattern, domainsearch.VideoRelevanceDescriptionOnly,
			domainsearch.VideoRelevanceDescriptionOnly,
		).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where(
			"v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL",
			domainvideo.StatusPublished,
			domainvideo.VisibilityPublic,
			[]string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady},
		).
		Where(
			"(v.title ILIKE ? ESCAPE '\\' OR v.description ILIKE ? ESCAPE '\\')",
			containsPattern,
			containsPattern,
		)

	ranked := db.Table("(?) AS ranked_videos", base)
	if cursor != nil {
		ranked = ranked.Where(
			`(
				relevance > ?
				OR (relevance = ? AND published_at < ?)
				OR (relevance = ? AND published_at = ? AND id < ?)
			)`,
			cursor.Relevance,
			cursor.Relevance, cursor.PublishedAt,
			cursor.Relevance, cursor.PublishedAt, cursor.VideoID,
		)
	}
	return ranked.
		Order("relevance ASC").
		Order("published_at DESC").
		Order("id DESC").
		Limit(limit)
}

func (model videoSearchModel) videoWithStatModel() videoWithStatModel {
	publishedAt := model.PublishedAt
	return videoWithStatModel{
		ID: model.ID, AuthorID: model.AuthorID, Title: model.Title, Description: model.Description,
		MediaURL: model.MediaURL, CoverURL: model.CoverURL, MediaAssetID: model.MediaAssetID,
		CoverAssetID: model.CoverAssetID, MediaStatus: model.MediaStatus, MediaErrorCode: model.MediaErrorCode,
		Status: model.Status, Visibility: model.Visibility, LikeCount: model.LikeCount,
		CommentCount: model.CommentCount, FavoriteCount: model.FavoriteCount,
		PublishedAt: &publishedAt, IdempotencyKey: model.IdempotencyKey,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}
