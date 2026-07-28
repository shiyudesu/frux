package test

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	applicationinteraction "GCFeed/internal/application/interaction"
	domainaccount "GCFeed/internal/domain/account"
	domaininteraction "GCFeed/internal/domain/interaction"
	domainvideo "GCFeed/internal/domain/video"
	infrajwt "GCFeed/internal/infra/jwt"
	interfaceshttpinteraction "GCFeed/internal/interfaces/http/interaction"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type interactionActionAPIResponse struct {
	VideoID       int64  `json:"video_id"`
	ActionType    string `json:"action_type"`
	Active        bool   `json:"active"`
	LikeCount     int    `json:"like_count"`
	FavoriteCount int    `json:"favorite_count"`
}

type interactionCommentAPIResponse struct {
	ID            int64     `json:"id"`
	VideoID       int64     `json:"video_id"`
	UserID        int64     `json:"user_id"`
	UserNickname  string    `json:"user_nickname"`
	UserAvatarURL string    `json:"user_avatar_url"`
	Content       string    `json:"content"`
	CreatedAt     time.Time `json:"created_at"`
	CommentCount  int       `json:"comment_count"`
}

type interactionCommentListAPIResponse struct {
	Items      []interactionCommentAPIResponse `json:"items"`
	NextCursor string                          `json:"next_cursor"`
	HasMore    bool                            `json:"has_more"`
}

type interactionDeleteCommentAPIResponse struct {
	CommentID    int64 `json:"comment_id"`
	Status       int   `json:"status"`
	CommentCount int   `json:"comment_count"`
}

type memoryInteractionVideo struct {
	ID         int64
	AuthorID   int64
	Status     int
	Visibility string
}

type memoryInteractionStat struct {
	LikeCount     int
	CommentCount  int
	FavoriteCount int
}

type memoryActionIdempotencyReceipt struct {
	Active    bool
	ActionID  int64
	Count     int
	CreatedAt time.Time
}

// memoryInteractionRepo 是互动测试用内存仓储，模拟点赞、收藏、评论和计数。
type memoryInteractionRepo struct {
	mu              sync.Mutex
	nextActionID    int64
	nextCommentID   int64
	videos          map[int64]memoryInteractionVideo
	stats           map[int64]memoryInteractionStat
	actions         map[string]*domaininteraction.Action
	actionEvents    map[string]*domaininteraction.AcceptedActionEvent
	actionReceipts  map[string]memoryActionIdempotencyReceipt
	latestEvents    map[string]*domaininteraction.AcceptedActionEvent
	profileHandoffs map[string]*domaininteraction.AcceptedActionEvent
	outcomeHandoffs map[string]*domaininteraction.AcceptedActionEvent
	persistErrors   []error
	persistCtxErr   error
	persistBounded  bool
	comments        map[int64]*domaininteraction.Comment
	commentIdem     map[string]int64
}

type memoryHotScoreRecorder struct {
	mu     sync.Mutex
	scores map[int64]int
	events []int
}

type memoryActionPipeline struct {
	mu                    sync.Mutex
	states                map[string]*applicationinteraction.ActionStateResult
	receipts              map[string]map[string]bool
	stats                 map[int64]memoryInteractionStat
	events                []*applicationinteraction.ActionChangedEvent
	publishErr            error
	versionCounters       map[string]int64
	enqueueOnPublishError bool
	countReadErrors       []error
	countReadHook         func()
	rollbackHook          func()
	rollbackErr           error
	rollbackContextErr    error
	rollbackHasDeadline   bool
	publishContextErr     error
	publishHasDeadline    bool
}

type memoryInteractionMessageWriter struct {
	mu       sync.Mutex
	messages []memoryInteractionMessage
}

type memoryInteractionStatCache struct {
	mu    sync.Mutex
	stats map[int64]*domaininteraction.VideoStat
}

type memoryInteractionMessage struct {
	UserID         int64
	Type           string
	Title          string
	EventID        string
	ActorID        int64
	ActorNickname  string
	ActorAvatarURL string
}

func newMemoryInteractionStatCache() *memoryInteractionStatCache {
	return &memoryInteractionStatCache{stats: map[int64]*domaininteraction.VideoStat{}}
}

func (c *memoryInteractionStatCache) SetVideoStat(ctx context.Context, stat *domaininteraction.VideoStat) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cloned := *stat
	c.stats[stat.VideoID] = &cloned
	return nil
}

func (c *memoryInteractionStatCache) StatForTest(videoID int64) *domaininteraction.VideoStat {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stats[videoID] == nil {
		return nil
	}
	cloned := *c.stats[videoID]
	return &cloned
}

func newMemoryInteractionRepo() *memoryInteractionRepo {
	return &memoryInteractionRepo{
		nextActionID:  1,
		nextCommentID: 1,
		videos: map[int64]memoryInteractionVideo{
			1001: {
				ID:         1001,
				AuthorID:   42,
				Status:     domainvideo.StatusPublished,
				Visibility: domainvideo.VisibilityPublic,
			},
			1002: {
				ID:         1002,
				AuthorID:   77,
				Status:     domainvideo.StatusPublished,
				Visibility: domainvideo.VisibilityPublic,
			},
		},
		stats:           map[int64]memoryInteractionStat{1001: {}, 1002: {}},
		actions:         map[string]*domaininteraction.Action{},
		actionEvents:    map[string]*domaininteraction.AcceptedActionEvent{},
		actionReceipts:  map[string]memoryActionIdempotencyReceipt{},
		latestEvents:    map[string]*domaininteraction.AcceptedActionEvent{},
		profileHandoffs: map[string]*domaininteraction.AcceptedActionEvent{},
		outcomeHandoffs: map[string]*domaininteraction.AcceptedActionEvent{},
		comments:        map[int64]*domaininteraction.Comment{},
		commentIdem:     map[string]int64{},
	}
}

func TestInteractionSyncFallbackSkipsNoTransitionRecommendationHandoffs(t *testing.T) {
	router, jwtManager, repo := newInteractionRouterWithRepo(t)
	token := signTestToken(t, jwtManager, 42)

	for _, request := range []struct {
		path      string
		key       string
		requestID string
	}{
		{path: "/api/videos/1001/like", key: "sync-first", requestID: "first-request"},
		{path: "/api/videos/1001/like", key: "sync-repeat", requestID: "second-request"},
		{path: "/api/videos/1001/like", key: "sync-repeat", requestID: "forged-replay-request"},
	} {
		response := performJSONRequestWithHeaders(
			router,
			http.MethodPut,
			request.path,
			"",
			ut.Header{Key: "Authorization", Value: "Bearer " + token},
			ut.Header{Key: "Idempotency-Key", Value: request.key},
			ut.Header{Key: "X-Recommendation-Request-ID", Value: request.requestID},
		)
		requireStatus(t, response, http.StatusOK)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.actionEvents) != 1 || len(repo.profileHandoffs) != 1 || len(repo.outcomeHandoffs) != 1 {
		t.Fatalf("no-transition sync actions created durable handoffs: receipts=%#v profile=%#v outcome=%#v", repo.actionEvents, repo.profileHandoffs, repo.outcomeHandoffs)
	}
	for _, event := range repo.actionEvents {
		if event.IdempotencyKey != "sync-first" || event.RecommendationRequestID != "first-request" {
			t.Fatalf("no-transition action overwrote durable signal attribution: %#v", event)
		}
	}
	if got := len(repo.actionReceipts); got != 2 {
		t.Fatalf("sync no-op keys did not receive durable receipts: %d", got)
	}
}

func TestInteractionSyncFallbackReplaysPayloadBoundActionReceipts(t *testing.T) {
	router, jwtManager, repo := newInteractionRouterWithRepo(t)
	token := signTestToken(t, jwtManager, 42)
	request := func(method string, path string, key string) *ut.ResponseRecorder {
		return performJSONRequestWithHeaders(
			router,
			method,
			path,
			"",
			ut.Header{Key: "Authorization", Value: "Bearer " + token},
			ut.Header{Key: "Idempotency-Key", Value: key},
		)
	}

	first := request(http.MethodPut, "/api/videos/1001/like", "like-replay")
	requireStatus(t, first, http.StatusOK)
	var original interactionActionAPIResponse
	decodeJSON(t, first, &original)

	unlike := request(http.MethodDelete, "/api/videos/1001/like", "unlike-new-key")
	requireStatus(t, unlike, http.StatusOK)

	replay := request(http.MethodPut, "/api/videos/1001/like", "like-replay")
	requireStatus(t, replay, http.StatusOK)
	var replayed interactionActionAPIResponse
	decodeJSON(t, replay, &replayed)
	if replayed != original {
		t.Fatalf("same desired action did not replay its original result: got=%+v want=%+v", replayed, original)
	}

	conflict := request(http.MethodDelete, "/api/videos/1001/like", "like-replay")
	requireStatus(t, conflict, http.StatusConflict)

	absentUnlike := request(http.MethodDelete, "/api/videos/1002/like", "absent-unlike")
	requireStatus(t, absentUnlike, http.StatusOK)
	var absent interactionActionAPIResponse
	decodeJSON(t, absentUnlike, &absent)
	if absent.Active || absent.LikeCount != 0 {
		t.Fatalf("unexpected absent unlike response: %+v", absent)
	}
	conflictingAbsentUnlike := request(http.MethodPut, "/api/videos/1002/like", "absent-unlike")
	requireStatus(t, conflictingAbsentUnlike, http.StatusConflict)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.actionEvents) != 2 || len(repo.profileHandoffs) != 2 || len(repo.outcomeHandoffs) != 0 {
		t.Fatalf("no-op receipts enqueued handoffs: events=%#v profile=%#v outcome=%#v", repo.actionEvents, repo.profileHandoffs, repo.outcomeHandoffs)
	}
	if len(repo.actionReceipts) != 3 {
		t.Fatalf("unexpected durable action receipt count: %#v", repo.actionReceipts)
	}
}

