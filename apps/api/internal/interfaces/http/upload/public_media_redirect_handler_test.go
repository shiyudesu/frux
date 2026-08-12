package interfaceshttpupload

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

type publicMediaRedirectStore struct {
	request  *domainmedia.PresignedRequest
	err      error
	key      string
	ttl      time.Duration
	calls    int
	metadata *domainmedia.ObjectMetadata
	body     string
}

func (s *publicMediaRedirectStore) PresignGet(
	_ context.Context,
	key string,
	expiry time.Duration,
) (*domainmedia.PresignedRequest, error) {
	s.calls++
	s.key = key
	s.ttl = expiry
	return s.request, s.err
}

func (s *publicMediaRedirectStore) Open(
	_ context.Context,
	key string,
) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	s.key = key
	return io.NopCloser(strings.NewReader(s.body)), s.metadata, nil
}

func (s *publicMediaRedirectStore) Head(
	_ context.Context,
	key string,
) (*domainmedia.ObjectMetadata, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.key = key
	return s.metadata, nil
}

type publicMediaRedirectAuthorizer struct {
	allowed bool
	err     error
	key     string
}

func (a *publicMediaRedirectAuthorizer) AuthorizePublicMediaObject(
	_ context.Context,
	key string,
) (bool, error) {
	a.key = key
	return a.allowed, a.err
}

func TestPublicMediaRedirectHandlerRedirectsGetAndServesHead(t *testing.T) {
	const (
		key       = "media/v2/generation/processed/1/v1/baseline.mp4"
		signedURL = "https://cn-zj1.rains3.com/frux1/media/v2/file.mp4?signature=test"
	)
	store := &publicMediaRedirectStore{
		request: &domainmedia.PresignedRequest{URL: signedURL},
		metadata: &domainmedia.ObjectMetadata{
			Key: key, ContentType: "video/mp4", SizeBytes: 1024, ETag: "etag",
		},
	}
	authorizer := &publicMediaRedirectAuthorizer{allowed: true}
	handler, err := NewPublicMediaRedirectHandler(store, authorizer, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	router := server.Default()
	router.GET("/media/*filepath", handler.Get)
	router.HEAD("/media/*filepath", handler.Head)

	get := ut.PerformRequest(router.Engine, http.MethodGet, "/media/"+key, nil)
	if get.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET status = %d", get.Code)
	}
	if location := get.Header().Get("Location"); location != signedURL {
		t.Fatalf("GET location = %q", location)
	}
	if cache := get.Header().Get("Cache-Control"); cache != "private, no-store" {
		t.Fatalf("GET cache control = %q", cache)
	}

	head := ut.PerformRequest(router.Engine, http.MethodHead, "/media/"+key, nil)
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d", head.Code)
	}
	if location := head.Header().Get("Location"); location != "" {
		t.Fatalf("HEAD redirected to %q", location)
	}
	if head.Header().Get("ETag") != `"etag"` ||
		head.Header().Get("Content-Length") != "1024" ||
		head.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("HEAD headers = %+v", head.Header())
	}
	if store.calls != 1 || store.key != key || store.ttl != time.Minute || authorizer.key != key {
		t.Fatalf(
			"inputs calls=%d key=%q ttl=%s authorized=%q",
			store.calls, store.key, store.ttl, authorizer.key,
		)
	}
}

