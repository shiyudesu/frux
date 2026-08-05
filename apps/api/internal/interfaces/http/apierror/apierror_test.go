package interfaceshttpapierror

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestWritePreservesStatusAndLegacyField(t *testing.T) {
	h := server.New(server.WithDisablePrintRoute(true))
	h.GET("/error", func(_ context.Context, c *app.RequestContext) {
		Write(c, http.StatusConflict, CodeConflict, "conflict")
	})

	response := ut.PerformRequest(h.Engine, http.MethodGet, "/error", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); got != `{"code":"CONFLICT","error":"conflict"}` {
		t.Fatalf("unexpected body: %s", got)
	}
}

func TestWriteInvalidRequestUsesSharedEnvelope(t *testing.T) {
	h := server.New(server.WithDisablePrintRoute(true))
	h.GET("/error", func(_ context.Context, c *app.RequestContext) {
		WriteInvalidRequest(c)
	})

	response := ut.PerformRequest(h.Engine, http.MethodGet, "/error", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); got != `{"code":"INVALID_REQUEST","error":"invalid request"}` {
		t.Fatalf("unexpected body: %s", got)
	}
}

func TestWriteInternalRedactsWrappedDetails(t *testing.T) {
	h := server.New(server.WithDisablePrintRoute(true))
	h.GET("/error", func(_ context.Context, c *app.RequestContext) {
		WriteInternalCode(c, CodeUploadRecordFailed, "failed to record upload", errors.New("dial tcp 10.0.0.7:3306: access denied"))
	})

	response := ut.PerformRequest(h.Engine, http.MethodGet, "/error", nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if body != `{"code":"UPLOAD_RECORD_FAILED","error":"failed to record upload"}` {
		t.Fatalf("unexpected body: %s", body)
	}
	if strings.Contains(body, "access denied") || strings.Contains(body, "10.0.0.7") {
		t.Fatalf("internal response leaked wrapped detail: %s", body)
	}
}
