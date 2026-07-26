package test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	applicationmedia "GCFeed/internal/application/media"
	domainmedia "GCFeed/internal/domain/media"
	inframedia "GCFeed/internal/infra/media"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	interfaceshttpupload "GCFeed/internal/interfaces/http/upload"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestMediaUploadSessionAPIFlow(t *testing.T) {
	now := time.Date(2026, 7, 26, 6, 0, 0, 0, time.UTC)
	repo := newUploadSessionRepository()
	store := &uploadSessionStore{}
	service := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, 15*time.Minute, "v1", 5,
		applicationmedia.WithNow(func() time.Time { return now }),
		applicationmedia.WithIDGenerator(func() (string, error) { return "session-1", nil }),
	)
	router := newUploadSessionRouter(service, 42)
	checksum := strings.Repeat("a", 64)

	create := performUploadSessionJSON(router, http.MethodPost, "/api/upload-sessions",
		`{"kind":"video","filename":"clip.mp4","content_type":"video/mp4","size_bytes":128,"checksum_sha256":"`+checksum+`"}`,
		ut.Header{Key: "Idempotency-Key", Value: "upload-1"},
	)
	requireStatus(t, create, http.StatusCreated)
	var created struct {
		ID     string `json:"id"`
		Mode   string `json:"mode"`
		Upload struct {
			URL string `json:"url"`
		} `json:"upload"`
	}
	decodeJSON(t, create, &created)
	if created.ID != "session-1" || created.Mode != "direct" || created.Upload.URL == "" {
		t.Fatalf("unexpected upload session response: %+v", created)
	}
	session := repo.sessions[created.ID]
	store.metadata = &domainmedia.ObjectMetadata{
		Key: session.ObjectKey, ContentType: session.ContentType, SizeBytes: session.SizeBytes,
		ChecksumSHA256: session.ChecksumSHA256,
	}

	complete := performUploadSessionJSON(router, http.MethodPost, "/api/upload-sessions/session-1/complete", "")
	requireStatus(t, complete, http.StatusOK)
	var completed struct {
		Asset struct {
			ID int64 `json:"id"`
		} `json:"asset"`
		Replayed bool `json:"replayed"`
	}
	decodeJSON(t, complete, &completed)
	if completed.Asset.ID == 0 || completed.Replayed || repo.job == nil {
		t.Fatalf("unexpected completion response: %+v job=%+v", completed, repo.job)
	}

	replay := performUploadSessionJSON(router, http.MethodPost, "/api/upload-sessions/session-1/complete", "")
	requireStatus(t, replay, http.StatusOK)
	decodeJSON(t, replay, &completed)
	if !completed.Replayed {
		t.Fatal("expected completion replay")
	}
}

func TestMediaUploadSessionRejectsExpiryOwnerAndChecksumMismatch(t *testing.T) {
	base := time.Date(2026, 7, 26, 6, 0, 0, 0, time.UTC)
	now := base
	repo := newUploadSessionRepository()
	store := &uploadSessionStore{}
	id := 0
	service := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, time.Minute, "v1", 5,
		applicationmedia.WithNow(func() time.Time { return now }),
		applicationmedia.WithIDGenerator(func() (string, error) {
			id++
			return "session-" + string(rune('0'+id)), nil
		}),
	)
	ownerRouter := newUploadSessionRouter(service, 42)
	checksum := strings.Repeat("b", 64)
	create := performUploadSessionJSON(ownerRouter, http.MethodPost, "/api/upload-sessions",
		`{"kind":"video","filename":"clip.mp4","content_type":"video/mp4","size_bytes":64,"checksum_sha256":"`+checksum+`"}`,
	)
	requireStatus(t, create, http.StatusCreated)
	var payload struct {
		ID string `json:"id"`
	}
	decodeJSON(t, create, &payload)
	session := repo.sessions[payload.ID]
	store.metadata = &domainmedia.ObjectMetadata{
		Key: session.ObjectKey, ContentType: session.ContentType, SizeBytes: session.SizeBytes,
		ChecksumSHA256: strings.Repeat("c", 64),
	}
	mismatch := performUploadSessionJSON(ownerRouter, http.MethodPost, "/api/upload-sessions/"+payload.ID+"/complete", "")
	requireStatus(t, mismatch, http.StatusConflict)

	store.metadata.ChecksumSHA256 = checksum
	otherOwner := newUploadSessionRouter(service, 7)
	forbidden := performUploadSessionJSON(otherOwner, http.MethodPost, "/api/upload-sessions/"+payload.ID+"/complete", "")
	requireStatus(t, forbidden, http.StatusConflict)

	now = base.Add(2 * time.Minute)
	expired := performUploadSessionJSON(ownerRouter, http.MethodPost, "/api/upload-sessions/"+payload.ID+"/complete", "")
	requireStatus(t, expired, http.StatusConflict)
}

