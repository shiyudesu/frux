package applicationsearch

import (
	domainmedia "GCFeed/internal/domain/media"
	domainsearch "GCFeed/internal/domain/search"
	"context"
	"fmt"
	"time"
)

const DefaultLimit = 20

type Service struct {
	videos domainsearch.VideoSearchIndex
	users  domainsearch.UserSearchIndex
}

type Request struct {
	Query  string
	Cursor string
	Limit  int
}

type VideoResult struct {
	ID              int64
	AuthorID        int64
	Title           string
	Description     string
	MediaURL        string
	CoverURL        string
	Status          int
	Visibility      string
	LikeCount       int
	CommentCount    int
	FavoriteCount   int
	PublishedAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	MediaStatus     string
	PlaybackSources []domainmedia.PlaybackSource
}

type UserResult struct {
	ID        int64
	Account   string
	Nickname  string
	AvatarURL string
	Bio       string
}

type VideoPage struct {
	Items      []VideoResult
	NextCursor string
	HasMore    bool
}

type UserPage struct {
	Items      []UserResult
	NextCursor string
	HasMore    bool
}

func New(videos domainsearch.VideoSearchIndex, users domainsearch.UserSearchIndex) *Service {
	return &Service{videos: videos, users: users}
}

func (s *Service) SearchVideos(ctx context.Context, request Request) (*VideoPage, error) {
	query, limit, err := normalizeRequest(request)
	if err != nil {
		return nil, err
	}
	cursor, err := DecodeVideoCursor(request.Cursor, query)
	if err != nil {
		return nil, err
	}
	items, err := s.videos.SearchVideos(ctx, query, cursor, limit+1)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	results := make([]VideoResult, 0, len(items))
	for _, item := range items {
		if item == nil || item.ID <= 0 || item.PublishedAt.IsZero() || !domainsearch.ValidVideoRelevance(item.Relevance) {
			return nil, ErrSearchFailed
		}
		results = append(results, VideoResult{
			ID: item.ID, AuthorID: item.AuthorID, Title: item.Title, Description: item.Description,
			MediaURL: item.MediaURL, CoverURL: item.CoverURL, Status: item.Status,
			Visibility: item.Visibility, LikeCount: item.LikeCount, CommentCount: item.CommentCount,
			FavoriteCount: item.FavoriteCount, PublishedAt: item.PublishedAt,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, MediaStatus: item.MediaStatus,
			PlaybackSources: item.PlaybackSources,
		})
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = EncodeVideoCursor(query, &domainsearch.VideoCursor{
			Relevance: last.Relevance, PublishedAt: last.PublishedAt, VideoID: last.ID,
		})
	}
	return &VideoPage{Items: results, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *Service) SearchUsers(ctx context.Context, request Request) (*UserPage, error) {
	query, limit, err := normalizeRequest(request)
	if err != nil {
		return nil, err
	}
	cursor, err := DecodeUserCursor(request.Cursor, query)
	if err != nil {
		return nil, err
	}
	items, err := s.users.SearchUsers(ctx, query, cursor, limit+1)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	results := make([]UserResult, 0, len(items))
	for _, item := range items {
		if item == nil || item.ID <= 0 || item.UpdatedAt.IsZero() || !domainsearch.ValidUserRelevance(item.Relevance) {
			return nil, ErrSearchFailed
		}
		results = append(results, UserResult{
			ID: item.ID, Account: item.Account, Nickname: item.Nickname,
			AvatarURL: item.AvatarURL, Bio: item.Bio,
		})
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = EncodeUserCursor(query, &domainsearch.UserCursor{
			Relevance: last.Relevance, UpdatedAt: last.UpdatedAt, UserID: last.ID,
		})
	}
	return &UserPage{Items: results, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func normalizeRequest(request Request) (string, int, error) {
	query, err := domainsearch.NormalizeQuery(request.Query)
	if err != nil {
		return "", 0, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > domainsearch.MaxLimit {
		return "", 0, domainsearch.ErrInvalidLimit
	}
	return query, limit, nil
}
