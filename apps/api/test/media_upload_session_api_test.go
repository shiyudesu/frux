package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	interfaceshttpupload "github.com/shiyudesu/frux/internal/interfaces/http/upload"

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

	replayedCreate := performUploadSessionJSON(router, http.MethodPost, "/api/upload-sessions",
		`{"kind":"video","filename":"clip.mp4","content_type":"video/mp4","size_bytes":128,"checksum_sha256":"`+checksum+`"}`,
		ut.Header{Key: "Idempotency-Key", Value: "upload-1"},
	)
	requireStatus(t, replayedCreate, http.StatusOK)
	var replayedSession struct {
		CompletedAssetID int64 `json:"completed_asset_id"`
		Replayed         bool  `json:"replayed"`
	}
	decodeJSON(t, replayedCreate, &replayedSession)
	if replayedSession.CompletedAssetID != completed.Asset.ID || !replayedSession.Replayed {
		t.Fatalf("unexpected completed session replay: %+v", replayedSession)
	}
}

func TestMediaUploadSessionRejectsUnsupportedOrOversizedFilesBeforeCreation(t *testing.T) {
	repo := newUploadSessionRepository()
	store := &uploadSessionStore{}
	service := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, 15*time.Minute, "v1", 5,
		applicationmedia.WithIDGenerator(func() (string, error) { return "unused-session", nil }),
	)
	router := newUploadSessionRouter(service, 42)
	checksum := strings.Repeat("d", 64)

	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "unsupported cover extension",
			body: `{"kind":"cover","filename":"cover.gif","content_type":"image/gif","size_bytes":128,"checksum_sha256":"` + checksum + `"}`,
			code: interfaceshttpapierror.CodeUploadFileTypeInvalid,
		},
		{
			name: "cover MIME does not match supported type",
			body: `{"kind":"cover","filename":"cover.jpg","content_type":"image/gif","size_bytes":128,"checksum_sha256":"` + checksum + `"}`,
			code: interfaceshttpapierror.CodeUploadFileTypeInvalid,
		},
		{
			name: "oversized cover",
			body: `{"kind":"cover","filename":"cover.png","content_type":"image/png","size_bytes":20971521,"checksum_sha256":"` + checksum + `"}`,
			code: interfaceshttpapierror.CodeUploadCoverTooLarge,
		},
		{
			name: "oversized video",
			body: `{"kind":"video","filename":"clip.mp4","content_type":"video/mp4","size_bytes":536870913,"checksum_sha256":"` + checksum + `"}`,
			code: interfaceshttpapierror.CodeUploadVideoTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performUploadSessionJSON(
				router, http.MethodPost, "/api/upload-sessions", tt.body,
				ut.Header{Key: "Idempotency-Key", Value: "invalid-" + strings.ReplaceAll(tt.name, " ", "-")},
			)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var body interfaceshttpapierror.Envelope
			decodeJSON(t, response, &body)
			if body.Code != tt.code {
				t.Fatalf("code = %q want %q body=%s", body.Code, tt.code, response.Body.String())
			}
		})
	}
	if len(repo.sessions) != 0 {
		t.Fatalf("invalid requests created sessions: %+v", repo.sessions)
	}
}