// GetVideoStat 模拟读取视频当前互动计数。
func (r *memoryInteractionRepo) GetVideoStat(ctx context.Context, videoID int64) (*domaininteraction.VideoStat, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.videoPublished(videoID) {
		return nil, domaininteraction.ErrVideoNotFound
	}
	stat := r.stats[videoID]
	return &domaininteraction.VideoStat{
		VideoID:       videoID,
		LikeCount:     stat.LikeCount,
		CommentCount:  stat.CommentCount,
		FavoriteCount: stat.FavoriteCount,
	}, nil
}

func (r *memoryInteractionRepo) GetActionState(ctx context.Context, userID int64, videoID int64, actionType string) (*domaininteraction.ActionStateSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memoryInteractionActionKey(userID, videoID, actionType)
	action := r.actions[key]
	if action == nil {
		return &domaininteraction.ActionStateSnapshot{}, nil
	}
	snapshot := &domaininteraction.ActionStateSnapshot{
		Exists:         true,
		Active:         action.Active(),
		IdempotencyKey: action.IdempotencyKey,
		UpdatedAt:      action.UpdatedAt,
	}
	if latest := r.latestEvents[key]; latest != nil {
		snapshot.RecommendationRequestID = latest.RecommendationRequestID
		snapshot.Version = latest.Version
		snapshot.EventID = latest.EventID
		snapshot.OccurredAt = latest.OccurredAt
	}
	return snapshot, nil
}

// GetVideoAuthorID 模拟读取公开视频作者。
func (r *memoryInteractionRepo) GetVideoAuthorID(ctx context.Context, videoID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.videoPublished(videoID) {
		return 0, domaininteraction.ErrVideoNotFound
	}
	return r.videos[videoID].AuthorID, nil
}

// GetUserProfile 模拟读取用户展示资料。
func (r *memoryInteractionRepo) GetUserProfile(ctx context.Context, userID int64) (*domaininteraction.UserProfile, error) {
	if userID <= 0 {
		return nil, domaininteraction.ErrInvalidUserID
	}
	return &domaininteraction.UserProfile{
		ID:        userID,
		Nickname:  memoryInteractionNickname(userID),
		AvatarURL: memoryInteractionAvatar(userID),
	}, nil
}

// SetAction 模拟点赞/收藏状态变更，并维护对应计数。
func (r *memoryInteractionRepo) SetAction(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string) (*domaininteraction.Action, int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.videoPublished(videoID) {
		return nil, 0, 0, domaininteraction.ErrVideoNotFound
	}
	return r.setActionLocked(userID, videoID, actionType, active, idempotencyKey, true)
}

func (r *memoryInteractionRepo) PersistAcceptedActionEvent(ctx context.Context, event *domaininteraction.AcceptedActionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.persistCtxErr = ctx.Err()
	_, r.persistBounded = ctx.Deadline()
	if len(r.persistErrors) > 0 {
		err := r.persistErrors[0]
		r.persistErrors = r.persistErrors[1:]
		if err != nil {
			return err
		}
	}
	if event == nil {
		return domaininteraction.ErrInvalidActionEvent
	}
	if existing := r.actionEvents[event.EventID]; existing != nil {
		if !sameMemoryAcceptedActionEvent(existing, event) {
			return domaininteraction.ErrActionEventConflict
		}
		return nil
	}
	video, exists := r.videos[event.VideoID]
	if !exists || video.Status == domainvideo.StatusDeleted {
		return domaininteraction.ErrVideoNotFound
	}
	key := memoryInteractionActionKey(event.UserID, event.VideoID, event.ActionType)
	cloned := *event
	r.actionEvents[event.EventID] = &cloned
	r.profileHandoffs[event.EventID] = &cloned
	if event.Active && event.RecommendationRequestID != "" {
		r.outcomeHandoffs[event.EventID] = &cloned
	}
	if latest := r.latestEvents[key]; latest != nil && !domaininteraction.ActionEventComesAfter(
		event.Version,
		event.OccurredAt,
		event.EventID,
		latest.Version,
		latest.OccurredAt,
		latest.EventID,
	) {
		return nil
	}
	if _, _, _, err := r.setActionLocked(event.UserID, event.VideoID, event.ActionType, event.Active, event.IdempotencyKey, false); err != nil {
		return err
	}
	r.latestEvents[key] = &cloned
	return nil
}

func (r *memoryInteractionRepo) SetActionWithAcceptedEvent(ctx context.Context, event *domaininteraction.AcceptedActionEvent) (*domaininteraction.Action, int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if event == nil {
		return nil, 0, 0, domaininteraction.ErrInvalidActionEvent
	}
	if !r.videoPublished(event.VideoID) {
		return nil, 0, 0, domaininteraction.ErrVideoNotFound
	}
	key := memoryInteractionActionKey(event.UserID, event.VideoID, event.ActionType)
	receiptKey := memoryInteractionActionReceiptKey(event.UserID, event.VideoID, event.ActionType, event.IdempotencyKey)
	if event.IdempotencyKey != "" {
		if receipt, exists := r.actionReceipts[receiptKey]; exists {
			if receipt.Active != event.Active {
				return nil, 0, 0, domaininteraction.ErrActionIdempotencyConflict
			}
			return domaininteraction.RestoreAction(
				receipt.ActionID, event.UserID, event.VideoID, event.ActionType,
				actionStatusForTest(receipt.Active), event.IdempotencyKey, receipt.CreatedAt, receipt.CreatedAt,
			), receipt.Count, 0, nil
		}
	}
	if existing := r.actionEvents[event.EventID]; existing != nil {
		if !sameMemoryAcceptedActionEvent(existing, event) {
			return nil, 0, 0, domaininteraction.ErrActionEventConflict
		}
		current := r.actions[key]
		if current == nil {
			return domaininteraction.RestoreAction(
				0, event.UserID, event.VideoID, event.ActionType, actionStatusForTest(event.Active), "", time.Time{}, time.Time{},
			), r.actionCount(event.VideoID, event.ActionType), 0, nil
		}
		return cloneInteractionAction(current), r.actionCount(event.VideoID, event.ActionType), 0, nil
	}

	current := r.actions[key]
	if current == nil && !event.Active {
		action := domaininteraction.RestoreAction(
			0, event.UserID, event.VideoID, event.ActionType, domaininteraction.ActionStatusCanceled, "", time.Time{}, time.Time{},
		)
		count := r.actionCount(event.VideoID, event.ActionType)
		r.storeActionReceipt(event, action, count)
		return action, count, 0, nil
	}
	if current != nil && current.Active() == event.Active {
		action := cloneInteractionAction(current)
		count := r.actionCount(event.VideoID, event.ActionType)
		r.storeActionReceipt(event, action, count)
		return action, count, 0, nil
	}

	accepted := *event
	if accepted.Version == 0 {
		accepted.Version = 1
		if latest := r.latestEvents[key]; latest != nil && latest.Version >= accepted.Version {
			accepted.Version = latest.Version + 1
		}
	}
	action, count, delta, err := r.setActionLocked(accepted.UserID, accepted.VideoID, accepted.ActionType, accepted.Active, accepted.IdempotencyKey, false)
	if err != nil {
		return nil, 0, 0, err
	}
	r.storeActionReceipt(&accepted, action, count)
	r.actionEvents[accepted.EventID] = &accepted
	r.latestEvents[key] = &accepted
	r.profileHandoffs[accepted.EventID] = &accepted
	if accepted.Active && accepted.RecommendationRequestID != "" {
		r.outcomeHandoffs[accepted.EventID] = &accepted
	}
	return action, count, delta, nil
}

func (r *memoryInteractionRepo) storeActionReceipt(event *domaininteraction.AcceptedActionEvent, action *domaininteraction.Action, count int) {
	if event == nil || strings.TrimSpace(event.IdempotencyKey) == "" {
		return
	}
	receipt := memoryActionIdempotencyReceipt{Active: event.Active, Count: count, CreatedAt: time.Now().UTC()}
	if action != nil {
		receipt.ActionID = action.ID
	}
	r.actionReceipts[memoryInteractionActionReceiptKey(event.UserID, event.VideoID, event.ActionType, event.IdempotencyKey)] = receipt
}

func actionStatusForTest(active bool) int {
	if active {
		return domaininteraction.ActionStatusActive
	}
	return domaininteraction.ActionStatusCanceled
}

