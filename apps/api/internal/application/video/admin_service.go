package applicationvideo

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	applicationadminaudit "github.com/shiyudesu/frux/internal/application/adminaudit"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
)

const adminVideoCursorVersion = 1

type AdminService struct {
	repository   domainvideo.AdminRepository
	cursorSecret []byte
	now          func() time.Time
}

type AdminOption func(*AdminService)

type AdminVideoSearchRequest struct {
	Status      int
	AuthorID    int64
	VideoID     int64
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Cursor      string
	Limit       int
}

type AdminVideoPage struct {
	Items      []*domainvideo.Video
	NextCursor string
	HasMore    bool
}

type AdminEnforcementRequest struct {
	VideoID         int64
	ActorID         int64
	ExpectedVersion int
	ReasonCode      string
	Note            string
}

type adminVideoCursorEnvelope struct {
	Version    int    `json:"v"`
	FilterHash string `json:"f"`
	CreatedAt  string `json:"t"`
	VideoID    int64  `json:"id"`
}

func NewAdmin(
	repository domainvideo.AdminRepository,
	cursorSecret string,
	options ...AdminOption,
) *AdminService {
	service := &AdminService{
		repository:   repository,
		cursorSecret: []byte(strings.TrimSpace(cursorSecret)),
		now:          func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func WithAdminClock(now func() time.Time) AdminOption {
	return func(service *AdminService) {
		if now != nil {
			service.now = now
		}
	}
}

func (s *AdminService) Search(
	ctx context.Context,
	request AdminVideoSearchRequest,
) (*AdminVideoPage, error) {
	if s == nil || s.repository == nil || len(s.cursorSecret) == 0 {
		return nil, ErrLoadVideoFailed
	}
	query, filterHash, err := normalizeAdminSearchRequest(request)
	if err != nil {
		return nil, err
	}
	query.Cursor, err = s.decodeCursor(request.Cursor, filterHash)
	if err != nil {
		return nil, err
	}
	items, err := s.repository.ListAdminVideos(ctx, query)
	if err != nil {
		if errors.Is(err, domainvideo.ErrInvalidCursor) {
			return nil, err
		}
		return nil, ErrLoadVideoFailed
	}
	limit := query.Limit - 1
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = s.encodeCursor(filterHash, &domainvideo.AdminVideoCursor{
			CreatedAt: last.CreatedAt,
			VideoID:   last.ID,
		})
	}
	return &AdminVideoPage{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *AdminService) TakeDown(
	ctx context.Context,
	request AdminEnforcementRequest,
) (*domainvideo.AdminTransitionResult, error) {
	return s.transition(ctx, request, domainvideo.LifecycleTakeOffline)
}

func (s *AdminService) Restore(
	ctx context.Context,
	request AdminEnforcementRequest,
) (*domainvideo.AdminTransitionResult, error) {
	return s.transition(ctx, request, domainvideo.LifecycleRestore)
}

func (s *AdminService) transition(
	ctx context.Context,
	request AdminEnforcementRequest,
	transition domainvideo.LifecycleTransition,
) (*domainvideo.AdminTransitionResult, error) {
	if s == nil || s.repository == nil {
		return nil, ErrUpdateVideoFailed
	}
	command, err := domainvideo.NormalizeAdminTransition(domainvideo.AdminTransitionCommand{
		VideoID: request.VideoID, ActorID: request.ActorID,
		ExpectedVersion: request.ExpectedVersion, Transition: transition,
		ReasonCode: request.ReasonCode, Note: request.Note, OccurredAt: s.now(),
	})
	if err != nil {
		return nil, err
	}
	action := domainadminaudit.ActionContentEnforce
	route := "/api/admin/videos/:videoId/enforcement"
	previousStatus, newStatus := "published", "offline"
	if transition == domainvideo.LifecycleRestore {
		action = domainadminaudit.ActionContentRestore
		route = "/api/admin/videos/:videoId/restoration"
		previousStatus, newStatus = "offline", "published"
	}
	fact, err := applicationadminaudit.BuildSuccessFact(applicationadminaudit.BuildInput{
		ActorID: command.ActorID, Permission: domainaccount.PermissionContentEnforce,
		Action: action, TargetType: domainadminaudit.TargetVideo,
		TargetID:  strconv.FormatInt(command.VideoID, 10),
		RequestID: domainadminaudit.NewRequestID(),
		Detail: map[string]string{
			"http_method": "POST", "previous_status": previousStatus,
			"new_status": newStatus, "reason_code": command.ReasonCode, "route": route,
		},
	}, command.OccurredAt)
	if err != nil {
		return nil, err
	}
	result, err := s.repository.CommitAdminTransition(ctx, command, fact)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Video == nil {
		return nil, ErrUpdateVideoFailed
	}
	return result, nil
}

func normalizeAdminSearchRequest(
	request AdminVideoSearchRequest,
) (domainvideo.AdminVideoQuery, string, error) {
	limit := request.Limit
	if limit == 0 {
		limit = 20
	}
	query, err := domainvideo.NormalizeAdminVideoQuery(domainvideo.AdminVideoQuery{
		Status: request.Status, AuthorID: request.AuthorID, VideoID: request.VideoID,
		Keyword: request.Keyword, CreatedFrom: request.CreatedFrom, CreatedTo: request.CreatedTo,
		Limit: limit + 1,
	})
	if err != nil {
		return domainvideo.AdminVideoQuery{}, "", err
	}
	filter := struct {
		Status      int    `json:"status"`
		AuthorID    int64  `json:"author_id"`
		VideoID     int64  `json:"video_id"`
		Keyword     string `json:"keyword"`
		CreatedFrom string `json:"created_from"`
		CreatedTo   string `json:"created_to"`
	}{
		Status: query.Status, AuthorID: query.AuthorID, VideoID: query.VideoID,
		Keyword: query.Keyword,
	}
	if query.CreatedFrom != nil {
		filter.CreatedFrom = query.CreatedFrom.UTC().Format(time.RFC3339Nano)
		filter.CreatedTo = query.CreatedTo.UTC().Format(time.RFC3339Nano)
	}
	payload, _ := json.Marshal(filter)
	sum := sha256.Sum256(payload)
	return query, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func (s *AdminService) encodeCursor(
	filterHash string,
	cursor *domainvideo.AdminVideoCursor,
) string {
	payload, err := json.Marshal(adminVideoCursorEnvelope{
		Version: adminVideoCursorVersion, FilterHash: filterHash,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano), VideoID: cursor.VideoID,
	})
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, s.cursorSecret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *AdminService) decodeCursor(
	raw, filterHash string,
) (*domainvideo.AdminVideoCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, domainvideo.ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, domainvideo.ErrInvalidCursor
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, domainvideo.ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, s.cursorSecret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, domainvideo.ErrInvalidCursor
	}
	var envelope adminVideoCursorEnvelope
	if json.Unmarshal(payload, &envelope) != nil ||
		envelope.Version != adminVideoCursorVersion ||
		envelope.FilterHash != filterHash ||
		envelope.VideoID <= 0 {
		return nil, domainvideo.ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
	if err != nil {
		return nil, domainvideo.ErrInvalidCursor
	}
	return &domainvideo.AdminVideoCursor{CreatedAt: createdAt.UTC(), VideoID: envelope.VideoID}, nil
}
