package test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	interfaceshttpupload "GCFeed/internal/interfaces/http/upload"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

type stubUploadProcessor struct {
	validateCalls  int
	faststartCalls int
	validateErr    error
	faststartErr   error
}

type stubUploadOwnershipRecorder struct {
	records []struct {
		ownerID  int64
		assetURL string
		kind     string
	}
	err error
}

func (r *stubUploadOwnershipRecorder) RecordLocalUpload(_ context.Context, ownerID int64, assetURL, kind string) error {
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, struct {
		ownerID  int64
		assetURL string
		kind     string
	}{ownerID: ownerID, assetURL: assetURL, kind: kind})
	return nil
}

func (p *stubUploadProcessor) ValidateVideo(ctx context.Context, path string) error {
	p.validateCalls++
	if p.validateErr != nil {
		return p.validateErr
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}

func (p *stubUploadProcessor) Faststart(ctx context.Context, path string) error {
	p.faststartCalls++
	if p.faststartErr != nil {
		return p.faststartErr
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte("faststart:"), data...), 0o644)
}

func TestUploadVideoValidationAndFaststart(t *testing.T) {
	router, root, processor, ownership := newUploadRouter(t)

	resp := performMultipartUpload(router, "/api/uploads", "video", "clip.mp4", sampleMP4Bytes())
	requireStatus(t, resp, http.StatusCreated)

	if processor.validateCalls != 1 || processor.faststartCalls != 1 {
		t.Fatalf("expected video validate and faststart once, got validate=%d faststart=%d", processor.validateCalls, processor.faststartCalls)
	}
	if len(ownership.records) != 1 || ownership.records[0].ownerID != 42 || ownership.records[0].kind != "video" {
		t.Fatalf("unexpected ownership records: %+v", ownership.records)
	}

	var payload struct {
		URL      string `json:"url"`
		Kind     string `json:"kind"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	decodeJSON(t, resp, &payload)
	if payload.Kind != "video" || filepath.Ext(payload.Filename) != ".mp4" {
		t.Fatalf("unexpected upload response: %+v", payload)
	}

	createdPath := filepath.Join(root, "video", payload.Filename)
	data, err := os.ReadFile(createdPath)
	if err != nil {
		t.Fatalf("read uploaded video: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("faststart:")) {
		t.Fatalf("expected faststart processor to rewrite video")
	}
}

func TestUploadValidationRejectsBadFiles(t *testing.T) {
	router, _, processor, ownership := newUploadRouter(t)

	badExt := performMultipartUpload(router, "/api/uploads", "video", "clip.exe", sampleMP4Bytes())
	requireStatus(t, badExt, http.StatusBadRequest)

	badMime := performMultipartUpload(router, "/api/uploads", "cover", "cover.jpg", []byte("plain text"))
	requireStatus(t, badMime, http.StatusBadRequest)

	badKind := performMultipartUpload(router, "/api/uploads", "archive", "file.bin", []byte("content"))
	requireStatus(t, badKind, http.StatusBadRequest)

	oversizedImage := performMultipartUpload(router, "/api/uploads", "cover", "cover.png", make([]byte, (20<<20)+1))
	requireStatus(t, oversizedImage, http.StatusBadRequest)

	cover := performMultipartUpload(router, "/api/uploads", "cover", "cover.png", samplePNGBytes())
	requireStatus(t, cover, http.StatusCreated)

	if processor.validateCalls != 0 || processor.faststartCalls != 0 {
		t.Fatalf("expected image uploads to skip video processor, got validate=%d faststart=%d", processor.validateCalls, processor.faststartCalls)
	}
	if len(ownership.records) != 1 || ownership.records[0].kind != "cover" {
		t.Fatalf("expected cover ownership record, got %+v", ownership.records)
	}
}

func TestUploadProcessingFailureRemovesTarget(t *testing.T) {
	router, root, processor, _ := newUploadRouter(t)
	processor.validateErr = errors.New("invalid video metadata")

	resp := performMultipartUpload(router, "/api/uploads", "video", "clip.mp4", sampleMP4Bytes())
	requireStatus(t, resp, http.StatusBadRequest)

	entries, err := os.ReadDir(filepath.Join(root, "video"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read video directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed video upload should remove target file")
	}
}

func TestUploadOwnershipFailureRemovesTarget(t *testing.T) {
	router, root, _, ownership := newUploadRouter(t)
	ownership.err = errors.New("database unavailable")

	resp := performMultipartUpload(router, "/api/uploads", "cover", "cover.png", samplePNGBytes())
	requireStatus(t, resp, http.StatusInternalServerError)
	entries, err := os.ReadDir(filepath.Join(root, "cover"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read cover directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatal("ownership failure should remove the uploaded file")
	}
}

func newUploadRouter(t *testing.T) (*server.Hertz, string, *stubUploadProcessor, *stubUploadOwnershipRecorder) {
	t.Helper()

	root := t.TempDir()
	processor := &stubUploadProcessor{}
	ownership := &stubUploadOwnershipRecorder{}
	handler := interfaceshttpupload.NewWithProcessor(
		root,
		processor,
		interfaceshttpupload.WithOwnershipRecorder(ownership),
	)

	router := server.New(server.WithDisablePreParseMultipartForm(true))
	api := router.Group("/api")
	api.POST("/uploads", func(ctx context.Context, c *app.RequestContext) {
		c.Set(interfaceshttpmiddleware.ContextUserIDKey, int64(42))
		c.Next(ctx)
	}, handler.Create)
	return router, root, processor, ownership
}

func performMultipartUpload(router *server.Hertz, path string, kind string, filename string, content []byte) *ut.ResponseRecorder {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("kind", kind)
	part, _ := writer.CreateFormFile("file", filename)
	_, _ = io.Copy(part, bytes.NewReader(content))
	_ = writer.Close()

	return ut.PerformRequest(
		router.Engine,
		http.MethodPost,
		path,
		&ut.Body{Body: body, Len: body.Len()},
		ut.Header{Key: "Content-Type", Value: writer.FormDataContentType()},
	)
}

func sampleMP4Bytes() []byte {
	data := make([]byte, 0, 128)
	data = append(data, 0x00, 0x00, 0x00, 0x18)
	data = append(data, []byte("ftypmp42")...)
	data = append(data, 0x00, 0x00, 0x00, 0x00)
	data = append(data, []byte("mp42isom")...)
	data = append(data, bytes.Repeat([]byte{0x00}, 80)...)
	return data
}

func samplePNGBytes() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00,
	}
}