func TestPublicMediaRedirectHandlerServesStableDASHManifest(t *testing.T) {
	const key = "media/v2/generation/processed/1/v1/dash/manifest.mpd"
	manifest := `<MPD><SegmentTemplate media="chunk-$Number$.m4s"/></MPD>`
	store := &publicMediaRedirectStore{
		body: manifest,
		metadata: &domainmedia.ObjectMetadata{
			Key: key, ContentType: "application/dash+xml",
			SizeBytes: int64(len(manifest)), ETag: "manifest-etag",
		},
	}
	handler, err := NewPublicMediaRedirectHandler(
		store,
		&publicMediaRedirectAuthorizer{allowed: true},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	router := server.Default()
	router.GET("/media/*filepath", handler.Get)

	response := ut.PerformRequest(router.Engine, http.MethodGet, "/media/"+key, nil)
	if response.Code != http.StatusOK || response.Body.String() != manifest {
		t.Fatalf("manifest response status=%d body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "" ||
		response.Header().Get("Cache-Control") != "public, max-age=60, must-revalidate" {
		t.Fatalf("manifest headers = %+v", response.Header())
	}
	if store.calls != 0 {
		t.Fatalf("manifest presign calls = %d", store.calls)
	}
}

func TestPublicMediaRedirectHandlerRejectsUnauthorizedAndInvalidKeys(t *testing.T) {
	store := &publicMediaRedirectStore{
		request: &domainmedia.PresignedRequest{URL: "https://signed.example.test/object"},
	}
	handler, err := NewPublicMediaRedirectHandler(
		store,
		&publicMediaRedirectAuthorizer{},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	router := server.Default()
	router.GET("/media/*filepath", handler.Get)

	for _, path := range []string{
		"/media/media/v2/private.mp4",
		"/media/processed/1/private.mp4",
	} {
		response := ut.PerformRequest(router.Engine, http.MethodGet, path, nil)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
	if store.calls != 0 {
		t.Fatalf("presigner calls = %d", store.calls)
	}
}

func TestPublicMediaRedirectHandlerReportsStorageFailure(t *testing.T) {
	store := &publicMediaRedirectStore{err: errors.New("storage unavailable")}
	handler, err := NewPublicMediaRedirectHandler(
		store,
		&publicMediaRedirectAuthorizer{allowed: true},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	router := server.Default()
	router.GET("/media/*filepath", handler.Get)

	response := ut.PerformRequest(
		router.Engine,
		http.MethodGet,
		"/media/media/v2/generation/file.mp4",
		nil,
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPublicMediaRedirectHandlerDoesNotCacheInvalidManifest(t *testing.T) {
	handler, err := NewPublicMediaRedirectHandler(
		&publicMediaRedirectStore{
			body: "short",
			metadata: &domainmedia.ObjectMetadata{
				Key:         "media/v2/generation/manifest.mpd",
				ContentType: "application/dash+xml",
				SizeBytes:   99,
				ETag:        "etag",
			},
		},
		&publicMediaRedirectAuthorizer{allowed: true},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	router := server.Default()
	router.GET("/media/*filepath", handler.Get)

	response := ut.PerformRequest(
		router.Engine,
		http.MethodGet,
		"/media/media/v2/generation/manifest.mpd",
		nil,
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	if cache := response.Header().Get("Cache-Control"); cache != "private, no-store" {
		t.Fatalf("cache control = %q", cache)
	}
}

func TestPublicMediaRedirectHandlerRejectsUnsafeTTL(t *testing.T) {
	_, err := NewPublicMediaRedirectHandler(
		&publicMediaRedirectStore{},
		&publicMediaRedirectAuthorizer{},
		time.Minute+time.Second,
	)
	if !errors.Is(err, errInvalidPublicMediaRedirectHandler) {
		t.Fatalf("error = %v", err)
	}
}

func TestPublicMediaRedirectPreservesRangeOnFollow(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	router := server.New(
		server.WithListener(listener),
		server.WithTransport(standard.NewTransporter),
	)
	signedURL := "http://" + listener.Addr().String() + "/signed"
	handler, err := NewPublicMediaRedirectHandler(
		&publicMediaRedirectStore{
			request: &domainmedia.PresignedRequest{URL: signedURL},
		},
		&publicMediaRedirectAuthorizer{allowed: true},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 1)
	router.GET("/media/*filepath", handler.Get)
	router.GET("/signed", func(_ context.Context, c *app.RequestContext) {
		requests <- string(c.GetHeader("Range"))
		c.Status(http.StatusNoContent)
	})

	runErr := make(chan error, 1)
	go func() { runErr <- router.Engine.Run() }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = router.Engine.Shutdown(shutdownCtx)
		<-runErr
	})

	publicURL := "http://" + listener.Addr().String() +
		"/media/media/v2/generation/file.mp4"
	request, _ := http.NewRequest(http.MethodGet, publicURL, nil)
	request.Header.Set("Range", "bytes=0-1023")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if received := <-requests; received != "bytes=0-1023" {
		t.Fatalf("redirected Range = %q", received)
	}
}
