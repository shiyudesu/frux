package test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	inframetrics "GCFeed/internal/infra/metrics"
	interfaceshttprouter "GCFeed/internal/interfaces/http/router"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestHealthAndMetricsRoutes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	h := server.New(
		server.WithListener(listener),
		server.WithTransport(standard.NewTransporter),
		server.WithDisablePrintRoute(true),
	)
	h.Use(inframetrics.HTTPMiddleware())
	h.GET("/health", interfaceshttprouter.HealthCheck)
	h.GET("/metrics", adaptor.HertzHandler(promhttp.Handler()))

	transport := &http.Transport{}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	runErr := make(chan error, 1)
	go func() {
		runErr <- h.Engine.Run()
	}()
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Engine.Shutdown(shutdownCtx); err != nil {
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

	baseURL := "http://" + listener.Addr().String()

	healthResp, err := client.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("expected health status %d, got %d", http.StatusOK, healthResp.StatusCode)
	}
	var healthPayload map[string]string
	if err := json.NewDecoder(healthResp.Body).Decode(&healthPayload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if healthPayload["message"] != "All is well" {
		t.Fatalf("unexpected health payload: %+v", healthPayload)
	}

	metricsResp, err := client.Get(baseURL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer metricsResp.Body.Close()
	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected metrics status %d, got %d", http.StatusOK, metricsResp.StatusCode)
	}
	metricsBody, err := io.ReadAll(metricsResp.Body)
	if err != nil {
		t.Fatalf("read metrics response: %v", err)
	}
	if !strings.Contains(string(metricsBody), "gcfeed_http_requests_total") {
		t.Fatalf("expected GCFeed HTTP metrics in response")
	}
}
