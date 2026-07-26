package interfaceshttpbinding

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	infrahttphertz "GCFeed/internal/infra/httphertz"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestBindJSONRejectsOversizedStream(t *testing.T) {
	h := server.New(
		server.WithMaxRequestBodySize(1024),
		server.WithStreamBody(true),
		server.WithDisablePrintRoute(true),
	)
	var bindErr error
	h.POST("/json", func(_ context.Context, c *app.RequestContext) {
		var payload map[string]any
		bindErr = BindJSON(c, &payload)
		if bindErr != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	body := append([]byte(`{"data":"`), bytes.Repeat([]byte("a"), MaxJSONBodyBytes)...)
	body = append(body, []byte(`"}`)...)
	resp := ut.PerformRequest(
		h.Engine,
		http.MethodPost,
		"/json",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
	if !errors.Is(bindErr, ErrJSONBodyTooLarge) {
		t.Fatalf("expected ErrJSONBodyTooLarge, got %v", bindErr)
	}
}

func TestBindJSONMarksConsumedStreamForKeepAlive(t *testing.T) {
	h := server.New(
		server.WithMaxRequestBodySize(1),
		server.WithStreamBody(true),
		server.WithDisablePrintRoute(true),
	)
	h.Use(infrahttphertz.RequestStreamCleanupMiddleware())
	h.POST("/json", func(_ context.Context, c *app.RequestContext) {
		var payload map[string]string
		if err := BindJSON(c, &payload); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}

		c.Status(http.StatusNoContent)
	})

	body := []byte(`{"data":"value"}`)
	resp := ut.PerformRequest(
		h.Engine,
		http.MethodPost,
		"/json",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusNoContent, resp.Code, resp.Body.String())
	}
	if got := resp.Header().Get("Connection"); got == "close" {
		t.Fatalf("fully consumed JSON stream should keep the connection reusable")
	}
}

func TestBindStrictJSONRejectsUnknownFieldsAndCustomLimit(t *testing.T) {
	h := server.New(server.WithDisablePrintRoute(true))
	var bindErr error
	h.POST("/json", func(_ context.Context, c *app.RequestContext) {
		var payload struct {
			Value string `json:"value"`
		}
		bindErr = BindStrictJSON(c, &payload, 32)
		if bindErr != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	unknown := []byte(`{"value":"ok","token":"prohibited"}`)
	resp := ut.PerformRequest(
		h.Engine,
		http.MethodPost,
		"/json",
		&ut.Body{Body: bytes.NewReader(unknown), Len: len(unknown)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
	if resp.Code != http.StatusBadRequest || bindErr == nil {
		t.Fatalf("expected strict unknown-field rejection, status=%d err=%v", resp.Code, bindErr)
	}

	oversized := []byte(`{"value":"abcdefghijklmnopqrstuvwxyz"}`)
	resp = ut.PerformRequest(
		h.Engine,
		http.MethodPost,
		"/json",
		&ut.Body{Body: bytes.NewReader(oversized), Len: len(oversized)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
	if resp.Code != http.StatusBadRequest || !errors.Is(bindErr, ErrJSONBodyTooLarge) {
		t.Fatalf("expected custom size rejection, status=%d err=%v", resp.Code, bindErr)
	}
}
