package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	applicationvideo "github.com/shiyudesu/frux/internal/application/video"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	interfaceshttpvideo "github.com/shiyudesu/frux/internal/interfaces/http/video"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type adminVideoAPIMemoryRepo struct {
	mu     sync.Mutex
	videos map[int64]*domainvideo.Video
}

func (r *adminVideoAPIMemoryRepo) ListAdminVideos(
	_ context.Context,
	query domainvideo.AdminVideoQuery,
) ([]*domainvideo.Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*domainvideo.Video, 0, len(r.videos))
	for _, video := range r.videos {
		if query.Status != 0 && video.Status != query.Status {
			continue
		}
		if query.AuthorID > 0 && video.AuthorID != query.AuthorID {
			continue
		}
		if query.VideoID > 0 && video.ID != query.VideoID {
			continue
		}
		copyVideo := *video
		items = append(items, &copyVideo)
	}
	return items, nil
}

func (r *adminVideoAPIMemoryRepo) CommitAdminTransition(
	_ context.Context,
	command domainvideo.AdminTransitionCommand,
	_ *domainadminaudit.Fact,
) (*domainvideo.AdminTransitionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	video := r.videos[command.VideoID]
	if video == nil {
		return nil, domainvideo.ErrVideoNotFound
	}
	if video.Version != command.ExpectedVersion {
		return nil, domainvideo.ErrVideoVersionConflict
	}
	previous := video.Status
	if err := video.ApplyLifecycleTransition(command.Transition, command.OccurredAt); err != nil {
		return nil, err
	}
	video.Version++
	copyVideo := *video
	return &domainvideo.AdminTransitionResult{
		Video: &copyVideo, PreviousStatus: previous,
	}, nil
}

type adminVideoPrincipalReader struct {
	principal *domainaccount.AdminPrincipal
}

func (r adminVideoPrincipalReader) FindAdminPrincipalByID(
	context.Context,
	int64,
) (*domainaccount.AdminPrincipal, error) {
	if r.principal == nil {
		return nil, errors.New("missing principal")
	}
	return r.principal, nil
}

func newAdminVideoAPIRouter(
	repository *adminVideoAPIMemoryRepo,
	principal *domainaccount.AdminPrincipal,
) *server.Hertz {
	service := applicationvideo.NewAdmin(repository, "api-cursor-secret")
	handler := interfaceshttpvideo.NewAdmin(service)
	router := server.New(server.WithDisablePrintRoute(true))
	auth := func(ctx context.Context, c *app.RequestContext) {
		c.Set(interfaceshttpmiddleware.ContextUserIDKey, principal.UserID)
		c.Next(ctx)
	}
	reader := adminVideoPrincipalReader{principal: principal}
	require := interfaceshttpmiddleware.NewRequireAdminPermission(
		reader, domainaccount.PermissionContentEnforce,
	)
	router.GET("/api/admin/videos", auth, require, handler.Search)
	router.POST("/api/admin/videos/:videoId/enforcement", auth, require, handler.TakeDown)
	router.POST("/api/admin/videos/:videoId/restoration", auth, require, handler.Restore)
	return router
}

func TestAdminVideoAPIFlow(t *testing.T) {
	now := time.Now().UTC()
	repository := &adminVideoAPIMemoryRepo{videos: map[int64]*domainvideo.Video{
		1: {
			ID: 1, AuthorID: 11, Title: "published", Status: domainvideo.StatusPublished,
			Version: 3, ReviewVersion: 1, Visibility: domainvideo.VisibilityPublic,
			PublishedAt: &now, CreatedAt: now, UpdatedAt: now,
		},
		2: {
			ID: 2, AuthorID: 12, Title: "rejected", Status: domainvideo.StatusRejected,
			Version: 1, ReviewVersion: 1, Visibility: domainvideo.VisibilityPublic,
			CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
		},
	}}
	operator := domainaccount.RestoreAdminPrincipal(
		8, domainaccount.StatusNormal, domainaccount.RoleOperator,
	)
	router := newAdminVideoAPIRouter(repository, operator)

	search := ut.PerformRequest(
		router.Engine, http.MethodGet, "/api/admin/videos?status=rejected&author_id=12", nil,
	)
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"id":2`) ||
		strings.Contains(search.Body.String(), `"id":1`) {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}
	takedown := performAdminVideoJSON(
		router, http.MethodPost, "/api/admin/videos/1/enforcement",
		`{"reason_code":"policy_violation","note":"confirmed","expected_version":3}`,
	)
	if takedown.Code != http.StatusOK ||
		!strings.Contains(takedown.Body.String(), `"status_name":"offline"`) ||
		!strings.Contains(takedown.Body.String(), `"audit_committed":true`) {
		t.Fatalf("takedown status=%d body=%s", takedown.Code, takedown.Body.String())
	}
	stale := performAdminVideoJSON(
		router, http.MethodPost, "/api/admin/videos/1/restoration",
		`{"reason_code":"compliance_restored","note":"","expected_version":3}`,
	)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
	restored := performAdminVideoJSON(
		router, http.MethodPost, "/api/admin/videos/1/restoration",
		`{"reason_code":"compliance_restored","note":"appeal accepted","expected_version":4}`,
	)
	if restored.Code != http.StatusOK ||
		!strings.Contains(restored.Body.String(), `"status_name":"published"`) {
		t.Fatalf("restore status=%d body=%s", restored.Code, restored.Body.String())
	}

	user := domainaccount.RestoreAdminPrincipal(
		9, domainaccount.StatusNormal, domainaccount.RoleUser,
	)
	forbiddenRouter := newAdminVideoAPIRouter(repository, user)
	forbidden := ut.PerformRequest(
		forbiddenRouter.Engine, http.MethodGet, "/api/admin/videos", nil,
	)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("forbidden status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(forbidden.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "ADMIN_PERMISSION_DENIED" {
		t.Fatalf("forbidden body=%s", forbidden.Body.String())
	}
}

func performAdminVideoJSON(
	router *server.Hertz,
	method, path, body string,
) *ut.ResponseRecorder {
	return ut.PerformRequest(
		router.Engine,
		method,
		path,
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
}
