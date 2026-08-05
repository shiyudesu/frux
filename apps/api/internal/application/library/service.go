package applicationlibrary

import (
	domainlibrary "github.com/shiyudesu/frux/internal/domain/library"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
)

const (
	defaultLimit       = 20
	maxReplenishRounds = 3
)

type Service struct {
	actions       domainlibrary.ActionIndex
	history       domainlibrary.HistoryIndex
	watchLater    domainlibrary.WatchLaterRepository
	videos        domainlibrary.VideoCatalog
	privacy       domainlibrary.PrivacyReader
	authors       domainlibrary.AuthorDisplayReader
	viewerActions domainlibrary.ViewerActionReader
}

type Page struct {
	Items      []*domainlibrary.VideoItem
	NextCursor string
	HasMore    bool
}

func New(
	actions domainlibrary.ActionIndex,
	history domainlibrary.HistoryIndex,
	watchLater domainlibrary.WatchLaterRepository,
	videos domainlibrary.VideoCatalog,
	privacy domainlibrary.PrivacyReader,
	authors domainlibrary.AuthorDisplayReader,
	viewerActions domainlibrary.ViewerActionReader,
) *Service {
	return &Service{
		actions: actions, history: history, watchLater: watchLater, videos: videos,
		privacy: privacy, authors: authors, viewerActions: viewerActions,
	}
}

func (s *Service) ListLiked(ctx context.Context, userID int64, cursor string, limit int) (*Page, error) {
	return s.listActions(ctx, userID, userID, "LIKE", cursor, limit, false)
}

func (s *Service) ListFavorites(ctx context.Context, userID int64, cursor string, limit int) (*Page, error) {
	return s.listActions(ctx, userID, userID, "FAVORITE", cursor, limit, false)
}

func (s *Service) ListPublicLiked(ctx context.Context, targetUserID int64, cursor string, limit int) (*Page, error) {
	if targetUserID <= 0 {
		return nil, domainlibrary.ErrInvalidUserID
	}
	allowed, err := s.privacy.LikedVideosPublic(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, domainlibrary.ErrLikedVideosPrivate
	}
	return s.listActions(ctx, targetUserID, 0, "LIKE", cursor, limit, true)
}

func (s *Service) listActions(ctx context.Context, ownerID, viewerID int64, actionType, cursorValue string, limit int, publicOnly bool) (*Page, error) {
	if ownerID <= 0 {
		return nil, domainlibrary.ErrInvalidUserID
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(cursorValue)
	if err != nil {
		return nil, err
	}
	items := make([]*domainlibrary.VideoItem, 0, limit)
	hasMore := false
	for round := 0; round < maxReplenishRounds && len(items) < limit; round++ {
		requestLimit := (limit-len(items))*2 + 1
		if requestLimit > domainlibrary.MaxLimit {
			requestLimit = domainlibrary.MaxLimit
		}
		candidates, err := s.actions.ListActionVideos(ctx, ownerID, actionType, cursor, requestLimit)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			break
		}
		videoIDs := candidateIDs(candidates)
		cards, err := s.videos.BatchGetReadable(ctx, viewerID, videoIDs, publicOnly)
		if err != nil {
			return nil, err
		}
		filled := false
		for index, candidate := range candidates {
			cursor = &domainlibrary.Cursor{UpdatedAt: candidate.UpdatedAt, VideoID: candidate.VideoID}
			card := cards[candidate.VideoID]
			if card == nil {
				continue
			}
			items = append(items, &domainlibrary.VideoItem{Video: cloneVideoCard(card), UpdatedAt: candidate.UpdatedAt})
			if len(items) == limit {
				hasMore = index < len(candidates)-1 || len(candidates) == requestLimit
				filled = true
				break
			}
		}
		if filled {
			break
		}
		if len(candidates) < requestLimit {
			break
		}
		hasMore = true
	}
	next := ""
	if hasMore && cursor != nil {
		next = encodeCursor(cursor)
	}
	return s.hydratePage(ctx, viewerID, &Page{Items: items, NextCursor: next, HasMore: hasMore})
}

