package infraembedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

func TestTongyiAdapterConfigFromEnvironment(t *testing.T) {
	t.Setenv("FRUX_MULTIMODAL_PROVIDER_LISTEN_ADDR", "127.0.0.1:18099")
	t.Setenv("FRUX_MULTIMODAL_HMAC_SECRET", multimodalTestSecret)
	t.Setenv("DASHSCOPE_MULTIMODAL_ENDPOINT", "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding")
	t.Setenv("DASHSCOPE_API_KEY", "sk-test-value")
	t.Setenv("FRUX_TONGYI_UPSTREAM_TIMEOUT", "5s")
	t.Setenv("FRUX_TONGYI_MAX_REQUEST_BYTES", "4194304")
	t.Setenv("FRUX_TONGYI_MAX_RESPONSE_BYTES", "1048576")
	t.Setenv("FRUX_TONGYI_SHUTDOWN_TIMEOUT", "7s")
	config, err := LoadTongyiAdapterConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.ListenAddress != "127.0.0.1:18099" || config.UpstreamTimeout != 5*time.Second ||
		config.MaxInboundRequestBytes != 4<<20 || config.MaxUpstreamResponseBytes != 1<<20 ||
		config.ShutdownTimeout != 7*time.Second {
		t.Fatalf("config=%#v", config)
	}
}

func TestTongyiAdapterConfigRejectsSecretsEndpointsAndBounds(t *testing.T) {
	base := tongyiTestConfig("https://workspace.example.com/multimodal", nil)
	for _, test := range []struct {
		name   string
		mutate func(*TongyiAdapterConfig)
	}{
		{name: "missing HMAC", mutate: func(c *TongyiAdapterConfig) { c.FruxHMACSecret = "" }},
		{name: "missing API key", mutate: func(c *TongyiAdapterConfig) { c.DashScopeAPIKey = "" }},
		{name: "HTTP upstream", mutate: func(c *TongyiAdapterConfig) { c.DashScopeEndpoint = "http://workspace.example.com/multimodal" }},
		{name: "endpoint credentials", mutate: func(c *TongyiAdapterConfig) { c.DashScopeEndpoint = "https://user@workspace.example.com/multimodal" }},
		{name: "invalid listen", mutate: func(c *TongyiAdapterConfig) { c.ListenAddress = "8099" }},
		{name: "timeout", mutate: func(c *TongyiAdapterConfig) { c.UpstreamTimeout = 10 * time.Millisecond }},
		{name: "request limit", mutate: func(c *TongyiAdapterConfig) { c.MaxInboundRequestBytes = 1024 }},
		{name: "response limit", mutate: func(c *TongyiAdapterConfig) { c.MaxUpstreamResponseBytes = 1024 }},
		{name: "shutdown", mutate: func(c *TongyiAdapterConfig) { c.ShutdownTimeout = 100 * time.Millisecond }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := base
			test.mutate(&config)
			if err := validateTongyiAdapterConfig(config); !errors.Is(err, ErrInvalidTongyiAdapterConfig) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestTongyiClientTranslatesQueryAndFusedVideo(t *testing.T) {
	var mutex sync.Mutex
	requests := make([]tongyiRequest, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer sk-test-value" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers=%v", request.Header)
		}
		var payload tongyiRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mutex.Lock()
		requests = append(requests, payload)
		mutex.Unlock()
		responseType := "text"
		if len(payload.Input.Contents) == 1 && len(payload.Input.Contents[0].MultiImages) > 0 {
			responseType = "fused"
		}
		writeTongyiUpstreamResponse(t, writer, responseType, tongyiNonNormalizedVector(), TongyiUsage{
			InputTokens: 809, ImageTokens: 804, TextTokens: 5, OutputTokens: 3, TotalTokens: 812,
		})
	}))
	defer server.Close()
	client := newTongyiTestClient(t, server, 2<<20, 1<<20, time.Second)

	queryResult, err := client.EmbedQuery(context.Background(), "城市雨夜")
	if err != nil {
		t.Fatal(err)
	}
	assertTongyiNormalizedVector(t, queryResult.Values)
	images := []applicationembedding.PreparedMultimodalImage{
		{MIMEType: "image/jpeg", Content: []byte("one")},
		{MIMEType: "image/webp", Content: []byte("two")},
	}
	videoResult, err := client.EmbedVideo(context.Background(), "雨夜街道", images)
	if err != nil {
		t.Fatal(err)
	}
	assertTongyiNormalizedVector(t, videoResult.Values)

	mutex.Lock()
	defer mutex.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	query := requests[0]
	if query.Model != TongyiUpstreamModel || query.Parameters.Dimension != TongyiDimension ||
		query.Parameters.OutputType != "dense" || query.Parameters.ResLevel != 0 ||
		len(query.Input.Contents) != 1 || query.Input.Contents[0].Text != "城市雨夜" ||
		len(query.Input.Contents[0].MultiImages) != 0 {
		t.Fatalf("query payload=%#v", query)
	}
	video := requests[1]
	if video.Parameters.ResLevel != TongyiResolutionLevel || len(video.Input.Contents) != 1 ||
		video.Input.Contents[0].Text != "雨夜街道" || len(video.Input.Contents[0].MultiImages) != 2 ||
		video.Input.Contents[0].MultiImages[0] != "data:image/jpeg;base64,b25l" ||
		video.Input.Contents[0].MultiImages[1] != "data:image/webp;base64,dHdv" {
		t.Fatalf("video payload=%#v", video)
	}
}

