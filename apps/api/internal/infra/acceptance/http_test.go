package infraacceptance

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientUsesBearerAndDecodesBoundedJSON(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers=%v", request.Header)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	client := acceptanceTestHTTPClient(t, server, 1<<20)
	var output struct {
		OK bool `json:"ok"`
	}
	if err := client.JSON(context.Background(), http.MethodPost, server.URL, "token", map[string]string{"a": "b"}, &output); err != nil || !output.OK {
		t.Fatalf("output=%#v err=%v", output, err)
	}
}

func TestHTTPClientRejectsRedirectOversizeMalformedAndStatus(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		code    HTTPFailureCode
	}{
		{name: "redirect", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/other")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}, code: HTTPFailureStatus},
		{name: "oversize", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bytes.Repeat([]byte("x"), (64<<10)+1)) }, code: HTTPFailureOversize},
		{name: "malformed", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":`)) }, code: HTTPFailureDecode},
		{name: "status", handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("secret body"))
		}, code: HTTPFailureStatus},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(test.handler)
			defer server.Close()
			limit := int64(1 << 20)
			if test.name == "oversize" {
				limit = 64 << 10
			}
			client := acceptanceTestHTTPClient(t, server, limit)
			var output map[string]any
			err := client.JSON(context.Background(), http.MethodGet, server.URL, "", nil, &output)
			if !IsHTTPError(err, test.code) || strings.Contains(err.Error(), "secret body") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestHTTPClientPreservesCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	client := acceptanceTestHTTPClient(t, server, 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Text(ctx, server.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestHTTPClientChecksHealthAndCollectsMetrics(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
		case "/metrics":
			_, _ = writer.Write([]byte("# TYPE frux_tongyi_provider_operations_total counter\nfrux_tongyi_provider_operations_total{operation=\"startup\",result=\"success\"} 1\n"))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := acceptanceTestHTTPClient(t, server, 1<<20)
	if err := client.CheckHealth(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.CollectMetrics(context.Background(), server.URL+"/metrics")
	if err != nil || len(snapshot) != 1 {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
}

func acceptanceTestHTTPClient(t testing.TB, server *httptest.Server, limit int64) *HTTPClient {
	t.Helper()
	client, err := NewHTTPClient(time.Second, limit, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}