func TestProtectedMediaAssetAccessIsOwnerBoundAndExpires(t *testing.T) {
	repo := newUploadSessionRepository()
	repo.assets[501] = &domainmedia.MediaAsset{
		ID: 501, OwnerID: 42, Kind: domainmedia.AssetKindVideo,
		StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/42/source.mp4",
		State: domainmedia.AssetStateReady,
	}
	store := &uploadSessionStore{}
	resolver, err := inframedia.NewURLResolver("https://cdn.example.test", store)
	if err != nil {
		t.Fatalf("create media URL resolver: %v", err)
	}
	service := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, time.Minute, "v1", 5,
		applicationmedia.WithURLResolver(resolver, 5*time.Minute),
	)
	owner := newUploadSessionRouter(service, 42)
	response := performUploadSessionJSON(owner, http.MethodGet, "/api/media-assets/501/access", "")
	requireStatus(t, response, http.StatusOK)
	var payload struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	decodeJSON(t, response, &payload)
	if payload.URL == "" || payload.ExpiresAt.Before(time.Now().UTC().Add(4*time.Minute)) {
		t.Fatalf("unexpected protected asset access: %+v", payload)
	}

	other := newUploadSessionRouter(service, 77)
	requireStatus(t, performUploadSessionJSON(other, http.MethodGet, "/api/media-assets/501/access", ""), http.StatusForbidden)

	deniedService := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, time.Minute, "v1", 5,
		applicationmedia.WithURLResolver(resolver, 5*time.Minute),
		applicationmedia.WithMediaAssetAuthorizer(mediaAssetAuthorizerStub{referenced: true, allowed: false}),
	)
	requireStatus(t, performUploadSessionJSON(newUploadSessionRouter(deniedService, 42), http.MethodGet, "/api/media-assets/501/access", ""), http.StatusForbidden)
}

type mediaAssetAuthorizerStub struct {
	referenced bool
	allowed    bool
}

func (s mediaAssetAuthorizerStub) AuthorizeMediaAsset(context.Context, int64, int64) (bool, bool, error) {
	return s.referenced, s.allowed, nil
}

type uploadSessionRepository struct {
	sessions map[string]*domainmedia.UploadSession
	assets   map[int64]*domainmedia.MediaAsset
	job      *domainmedia.MediaProcessingJob
	nextID   int64
}

func newUploadSessionRepository() *uploadSessionRepository {
	return &uploadSessionRepository{sessions: map[string]*domainmedia.UploadSession{}, assets: map[int64]*domainmedia.MediaAsset{}, nextID: 100}
}

func (r *uploadSessionRepository) CreateUploadSession(_ context.Context, session *domainmedia.UploadSession) (*domainmedia.UploadSession, bool, error) {
	for _, existing := range r.sessions {
		if session.IdempotencyKey != "" && existing.OwnerID == session.OwnerID && existing.IdempotencyKey == session.IdempotencyKey {
			if existing.RequestFingerprint != session.RequestFingerprint {
				return nil, false, domainmedia.ErrUploadSessionConflict
			}
			copy := *existing
			return &copy, false, nil
		}
	}
	copy := *session
	copy.CreatedAt = time.Now().UTC()
	copy.UpdatedAt = copy.CreatedAt
	r.sessions[copy.ID] = &copy
	return &copy, true, nil
}

func (r *uploadSessionRepository) FindUploadSession(_ context.Context, sessionID string) (*domainmedia.UploadSession, error) {
	session := r.sessions[sessionID]
	if session == nil {
		return nil, domainmedia.ErrUploadSessionNotFound
	}
	copy := *session
	return &copy, nil
}

func (r *uploadSessionRepository) CompleteUploadSession(_ context.Context, sessionID string, asset *domainmedia.MediaAsset, completedAt time.Time) (*domainmedia.UploadSession, *domainmedia.MediaAsset, bool, error) {
	session := r.sessions[sessionID]
	if session == nil {
		return nil, nil, false, domainmedia.ErrUploadSessionNotFound
	}
	if session.CompletedAssetID > 0 {
		assetCopy := *r.assets[session.CompletedAssetID]
		sessionCopy := *session
		return &sessionCopy, &assetCopy, true, nil
	}
	r.nextID++
	assetCopy := *asset
	assetCopy.ID = r.nextID
	r.assets[assetCopy.ID] = &assetCopy
	session.State = domainmedia.UploadSessionStateCompleted
	session.CompletedAssetID = assetCopy.ID
	session.CompletedAt = &completedAt
	sessionCopy := *session
	return &sessionCopy, &assetCopy, false, nil
}