func (r *memoryInteractionRepo) setActionLocked(userID int64, videoID int64, actionType string, active bool, idempotencyKey string, respectIdempotency bool) (*domaininteraction.Action, int, int, error) {
	key := memoryInteractionActionKey(userID, videoID, actionType)
	action, exists := r.actions[key]
	if respectIdempotency && exists && idempotencyKey != "" && action.IdempotencyKey == strings.TrimSpace(idempotencyKey) {
		// 幂等键命中时直接返回当前状态和计数，模拟真实仓储重放逻辑。
		return cloneInteractionAction(action), r.actionCount(videoID, actionType), 0, nil
	}

	nextStatus := domaininteraction.ActionStatusCanceled
	if active {
		nextStatus = domaininteraction.ActionStatusActive
	}

	delta := 0
	if !exists {
		action = &domaininteraction.Action{
			ID:             r.nextActionID,
			UserID:         userID,
			VideoID:        videoID,
			ActionType:     actionType,
			Status:         nextStatus,
			IdempotencyKey: strings.TrimSpace(idempotencyKey),
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		r.nextActionID++
		r.actions[key] = action
		if active {
			delta = 1
			r.addActionCount(videoID, actionType, 1)
		}
		return cloneInteractionAction(action), r.actionCount(videoID, actionType), delta, nil
	}

	if action.Status != nextStatus {
		if active {
			delta = 1
			r.addActionCount(videoID, actionType, 1)
		} else {
			delta = -1
			r.addActionCount(videoID, actionType, -1)
		}
	}
	action.Status = nextStatus
	action.IdempotencyKey = strings.TrimSpace(idempotencyKey)
	action.UpdatedAt = time.Now()
	return cloneInteractionAction(action), r.actionCount(videoID, actionType), delta, nil
}

// CreateComment 模拟评论创建，并维护视频评论数和评论幂等索引。
func (r *memoryInteractionRepo) CreateComment(ctx context.Context, comment *domaininteraction.Comment) (*domaininteraction.Comment, int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.videoPublished(comment.VideoID) {
		return nil, 0, 0, domaininteraction.ErrVideoNotFound
	}
	if comment.IdempotencyKey != "" {
		key := memoryInteractionCommentIdemKey(comment.UserID, comment.IdempotencyKey)
		if id, exists := r.commentIdem[key]; exists {
			existing := r.comments[id]
			return cloneInteractionComment(existing), r.stats[existing.VideoID].CommentCount, 0, nil
		}
	}

	now := time.Now()
	comment.ID = r.nextCommentID
	r.nextCommentID++
	comment.UserNickname = memoryInteractionNickname(comment.UserID)
	comment.UserAvatarURL = memoryInteractionAvatar(comment.UserID)
	comment.CreatedAt = now
	comment.UpdatedAt = now
	r.comments[comment.ID] = cloneInteractionComment(comment)
	if comment.IdempotencyKey != "" {
		r.commentIdem[memoryInteractionCommentIdemKey(comment.UserID, comment.IdempotencyKey)] = comment.ID
	}
	stat := r.stats[comment.VideoID]
	stat.CommentCount++
	r.stats[comment.VideoID] = stat
	return cloneInteractionComment(comment), stat.CommentCount, 1, nil
}

// FindCommentByUserAndIdempotencyKey 模拟评论创建接口的幂等查询。
func (r *memoryInteractionRepo) FindCommentByUserAndIdempotencyKey(ctx context.Context, userID int64, idempotencyKey string) (*domaininteraction.Comment, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id, exists := r.commentIdem[memoryInteractionCommentIdemKey(userID, idempotencyKey)]
	if !exists {
		return nil, 0, domaininteraction.ErrCommentNotFound
	}
	comment := r.comments[id]
	return cloneInteractionComment(comment), r.stats[comment.VideoID].CommentCount, nil
}

// ListComments 模拟评论列表游标分页，排序规则与真实仓储一致。
func (r *memoryInteractionRepo) ListComments(ctx context.Context, videoID int64, cursor *domaininteraction.CommentCursor, limit int) ([]*domaininteraction.Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.videoPublished(videoID) {
		return nil, domaininteraction.ErrVideoNotFound
	}

	comments := make([]*domaininteraction.Comment, 0)
	for _, comment := range r.comments {
		if comment.VideoID != videoID || comment.Status != domaininteraction.CommentStatusNormal {
			continue
		}
		if cursor != nil && !comment.CreatedAt.Before(cursor.CreatedAt) && !(comment.CreatedAt.Equal(cursor.CreatedAt) && comment.ID < cursor.CommentID) {
			continue
		}
		comments = append(comments, cloneInteractionComment(comment))
	}
	sort.Slice(comments, func(i, j int) bool {
		if comments[i].CreatedAt.Equal(comments[j].CreatedAt) {
			return comments[i].ID > comments[j].ID
		}
		return comments[i].CreatedAt.After(comments[j].CreatedAt)
	})
	if limit > len(comments) {
		limit = len(comments)
	}
	return comments[:limit], nil
}

func TestInteractionCommentListFollowsVideoVisibility(t *testing.T) {
	router, jwtManager, repo := newInteractionRouterWithRepo(t)
	commenterToken := signTestToken(t, jwtManager, 77)
	authorToken := signTestToken(t, jwtManager, 42)

	createResponse := performVideoJSONRequest(
		router,
		http.MethodPost,
		"/api/videos/1001/comments",
		`{"content":"visibility comment"}`,
		commenterToken,
		"comment-visibility-1",
	)
	requireStatus(t, createResponse, http.StatusCreated)

	assertCommentListStatus := func(want int) {
		t.Helper()
		response := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments", "", "")
		requireStatus(t, response, want)
		if want == http.StatusOK {
			var list interactionCommentListAPIResponse
			decodeJSON(t, response, &list)
			if len(list.Items) != 1 || list.Items[0].Content != "visibility comment" {
				t.Fatalf("unexpected visible comments: %+v", list)
			}
		}
	}

	assertCommentListStatus(http.StatusOK)
	repo.setVideoVisibilityForTest(1001, domainvideo.VisibilityPrivate)
	assertCommentListStatus(http.StatusNotFound)
	repo.setVideoVisibilityForTest(1001, domainvideo.VisibilityPublic)
	assertCommentListStatus(http.StatusOK)
	repo.setVideoStatusForTest(1001, domainvideo.StatusOffline)
	assertCommentListStatus(http.StatusNotFound)
	repo.setVideoStatusForTest(1001, domainvideo.StatusPublished)
	assertCommentListStatus(http.StatusOK)
	repo.setVideoStatusForTest(1001, domainvideo.StatusDeleted)
	assertCommentListStatus(http.StatusNotFound)

	deleteResponse := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", authorToken)
	requireStatus(t, deleteResponse, http.StatusOK)
}

// DeleteComment 模拟评论软删除，以及评论作者、视频作者、管理员三种删除权限。
func (r *memoryInteractionRepo) DeleteComment(ctx context.Context, commentID int64, userID int64, role string) (*domaininteraction.Comment, int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	comment, exists := r.comments[commentID]
	if !exists {
		return nil, 0, 0, domaininteraction.ErrCommentNotFound
	}
	video := r.videos[comment.VideoID]
	if comment.UserID != userID && video.AuthorID != userID && role != domainaccount.RoleAdmin {
		return nil, 0, 0, domaininteraction.ErrCommentPermissionDenied
	}
	delta := 0
	if comment.Status != domaininteraction.CommentStatusDeleted {
		comment.Status = domaininteraction.CommentStatusDeleted
		comment.UpdatedAt = time.Now()
		delta = -1
		stat := r.stats[comment.VideoID]
		if stat.CommentCount > 0 {
			stat.CommentCount--
		}
		r.stats[comment.VideoID] = stat
	}
	return cloneInteractionComment(comment), r.stats[comment.VideoID].CommentCount, delta, nil
}

// TestInteractionActionFlow 覆盖点赞、幂等重放、取消点赞和收藏。
func TestInteractionActionFlow(t *testing.T) {
	router, jwtManager := newInteractionRouter(t)
	token := signTestToken(t, jwtManager, 42)

	likeResponse := performVideoJSONRequest(router, http.MethodPut, "/api/videos/1001/like", "", token, "like-1")
	requireStatus(t, likeResponse, http.StatusOK)

	var liked interactionActionAPIResponse
	decodeJSON(t, likeResponse, &liked)
	if liked.ActionType != domaininteraction.ActionTypeLike || !liked.Active || liked.LikeCount != 1 {
		t.Fatalf("unexpected like response: %+v", liked)
	}

	replayResponse := performVideoJSONRequest(router, http.MethodPut, "/api/videos/1001/like", "", token, "like-1")
	requireStatus(t, replayResponse, http.StatusOK)
	var replayed interactionActionAPIResponse
	decodeJSON(t, replayResponse, &replayed)
	if !replayed.Active || replayed.LikeCount != 1 {
		t.Fatalf("unexpected replay response: %+v", replayed)
	}

	unlikeResponse := performVideoJSONRequest(router, http.MethodDelete, "/api/videos/1001/like", "", token, "like-2")
	requireStatus(t, unlikeResponse, http.StatusOK)
	var unliked interactionActionAPIResponse
	decodeJSON(t, unlikeResponse, &unliked)
	if unliked.Active || unliked.LikeCount != 0 {
		t.Fatalf("unexpected unlike response: %+v", unliked)
	}

	favoriteResponse := performVideoJSONRequest(router, http.MethodPut, "/api/videos/1001/favorite", "", token, "favorite-1")
	requireStatus(t, favoriteResponse, http.StatusOK)
	var favorited interactionActionAPIResponse
	decodeJSON(t, favoriteResponse, &favorited)
	if favorited.ActionType != domaininteraction.ActionTypeFavorite || !favorited.Active || favorited.FavoriteCount != 1 {
		t.Fatalf("unexpected favorite response: %+v", favorited)
	}
}

func TestInteractionSyncFallbackCreatesRecommendationHandoffs(t *testing.T) {
	router, jwtManager, repo := newInteractionRouterWithRepo(t)
	token := signTestToken(t, jwtManager, 42)
	requestID := strings.Repeat("r", domaininteraction.MaxRecommendationRequestIDLength)

	for _, request := range []struct {
		path       string
		key        string
		actionType string
	}{
		{path: "/api/videos/1001/like", key: "sync-recommend-like", actionType: domaininteraction.ActionTypeLike},
		{path: "/api/videos/1002/favorite", key: "sync-recommend-favorite", actionType: domaininteraction.ActionTypeFavorite},
	} {
		response := performJSONRequestWithHeaders(
			router,
			http.MethodPut,
			request.path,
			"",
			ut.Header{Key: "Authorization", Value: "Bearer " + token},
			ut.Header{Key: "Idempotency-Key", Value: request.key},
			ut.Header{Key: "X-Recommendation-Request-ID", Value: requestID},
		)
		requireStatus(t, response, http.StatusOK)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.actionEvents) != 2 || len(repo.profileHandoffs) != 2 || len(repo.outcomeHandoffs) != 2 {
		t.Fatalf("sync fallback did not atomically retain action/profile/outcome handoffs: receipts=%#v profile=%#v outcome=%#v", repo.actionEvents, repo.profileHandoffs, repo.outcomeHandoffs)
	}
	for eventID, event := range repo.actionEvents {
		if event.RecommendationRequestID != requestID || event.Version <= 0 || repo.profileHandoffs[eventID] == nil || repo.outcomeHandoffs[eventID] == nil {
			t.Fatalf("sync receipt lost bounded recommendation attribution or handoff: event=%#v", event)
		}
	}
}

// TestInteractionCommentFlow 覆盖创建评论、幂等重放、列表、权限删除和重复删除。
func TestInteractionCommentFlow(t *testing.T) {
	router, jwtManager := newInteractionRouter(t)
	authorToken := signTestToken(t, jwtManager, 42)
	commenterToken := signTestToken(t, jwtManager, 77)
	otherToken := signTestToken(t, jwtManager, 99)

	createResponse := performVideoJSONRequest(router, http.MethodPost, "/api/videos/1001/comments", `{"content":" first comment "}`, commenterToken, "comment-1")
	requireStatus(t, createResponse, http.StatusCreated)

	var created interactionCommentAPIResponse
	decodeJSON(t, createResponse, &created)
	if created.ID == 0 || created.UserID != 77 || created.Content != "first comment" || created.CommentCount != 1 {
		t.Fatalf("unexpected comment response: %+v", created)
	}

	replayResponse := performVideoJSONRequest(router, http.MethodPost, "/api/videos/1001/comments", `{"content":"changed"}`, commenterToken, "comment-1")
	requireStatus(t, replayResponse, http.StatusCreated)
	var replayed interactionCommentAPIResponse
	decodeJSON(t, replayResponse, &replayed)
	if replayed.ID != created.ID || replayed.Content != created.Content || replayed.CommentCount != 1 {
		t.Fatalf("unexpected replay comment response: %+v", replayed)
	}

	listResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?limit=10", "", "")
	requireStatus(t, listResponse, http.StatusOK)
	var list interactionCommentListAPIResponse
	decodeJSON(t, listResponse, &list)
	if len(list.Items) != 1 || list.Items[0].ID != created.ID || list.Items[0].UserNickname != "user-77" {
		t.Fatalf("unexpected comment list response: %+v", list)
	}

	forbiddenDelete := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", otherToken)
	requireStatus(t, forbiddenDelete, http.StatusForbidden)

	authorDelete := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", authorToken)
	requireStatus(t, authorDelete, http.StatusOK)
	var deleted interactionDeleteCommentAPIResponse
	decodeJSON(t, authorDelete, &deleted)
	if deleted.CommentID != created.ID || deleted.Status != domaininteraction.CommentStatusDeleted || deleted.CommentCount != 0 {
		t.Fatalf("unexpected delete response: %+v", deleted)
	}

	repeatDelete := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", authorToken)
	requireStatus(t, repeatDelete, http.StatusOK)
	var repeat interactionDeleteCommentAPIResponse
	decodeJSON(t, repeatDelete, &repeat)
	if repeat.CommentCount != 0 {
		t.Fatalf("unexpected repeat delete response: %+v", repeat)
	}
}

// TestInteractionValidation 覆盖互动接口的登录态、参数和资源校验。
func TestInteractionValidation(t *testing.T) {
	router, jwtManager := newInteractionRouter(t)
	token := signTestToken(t, jwtManager, 42)

	unauthorizedLike := performJSONRequest(router, http.MethodPut, "/api/videos/1001/like", "", "")
	requireStatus(t, unauthorizedLike, http.StatusUnauthorized)

	badLike := performJSONRequest(router, http.MethodPut, "/api/videos/0/like", "", token)
	requireStatus(t, badLike, http.StatusBadRequest)

	missingVideo := performJSONRequest(router, http.MethodPut, "/api/videos/404/like", "", token)
	requireStatus(t, missingVideo, http.StatusNotFound)

	emptyComment := performJSONRequest(router, http.MethodPost, "/api/videos/1001/comments", `{"content":"   "}`, token)
	requireStatus(t, emptyComment, http.StatusBadRequest)

	badList := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?limit=0", "", "")
	requireStatus(t, badList, http.StatusBadRequest)
}

// TestInteractionHotScoreRecorder 覆盖真实互动变化写入热榜增量。
func TestInteractionHotScoreRecorder(t *testing.T) {
	repo := newMemoryInteractionRepo()
	recorder := newMemoryHotScoreRecorder()
	service := applicationinteraction.New(repo, applicationinteraction.WithHotScoreRecorder(recorder))

	if _, err := service.Like(context.Background(), 42, 1001, "like-1"); err != nil {
		t.Fatalf("like: %v", err)
	}
	if _, err := service.Like(context.Background(), 42, 1001, "like-1"); err != nil {
		t.Fatalf("like replay: %v", err)
	}
	if _, err := service.Favorite(context.Background(), 42, 1001, "favorite-1"); err != nil {
		t.Fatalf("favorite: %v", err)
	}
	if _, err := service.Unlike(context.Background(), 42, 1001, "like-2"); err != nil {
		t.Fatalf("unlike: %v", err)
	}
	created, err := service.CreateComment(context.Background(), 77, 1001, "hot comment", "comment-1")
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if _, err := service.CreateComment(context.Background(), 77, 1001, "hot comment replay", "comment-1"); err != nil {
		t.Fatalf("comment replay: %v", err)
	}
	if _, err := service.DeleteComment(context.Background(), created.Comment.ID, 77, domainaccount.RoleUser); err != nil {
		t.Fatalf("delete comment: %v", err)
	}

	if recorder.Score(1001) != 4 {
		t.Fatalf("unexpected hot score: %d", recorder.Score(1001))
	}
	if recorder.EventCount() != 5 {
		t.Fatalf("unexpected hot event count: %d", recorder.EventCount())
	}
}

func TestInteractionCommentSyncsStatCache(t *testing.T) {
	repo := newMemoryInteractionRepo()
	cache := newMemoryInteractionStatCache()
	service := applicationinteraction.New(repo, applicationinteraction.WithStatCache(cache))

	created, err := service.CreateComment(context.Background(), 77, 1001, "cache comment", "comment-cache-1")
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	stat := cache.StatForTest(1001)
	if stat == nil || stat.CommentCount != 1 {
		t.Fatalf("expected comment count cache to be 1, got %+v", stat)
	}

	if _, err := service.DeleteComment(context.Background(), created.Comment.ID, 77, domainaccount.RoleUser); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	stat = cache.StatForTest(1001)
	if stat == nil || stat.CommentCount != 0 {
		t.Fatalf("expected comment count cache to be 0, got %+v", stat)
	}
}

// TestInteractionMessageWriter 覆盖点赞和评论成功后给视频作者写消息。
func TestInteractionMessageWriter(t *testing.T) {
	repo := newMemoryInteractionRepo()
	writer := newMemoryInteractionMessageWriter()
	service := applicationinteraction.New(repo, applicationinteraction.WithMessageWriter(writer))

	if _, err := service.Like(context.Background(), 77, 1001, "like-notify-1"); err != nil {
		t.Fatalf("like: %v", err)
	}
	if _, err := service.Like(context.Background(), 77, 1001, "like-notify-1"); err != nil {
		t.Fatalf("like replay: %v", err)
	}
	if _, err := service.CreateComment(context.Background(), 77, 1001, "notify comment", "comment-notify-1"); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, err := service.CreateComment(context.Background(), 42, 1001, "own comment", "comment-own-1"); err != nil {
		t.Fatalf("own comment: %v", err)
	}

	messages := writer.Messages()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %+v", messages)
	}
	if messages[0].UserID != 42 || messages[0].Type != "LIKE" {
		t.Fatalf("unexpected like message: %+v", messages[0])
	}
	if messages[0].ActorID != 77 || messages[0].ActorNickname != memoryInteractionNickname(77) || messages[0].ActorAvatarURL != memoryInteractionAvatar(77) {
		t.Fatalf("unexpected like actor: %+v", messages[0])
	}
	if messages[1].UserID != 42 || messages[1].Type != "COMMENT" {
		t.Fatalf("unexpected comment message: %+v", messages[1])
	}
	if messages[1].ActorID != 77 || messages[1].ActorNickname != memoryInteractionNickname(77) || messages[1].ActorAvatarURL != memoryInteractionAvatar(77) {
		t.Fatalf("unexpected comment actor: %+v", messages[1])
	}
}

