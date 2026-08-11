package interfaceshttprouter

import (
	"context"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
	domainlibrary "github.com/shiyudesu/frux/internal/domain/library"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
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
			MediaStatus: video.MediaStatus, PlaybackSources: video.PlaybackSources,
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

type authorDisplayReaderAdapter struct {
	source domainaccount.AuthorDisplayReader
}

func (a authorDisplayReaderAdapter) BatchGetAuthorDisplays(ctx context.Context, authorIDs []int64) (map[int64]*domainlibrary.AuthorDisplay, error) {
	displays, err := a.source.BatchGetAuthorDisplays(ctx, authorIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]*domainlibrary.AuthorDisplay, len(displays))
	for authorID, display := range displays {
		if display == nil {
			continue
		}
		result[authorID] = &domainlibrary.AuthorDisplay{
			AuthorID: display.UserID, Nickname: display.Nickname, AvatarURL: display.AvatarURL,
		}
	}
	return result, nil
}

type viewerActionReaderAdapter struct {
	source domaininteraction.ViewerActionStateReader
}

func (a viewerActionReaderAdapter) BatchGetViewerActionStates(ctx context.Context, viewerID int64, videoIDs []int64) (map[int64]*domainlibrary.ViewerActionState, error) {
	states, err := a.source.BatchGetViewerActionStates(ctx, viewerID, videoIDs)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]*domainlibrary.ViewerActionState, len(states))
	for videoID, state := range states {
		if state == nil {
			continue
		}
		result[videoID] = &domainlibrary.ViewerActionState{
			VideoID: state.VideoID, Liked: state.Liked, Favorited: state.Favorited,
		}
	}
	return result, nil
}