func TestTongyiClientValidatesResponsesAndMapsFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       func() []byte
		retryable  bool
		retryAfter string
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, retryable: true, retryAfter: "9", body: func() []byte { return []byte(`{"code":"Throttled","message":"secret"}`) }},
		{name: "server failure", status: http.StatusBadGateway, retryable: true, body: func() []byte { return []byte(`{"message":"secret"}`) }},
		{name: "credential rejection", status: http.StatusUnauthorized, retryable: false, body: func() []byte { return []byte(`{"message":"invalid key secret"}`) }},
		{name: "malformed", status: http.StatusOK, retryable: false, body: func() []byte { return []byte(`{"output":`) }},
		{name: "zero vector", status: http.StatusOK, retryable: false, body: func() []byte {
			return tongyiResponseBody("text", make([]float64, TongyiDimension), TongyiUsage{InputTokens: 1, TotalTokens: 1})
		}},
		{name: "wrong dimension", status: http.StatusOK, retryable: false, body: func() []byte {
			return tongyiResponseBody("text", []float64{1}, TongyiUsage{InputTokens: 1, TotalTokens: 1})
		}},
		{name: "wrong type", status: http.StatusOK, retryable: false, body: func() []byte {
			return tongyiResponseBody("fused", tongyiNonNormalizedVector(), TongyiUsage{InputTokens: 1, TotalTokens: 1})
		}},
		{name: "invalid usage", status: http.StatusOK, retryable: false, body: func() []byte { return tongyiResponseBody("text", tongyiNonNormalizedVector(), TongyiUsage{}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					writer.Header().Set("Retry-After", test.retryAfter)
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write(test.body())
			}))
			defer server.Close()
			client := newTongyiTestClient(t, server, 2<<20, 1<<20, time.Second)
			_, err := client.EmbedQuery(context.Background(), "query")
			var upstreamError *TongyiUpstreamError
			if !errors.As(err, &upstreamError) || upstreamError.Retryable != test.retryable || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%v", err)
			}
			if test.retryAfter != "" && upstreamError.RetryAfter != 9*time.Second {
				t.Fatalf("retry after=%v", upstreamError.RetryAfter)
			}
		})
	}
}

func TestTongyiClientBoundsRedirectTimeoutCancellationAndResponse(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Location", "/other")
			writer.WriteHeader(http.StatusTemporaryRedirect)
			_, _ = writer.Write([]byte(`{"message":"redirect"}`))
		}))
		defer server.Close()
		client := newTongyiTestClient(t, server, 2<<20, 1<<20, time.Second)
		_, err := client.EmbedQuery(context.Background(), "query")
		var upstreamError *TongyiUpstreamError
		if !errors.As(err, &upstreamError) || upstreamError.Retryable {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			select {
			case <-request.Context().Done():
			case <-time.After(250 * time.Millisecond):
				writeTongyiUpstreamResponse(t, writer, "text", tongyiNonNormalizedVector(), TongyiUsage{InputTokens: 1, TotalTokens: 1})
			}
		}))
		defer server.Close()
		client := newTongyiTestClient(t, server, 2<<20, 1<<20, 100*time.Millisecond)
		_, err := client.EmbedQuery(context.Background(), "query")
		var upstreamError *TongyiUpstreamError
		if !errors.As(err, &upstreamError) || !upstreamError.Retryable {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		client := newTongyiTestClient(t, server, 2<<20, 1<<20, time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.EmbedQuery(ctx, "query")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("oversized response", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write(bytes.Repeat([]byte("x"), (64<<10)+1))
		}))
		defer server.Close()
		client := newTongyiTestClient(t, server, 2<<20, 64<<10, time.Second)
		_, err := client.EmbedQuery(context.Background(), "query")
		var upstreamError *TongyiUpstreamError
		if !errors.As(err, &upstreamError) || upstreamError.Retryable {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestValidateTongyiResponseRejectsNonFiniteVector(t *testing.T) {
	response := tongyiResponse{}
	response.Output.Embeddings = append(response.Output.Embeddings, struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
		Type      string    `json:"type"`
	}{Index: 0, Embedding: tongyiNonNormalizedVector(), Type: "text"})
	response.Output.Embeddings[0].Embedding[0] = math.NaN()
	response.Usage.InputTokens = 1
	response.Usage.TotalTokens = 1
	if _, err := validateTongyiResponse(response, "text"); err == nil {
		t.Fatal("expected non-finite vector error")
	}
}

