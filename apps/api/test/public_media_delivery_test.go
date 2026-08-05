package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	interfaceshttpupload "github.com/shiyudesu/frux/internal/interfaces/http/upload"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

func TestPublicMediaImmutableRangeHeadAndETag(t *testing.T) {
	root := t.TempDir()
	store, err := inframedia.NewLocalStore(root)
	if err != nil {
		t.Fatalf("create local media store: %v", err)
	}
	content := []byte("0123456789abcdef")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	key := "media/10/v1/" + checksum + "/baseline.mp4"
	if _, err := store.Put(context.Background(), key, bytes.NewReader(content), int64(len(content)), "video/mp4", checksum); err != nil {
		t.Fatalf("put public media: %v", err)
	}
	handler, err := interfaceshttpupload.NewPublicMediaHandler(store, root, "/media")
	if err != nil {
		t.Fatalf("create public media handler: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	router := server.New(server.WithListener(listener), server.WithTransport(standard.NewTransporter))
	router.GET("/media/*filepath", handler.Get)
	router.HEAD("/media/*filepath", handler.Head)
	runErr := make(chan error, 1)
	go func() { runErr <- router.Engine.Run() }()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = router.Engine.Shutdown(shutdownCtx)
		<-runErr
	})

	url := "http://" + listener.Addr().String() + "/media/" + key
	request, _ := http.NewRequest(http.MethodGet, url, nil)
	request.Header.Set("Range", "bytes=0-7")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request public range: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected partial content, got %d", response.StatusCode)
	}
	body, _ := io.ReadAll(response.Body)
	if string(body) != "01234567" {
		t.Fatalf("unexpected range body %q", body)
	}
	if response.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected cache control %q", response.Header.Get("Cache-Control"))
	}
	etag := response.Header.Get("ETag")
	if etag != `"`+checksum+`"` {
		t.Fatalf("unexpected ETag %q", etag)
	}

	head, err := http.Head(url)
	if err != nil {
		t.Fatalf("head public media: %v", err)
	}
	_ = head.Body.Close()
	if head.StatusCode != http.StatusOK || head.ContentLength != int64(len(content)) {
		t.Fatalf("unexpected HEAD response: status=%d length=%d", head.StatusCode, head.ContentLength)
	}

	conditional, _ := http.NewRequest(http.MethodGet, url, nil)
	conditional.Header.Set("If-None-Match", etag)
	notModified, err := http.DefaultClient.Do(conditional)
	if err != nil {
		t.Fatalf("conditional media request: %v", err)
	}
	_ = notModified.Body.Close()
	if notModified.StatusCode != http.StatusNotModified {
		t.Fatalf("expected not modified, got %d", notModified.StatusCode)
	}
}
