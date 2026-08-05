package interfaceshttpupload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"
)

func TestValidateVideoMetadata(t *testing.T) {
	valid := &probeResult{
		Streams: []probeStream{
			{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080},
			{CodecType: "audio", CodecName: "aac"},
		},
		Format: probeFormat{Duration: "60.5"},
	}
	if err := validateVideoMetadata(valid); err != nil {
		t.Fatalf("expected valid metadata, got %v", err)
	}

	for _, codecName := range []string{"h264", "h265", "hevc", "vp8", "vp9", "av1"} {
		metadata := &probeResult{
			Streams: []probeStream{
				{CodecType: "video", CodecName: codecName, Width: 1920, Height: 1080},
				{CodecType: "audio", CodecName: "aac"},
			},
			Format: probeFormat{Duration: "60.5"},
		}
		if err := validateVideoMetadata(metadata); err != nil {
			t.Fatalf("expected %s metadata to pass, got %v", codecName, err)
		}
	}

	long := *valid
	long.Format = probeFormat{Duration: "900"}
	if err := validateVideoMetadata(&long); err == nil {
		t.Fatalf("expected long video metadata to fail")
	}

	large := *valid
	large.Streams = []probeStream{{CodecType: "video", CodecName: "h264", Width: 4096, Height: 2160}}
	if err := validateVideoMetadata(&large); err == nil {
		t.Fatalf("expected large video metadata to fail")
	}

	codec := *valid
	codec.Streams = []probeStream{{CodecType: "video", CodecName: "mpeg2video", Width: 1280, Height: 720}}
	if err := validateVideoMetadata(&codec); err == nil {
		t.Fatalf("expected unsupported codec metadata to fail")
	}
}

func TestCreateFaststartTempFileUsesMP4Extension(t *testing.T) {
	dir := t.TempDir()
	tmp, err := createFaststartTempFile(filepath.Join(dir, "clip.mp4"))
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	if filepath.Dir(tmpPath) != dir {
		t.Fatalf("expected temp file in upload dir, got %s", tmpPath)
	}
	if !strings.HasSuffix(tmpPath, ".faststart.mp4") {
		t.Fatalf("expected mp4 temp extension, got %s", tmpPath)
	}
}

func TestReadUploadFormRejectsBodyLimit(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(make([]byte, 2048)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	h := server.New(server.WithDisablePreParseMultipartForm(true))
	h.POST("/upload", func(_ context.Context, c *app.RequestContext) {
		form, _, _, err := readUploadForm(c, 1024)
		if form != nil {
			_ = form.RemoveAll()
		}
		if errors.Is(err, errUploadTooLarge) {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusInternalServerError)
	})

	resp := ut.PerformRequest(
		h.Engine,
		http.MethodPost,
		"/upload",
		&ut.Body{Body: body, Len: body.Len()},
		ut.Header{Key: "Content-Type", Value: writer.FormDataContentType()},
	)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusBadRequest, resp.Code, resp.Body.String())
	}
}

func TestWriteUploadProcessingErrorRedactsWrappedDetails(t *testing.T) {
	h := server.New(server.WithDisablePrintRoute(true))
	h.GET("/upload", func(_ context.Context, c *app.RequestContext) {
		writeUploadProcessingError(c, fmt.Errorf("%w: ffmpeg stderr contains bucket=secret", errFaststartFailed))
	})

	resp := ut.PerformRequest(h.Engine, http.MethodGet, "/upload", nil)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusInternalServerError, resp.Code, resp.Body.String())
	}
	if got := resp.Body.String(); got != `{"code":"`+interfaceshttpapierror.CodeUploadProcessingFailed+`","error":"video faststart failed"}` {
		t.Fatalf("unexpected upload processing body: %s", got)
	}
	if strings.Contains(resp.Body.String(), "bucket=secret") {
		t.Fatalf("wrapped upload processing detail leaked: %s", resp.Body.String())
	}
}