// TestInteractionAsyncActionPipeline 覆盖点赞收藏先写快速状态，再由事件 Worker 落库。
func TestInteractionAsyncActionPipeline(t *testing.T) {
	repo := newMemoryInteractionRepo()
	recorder := newMemoryHotScoreRecorder()
	pipeline := newMemoryActionPipeline()
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithHotScoreRecorder(recorder),
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	liked, err := service.LikeWithRecommendation(context.Background(), 42, 1001, "like-async-1", "recommendation-request")
	if err != nil {
		t.Fatalf("like: %v", err)
	}
	if !liked.Active || liked.LikeCount != 1 {
		t.Fatalf("unexpected async like result: %+v", liked)
	}
	if repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike) != 0 {
		t.Fatalf("repo should not be updated before worker")
	}
	if pipeline.EventCount() != 1 {
		t.Fatalf("unexpected event count: %d", pipeline.EventCount())
	}
	if event := pipeline.EventsForTest()[0]; event.RecommendationRequestID != "recommendation-request" {
		t.Fatalf("recommendation request id was not propagated: %#v", event)
	}
	noTransition, err := service.LikeWithRecommendation(context.Background(), 42, 1001, "like-async-2", "new-recommendation-request")
	if err != nil {
		t.Fatalf("repeat like with a new key: %v", err)
	}
	if !noTransition.Active || noTransition.LikeCount != 1 || pipeline.EventCount() != 1 {
		t.Fatalf("no-transition async like created a new signal: result=%+v events=%d", noTransition, pipeline.EventCount())
	}
	replayedNoTransition, err := service.LikeWithRecommendation(context.Background(), 42, 1001, "like-async-2", "different-recommendation-request")
	if err != nil {
		t.Fatalf("no-transition like replay: %v", err)
	}
	if !replayedNoTransition.Active || replayedNoTransition.LikeCount != 1 || pipeline.EventCount() != 1 {
		t.Fatalf("no-transition replay created a signal: result=%+v events=%d", replayedNoTransition, pipeline.EventCount())
	}

	replayed, err := service.LikeWithRecommendation(context.Background(), 42, 1001, "like-async-1", "different-recommendation-request")
	if err != nil {
		t.Fatalf("like replay: %v", err)
	}
	if replayed.LikeCount != 1 || pipeline.EventCount() != 2 {
		t.Fatalf("unexpected async replay: result=%+v events=%d", replayed, pipeline.EventCount())
	}

	if _, err := service.Unlike(context.Background(), 42, 1001, "like-async-2"); !errors.Is(err, domaininteraction.ErrActionIdempotencyConflict) {
		t.Fatalf("opposite no-transition payload did not conflict: %v", err)
	}
	if pipeline.EventCount() != 2 {
		t.Fatalf("conflicting no-transition payload published an event: %d", pipeline.EventCount())
	}
	replayedEvents := pipeline.EventsForTest()
	if replayedEvents[0].EventID != replayedEvents[1].EventID {
		t.Fatalf("idempotent retry must republish the same recoverable event: %+v", replayedEvents)
	}
	if replayedEvents[1].RecommendationRequestID != "recommendation-request" {
		t.Fatalf("idempotent replay reattributed action: %+v", replayedEvents[1])
	}

	worker := applicationinteraction.NewActionWorker(repo, pipeline)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	if repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike) != 1 {
		t.Fatalf("repo should be updated by worker")
	}
}

