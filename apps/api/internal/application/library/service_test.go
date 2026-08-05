package applicationlibrary

import (
	domainlibrary "github.com/shiyudesu/frux/internal/domain/library"
	"context"
	"errors"
	"sort"
	"testing"
	"time"
)

type actionIndexStub struct {
	items []domainlibrary.VideoCandidate
}

func (s actionIndexStub) ListActionVideos(_ context.Context, _ int64, _ string, cursor *domainlibrary.Cursor, limit int) ([]domainlibrary.VideoCandidate, error) {
	start := 0
	if cursor != nil {
		for index, item := range s.items {
			if item.UpdatedAt.Equal(cursor.UpdatedAt) && item.VideoID == cursor.VideoID {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(s.items) {
		end = len(s.items)
	}
	return append([]domainlibrary.VideoCandidate(nil), s.items[start:end]...), nil
}

type catalogStub struct {
	cards map[int64]*domainlibrary.VideoCard
}

func (s catalogStub) BatchGetReadable(_ context.Context, _ int64, ids []int64, publicOnly bool) (map[int64]*domainlibrary.VideoCard, error) {
	result := map[int64]*domainlibrary.VideoCard{}
	for _, id := range ids {
		card := s.cards[id]
		if card == nil {
			continue
		}
		if publicOnly && card.Visibility != "public" {
			continue
		}
		result[id] = card
	}
	return result, nil
}

type privacyStub bool

func (s privacyStub) LikedVideosPublic(context.Context, int64) (bool, error) {
	return bool(s), nil
}

type authorReaderStub struct {
	displays  map[int64]*domainlibrary.AuthorDisplay
	calls     int
	requested [][]int64
}

func (s *authorReaderStub) BatchGetAuthorDisplays(_ context.Context, authorIDs []int64) (map[int64]*domainlibrary.AuthorDisplay, error) {
	s.calls++
	s.requested = append(s.requested, append([]int64(nil), authorIDs...))
	return s.displays, nil
}

type viewerActionReaderStub struct {
	states    map[int64]*domainlibrary.ViewerActionState
	calls     int
	viewerIDs []int64
	requested [][]int64
}

func (s *viewerActionReaderStub) BatchGetViewerActionStates(_ context.Context, viewerID int64, videoIDs []int64) (map[int64]*domainlibrary.ViewerActionState, error) {
	s.calls++
	s.viewerIDs = append(s.viewerIDs, viewerID)
	s.requested = append(s.requested, append([]int64(nil), videoIDs...))
	return s.states, nil
}

func newTestService(
	actions domainlibrary.ActionIndex,
	history domainlibrary.HistoryIndex,
	watchLater domainlibrary.WatchLaterRepository,
	videos domainlibrary.VideoCatalog,
	privacy domainlibrary.PrivacyReader,
) *Service {
	return New(actions, history, watchLater, videos, privacy, &authorReaderStub{}, &viewerActionReaderStub{})
}

type watchLaterStub struct {
	facts     map[int64]*domainlibrary.WatchLater
	listCalls int
}

func (s *watchLaterStub) SetWatchLater(_ context.Context, fact *domainlibrary.WatchLater) (*domainlibrary.WatchLater, error) {
	now := time.Now().UTC()
	existing := s.facts[fact.VideoID]
	if existing != nil && existing.Status == fact.Status {
		cloned := *existing
		return &cloned, nil
	}
	fact.CreatedAt = now
	fact.UpdatedAt = now
	cloned := *fact
	s.facts[fact.VideoID] = &cloned
	return fact, nil
}

func (s *watchLaterStub) ListWatchLater(_ context.Context, _ int64, cursor *domainlibrary.Cursor, limit int) ([]domainlibrary.VideoCandidate, error) {
	s.listCalls++
	items := make([]domainlibrary.VideoCandidate, 0)
	for _, fact := range s.facts {
		if fact.Active() {
			items = append(items, domainlibrary.VideoCandidate{VideoID: fact.VideoID, UpdatedAt: fact.UpdatedAt})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].VideoID > items[j].VideoID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	start := 0
	if cursor != nil {
		for index, item := range items {
			if item.UpdatedAt.Equal(cursor.UpdatedAt) && item.VideoID == cursor.VideoID {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return append([]domainlibrary.VideoCandidate(nil), items[start:end]...), nil
}

type historyStub struct {
	items     []domainlibrary.HistoryCandidate
	listCalls int
}

func (s *historyStub) ListHistoryVideos(_ context.Context, _ int64, cursor *domainlibrary.Cursor, limit int) ([]domainlibrary.HistoryCandidate, error) {
	s.listCalls++
	start := 0
	if cursor != nil {
		for index, item := range s.items {
			if item.UpdatedAt.Equal(cursor.UpdatedAt) && item.VideoID == cursor.VideoID {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(s.items) {
		end = len(s.items)
	}
	return append([]domainlibrary.HistoryCandidate(nil), s.items[start:end]...), nil
}
func (s *historyStub) DeleteHistory(_ context.Context, _ int64, videoID int64) error {
	filtered := s.items[:0]
	for _, item := range s.items {
		if item.VideoID != videoID {
			filtered = append(filtered, item)
		}
	}
	s.items = filtered
	return nil
}
func (s *historyStub) ClearHistory(context.Context, int64) error {
	s.items = nil
	return nil
}

func TestLikedLibraryReplenishesAndPaginates(t *testing.T) {
	now := time.Now().UTC()
	candidates := make([]domainlibrary.VideoCandidate, 0, 6)
	for id := int64(6); id >= 1; id-- {
		candidates = append(candidates, domainlibrary.VideoCandidate{VideoID: id, UpdatedAt: now.Add(time.Duration(id) * time.Second)})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt) })
	cards := map[int64]*domainlibrary.VideoCard{
		3: {ID: 3, Visibility: "public"},
		2: {ID: 2, Visibility: "public"},
		1: {ID: 1, Visibility: "public"},
	}
	service := newTestService(actionIndexStub{items: candidates}, &historyStub{}, &watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}}, catalogStub{cards: cards}, privacyStub(true))
	page, err := service.ListLiked(context.Background(), 7, "", 2)
	if err != nil {
		t.Fatalf("list liked: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Video.ID != 3 || page.Items[1].Video.ID != 2 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("unexpected replenished page: %+v", page)
	}
	next, err := service.ListLiked(context.Background(), 7, page.NextCursor, 2)
	if err != nil {
		t.Fatalf("list next: %v", err)
	}
	if len(next.Items) != 1 || next.Items[0].Video.ID != 1 {
		t.Fatalf("unexpected next page: %+v", next)
	}
}

func TestHistoryLibraryReplenishesUnreadableCandidates(t *testing.T) {
	now := time.Now().UTC()
	history := &historyStub{}
	for id := int64(14); id >= 1; id-- {
		history.items = append(history.items, domainlibrary.HistoryCandidate{
			VideoID: id, UpdatedAt: now.Add(time.Duration(id) * time.Second), LastWatchMs: int(id) * 100,
		})
	}
	service := newTestService(
		actionIndexStub{},
		history,
		&watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}},
		catalogStub{cards: map[int64]*domainlibrary.VideoCard{
			2: {ID: 2, Visibility: "public"},
			1: {ID: 1, Visibility: "public"},
		}},
		privacyStub(true),
	)

	page, err := service.ListHistory(context.Background(), 7, "", 2)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Video.ID != 2 || page.Items[1].Video.ID != 1 {
		t.Fatalf("older readable history was stranded: %+v", page)
	}
	if page.Items[0].History == nil || page.Items[0].History.LastWatchMs != 200 || page.HasMore || page.NextCursor != "" {
		t.Fatalf("unexpected replenished history metadata: %+v", page)
	}
	if history.listCalls != maxReplenishRounds {
		t.Fatalf("history replenishment calls = %d, want %d", history.listCalls, maxReplenishRounds)
	}
}

func TestWatchLaterLibraryReplenishesUnreadableCandidates(t *testing.T) {
	now := time.Now().UTC()
	watchLater := &watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}}
	for id := int64(14); id >= 1; id-- {
		watchLater.facts[id] = &domainlibrary.WatchLater{
			UserID: 7, VideoID: id, Status: domainlibrary.WatchLaterStatusActive,
			CreatedAt: now, UpdatedAt: now.Add(time.Duration(id) * time.Second),
		}
	}
	service := newTestService(
		actionIndexStub{},
		&historyStub{},
		watchLater,
		catalogStub{cards: map[int64]*domainlibrary.VideoCard{
			2: {ID: 2, Visibility: "public"},
			1: {ID: 1, Visibility: "public"},
		}},
		privacyStub(true),
	)

	page, err := service.ListWatchLater(context.Background(), 7, "", 2)
	if err != nil {
		t.Fatalf("list watch later: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Video.ID != 2 || page.Items[1].Video.ID != 1 {
		t.Fatalf("older readable watch-later items were stranded: %+v", page)
	}
	if page.HasMore || page.NextCursor != "" {
		t.Fatalf("unexpected replenished watch-later pagination: %+v", page)
	}
	if watchLater.listCalls != maxReplenishRounds {
		t.Fatalf("watch-later replenishment calls = %d, want %d", watchLater.listCalls, maxReplenishRounds)
	}
}

func TestHistoryLibraryLimitsReplenishmentRounds(t *testing.T) {
	now := time.Now().UTC()
	history := &historyStub{}
	for id := int64(16); id >= 1; id-- {
		history.items = append(history.items, domainlibrary.HistoryCandidate{
			VideoID: id, UpdatedAt: now.Add(time.Duration(id) * time.Second), LastWatchMs: int(id) * 100,
		})
	}
	service := newTestService(
		actionIndexStub{},
		history,
		&watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}},
		catalogStub{cards: map[int64]*domainlibrary.VideoCard{1: {ID: 1, Visibility: "public"}}},
		privacyStub(true),
	)

	page, err := service.ListHistory(context.Background(), 7, "", 2)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(page.Items) != 0 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("unexpected bounded history page: %+v", page)
	}
	if history.listCalls != maxReplenishRounds {
		t.Fatalf("history replenishment calls = %d, want %d", history.listCalls, maxReplenishRounds)
	}

	next, err := service.ListHistory(context.Background(), 7, page.NextCursor, 2)
	if err != nil {
		t.Fatalf("list next history page: %v", err)
	}
	if len(next.Items) != 1 || next.Items[0].Video.ID != 1 || next.HasMore || next.NextCursor != "" {
		t.Fatalf("older readable history was stranded after round limit: %+v", next)
	}
}

func TestWatchLaterLibraryLimitsReplenishmentRounds(t *testing.T) {
	now := time.Now().UTC()
	watchLater := &watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}}
	for id := int64(16); id >= 1; id-- {
		watchLater.facts[id] = &domainlibrary.WatchLater{
			UserID: 7, VideoID: id, Status: domainlibrary.WatchLaterStatusActive,
			CreatedAt: now, UpdatedAt: now.Add(time.Duration(id) * time.Second),
		}
	}
	service := newTestService(
		actionIndexStub{},
		&historyStub{},
		watchLater,
		catalogStub{cards: map[int64]*domainlibrary.VideoCard{1: {ID: 1, Visibility: "public"}}},
		privacyStub(true),
	)

	page, err := service.ListWatchLater(context.Background(), 7, "", 2)
	if err != nil {
		t.Fatalf("list watch later: %v", err)
	}
	if len(page.Items) != 0 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("unexpected bounded watch-later page: %+v", page)
	}
	if watchLater.listCalls != maxReplenishRounds {
		t.Fatalf("watch-later replenishment calls = %d, want %d", watchLater.listCalls, maxReplenishRounds)
	}

	next, err := service.ListWatchLater(context.Background(), 7, page.NextCursor, 2)
	if err != nil {
		t.Fatalf("list next watch-later page: %v", err)
	}
	if len(next.Items) != 1 || next.Items[0].Video.ID != 1 || next.HasMore || next.NextCursor != "" {
		t.Fatalf("older readable watch-later item was stranded after round limit: %+v", next)
	}
}