func (r *uploadSessionRepository) CreateOrGetProcessingJob(_ context.Context, job *domainmedia.MediaProcessingJob) (*domainmedia.MediaProcessingJob, bool, error) {
	if r.job != nil {
		copy := *r.job
		return &copy, false, nil
	}
	copy := *job
	copy.ID = 1
	r.job = &copy
	return &copy, true, nil
}

func (r *uploadSessionRepository) FindAssetByID(_ context.Context, assetID int64) (*domainmedia.MediaAsset, error) {
	asset := r.assets[assetID]
	if asset == nil {
		return nil, domainmedia.ErrMediaAssetNotFound
	}
	copy := *asset
	return &copy, nil
}

func (r *uploadSessionRepository) UpdateAsset(_ context.Context, asset *domainmedia.MediaAsset) error {
	r.assets[asset.ID] = asset
	return nil
}

func (r *uploadSessionRepository) RenewExpiredUploadSession(_ context.Context, expiredSessionID string, replacement *domainmedia.UploadSession) (*domainmedia.UploadSession, error) {
	delete(r.sessions, expiredSessionID)
	copy := *replacement
	r.sessions[copy.ID] = &copy
	return &copy, nil
}

func (*uploadSessionRepository) UpsertVariants(context.Context, []*domainmedia.MediaVariant) error {
	return nil
}

type uploadSessionStore struct {
	metadata *domainmedia.ObjectMetadata
}

func (*uploadSessionStore) Put(context.Context, string, io.Reader, int64, string, string) (*domainmedia.ObjectMetadata, error) {
	return nil, nil
}

func (*uploadSessionStore) Open(context.Context, string) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	return nil, nil, errors.New("not implemented")
}

func (s *uploadSessionStore) Head(context.Context, string) (*domainmedia.ObjectMetadata, error) {
	if s.metadata == nil {
		return nil, domainmedia.ErrObjectNotFound
	}
	copy := *s.metadata
	return &copy, nil
}

func (*uploadSessionStore) Delete(context.Context, string) error {
	return nil
}

func (*uploadSessionStore) List(context.Context, string) ([]domainmedia.ObjectMetadata, error) {
	return nil, nil
}

func (*uploadSessionStore) PresignPut(_ context.Context, key, _ string, _ string, _ int64, expiry time.Duration) (*domainmedia.PresignedRequest, error) {
	return &domainmedia.PresignedRequest{
		URL: "https://objects.example.test/" + key, Method: "PUT",
		Headers: map[string]string{}, ExpiresAt: time.Now().UTC().Add(expiry),
	}, nil
}

func (*uploadSessionStore) PresignGet(_ context.Context, key string, expiry time.Duration) (*domainmedia.PresignedRequest, error) {
	return &domainmedia.PresignedRequest{
		URL: "https://signed.example.test/" + key, Method: http.MethodGet, ExpiresAt: time.Now().UTC().Add(expiry),
	}, nil
}

func newUploadSessionRouter(service *applicationmedia.Service, userID int64) *server.Hertz {
	handler := interfaceshttpupload.NewSessionHandler(service)
	router := server.New()
	api := router.Group("/api")
	sessions := api.Group("/upload-sessions", func(ctx context.Context, c *app.RequestContext) {
		c.Set(interfaceshttpmiddleware.ContextUserIDKey, userID)
		c.Next(ctx)
	})
	sessions.POST("", handler.Create)
	sessions.POST("/:sessionId/complete", handler.Complete)
	api.GET("/media-assets/:assetId/access", func(ctx context.Context, c *app.RequestContext) {
		c.Set(interfaceshttpmiddleware.ContextUserIDKey, userID)
		c.Next(ctx)
	}, handler.Access)
	return router
}

func performUploadSessionJSON(router *server.Hertz, method, path, body string, headers ...ut.Header) *ut.ResponseRecorder {
	if body == "" {
		body = "{}"
	}
	allHeaders := append([]ut.Header{{Key: "Content-Type", Value: "application/json"}}, headers...)
	return ut.PerformRequest(router.Engine, method, path, &ut.Body{Body: bytes.NewBufferString(body), Len: len(body)}, allHeaders...)
}
