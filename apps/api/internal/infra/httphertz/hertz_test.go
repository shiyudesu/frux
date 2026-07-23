package infrahttphertz

import (
	"net"
	"net/http"
	"testing"

	infraconfig "GCFeed/internal/infra/config"

	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestInitConfiguresStreamingAndDisconnectCancellation(t *testing.T) {
	h, err := Init(&infraconfig.Config{Port: 0})
	if err != nil {
		t.Fatalf("init hertz: %v", err)
	}
	defer h.Engine.Close()
	options := h.Engine.GetOptions()

	if !options.StreamRequestBody {
		t.Fatalf("expected request body streaming to be enabled")
	}
	if !options.DisablePreParseMultipartForm {
		t.Fatalf("expected multipart pre-parsing to be disabled")
	}
	if !options.SenseClientDisconnection {
		t.Fatalf("expected client disconnection sensing to be enabled")
	}
	if options.MaxRequestBodySize != maxBufferedRequestBodySize {
		t.Fatalf("expected buffered request threshold %d, got %d", maxBufferedRequestBodySize, options.MaxRequestBodySize)
	}
	if options.MaxKeepBodySize != 0 {
		t.Fatalf("expected large request buffers not to be retained, got %d", options.MaxKeepBodySize)
	}
	if options.ReadTimeout != requestReadTimeout {
		t.Fatalf("expected read timeout %s, got %s", requestReadTimeout, options.ReadTimeout)
	}
}

func TestInitPreservesNotFoundContract(t *testing.T) {
	h, err := Init(&infraconfig.Config{Port: 0})
	if err != nil {
		t.Fatalf("init hertz: %v", err)
	}
	defer h.Engine.Close()
	resp := ut.PerformRequest(h.Engine, http.MethodGet, "/missing", nil)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
	if got := resp.Body.String(); got != "404 page not found" {
		t.Fatalf("unexpected not found body %q", got)
	}
	if got := resp.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected not found content type %q", got)
	}
}

func TestInitReturnsBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	if h, err := Init(&infraconfig.Config{Port: port}); err == nil {
		_ = h.Engine.Close()
		t.Fatalf("expected init bind failure")
	}
}