func TestPublicLikesPrivacyAndWatchLaterIdempotency(t *testing.T) {
	watchLater := &watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}}
	catalog := catalogStub{cards: map[int64]*domainlibrary.VideoCard{10: {ID: 10, Visibility: "public"}}}
	authors := &authorReaderStub{}
	viewerActions := &viewerActionReaderStub{}
	privateService := New(actionIndexStub{}, &historyStub{}, watchLater, catalog, privacyStub(false), authors, viewerActions)
	if _, err := privateService.ListPublicLiked(context.Background(), 7, "", 20); !errors.Is(err, domainlibrary.ErrLikedVideosPrivate) {
		t.Fatalf("expected privacy error, got %v", err)
	}
	if authors.calls != 0 || viewerActions.calls != 0 {
		t.Fatalf("private likes triggered hydration reads: authors=%d viewer_actions=%d", authors.calls, viewerActions.calls)
	}

	service := newTestService(actionIndexStub{}, &historyStub{}, watchLater, catalog, privacyStub(true))
	first, err := service.SetWatchLater(context.Background(), 7, 10, true)
	if err != nil {
		t.Fatalf("add watch later: %v", err)
	}
	second, err := service.SetWatchLater(context.Background(), 7, 10, true)
	if err != nil {
		t.Fatalf("replay watch later: %v", err)
	}
	if !first.UpdatedAt.Equal(second.UpdatedAt) || len(watchLater.facts) != 1 {
		t.Fatalf("watch later replay was not idempotent: first=%+v second=%+v", first, second)
	}
	if _, err := service.SetWatchLater(context.Background(), 7, 404, true); !errors.Is(err, domainlibrary.ErrVideoNotFound) {
		t.Fatalf("expected unreadable video error, got %v", err)
	}
}