func TestInteractionAcceptedActionPersistsAfterPrivacyChange(t *testing.T) {
	repo := newMemoryInteractionRepo()
	pipeline := newMemoryActionPipeline()
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.Like(context.Background(), 7, 1001, "accepted-like"); err != nil {
		t.Fatalf("accept public like: %v", err)
	}
	repo.setVideoVisibilityForTest(1001, domainvideo.VisibilityPrivate)
	if _, err := service.Like(context.Background(), 8, 1001, "private-like"); !errors.Is(err, domaininteraction.ErrVideoNotFound) {
		t.Fatalf("new private-video interaction should be rejected, got %v", err)
	}
	if got := pipeline.EventCount(); got != 1 {
		t.Fatalf("private-video request published an event: %d", got)
	}

	worker := applicationinteraction.NewActionWorker(repo, pipeline)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("persist accepted event after privacy change: %v", err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("replay accepted event: %v", err)
	}
	if got := repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike); got != 1 {
		t.Fatalf("duplicate delivery changed durable like count: %d", got)
	}
	if got := repo.ActionEventCountForTest(); got != 1 {
		t.Fatalf("expected one durable event receipt, got %d", got)
	}
}

func TestInteractionAsyncActionEventsKeepNewestStateWhenDeliveredOutOfOrder(t *testing.T) {
	repo := newMemoryInteractionRepo()
	pipeline := newMemoryActionPipeline()
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.LikeWithRecommendation(context.Background(), 7, 1001, "ordered-like", "ordered-like-request"); err != nil {
		t.Fatalf("accept like: %v", err)
	}
	if _, err := service.Unlike(context.Background(), 7, 1001, "ordered-unlike"); err != nil {
		t.Fatalf("accept unlike: %v", err)
	}
	events := pipeline.EventsForTest()
	if len(events) != 2 {
		t.Fatalf("expected two action events, got %+v", events)
	}
	if !domaininteraction.ActionEventComesAfter(
		events[1].Version,
		events[1].OccurredAt,
		events[1].EventID,
		events[0].Version,
		events[0].OccurredAt,
		events[0].EventID,
	) {
		t.Fatalf("service did not emit an ordered unlike after like: %+v", events)
	}
	if events[0].Version != 1 || events[1].Version != 2 {
		t.Fatalf("expected atomic monotonic versions, got %+v", events)
	}

	worker := applicationinteraction.NewActionWorker(repo, nil)
	if err := worker.HandleActionChanged(context.Background(), events[1]); err != nil {
		t.Fatalf("persist newer unlike: %v", err)
	}
	if err := worker.HandleActionChanged(context.Background(), events[0]); err != nil {
		t.Fatalf("acknowledge stale like: %v", err)
	}
	if err := worker.HandleActionChanged(context.Background(), events[1]); err != nil {
		t.Fatalf("acknowledge duplicate unlike: %v", err)
	}

	if repo.ActionActiveForTest(7, 1001, domaininteraction.ActionTypeLike) {
		t.Fatal("stale like reverted the newer unlike")
	}
	if got := repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike); got != 0 {
		t.Fatalf("stale or duplicate event changed the like aggregate: %d", got)
	}
	if got := repo.ActionEventCountForTest(); got != 2 {
		t.Fatalf("expected receipts for both distinct events, got %d", got)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.profileHandoffs) != 2 || repo.profileHandoffs[events[0].EventID] == nil {
		t.Fatalf("stale action was not retained for profile projection: %#v", repo.profileHandoffs)
	}
	if len(repo.outcomeHandoffs) != 1 || repo.outcomeHandoffs[events[0].EventID] == nil {
		t.Fatalf("stale action was not retained for outcome attribution: %#v", repo.outcomeHandoffs)
	}
}

func TestInteractionAsyncPublishFailurePersistsAcceptedEventOnce(t *testing.T) {
	repo := newMemoryInteractionRepo()
	pipeline := newMemoryActionPipeline()
	pipeline.publishErr = errors.New("publish result unknown")
	pipeline.enqueueOnPublishError = true
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	result, err := service.Like(context.Background(), 7, 1001, "publish-fallback-like")
	if err != nil {
		t.Fatalf("persist accepted event after publish failure: %v", err)
	}
	if !result.Active || result.LikeCount != 1 {
		t.Fatalf("unexpected publish fallback result: %+v", result)
	}
	if got := repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike); got != 1 {
		t.Fatalf("publish fallback did not persist durable aggregate: %d", got)
	}
	if got := repo.ActionEventCountForTest(); got != 1 {
		t.Fatalf("publish fallback did not persist one receipt: %d", got)
	}

	pipeline.publishErr = nil
	worker := applicationinteraction.NewActionWorker(repo, pipeline)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("consume ambiguously published duplicate: %v", err)
	}
	if got := repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike); got != 1 {
		t.Fatalf("duplicate delivery changed publish fallback aggregate: %d", got)
	}
}

