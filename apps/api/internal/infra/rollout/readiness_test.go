package rollout

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeSessionSemanticRuntimeReady(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		status  int
		want    bool
		wantErr bool
	}{
		{name: "ready", body: "# HELP x\nfrux_recommendation_session_semantic_runtime_ready 1\n", status: 200, want: true},
		{name: "not ready", body: "frux_recommendation_session_semantic_runtime_ready 0\n", status: 200},
		{name: "missing", body: "other_metric 1\n", status: 200, wantErr: true},
		{name: "duplicate", body: "frux_recommendation_session_semantic_runtime_ready 1\nfrux_recommendation_session_semantic_runtime_ready 1\n", status: 200, wantErr: true},
		{name: "malformed", body: "frux_recommendation_session_semantic_runtime_ready nope\n", status: 200, wantErr: true},
		{name: "nan", body: "frux_recommendation_session_semantic_runtime_ready NaN\n", status: 200, wantErr: true},
		{name: "extra labels", body: "frux_recommendation_session_semantic_runtime_ready{user=\"1\"} 1\n", status: 200, wantErr: true},
		{name: "http failure", body: "", status: 503, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			ready, err := ProbeSessionSemanticRuntimeReady(context.Background(), server.URL, time.Second, 1<<20)
			if ready != test.want || (err != nil) != test.wantErr {
				t.Fatalf("ready=%v error=%v", ready, err)
			}
		})
	}
}

func TestReadinessEndpointAndResponseBounds(t *testing.T) {
	if _, err := validateReadinessEndpoint("http://metrics.example.com/metrics"); !errors.Is(err, ErrInvalidReadinessEndpoint) {
		t.Fatalf("remote HTTP error=%v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", 70<<10)))
	}))
	defer server.Close()
	if _, err := ProbeSessionSemanticRuntimeReady(context.Background(), server.URL, time.Second, 64<<10); !errors.Is(err, ErrInvalidReadinessResponse) {
		t.Fatalf("oversized error=%v", err)
	}
}
