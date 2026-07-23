package test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	infrahttphertz "GCFeed/internal/infra/httphertz"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

func TestUploadStaticRangeRequest(t *testing.T) {
	uploadDir := t.TempDir()
	videoDir := filepath.Join(uploadDir, "video")
	if err := os.MkdirAll(videoDir, 0o755); err != nil {
		t.Fatalf("create video dir: %v", err)
	}

	content := []byte("0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(filepath.Join(videoDir, "range.mp4"), content, 0o644); err != nil {
		t.Fatalf("write test video: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "image"), samplePNGBytes(), 0o644); err != nil {
		t.Fatalf("write content sniff file: %v", err)
	}
	listingDir := filepath.Join(uploadDir, "listing")
	if err := os.MkdirAll(listingDir, 0o755); err != nil {
		t.Fatalf("create listing directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(listingDir, "entry.txt"), []byte("entry"), 0o644); err != nil {
		t.Fatalf("write listing entry: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	router := server.New(
		server.WithListener(listener),
		server.WithTransport(standard.NewTransporter),
	)
	staticGetHandler, staticHeadHandler, err := infrahttphertz.StaticHandlers(uploadDir, "/uploads")
	if err != nil {
		t.Fatalf("create static handlers: %v", err)
	}
	router.GET("/uploads/*filepath", staticGetHandler)
	router.HEAD("/uploads/*filepath", staticHeadHandler)

	transport := &http.Transport{}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	runErr := make(chan error, 1)
	go func() {
		runErr <- router.Engine.Run()
	}()
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := router.Engine.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown hertz server: %v", err)
		}
		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("run hertz server: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("hertz server did not stop")
		}
	})

	url := "http://" + listener.Addr().String() + "/uploads/video/range.mp4"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new range request: %v", err)
	}
	req.Header.Set("Range", "bytes=0-15")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("perform range request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected status %d, got %d", http.StatusPartialContent, resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes 0-15/32" {
		t.Fatalf("expected content range bytes 0-15/32, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read range body: %v", err)
	}
	if got := string(body); got != string(content[:16]) {
		t.Fatalf("expected ranged body %q, got %q", string(content[:16]), got)
	}

	headReq, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		t.Fatalf("new head request: %v", err)
	}
	headResp, err := client.Do(headReq)
	if err != nil {
		t.Fatalf("perform head request: %v", err)
	}
	defer headResp.Body.Close()
	if headResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HEAD status %d, got %d", http.StatusOK, headResp.StatusCode)
	}
	if headResp.ContentLength != int64(len(content)) {
		t.Fatalf("expected HEAD content length %d, got %d", len(content), headResp.ContentLength)
	}

	rangeHeadReq, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		t.Fatalf("new range head request: %v", err)
	}
	rangeHeadReq.Header.Set("Range", "bytes=0-15")
	rangeHeadResp, err := client.Do(rangeHeadReq)
	if err != nil {
		t.Fatalf("perform range head request: %v", err)
	}
	defer rangeHeadResp.Body.Close()
	if rangeHeadResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected range HEAD status %d, got %d", http.StatusPartialContent, rangeHeadResp.StatusCode)
	}
	if got := rangeHeadResp.Header.Get("Content-Range"); got != "bytes 0-15/32" {
		t.Fatalf("expected range HEAD content range bytes 0-15/32, got %q", got)
	}
	if rangeHeadResp.ContentLength != 16 {
		t.Fatalf("expected range HEAD content length 16, got %d", rangeHeadResp.ContentLength)
	}

	conditionalReq, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		t.Fatalf("new conditional head request: %v", err)
	}
	conditionalReq.Header.Set("If-Modified-Since", headResp.Header.Get("Last-Modified"))
	conditionalResp, err := client.Do(conditionalReq)
	if err != nil {
		t.Fatalf("perform conditional head request: %v", err)
	}
	defer conditionalResp.Body.Close()
	if conditionalResp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected conditional HEAD status %d, got %d", http.StatusNotModified, conditionalResp.StatusCode)
	}

	sniffReq, err := http.NewRequest(http.MethodHead, "http://"+listener.Addr().String()+"/uploads/image", nil)
	if err != nil {
		t.Fatalf("new content sniff request: %v", err)
	}
	sniffResp, err := client.Do(sniffReq)
	if err != nil {
		t.Fatalf("perform content sniff request: %v", err)
	}
	defer sniffResp.Body.Close()
	if got := sniffResp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("expected sniffed content type image/png, got %q", got)
	}

	listingResp, err := client.Head("http://" + listener.Addr().String() + "/uploads/listing/")
	if err != nil {
		t.Fatalf("perform directory HEAD request: %v", err)
	}
	defer listingResp.Body.Close()
	if listingResp.StatusCode != http.StatusOK {
		t.Fatalf("expected directory HEAD status %d, got %d", http.StatusOK, listingResp.StatusCode)
	}

	listingGetResp, err := client.Get("http://" + listener.Addr().String() + "/uploads/listing/")
	if err != nil {
		t.Fatalf("perform directory GET request: %v", err)
	}
	defer listingGetResp.Body.Close()
	listingBody, err := io.ReadAll(listingGetResp.Body)
	if err != nil {
		t.Fatalf("read directory response: %v", err)
	}
	if strings.Contains(string(listingBody), "entry.txt") {
		t.Fatalf("directory response must not expose uploaded filenames")
	}

	noRedirectClient := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	redirectResp, err := noRedirectClient.Head("http://" + listener.Addr().String() + "/uploads/listing")
	if err != nil {
		t.Fatalf("perform directory redirect HEAD request: %v", err)
	}
	defer redirectResp.Body.Close()
	if redirectResp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected directory redirect status %d, got %d", http.StatusMovedPermanently, redirectResp.StatusCode)
	}
}