func TestInteractionAsyncPublishAndPersistenceFailureRollsBackForRetry(t *testing.T) {
	repo := newMemoryInteractionRepo()
	repo.persistErrors = []error{errors.New("database unavailable")}
	pipeline := newMemoryActionPipeline()
	pipeline.publishErr = errors.New("publish failed")
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.Like(context.Background(), 7, 1001, "retryable-like"); !errors.Is(err, applicationinteraction.ErrUpdateInteractionFailed) {
		t.Fatalf("expected failed accepted write, got %v", err)
	}
	pipeline.publishErr = nil
	result, err := service.Like(context.Background(), 7, 1001, "retryable-like")
	if err != nil {
		t.Fatalf("retry like: %v", err)
	}
	if !result.Active || result.LikeCount != 1 {
		t.Fatalf("retry did not restore accepted Redis state: %+v", result)
	}
	events := pipeline.EventsForTest()
	if len(events) != 1 || events[0].Version != 2 {
		t.Fatalf("retry should publish the next monotonic version after rollback: %+v", events)
	}
	worker := applicationinteraction.NewActionWorker(repo, pipeline)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("consume retry event: %v", err)
	}
	if !repo.ActionActiveForTest(7, 1001, domaininteraction.ActionTypeLike) {
		t.Fatal("retry event was not durably persisted")
	}
	if got := repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike); got != 1 {
		t.Fatalf("retry corrupted durable count: %d", got)
	}
}

func TestInteractionAsyncCountReadFailureRollsBackWithDetachedContext(t *testing.T) {
	repo := newMemoryInteractionRepo()
	pipeline := newMemoryActionPipeline()
	countReadErr := errors.New("count read failed")
	pipeline.countReadErrors = []error{countReadErr}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	pipeline.countReadHook = cancelRequest
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.Like(requestCtx, 7, 1001, "count-read-like"); !errors.Is(err, countReadErr) ||
		!errors.Is(err, applicationinteraction.ErrUpdateInteractionFailed) {
		t.Fatalf("expected count-read failure, got %v", err)
	}
	if pipeline.rollbackContextErr != nil {
		t.Fatalf("rollback inherited canceled request context: %v", pipeline.rollbackContextErr)
	}
	if !pipeline.rollbackHasDeadline {
		t.Fatal("rollback compensation context was not bounded")
	}
	if got := pipeline.EventCount(); got != 0 {
		t.Fatalf("rolled-back mutation was published: %d", got)
	}

	result, err := service.Like(context.Background(), 7, 1001, "count-read-like")
	if err != nil {
		t.Fatalf("retry rolled-back mutation: %v", err)
	}
	if !result.Active || result.LikeCount != 1 {
		t.Fatalf("rollback did not restore the pre-mutation state: %+v", result)
	}
	events := pipeline.EventsForTest()
	if len(events) != 1 || events[0].Version != 2 {
		t.Fatalf("retry should publish the next version after rollback: %+v", events)
	}
}

func TestInteractionAsyncCountReadFailurePersistsRecoveryWhenRollbackFails(t *testing.T) {
	repo := newMemoryInteractionRepo()
	pipeline := newMemoryActionPipeline()
	countReadErr := errors.New("count read failed")
	rollbackErr := errors.New("rollback unavailable")
	pipeline.countReadErrors = []error{countReadErr}
	pipeline.rollbackErr = rollbackErr
	pipeline.publishErr = errors.New("recovery publish failed")
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	pipeline.countReadHook = cancelRequest
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.Like(requestCtx, 7, 1001, "recovery-like"); !errors.Is(err, countReadErr) ||
		!errors.Is(err, rollbackErr) ||
		!errors.Is(err, applicationinteraction.ErrUpdateInteractionFailed) {
		t.Fatalf("expected visible count-read and rollback failures, got %v", err)
	}
	if pipeline.rollbackContextErr != nil || !pipeline.rollbackHasDeadline {
		t.Fatalf("unexpected rollback context: err=%v deadline=%v", pipeline.rollbackContextErr, pipeline.rollbackHasDeadline)
	}
	if pipeline.publishContextErr != nil || !pipeline.publishHasDeadline {
		t.Fatalf("unexpected recovery publish context: err=%v deadline=%v", pipeline.publishContextErr, pipeline.publishHasDeadline)
	}
	if repo.persistCtxErr != nil || !repo.persistBounded {
		t.Fatalf("unexpected recovery persistence context: err=%v deadline=%v", repo.persistCtxErr, repo.persistBounded)
	}
	if got := pipeline.EventCount(); got != 0 {
		t.Fatalf("failed recovery publish unexpectedly queued an event: %d", got)
	}
	if got := repo.ActionEventCountForTest(); got != 1 {
		t.Fatalf("rollback and publish failure did not persist one recovery receipt: %d", got)
	}
	if !repo.ActionActiveForTest(7, 1001, domaininteraction.ActionTypeLike) {
		t.Fatal("recovery persistence did not preserve the committed Redis state")
	}
	if got := repo.ActionCountForTest(1001, domaininteraction.ActionTypeLike); got != 1 {
		t.Fatalf("recovery persistence corrupted durable count: %d", got)
	}
}

func TestInteractionAsyncCountReadFailureRetainsOriginalEventForRetry(t *testing.T) {
	repo := newMemoryInteractionRepo()
	persistErr := errors.New("recovery persistence failed")
	repo.persistErrors = []error{persistErr}
	pipeline := newMemoryActionPipeline()
	countReadErr := errors.New("count read failed")
	retryCountReadErr := errors.New("retry count read failed")
	rollbackErr := errors.New("rollback unavailable")
	publishErr := errors.New("recovery publish failed")
	pipeline.countReadErrors = []error{countReadErr, retryCountReadErr}
	pipeline.rollbackErr = rollbackErr
	pipeline.publishErr = publishErr
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.Like(context.Background(), 7, 1001, "retry-recovery-like"); !errors.Is(err, countReadErr) ||
		!errors.Is(err, rollbackErr) ||
		!errors.Is(err, publishErr) ||
		!errors.Is(err, persistErr) {
		t.Fatalf("expected all first recovery failures, got %v", err)
	}
	if got := pipeline.EventCount(); got != 0 {
		t.Fatalf("failed first recovery unexpectedly queued an event: %d", got)
	}
	if got := repo.ActionEventCountForTest(); got != 0 {
		t.Fatalf("failed first recovery unexpectedly persisted an event: %d", got)
	}

	pipeline.publishErr = nil
	if _, err := service.Like(context.Background(), 7, 1001, "retry-recovery-like"); !errors.Is(err, retryCountReadErr) {
		t.Fatalf("expected retry count-read failure after event recovery, got %v", err)
	}
	events := pipeline.EventsForTest()
	if len(events) != 1 || events[0].Version != 1 || events[0].EventID == "" {
		t.Fatalf("retry did not republish the original committed event: %+v", events)
	}

	worker := applicationinteraction.NewActionWorker(repo, pipeline)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("persist retried recovery event: %v", err)
	}
	if !repo.ActionActiveForTest(7, 1001, domaininteraction.ActionTypeLike) {
		t.Fatal("retried recovery event did not persist the committed state")
	}
}

func TestInteractionAsyncCountReadFailureDoesNotRollbackConcurrentNewerMutation(t *testing.T) {
	repo := newMemoryInteractionRepo()
	pipeline := newMemoryActionPipeline()
	countReadErr := errors.New("count read failed")
	pipeline.countReadErrors = []error{countReadErr}
	pipeline.rollbackHook = func() {
		_, err := pipeline.SetActionState(
			context.Background(),
			7,
			1001,
			domaininteraction.ActionTypeLike,
			false,
			"newer-unlike",
			&domaininteraction.VideoStat{VideoID: 1001},
			&domaininteraction.ActionStateSnapshot{},
			applicationinteraction.ActionMutation{EventID: "newer-unlike-event", OccurredAt: time.Now().UTC().Add(time.Second)},
		)
		if err != nil {
			t.Fatalf("create concurrent newer mutation: %v", err)
		}
	}
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.Like(context.Background(), 7, 1001, "failed-count-like"); !errors.Is(err, countReadErr) {
		t.Fatalf("expected failed older count read, got %v", err)
	}
	if got := pipeline.EventCount(); got != 0 {
		t.Fatalf("superseded older mutation was recovered: %d", got)
	}

	result, err := service.Unlike(context.Background(), 7, 1001, "newer-unlike")
	if err != nil {
		t.Fatalf("publish concurrent newer mutation: %v", err)
	}
	if result.Active || result.LikeCount != 0 {
		t.Fatalf("older compensation overwrote newer state: %+v", result)
	}
	events := pipeline.EventsForTest()
	if len(events) != 1 || events[0].Version != 2 || events[0].Active {
		t.Fatalf("expected only the newer unlike event, got %+v", events)
	}
}