func TestMediaUploadSessionReplaysLegacyCompletedFormat(t *testing.T) {
	now := time.Date(2026, 8, 7, 14, 30, 0, 0, time.UTC)
	repo := newUploadSessionRepository()
	store := &uploadSessionStore{}
	checksum := strings.Repeat("e", 64)
	idempotencyKey := "legacy-cover-upload"
	fingerprint := uploadSessionFingerprint(
		42, domainmedia.AssetKindCover, "cover.gif", "image/gif", 128, checksum,
	)
	session, err := domainmedia.NewUploadSession(
		"legacy-cover-session", 42, domainmedia.AssetKindCover, domainmedia.StorageBackendS3,
		"uploads/42/legacy-cover-session/cover/source.gif", "image/gif", 128, checksum,
		idempotencyKey, fingerprint, now.Add(15*time.Minute), now,
	)
	if err != nil {
		t.Fatalf("create legacy session: %v", err)
	}
	session.State = domainmedia.UploadSessionStateCompleted
	session.CompletedAssetID = 901
	session.CompletedAt = &now
	repo.sessions[session.ID] = session
	repo.assets[901] = &domainmedia.MediaAsset{
		ID: 901, OwnerID: 42, Kind: domainmedia.AssetKindCover,
		StorageBackend: domainmedia.StorageBackendS3, ObjectKey: session.ObjectKey,
		ContentType: "image/gif", SizeBytes: 128, ChecksumSHA256: checksum,
		State: domainmedia.AssetStateReady,
	}
	service := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, 15*time.Minute, "v1", 5,
		applicationmedia.WithNow(func() time.Time { return now }),
	)
	router := newUploadSessionRouter(service, 42)

	response := performUploadSessionJSON(router, http.MethodPost, "/api/upload-sessions",
		`{"kind":"cover","filename":"cover.gif","content_type":"image/gif","size_bytes":128,"checksum_sha256":"`+checksum+`"}`,
		ut.Header{Key: "Idempotency-Key", Value: idempotencyKey},
	)
	requireStatus(t, response, http.StatusOK)
	var payload struct {
		CompletedAssetID int64 `json:"completed_asset_id"`
		Replayed         bool  `json:"replayed"`
	}
	decodeJSON(t, response, &payload)
	if payload.CompletedAssetID != 901 || !payload.Replayed {
		t.Fatalf("unexpected legacy replay: %+v", payload)
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
	assertAPIError(t, mismatch, http.StatusConflict, interfaceshttpapierror.CodeUploadSessionConflict, applicationmedia.ErrUploadObjectMismatch.Error())

	store.metadata.ChecksumSHA256 = checksum
	otherOwner := newUploadSessionRouter(service, 7)
	forbidden := performUploadSessionJSON(otherOwner, http.MethodPost, "/api/upload-sessions/"+payload.ID+"/complete", "")
	assertAPIError(t, forbidden, http.StatusConflict, interfaceshttpapierror.CodeUploadSessionConflict, domainmedia.ErrUploadSessionConflict.Error())

	now = base.Add(2 * time.Minute)
	expired := performUploadSessionJSON(ownerRouter, http.MethodPost, "/api/upload-sessions/"+payload.ID+"/complete", "")
	assertAPIError(t, expired, http.StatusConflict, interfaceshttpapierror.CodeUploadSessionConflict, domainmedia.ErrUploadSessionExpired.Error())
}

