package inframetrics

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHTTPMiddlewareUsesNormalizedRoute(t *testing.T) {
	h := server.New(server.WithDisablePrintRoute(true))
	h.Use(HTTPMiddleware())
	h.GET("/items/:id", func(_ context.Context, c *app.RequestContext) {
		c.Status(http.StatusNoContent)
	})

	normalized := HTTPRequestsTotal.WithLabelValues(http.MethodGet, "/items/:id", "204")
	raw := HTTPRequestsTotal.WithLabelValues(http.MethodGet, "/items/42", "204")
	normalizedBefore := testutil.ToFloat64(normalized)
	rawBefore := testutil.ToFloat64(raw)

	resp := ut.PerformRequest(h.Engine, http.MethodGet, "/items/42", nil)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, resp.Code)
	}
	if got := testutil.ToFloat64(normalized) - normalizedBefore; got != 1 {
		t.Fatalf("expected normalized route counter delta 1, got %v", got)
	}
	if got := testutil.ToFloat64(raw) - rawBefore; got != 0 {
		t.Fatalf("expected raw route counter delta 0, got %v", got)
	}
}