func TestInteractionAsyncFailureDoesNotRollbackConcurrentNewerMutation(t *testing.T) {
	repo := newMemoryInteractionRepo()
	repo.persistErrors = []error{errors.New("database unavailable")}
	pipeline := newMemoryActionPipeline()
	pipeline.publishErr = errors.New("publish failed")
	pipeline.rollbackHook = func() {
		_, err := pipeline.SetActionState(
			context.Background(),
			7,
			1001,
			domaininteraction.ActionTypeLike,
			false,
			"newer-unlike",
			&domaininteraction.VideoStat{VideoID: 1001},
			&domaininteraction.ActionStateSnapshot{},
			applicationinteraction.ActionMutation{EventID: "newer-unlike-event", OccurredAt: time.Now().UTC().Add(time.Second)},
		)
		if err != nil {
			t.Fatalf("create concurrent newer mutation: %v", err)
		}
	}
	service := applicationinteraction.New(
		repo,
		applicationinteraction.WithAsyncActionPipeline(pipeline, pipeline),
	)

	if _, err := service.Like(context.Background(), 7, 1001, "failed-like"); !errors.Is(err, applicationinteraction.ErrUpdateInteractionFailed) {
		t.Fatalf("expected failed older mutation, got %v", err)
	}
	pipeline.publishErr = nil
	result, err := service.Unlike(context.Background(), 7, 1001, "newer-unlike")
	if err != nil {
		t.Fatalf("publish newer mutation: %v", err)
	}
	if result.Active || result.LikeCount != 0 {
		t.Fatalf("older rollback overwrote newer mutation: %+v", result)
	}
	events := pipeline.EventsForTest()
	if len(events) != 1 || events[0].Version != 2 || events[0].Active {
		t.Fatalf("expected only the newer unlike event, got %+v", events)
	}
	worker := applicationinteraction.NewActionWorker(repo, pipeline)
	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("consume newer mutation: %v", err)
	}
	if repo.ActionActiveForTest(7, 1001, domaininteraction.ActionTypeLike) {
		t.Fatal("durable action did not keep newer unlike")
	}
}

// newInteractionRouter 只装配互动相关 RESTful 路由。
func newInteractionRouter(t *testing.T) (*server.Hertz, *infrajwt.Manager) {
	t.Helper()
	router, jwtManager, _ := newInteractionRouterWithRepo(t)
	return router, jwtManager
}

func newInteractionRouterWithRepo(t *testing.T) (*server.Hertz, *infrajwt.Manager, *memoryInteractionRepo) {
	t.Helper()

	router := server.New()

	jwtManager, err := infrajwt.NewManager("test-secret", "15m")
	if err != nil {
		t.Fatalf("new jwt manager: %v", err)
	}

	repo := newMemoryInteractionRepo()
	service := applicationinteraction.New(repo)
	handler := interfaceshttpinteraction.New(service)
	authMiddleware := interfaceshttpmiddleware.NewJWTAuth(jwtManager)

	api := router.Group("/api")
	videos := api.Group("/videos")
	videos.PUT("/:videoId/like", authMiddleware, handler.Like)
	videos.DELETE("/:videoId/like", authMiddleware, handler.Unlike)
	videos.PUT("/:videoId/favorite", authMiddleware, handler.Favorite)
	videos.DELETE("/:videoId/favorite", authMiddleware, handler.Unfavorite)
	videos.POST("/:videoId/comments", authMiddleware, handler.CreateComment)
	videos.GET("/:videoId/comments", handler.ListComments)
	api.DELETE("/comments/:commentId", authMiddleware, handler.DeleteComment)

	return router, jwtManager, repo
}

// videoPublished 模拟互动前校验视频是否可互动。
func (r *memoryInteractionRepo) videoPublished(videoID int64) bool {
	video, exists := r.videos[videoID]
	return exists &&
		video.Status == domainvideo.StatusPublished &&
		video.Visibility == domainvideo.VisibilityPublic
}

func (r *memoryInteractionRepo) setVideoVisibilityForTest(videoID int64, visibility string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	video := r.videos[videoID]
	video.Visibility = visibility
	r.videos[videoID] = video
}

func (r *memoryInteractionRepo) setVideoStatusForTest(videoID int64, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	video := r.videos[videoID]
	video.Status = status
	r.videos[videoID] = video
}

// addActionCount 根据行为类型增加或减少点赞/收藏数。
func (r *memoryInteractionRepo) addActionCount(videoID int64, actionType string, delta int) {
	stat := r.stats[videoID]
	if actionType == domaininteraction.ActionTypeLike {
		stat.LikeCount = clampMemoryCount(stat.LikeCount + delta)
	} else {
		stat.FavoriteCount = clampMemoryCount(stat.FavoriteCount + delta)
	}
	r.stats[videoID] = stat
}

// actionCount 根据行为类型返回当前点赞数或收藏数。
func (r *memoryInteractionRepo) actionCount(videoID int64, actionType string) int {
	stat := r.stats[videoID]
	if actionType == domaininteraction.ActionTypeLike {
		return stat.LikeCount
	}
	return stat.FavoriteCount
}

func (r *memoryInteractionRepo) ActionCountForTest(videoID int64, actionType string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actionCount(videoID, actionType)
}

func (r *memoryInteractionRepo) ActionEventCountForTest() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.actionEvents)
}

func (r *memoryInteractionRepo) ActionReceiptCountForTest() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.actionReceipts)
}

func (r *memoryInteractionRepo) ActionActiveForTest(userID int64, videoID int64, actionType string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	action := r.actions[memoryInteractionActionKey(userID, videoID, actionType)]
	return action != nil && action.Active()
}

func sameMemoryAcceptedActionEvent(left *domaininteraction.AcceptedActionEvent, right *domaininteraction.AcceptedActionEvent) bool {
	return left.EventID == right.EventID &&
		left.UserID == right.UserID &&
		left.VideoID == right.VideoID &&
		left.ActionType == right.ActionType &&
		left.Active == right.Active &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.RecommendationRequestID == right.RecommendationRequestID &&
		left.Version == right.Version &&
		left.OccurredAt.Equal(right.OccurredAt)
}

// memoryInteractionActionKey 模拟 user_id + video_id + action_type 唯一索引。
func memoryInteractionActionKey(userID int64, videoID int64, actionType string) string {
	return strings.Join([]string{int64String(userID), int64String(videoID), actionType}, ":")
}

func memoryInteractionActionReceiptKey(userID int64, videoID int64, actionType string, idempotencyKey string) string {
	return memoryInteractionActionKey(userID, videoID, actionType) + ":" + strings.TrimSpace(idempotencyKey)
}

// memoryInteractionCommentIdemKey 模拟 user_id + idempotency_key 唯一索引。
func memoryInteractionCommentIdemKey(userID int64, idempotencyKey string) string {
	return int64String(userID) + ":" + strings.TrimSpace(idempotencyKey)
}

// int64String 统一测试里 int64 ID 的字符串转换。
func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}

// memoryInteractionNickname 为测试评论补齐用户昵称。
func memoryInteractionNickname(userID int64) string {
	if userID == 77 {
		return "user-77"
	}
	return "user"
}

// memoryInteractionAvatar 为测试评论补齐用户头像。
func memoryInteractionAvatar(userID int64) string {
	if userID == 77 {
		return "https://example.com/avatar-77.jpg"
	}
	return ""
}

// cloneInteractionAction 返回互动行为副本，隔离仓储内部状态。
func cloneInteractionAction(action *domaininteraction.Action) *domaininteraction.Action {
	cloned := *action
	return &cloned
}

// cloneInteractionComment 返回评论副本，隔离仓储内部状态。
func cloneInteractionComment(comment *domaininteraction.Comment) *domaininteraction.Comment {
	cloned := *comment
	return &cloned
}

// clampMemoryCount 防止测试仓储中的计数被减成负数。
func clampMemoryCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func newMemoryHotScoreRecorder() *memoryHotScoreRecorder {
	return &memoryHotScoreRecorder{
		scores: map[int64]int{},
		events: []int{},
	}
}

func (r *memoryHotScoreRecorder) AddHotScore(ctx context.Context, videoID int64, scoreDelta int, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.scores[videoID] += scoreDelta
	r.events = append(r.events, scoreDelta)
	return nil
}

func (r *memoryHotScoreRecorder) Score(videoID int64) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scores[videoID]
}

func (r *memoryHotScoreRecorder) EventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func newMemoryActionPipeline() *memoryActionPipeline {
	return &memoryActionPipeline{
		states:          map[string]*applicationinteraction.ActionStateResult{},
		receipts:        map[string]map[string]bool{},
		stats:           map[int64]memoryInteractionStat{},
		events:          []*applicationinteraction.ActionChangedEvent{},
		versionCounters: map[string]int64{},
	}
}

func newMemoryInteractionMessageWriter() *memoryInteractionMessageWriter {
	return &memoryInteractionMessageWriter{messages: []memoryInteractionMessage{}}
}

func (w *memoryInteractionMessageWriter) CreateFromEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.messages = append(w.messages, memoryInteractionMessage{
		UserID:  userID,
		Type:    messageType,
		Title:   title,
		EventID: eventID,
	})
	return nil, nil
}

func (w *memoryInteractionMessageWriter) CreateFromActorEvent(ctx context.Context, userID int64, messageType string, title string, content string, eventID string, idempotencyKey string, actorID int64, actorNickname string, actorAvatarURL string) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.messages = append(w.messages, memoryInteractionMessage{
		UserID:         userID,
		Type:           messageType,
		Title:          title,
		EventID:        eventID,
		ActorID:        actorID,
		ActorNickname:  actorNickname,
		ActorAvatarURL: actorAvatarURL,
	})
	return nil, nil
}

