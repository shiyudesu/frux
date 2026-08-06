package interfaceshttpupload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	inframedia "github.com/shiyudesu/frux/internal/infra/media"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestProtectedMediaHandlerRequiresValidSignedURL(t *testing.T) {
	root := t.TempDir()
	store, err := inframedia.NewLocalStore(root)
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	content := []byte("protected review media")
	sum := sha256.Sum256(content)
	key := "processed/1/v1/baseline.mp4"
	if _, err := store.Put(
		context.Background(), key, bytes.NewReader(content), int64(len(content)),
		"video/mp4", hex.EncodeToString(sum[:]),
	); err != nil {
		t.Fatalf("put protected media: %v", err)
	}
	signer, err := inframedia.NewLocalProtectedURLSigner(
		"/review-media", "preview-secret", 5*time.Minute,
	)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	handler, err := NewProtectedMediaHandler(store, root, "/review-media", signer)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router := server.Default()
	router.GET("/review-media/*filepath", handler.Get)
	router.HEAD("/review-media/*filepath", handler.Head)

	unsigned := ut.PerformRequest(
		router.Engine, http.MethodGet, "/review-media/"+key, nil,
	)
	if unsigned.Code != http.StatusNotFound {
		t.Fatalf("unsigned status = %d", unsigned.Code)
	}
	signedURL, _, err := signer.Sign(key, 5*time.Minute)
	if err != nil {
		t.Fatalf("sign URL: %v", err)
	}
	response := ut.PerformRequest(router.Engine, http.MethodHead, signedURL, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("signed response status=%d", response.Code)
	}
	if cache := response.Header().Get("Cache-Control"); cache != "private, no-store" {
		t.Fatalf("cache control = %q", cache)
	}
	tampered := strings.Replace(signedURL, "signature=", "signature=x", 1)
	rejected := ut.PerformRequest(router.Engine, http.MethodGet, tampered, nil)
	if rejected.Code != http.StatusNotFound {
		t.Fatalf("tampered status = %d", rejected.Code)
	}
}