func TestProtectedMediaAssetAccessIsOwnerBoundAndExpires(t *testing.T) {
	repo := newUploadSessionRepository()
	repo.assets[501] = &domainmedia.MediaAsset{
		ID: 501, OwnerID: 42, Kind: domainmedia.AssetKindVideo,
		StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/42/source.mp4",
		State: domainmedia.AssetStateReady,
	}
	repo.variants[501] = []*domainmedia.MediaVariant{{
		ID: 601, AssetID: 501, Role: domainmedia.VariantRoleBaseline,
		State: domainmedia.VariantStateReady, ObjectKey: "processed/501/baseline.mp4",
	}}
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
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("protected asset response was cacheable: %q", response.Header().Get("Cache-Control"))
	}
	var payload struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	decodeJSON(t, response, &payload)
	if payload.URL != "https://signed.example.test/processed/501/baseline.mp4" ||
		payload.ExpiresAt.Before(time.Now().UTC().Add(4*time.Minute)) {
		t.Fatalf("unexpected protected asset access: %+v", payload)
	}

	other := newUploadSessionRouter(service, 77)
	assertAPIError(t, performUploadSessionJSON(other, http.MethodGet, "/api/media-assets/501/access", ""), http.StatusForbidden, interfaceshttpapierror.CodeUploadAssetPermissionDenied, domainmedia.ErrMediaAssetPermissionDenied.Error())

	deniedService := applicationmedia.New(
		repo, store, domainmedia.StorageBackendS3, time.Minute, "v1", 5,
		applicationmedia.WithURLResolver(resolver, 5*time.Minute),
		applicationmedia.WithMediaAssetAuthorizer(mediaAssetAuthorizerStub{referenced: true, allowed: false}),
	)
	assertAPIError(t, performUploadSessionJSON(newUploadSessionRouter(deniedService, 42), http.MethodGet, "/api/media-assets/501/access", ""), http.StatusForbidden, interfaceshttpapierror.CodeUploadAssetPermissionDenied, domainmedia.ErrMediaAssetPermissionDenied.Error())

	repo.assets[502] = &domainmedia.MediaAsset{
		ID: 502, OwnerID: 42, Kind: domainmedia.AssetKindCover,
		StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/42/cover.png",
		State: domainmedia.AssetStateReady,
	}
	repo.variants[502] = []*domainmedia.MediaVariant{{
		ID: 602, AssetID: 502, Role: domainmedia.VariantRoleCover,
		State: domainmedia.VariantStateReady, ObjectKey: "processed/502/cover.jpg",
	}}
	coverResponse := performUploadSessionJSON(
		owner, http.MethodGet, "/api/media-assets/502/access", "",
	)
	requireStatus(t, coverResponse, http.StatusOK)
	var coverPayload struct {
		URL string `json:"url"`
	}
	decodeJSON(t, coverResponse, &coverPayload)
	if coverPayload.URL != "https://signed.example.test/processed/502/cover.jpg" {
		t.Fatalf("unexpected protected cover access: %+v", coverPayload)
	}

	repo.assets[503] = &domainmedia.MediaAsset{
		ID: 503, OwnerID: 42, Kind: domainmedia.AssetKindVideo,
		StorageBackend: domainmedia.StorageBackendS3, ObjectKey: "uploads/42/processing.mov",
		State: domainmedia.AssetStateProcessing,
	}
	processingResponse := performUploadSessionJSON(
		owner, http.MethodGet, "/api/media-assets/503/access", "",
	)
	requireStatus(t, processingResponse, http.StatusOK)
	var processingPayload struct {
		URL string `json:"url"`
	}
	decodeJSON(t, processingResponse, &processingPayload)
	if processingPayload.URL != "https://signed.example.test/uploads/42/processing.mov" {
		t.Fatalf("processing asset did not fall back to original: %+v", processingPayload)
	}
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
	variants map[int64][]*domainmedia.MediaVariant
	job      *domainmedia.MediaProcessingJob
	nextID   int64
}

func newUploadSessionRepository() *uploadSessionRepository {
	return &uploadSessionRepository{
		sessions: map[string]*domainmedia.UploadSession{},
		assets:   map[int64]*domainmedia.MediaAsset{},
		variants: map[int64][]*domainmedia.MediaVariant{},
		nextID:   100,
	}
}

func (r *uploadSessionRepository) ListReadyVariants(
	_ context.Context,
	assetID int64,
) ([]*domainmedia.MediaVariant, error) {
	return r.variants[assetID], nil
}

func uploadSessionFingerprint(
	ownerID int64,
	kind string,
	filename string,
	contentType string,
	sizeBytes int64,
	checksum string,
) string {
	payload, err := json.Marshal(struct {
		OwnerID        int64  `json:"owner_id"`
		Kind           string `json:"kind"`
		Filename       string `json:"filename"`
		ContentType    string `json:"content_type"`
		SizeBytes      int64  `json:"size_bytes"`
		ChecksumSHA256 string `json:"checksum_sha256"`
	}{
		OwnerID: ownerID, Kind: kind, Filename: filename, ContentType: contentType,
		SizeBytes: sizeBytes, ChecksumSHA256: checksum,
	})
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
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

func (r *uploadSessionRepository) FindUploadSessionByOwnerAndIdempotencyKey(
	_ context.Context,
	ownerID int64,
	idempotencyKey string,
) (*domainmedia.UploadSession, error) {
	for _, session := range r.sessions {
		if session.OwnerID == ownerID && session.IdempotencyKey == idempotencyKey {
			copy := *session
			return &copy, nil
		}
	}
	return nil, domainmedia.ErrUploadSessionNotFound
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
