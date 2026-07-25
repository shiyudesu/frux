package test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	infrajwt "GCFeed/internal/infra/jwt"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	interfaceshttpupload "GCFeed/internal/interfaces/http/upload"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

type assetDecision struct {
	referenced bool
	public     bool
	ownerID    int64
	deleted    bool
}

type assetAuthorizerStub map[string]assetDecision

func (s assetAuthorizerStub) AuthorizeLocalAsset(_ context.Context, assetURL string, viewerID int64) (bool, bool, bool, error) {
	decision, ok := s[assetURL]
	if !ok {
		return false, false, false, nil
	}
	allowed := decision.public || (!decision.deleted && viewerID > 0 && viewerID == decision.ownerID)
	return decision.referenced, decision.public, allowed, nil
}

func TestProtectedUploadAssetDelivery(t *testing.T) {
	root, err := os.MkdirTemp(".", ".asset-auth-test-")
	if err != nil {
		t.Fatalf("create asset directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove asset directory: %v", err)
		}
	})
	for _, kind := range []string{"video", "cover", "avatar", "file"} {
		if err := os.MkdirAll(filepath.Join(root, kind), 0o755); err != nil {
			t.Fatalf("create %s directory: %v", kind, err)
		}
	}
	files := map[string]string{
		"video/public.mp4":    "public-video",
		"video/private.mp4":   "private-video",
		"video/offline.mp4":   "offline-video",
		"video/deleted.mp4":   "deleted-video",
		"video/unclaimed.mp4": "unclaimed-video",
		"cover/private.jpg":   "private-cover",
		"avatar/profile.jpg":  "public-avatar",
		"file/private.mp4":    "private-file-kind-video",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	authorizer := assetAuthorizerStub{
		"/uploads/video/public.mp4":  {referenced: true, public: true, ownerID: 42},
		"/uploads/video/private.mp4": {referenced: true, ownerID: 42},
		"/uploads/video/offline.mp4": {referenced: true, ownerID: 42},
		"/uploads/video/deleted.mp4": {referenced: true, ownerID: 42, deleted: true},
		"/uploads/cover/private.jpg": {referenced: true, ownerID: 42},
		"/uploads/file/private.mp4":  {referenced: true, ownerID: 42},
	}
	assetHandler, err := interfaceshttpupload.NewAssetHandler(root, "/uploads", authorizer)
	if err != nil {
		t.Fatalf("create asset handler: %v", err)
	}
	jwtManager, err := infrajwt.NewManager("asset-test-secret", "15m")
	if err != nil {
		t.Fatalf("create jwt manager: %v", err)
	}
	optionalAuth := interfaceshttpmiddleware.NewOptionalJWTAuth(jwtManager)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	router := server.New(server.WithListener(listener), server.WithTransport(standard.NewTransporter))
	router.GET("/uploads/*filepath", optionalAuth, assetHandler.Get)
	router.HEAD("/uploads/*filepath", optionalAuth, assetHandler.Head)
	runErr := make(chan error, 1)
	go func() {
		runErr <- router.Engine.Run()
	}()
	transport := &http.Transport{}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := router.Engine.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("run server: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server did not stop")
		}
	})

	baseURL := "http://" + listener.Addr().String()
	requireAssetStatus(t, client, baseURL+"/uploads/video/public.mp4", "", "", http.StatusOK)
	requireAssetStatus(t, client, baseURL+"/uploads/video/private.mp4", "", "", http.StatusNotFound)
	requireAssetStatus(t, client, baseURL+"/uploads/video/offline.mp4", "", "", http.StatusNotFound)
	requireAssetStatus(t, client, baseURL+"/uploads/video/unclaimed.mp4", "", "", http.StatusNotFound)
	requireAssetStatus(t, client, baseURL+"/uploads/avatar/profile.jpg", "", "", http.StatusOK)
	requireAssetStatus(t, client, baseURL+"/uploads/file/private.mp4", "", "", http.StatusNotFound)

	ownerToken, err := jwtManager.SignAccessToken(42, "user")
	if err != nil {
		t.Fatalf("sign owner token: %v", err)
	}
	ownerResponse := requireAssetStatus(
		t,
		client,
		baseURL+"/uploads/video/private.mp4",
		"Bearer "+ownerToken,
		"",
		http.StatusOK,
	)
	if cookies := ownerResponse.Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("authorized asset response refreshed cookies: %v", cookies)
	}
	if cacheControl := ownerResponse.Header.Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected protected asset no-store, got %q", cacheControl)
	}

	assetCookie := interfaceshttpmiddleware.AssetTokenCookieName + "=" + ownerToken
	requireAssetStatus(t, client, baseURL+"/uploads/cover/private.jpg", "", assetCookie, http.StatusNotFound)
	requireAssetStatus(
		t,
		client,
		baseURL+"/uploads/cover/private.jpg",
		"",
		interfaceshttpmiddleware.AssetActiveCookieName+"=1",
		http.StatusNotFound,
	)
	activeAssetCookie := assetCookie + "; " + interfaceshttpmiddleware.AssetActiveCookieName + "=1"
	requireAssetStatus(t, client, baseURL+"/uploads/cover/private.jpg", "", activeAssetCookie, http.StatusOK)
	requireAssetStatus(t, client, baseURL+"/uploads/file/private.mp4", "", activeAssetCookie, http.StatusOK)
	requireAssetStatus(t, client, baseURL+"/uploads/video/offline.mp4", "", activeAssetCookie, http.StatusOK)
	requireAssetStatus(t, client, baseURL+"/uploads/video/deleted.mp4", "", activeAssetCookie, http.StatusNotFound)

	otherToken, err := jwtManager.SignAccessToken(77, "user")
	if err != nil {
		t.Fatalf("sign other token: %v", err)
	}
	requireAssetStatus(
		t,
		client,
		baseURL+"/uploads/video/private.mp4",
		"",
		interfaceshttpmiddleware.AssetTokenCookieName+"="+otherToken+"; "+interfaceshttpmiddleware.AssetActiveCookieName+"=1",
		http.StatusNotFound,
	)

	rangeRequest, err := http.NewRequest(http.MethodGet, baseURL+"/uploads/video/public.mp4", nil)
	if err != nil {
		t.Fatalf("create range request: %v", err)
	}
	rangeRequest.Header.Set("Range", "bytes=0-5")
	rangeResponse, err := client.Do(rangeRequest)
	if err != nil {
		t.Fatalf("perform range request: %v", err)
	}
	defer rangeResponse.Body.Close()
	if rangeResponse.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected range status %d, got %d", http.StatusPartialContent, rangeResponse.StatusCode)
	}
	body, err := io.ReadAll(rangeResponse.Body)
	if err != nil {
		t.Fatalf("read range response: %v", err)
	}
	if string(body) != "public" {
		t.Fatalf("unexpected range body %q", string(body))
	}
}

func requireAssetStatus(t *testing.T, client *http.Client, url, authorization, cookie string, status int) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create asset request: %v", err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform asset request: %v", err)
	}
	if response.StatusCode != status {
		_ = response.Body.Close()
		t.Fatalf("expected asset status %d, got %d for %s", status, response.StatusCode, url)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return response
}