func TestTongyiUsageMetricsUseClosedLabels(t *testing.T) {
	operation := inframetrics.TongyiProviderOperationsTotal.WithLabelValues("query", "success")
	unknown := inframetrics.TongyiProviderOperationsTotal.WithLabelValues("unknown", "unknown")
	input := inframetrics.TongyiProviderTokensTotal.WithLabelValues("video", "input")
	beforeOperation := testutil.ToFloat64(operation)
	beforeUnknown := testutil.ToFloat64(unknown)
	beforeInput := testutil.ToFloat64(input)
	inframetrics.ObserveTongyiProvider("query", "success", time.Millisecond)
	inframetrics.ObserveTongyiProvider("secret endpoint", "raw body", time.Millisecond)
	inframetrics.ObserveTongyiUsage("video", 10, 7, 3, 1)
	if testutil.ToFloat64(operation)-beforeOperation != 1 || testutil.ToFloat64(unknown)-beforeUnknown != 1 ||
		testutil.ToFloat64(input)-beforeInput != 10 {
		t.Fatal("Tongyi metrics did not fold to closed labels")
	}
}

func tongyiTestConfig(endpoint string, httpClient *http.Client) TongyiAdapterConfig {
	return TongyiAdapterConfig{
		ListenAddress: "127.0.0.1:8099", FruxHMACSecret: multimodalTestSecret,
		DashScopeEndpoint: endpoint, DashScopeAPIKey: "sk-test-value",
		UpstreamTimeout: time.Second, MaxInboundRequestBytes: 2 << 20,
		MaxUpstreamResponseBytes: 1 << 20, ShutdownTimeout: 5 * time.Second,
		HTTPClient: httpClient,
	}
}

func newTongyiTestClient(t testing.TB, server *httptest.Server, maxRequest, maxResponse int64, timeout time.Duration) *TongyiClient {
	t.Helper()
	config := tongyiTestConfig(server.URL, server.Client())
	config.UpstreamTimeout = timeout
	config.MaxInboundRequestBytes = maxRequest
	config.MaxUpstreamResponseBytes = maxResponse
	client, err := NewTongyiClient(config)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeTongyiUpstreamResponse(t testing.TB, writer http.ResponseWriter, responseType string, vector []float64, usage TongyiUsage) {
	t.Helper()
	body := tongyiResponseBody(responseType, vector, usage)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	if _, err := writer.Write(body); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func tongyiResponseBody(responseType string, vector []float64, usage TongyiUsage) []byte {
	response := map[string]any{
		"output": map[string]any{"embeddings": []any{map[string]any{
			"index": 0, "embedding": vector, "type": responseType,
		}}},
		"usage": map[string]any{
			"input_tokens": usage.InputTokens,
			"input_tokens_details": map[string]any{
				"image_tokens": usage.ImageTokens, "text_tokens": usage.TextTokens,
			},
			"output_tokens": usage.OutputTokens, "total_tokens": usage.TotalTokens,
		},
		"request_id": "upstream-request-id-must-not-leak",
	}
	body, err := json.Marshal(response)
	if err != nil {
		panic(err)
	}
	return body
}

func tongyiNonNormalizedVector() []float64 {
	vector := make([]float64, TongyiDimension)
	vector[0] = 3
	vector[1] = 4
	return vector
}

func assertTongyiNormalizedVector(t testing.TB, vector []float64) {
	t.Helper()
	if len(vector) != TongyiDimension || math.Abs(vector[0]-0.6) > 1e-12 || math.Abs(vector[1]-0.8) > 1e-12 {
		t.Fatalf("vector prefix=%v length=%d", vector[:min(len(vector), 2)], len(vector))
	}
}