func (w *memoryInteractionMessageWriter) Messages() []memoryInteractionMessage {
	w.mu.Lock()
	defer w.mu.Unlock()

	items := make([]memoryInteractionMessage, len(w.messages))
	copy(items, w.messages)
	return items
}

func (p *memoryActionPipeline) SetActionState(ctx context.Context, userID int64, videoID int64, actionType string, active bool, idempotencyKey string, initialStat *domaininteraction.VideoStat, initialState *domaininteraction.ActionStateSnapshot, mutation applicationinteraction.ActionMutation) (*applicationinteraction.ActionStateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := memoryInteractionActionKey(userID, videoID, actionType)
	state := p.states[key]
	if _, exists := p.stats[videoID]; !exists && initialStat != nil {
		p.stats[videoID] = memoryInteractionStat{
			LikeCount:     initialStat.LikeCount,
			CommentCount:  initialStat.CommentCount,
			FavoriteCount: initialStat.FavoriteCount,
		}
	}
	previous := domaininteraction.ActionStateSnapshot{}
	if state != nil {
		previous = domaininteraction.ActionStateSnapshot{
			Exists:                  true,
			Active:                  state.Active,
			IdempotencyKey:          state.IdempotencyKey,
			RecommendationRequestID: state.RecommendationRequestID,
			Version:                 state.Version,
			EventID:                 state.EventID,
			OccurredAt:              state.OccurredAt,
			UpdatedAt:               state.OccurredAt,
		}
	} else if initialState != nil {
		previous = *initialState
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if receiptPayload, found := p.receipts[key][idempotencyKey]; found {
		if receiptPayload != active {
			return nil, domaininteraction.ErrActionIdempotencyConflict
		}
		result := &applicationinteraction.ActionStateResult{
			UserID:         userID,
			VideoID:        videoID,
			ActionType:     actionType,
			Active:         previous.Active,
			IdempotencyKey: idempotencyKey,
			Version:        previous.Version,
		}
		stat := p.stats[videoID]
		result.LikeCount = stat.LikeCount
		result.FavoriteCount = stat.FavoriteCount
		return p.finishActionStateResultLocked(result)
	}
	if previous.Exists && idempotencyKey != "" && previous.IdempotencyKey == idempotencyKey {
		if previous.Active != active {
			return nil, domaininteraction.ErrActionIdempotencyConflict
		}
		result := &applicationinteraction.ActionStateResult{
			UserID:                  userID,
			VideoID:                 videoID,
			ActionType:              actionType,
			Active:                  previous.Active,
			IdempotencyKey:          previous.IdempotencyKey,
			RecommendationRequestID: previous.RecommendationRequestID,
			Version:                 previous.Version,
			EventID:                 previous.EventID,
			OccurredAt:              previous.OccurredAt,
			ShouldPublish:           state != nil && previous.EventID != "",
		}
		stat := p.stats[videoID]
		result.LikeCount = stat.LikeCount
		result.FavoriteCount = stat.FavoriteCount
		return p.finishActionStateResultLocked(result)
	}

	delta := 0
	if !previous.Exists {
		if active {
			delta = 1
		}
	} else if previous.Active != active {
		if active {
			delta = 1
		} else {
			delta = -1
		}
	}
	if initialState != nil && initialState.Version > p.versionCounters[key] {
		p.versionCounters[key] = initialState.Version
	}
	if delta == 0 {
		if idempotencyKey != "" {
			if p.receipts[key] == nil {
				p.receipts[key] = map[string]bool{}
			}
			p.receipts[key][idempotencyKey] = active
		}
		result := &applicationinteraction.ActionStateResult{
			UserID:                  userID,
			VideoID:                 videoID,
			ActionType:              actionType,
			Active:                  previous.Active,
			IdempotencyKey:          idempotencyKey,
			RecommendationRequestID: previous.RecommendationRequestID,
			Version:                 previous.Version,
			EventID:                 previous.EventID,
			OccurredAt:              previous.OccurredAt,
		}
		stat := p.stats[videoID]
		result.LikeCount = stat.LikeCount
		result.FavoriteCount = stat.FavoriteCount
		if state == nil {
			p.states[key] = cloneActionStateResult(result)
		}
		return p.finishActionStateResultLocked(result)
	}
	p.versionCounters[key]++

	stat := p.stats[videoID]
	if actionType == domaininteraction.ActionTypeLike {
		stat.LikeCount = clampMemoryCount(stat.LikeCount + delta)
	} else {
		stat.FavoriteCount = clampMemoryCount(stat.FavoriteCount + delta)
	}
	p.stats[videoID] = stat

	result := &applicationinteraction.ActionStateResult{
		UserID:                  userID,
		VideoID:                 videoID,
		ActionType:              actionType,
		Active:                  active,
		LikeCount:               stat.LikeCount,
		FavoriteCount:           stat.FavoriteCount,
		Delta:                   delta,
		IdempotencyKey:          strings.TrimSpace(idempotencyKey),
		RecommendationRequestID: strings.TrimSpace(mutation.RecommendationRequestID),
		Version:                 p.versionCounters[key],
		EventID:                 mutation.EventID,
		OccurredAt:              mutation.OccurredAt,
		ShouldPublish:           delta != 0,
		CanRollback:             true,
		Previous:                previous,
	}
	p.states[key] = result
	return p.finishActionStateResultLocked(result)
}

func (p *memoryActionPipeline) RollbackActionState(ctx context.Context, state *applicationinteraction.ActionStateResult) (bool, error) {
	p.mu.Lock()
	hook := p.rollbackHook
	p.rollbackHook = nil
	rollbackErr := p.rollbackErr
	p.rollbackContextErr = ctx.Err()
	_, p.rollbackHasDeadline = ctx.Deadline()
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	if rollbackErr != nil {
		return false, rollbackErr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := memoryInteractionActionKey(state.UserID, state.VideoID, state.ActionType)
	current := p.states[key]
	if current == nil || current.Version != state.Version || current.EventID != state.EventID {
		return false, nil
	}
	stat := p.stats[state.VideoID]
	if state.ActionType == domaininteraction.ActionTypeLike {
		stat.LikeCount = clampMemoryCount(stat.LikeCount - state.Delta)
	} else {
		stat.FavoriteCount = clampMemoryCount(stat.FavoriteCount - state.Delta)
	}
	p.stats[state.VideoID] = stat
	if state.Previous.Exists {
		p.states[key] = &applicationinteraction.ActionStateResult{
			UserID:                  state.UserID,
			VideoID:                 state.VideoID,
			ActionType:              state.ActionType,
			Active:                  state.Previous.Active,
			LikeCount:               stat.LikeCount,
			FavoriteCount:           stat.FavoriteCount,
			IdempotencyKey:          state.Previous.IdempotencyKey,
			RecommendationRequestID: state.Previous.RecommendationRequestID,
			Version:                 state.Previous.Version,
			EventID:                 state.Previous.EventID,
			OccurredAt:              state.Previous.OccurredAt,
		}
	} else {
		delete(p.states, key)
	}
	return true, nil
}

func (p *memoryActionPipeline) PublishActionChanged(ctx context.Context, event *applicationinteraction.ActionChangedEvent) error {
	p.mu.Lock()
	cloned := *event
	p.publishContextErr = ctx.Err()
	_, p.publishHasDeadline = ctx.Deadline()
	err := p.publishErr
	if err == nil || p.enqueueOnPublishError {
		p.events = append(p.events, &cloned)
	}
	p.mu.Unlock()
	return err
}

func (p *memoryActionPipeline) finishActionStateResultLocked(result *applicationinteraction.ActionStateResult) (*applicationinteraction.ActionStateResult, error) {
	if len(p.countReadErrors) == 0 {
		return cloneActionStateResult(result), nil
	}
	err := p.countReadErrors[0]
	p.countReadErrors = p.countReadErrors[1:]
	if p.countReadHook != nil {
		hook := p.countReadHook
		p.countReadHook = nil
		hook()
	}
	return cloneActionStateResult(result), err
}

func (p *memoryActionPipeline) ConsumeActionChanged(ctx context.Context, handler func(context.Context, *applicationinteraction.ActionChangedEvent) error) error {
	p.mu.Lock()
	events := make([]*applicationinteraction.ActionChangedEvent, 0, len(p.events))
	for _, event := range p.events {
		cloned := *event
		events = append(events, &cloned)
	}
	p.mu.Unlock()

	for _, event := range events {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (p *memoryActionPipeline) EventCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

func (p *memoryActionPipeline) EventsForTest() []*applicationinteraction.ActionChangedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := make([]*applicationinteraction.ActionChangedEvent, 0, len(p.events))
	for _, event := range p.events {
		cloned := *event
		events = append(events, &cloned)
	}
	return events
}

func cloneActionStateResult(result *applicationinteraction.ActionStateResult) *applicationinteraction.ActionStateResult {
	cloned := *result
	return &cloned
}

var _ domaininteraction.Repository = (*memoryInteractionRepo)(nil)