func (s *Service) ListHistory(ctx context.Context, userID int64, cursorValue string, limit int) (*Page, error) {
	if userID <= 0 {
		return nil, domainlibrary.ErrInvalidUserID
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(cursorValue)
	if err != nil {
		return nil, err
	}
	items := make([]*domainlibrary.VideoItem, 0, limit)
	hasMore := false
	for round := 0; round < maxReplenishRounds && len(items) < limit; round++ {
		requestLimit := replenishmentLimit(limit - len(items))
		candidates, err := s.history.ListHistoryVideos(ctx, userID, cursor, requestLimit)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			hasMore = false
			break
		}
		cards, err := s.videos.BatchGetReadable(ctx, userID, historyCandidateIDs(candidates), false)
		if err != nil {
			return nil, err
		}
		filled := false
		for index := range candidates {
			candidate := candidates[index]
			cursor = &domainlibrary.Cursor{UpdatedAt: candidate.UpdatedAt, VideoID: candidate.VideoID}
			if card := cards[candidate.VideoID]; card != nil {
				items = append(items, &domainlibrary.VideoItem{Video: cloneVideoCard(card), UpdatedAt: candidate.UpdatedAt, History: &candidate})
			}
			if len(items) == limit {
				hasMore = index < len(candidates)-1 || len(candidates) == requestLimit
				filled = true
				break
			}
		}
		if filled {
			break
		}
		if len(candidates) < requestLimit {
			hasMore = false
			break
		}
		hasMore = true
	}
	next := ""
	if hasMore && cursor != nil {
		next = encodeCursor(cursor)
	}
	return s.hydratePage(ctx, userID, &Page{Items: items, NextCursor: next, HasMore: hasMore})
}

func (s *Service) DeleteHistory(ctx context.Context, userID, videoID int64) error {
	if userID <= 0 {
		return domainlibrary.ErrInvalidUserID
	}
	if videoID <= 0 {
		return domainlibrary.ErrInvalidVideoID
	}
	return s.history.DeleteHistory(ctx, userID, videoID)
}

func (s *Service) ClearHistory(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return domainlibrary.ErrInvalidUserID
	}
	return s.history.ClearHistory(ctx, userID)
}

func (s *Service) SetWatchLater(ctx context.Context, userID, videoID int64, active bool) (*domainlibrary.WatchLater, error) {
	fact, err := domainlibrary.NewWatchLater(userID, videoID, active)
	if err != nil {
		return nil, err
	}
	if active {
		cards, err := s.videos.BatchGetReadable(ctx, userID, []int64{videoID}, false)
		if err != nil {
			return nil, err
		}
		if cards[videoID] == nil {
			return nil, domainlibrary.ErrVideoNotFound
		}
	}
	return s.watchLater.SetWatchLater(ctx, fact)
}

func (s *Service) ListWatchLater(ctx context.Context, userID int64, cursorValue string, limit int) (*Page, error) {
	if userID <= 0 {
		return nil, domainlibrary.ErrInvalidUserID
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return nil, err
	}
	cursor, err := decodeCursor(cursorValue)
	if err != nil {
		return nil, err
	}
	items := make([]*domainlibrary.VideoItem, 0, limit)
	hasMore := false
	for round := 0; round < maxReplenishRounds && len(items) < limit; round++ {
		requestLimit := replenishmentLimit(limit - len(items))
		candidates, err := s.watchLater.ListWatchLater(ctx, userID, cursor, requestLimit)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			hasMore = false
			break
		}
		cards, err := s.videos.BatchGetReadable(ctx, userID, candidateIDs(candidates), false)
		if err != nil {
			return nil, err
		}
		filled := false
		for index, candidate := range candidates {
			cursor = &domainlibrary.Cursor{UpdatedAt: candidate.UpdatedAt, VideoID: candidate.VideoID}
			if card := cards[candidate.VideoID]; card != nil {
				items = append(items, &domainlibrary.VideoItem{Video: cloneVideoCard(card), UpdatedAt: candidate.UpdatedAt})
			}
			if len(items) == limit {
				hasMore = index < len(candidates)-1 || len(candidates) == requestLimit
				filled = true
				break
			}
		}
		if filled {
			break
		}
		if len(candidates) < requestLimit {
			hasMore = false
			break
		}
		hasMore = true
	}
	next := ""
	if hasMore && cursor != nil {
		next = encodeCursor(cursor)
	}
	return s.hydratePage(ctx, userID, &Page{Items: items, NextCursor: next, HasMore: hasMore})
}