func TestHistoryDeletionOnlyMutatesProjection(t *testing.T) {
	history := &historyStub{items: []domainlibrary.HistoryCandidate{{VideoID: 1}, {VideoID: 2}}}
	service := newTestService(actionIndexStub{}, history, &watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}}, catalogStub{}, privacyStub(true))
	if err := service.DeleteHistory(context.Background(), 7, 1); err != nil {
		t.Fatalf("delete history: %v", err)
	}
	if len(history.items) != 1 || history.items[0].VideoID != 2 {
		t.Fatalf("unexpected history projection: %+v", history.items)
	}
	if err := service.ClearHistory(context.Background(), 7); err != nil || len(history.items) != 0 {
		t.Fatalf("clear history: err=%v items=%+v", err, history.items)
	}
}

func TestLibraryHydratesQueueCardsWithBoundedBatchReaders(t *testing.T) {
	now := time.Now().UTC()
	sourceCards := map[int64]*domainlibrary.VideoCard{
		3: {ID: 3, AuthorID: 9, Visibility: "public", AuthorNickname: "stale", Liked: true},
		2: {ID: 2, AuthorID: 9, Visibility: "public", AuthorAvatarURL: "stale", Favorited: true},
	}
	authors := &authorReaderStub{displays: map[int64]*domainlibrary.AuthorDisplay{
		9: {AuthorID: 9, Nickname: "author", AvatarURL: "avatar"},
	}}
	viewerActions := &viewerActionReaderStub{states: map[int64]*domainlibrary.ViewerActionState{
		3: {VideoID: 3, Liked: true},
		2: {VideoID: 2, Favorited: true},
	}}
	service := New(
		actionIndexStub{items: []domainlibrary.VideoCandidate{
			{VideoID: 3, UpdatedAt: now.Add(3 * time.Second)},
			{VideoID: 2, UpdatedAt: now.Add(2 * time.Second)},
		}},
		&historyStub{},
		&watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}},
		catalogStub{cards: sourceCards},
		privacyStub(true),
		authors,
		viewerActions,
	)

	page, err := service.ListLiked(context.Background(), 7, "", 2)
	if err != nil {
		t.Fatalf("list liked: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Video.ID != 3 || page.Items[1].Video.ID != 2 {
		t.Fatalf("library order changed during hydration: %+v", page.Items)
	}
	first, second := page.Items[0].Video, page.Items[1].Video
	if first.AuthorNickname != "author" || first.AuthorAvatarURL != "avatar" || !first.Liked || first.Favorited {
		t.Fatalf("unexpected first queue card: %+v", first)
	}
	if second.AuthorNickname != "author" || second.AuthorAvatarURL != "avatar" || second.Liked || !second.Favorited {
		t.Fatalf("unexpected second queue card: %+v", second)
	}
	if authors.calls != 1 || len(authors.requested) != 1 || len(authors.requested[0]) != 1 || authors.requested[0][0] != 9 {
		t.Fatalf("author display reads were not one bounded batch: calls=%d requested=%v", authors.calls, authors.requested)
	}
	if viewerActions.calls != 1 || len(viewerActions.viewerIDs) != 1 || viewerActions.viewerIDs[0] != 7 ||
		len(viewerActions.requested) != 1 || len(viewerActions.requested[0]) != 2 ||
		viewerActions.requested[0][0] != 3 || viewerActions.requested[0][1] != 2 {
		t.Fatalf("viewer action reads were not one bounded batch: calls=%d viewers=%v requested=%v", viewerActions.calls, viewerActions.viewerIDs, viewerActions.requested)
	}
	if sourceCards[3].AuthorNickname != "stale" || !sourceCards[3].Liked ||
		sourceCards[2].AuthorAvatarURL != "stale" || !sourceCards[2].Favorited {
		t.Fatalf("catalog cards were mutated with viewer hydration: %+v", sourceCards)
	}
}

