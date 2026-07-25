package interfaceshttprouter

import (
	domainaccount "GCFeed/internal/domain/account"
	domainexposure "GCFeed/internal/domain/exposure"
	domaininteraction "GCFeed/internal/domain/interaction"
	domainlibrary "GCFeed/internal/domain/library"
	domainvideo "GCFeed/internal/domain/video"
	"context"
)

type actionIndexAdapter struct {
	source domaininteraction.ActionIndex
}

func (a actionIndexAdapter) ListActionVideos(ctx context.Context, userID int64, actionType string, cursor *domainlibrary.Cursor, limit int) ([]domainlibrary.VideoCandidate, error) {
	var sourceCursor *domaininteraction.ActionCursor
	if cursor != nil {
		sourceCursor = &domaininteraction.ActionCursor{UpdatedAt: cursor.UpdatedAt, VideoID: cursor.VideoID}
	}
	items, err := a.source.ListActiveActionVideoIDs(ctx, userID, actionType, sourceCursor, limit)
	if err != nil {
		return nil, err
	}
	result := make([]domainlibrary.VideoCandidate, 0, len(items))
	for _, item := range items {
		result = append(result, domainlibrary.VideoCandidate{VideoID: item.VideoID, UpdatedAt: item.UpdatedAt})
	}
	return result, nil
}

type historyIndexAdapter struct {
	source domainexposure.HistoryRepository
}

func (a historyIndexAdapter) ListHistoryVideos(ctx context.Context, userID int64, cursor *domainlibrary.Cursor, limit int) ([]domainlibrary.HistoryCandidate, error) {
	var sourceCursor *domainexposure.HistoryCursor
	if cursor != nil {
		sourceCursor = &domainexposure.HistoryCursor{LastWatchedAt: cursor.UpdatedAt, VideoID: cursor.VideoID}
	}
	items, err := a.source.ListHistory(ctx, userID, sourceCursor, limit)
	if err != nil {
		return nil, err
	}
	result := make([]domainlibrary.HistoryCandidate, 0, len(items))
	for _, item := range items {
		result = append(result, domainlibrary.HistoryCandidate{
			VideoID: item.VideoID, UpdatedAt: item.LastWatchedAt, LastScene: item.LastScene,
			LastEventType: item.LastEventType, LastPositionMs: item.LastPositionMs,
			LastWatchMs: item.LastWatchMs, Completed: item.Completed,
		})
	}
	return result, nil
}

func (a historyIndexAdapter) DeleteHistory(ctx context.Context, userID, videoID int64) error {
	return a.source.DeleteHistory(ctx, userID, videoID)
}

func (a historyIndexAdapter) ClearHistory(ctx context.Context, userID int64) error {
	return a.source.ClearHistory(ctx, userID)
}

type videoCatalogAdapter struct {
	source domainvideo.ManagementRepository
}

func (a videoCatalogAdapter) BatchGetReadable(ctx context.Context, viewerID int64, videoIDs []int64, publicOnly bool) (map[int64]*domainlibrary.VideoCard, error) {
	videos, err := a.source.BatchGetReadable(ctx, viewerID, videoIDs, publicOnly)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]*domainlibrary.VideoCard, len(videos))
	for id, video := range videos {
		result[id] = &domainlibrary.VideoCard{
			ID: video.ID, AuthorID: video.AuthorID, Title: video.Title, Description: video.Description,
			MediaURL: video.MediaURL, CoverURL: video.CoverURL, Status: video.Status, Visibility: video.Visibility,
			LikeCount: video.LikeCount, CommentCount: video.CommentCount, FavoriteCount: video.FavoriteCount,
			PublishedAt: video.PublishedAt, CreatedAt: video.CreatedAt, UpdatedAt: video.UpdatedAt,
		}
	}
	return result, nil
}

type privacyReaderAdapter struct {
	source domainaccount.ProfileSettingRepository
}

func (a privacyReaderAdapter) LikedVideosPublic(ctx context.Context, userID int64) (bool, error) {
	setting, err := a.source.GetProfileSetting(ctx, userID)
	if err != nil {
		return false, err
	}
	return setting.LikedVisibility == domainaccount.ProfileVisibilityPublic, nil
}