func (s *Service) hydratePage(ctx context.Context, viewerID int64, page *Page) (*Page, error) {
	if page == nil || len(page.Items) == 0 {
		return page, nil
	}
	authorIDs := make([]int64, 0, len(page.Items))
	videoIDs := make([]int64, 0, len(page.Items))
	seenAuthors := make(map[int64]struct{}, len(page.Items))
	seenVideos := make(map[int64]struct{}, len(page.Items))
	for _, item := range page.Items {
		if item == nil || item.Video == nil {
			continue
		}
		if item.Video.ID > 0 {
			if _, exists := seenVideos[item.Video.ID]; !exists {
				seenVideos[item.Video.ID] = struct{}{}
				videoIDs = append(videoIDs, item.Video.ID)
			}
		}
		if item.Video.AuthorID > 0 {
			if _, exists := seenAuthors[item.Video.AuthorID]; exists {
				continue
			}
			seenAuthors[item.Video.AuthorID] = struct{}{}
			authorIDs = append(authorIDs, item.Video.AuthorID)
		}
	}
	authors := map[int64]*domainlibrary.AuthorDisplay{}
	var err error
	if len(authorIDs) > 0 {
		authors, err = s.authors.BatchGetAuthorDisplays(ctx, authorIDs)
		if err != nil {
			return nil, err
		}
	}
	viewerActions := map[int64]*domainlibrary.ViewerActionState{}
	if viewerID > 0 && len(videoIDs) > 0 {
		viewerActions, err = s.viewerActions.BatchGetViewerActionStates(ctx, viewerID, videoIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, item := range page.Items {
		if item == nil || item.Video == nil {
			continue
		}
		if author := authors[item.Video.AuthorID]; author != nil {
			item.Video.AuthorNickname = author.Nickname
			item.Video.AuthorAvatarURL = author.AvatarURL
		}
		if state := viewerActions[item.Video.ID]; state != nil {
			item.Video.Liked = state.Liked
			item.Video.Favorited = state.Favorited
		}
	}
	return page, nil
}

func cloneVideoCard(card *domainlibrary.VideoCard) *domainlibrary.VideoCard {
	if card == nil {
		return nil
	}
	cloned := *card
	cloned.PlaybackSources = append([]domainmedia.PlaybackSource(nil), card.PlaybackSources...)
	cloned.AuthorNickname = ""
	cloned.AuthorAvatarURL = ""
	cloned.Liked = false
	cloned.Favorited = false
	return &cloned
}

func replenishmentLimit(remaining int) int {
	limit := remaining*2 + 1
	if limit > domainlibrary.MaxLimit {
		return domainlibrary.MaxLimit
	}
	return limit
}

func normalizeLimit(value int) (int, error) {
	if value == 0 {
		return defaultLimit, nil
	}
	if value < 1 || value > domainlibrary.MaxLimit {
		return 0, domainlibrary.ErrInvalidLimit
	}
	return value, nil
}

func candidateIDs(candidates []domainlibrary.VideoCandidate) []int64 {
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.VideoID)
	}
	return ids
}

func historyCandidateIDs(candidates []domainlibrary.HistoryCandidate) []int64 {
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.VideoID)
	}
	return ids
}

func encodeCursor(cursor *domainlibrary.Cursor) string {
	content, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(content)
}

func decodeCursor(value string) (*domainlibrary.Cursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	content, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, domainlibrary.ErrInvalidCursor
	}
	var cursor domainlibrary.Cursor
	if err := json.Unmarshal(content, &cursor); err != nil || cursor.UpdatedAt.IsZero() || cursor.VideoID <= 0 {
		return nil, domainlibrary.ErrInvalidCursor
	}
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}