func TestPublicLibraryHydratesAuthorWithoutViewerActionRead(t *testing.T) {
	now := time.Now().UTC()
	authors := &authorReaderStub{displays: map[int64]*domainlibrary.AuthorDisplay{
		9: {AuthorID: 9, Nickname: "public author", AvatarURL: "public avatar"},
	}}
	viewerActions := &viewerActionReaderStub{}
	service := New(
		actionIndexStub{items: []domainlibrary.VideoCandidate{{VideoID: 3, UpdatedAt: now}}},
		&historyStub{},
		&watchLaterStub{facts: map[int64]*domainlibrary.WatchLater{}},
		catalogStub{cards: map[int64]*domainlibrary.VideoCard{
			3: {ID: 3, AuthorID: 9, Visibility: "public", Liked: true, Favorited: true},
		}},
		privacyStub(true),
		authors,
		viewerActions,
	)

	page, err := service.ListPublicLiked(context.Background(), 42, "", 20)
	if err != nil {
		t.Fatalf("list public liked: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("unexpected public page: %+v", page)
	}
	card := page.Items[0].Video
	if card.AuthorNickname != "public author" || card.AuthorAvatarURL != "public avatar" || card.Liked || card.Favorited {
		t.Fatalf("anonymous public queue card leaked viewer state: %+v", card)
	}
	if authors.calls != 1 || viewerActions.calls != 0 {
		t.Fatalf("unexpected public hydration calls: authors=%d viewer_actions=%d", authors.calls, viewerActions.calls)
	}
}
