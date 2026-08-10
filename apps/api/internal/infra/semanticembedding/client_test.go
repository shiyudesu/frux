package infrasemanticembedding

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

const semanticTestToken = "Strong-Internal-Token-For-Semantic-123!"

func validMetadataResponse() map[string]any {
	return map[string]any{
		"model":               domainembedding.SemanticModelName,
		"revision":            domainembedding.SemanticRevision,
		"dimension":           domainembedding.SemanticDimension,
		"max_sequence_tokens": 128, "dtype": "float32",
		"normalized": true, "device": "cpu",
		"limits": map[string]any{
			"max_batch_size": 32, "max_title_codepoints": 200,
			"max_description_codepoints": 2000,
			"max_total_codepoints":       16384, "max_request_bytes": 131072,
		},
	}
}

func unitSemanticVector() []float64 {
	vector := make([]float64, domainembedding.SemanticDimension)
	value := 1 / math.Sqrt(float64(domainembedding.SemanticDimension))
	for index := range vector {
		vector[index] = value
	}
	return vector
}

func newSemanticTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(infraconfig.SemanticEmbeddingConfig{
		Enabled: true, BaseURL: server.URL,
		MetadataTimeout: "3s", RequestTimeout: "17s",
	}, semanticTestToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestClientValidatesMetadataAndOrderedEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Internal-Token") != semanticTestToken {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/internal/v1/model" {
			_ = json.NewEncoder(writer).Encode(validMetadataResponse())
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model":     domainembedding.SemanticModelName,
			"revision":  domainembedding.SemanticRevision,
			"dimension": domainembedding.SemanticDimension,
			"items": []any{map[string]any{
				"id": "video:7", "index": 0, "embedding": unitSemanticVector(),
			}},
		})
	}))
	defer server.Close()
	client, err := New(infraconfig.SemanticEmbeddingConfig{
		Enabled: true, BaseURL: server.URL,
		MetadataTimeout: "3s", RequestTimeout: "17s",
	}, semanticTestToken)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.ValidateMetadata(context.Background()); err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
		ID: "video:7", Title: "标题", Description: "",
	}})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != domainembedding.SemanticDimension {
		t.Fatalf("vectors=%d err=%v", len(vectors), err)
	}
}

func TestClientMetadataContractAndResponseBounds(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "model", mutate: func(value map[string]any) { value["model"] = "other" }},
		{name: "revision", mutate: func(value map[string]any) { value["revision"] = "other" }},
		{name: "dimension", mutate: func(value map[string]any) { value["dimension"] = 383 }},
		{name: "dtype", mutate: func(value map[string]any) { value["dtype"] = "float64" }},
		{name: "normalization", mutate: func(value map[string]any) { value["normalized"] = false }},
		{name: "device", mutate: func(value map[string]any) { value["device"] = "cuda" }},
		{name: "limits", mutate: func(value map[string]any) {
			value["limits"].(map[string]any)["max_batch_size"] = 31
		}},
		{name: "unknown field", mutate: func(value map[string]any) { value["secret"] = "raw" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newSemanticTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				response := validMetadataResponse()
				test.mutate(response)
				_ = json.NewEncoder(writer).Encode(response)
			}))
			var semanticErr *applicationembedding.SemanticError
			if err := client.ValidateMetadata(context.Background()); !errors.As(err, &semanticErr) ||
				semanticErr.Result != applicationembedding.SemanticContract ||
				!semanticErr.Terminal {
				t.Fatalf("metadata error = %#v", err)
			}
		})
	}
	client := newSemanticTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"padding":"` + strings.Repeat("x", maxMetadataBytes) + `"}`))
	}))
	if err := client.ValidateMetadata(context.Background()); err == nil {
		t.Fatal("oversized metadata was accepted")
	}
}

func TestClientEmbeddingContractFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "model", mutate: func(value map[string]any) { value["model"] = "other" }},
		{name: "revision", mutate: func(value map[string]any) { value["revision"] = "other" }},
		{name: "dimension", mutate: func(value map[string]any) { value["dimension"] = 383 }},
		{name: "count", mutate: func(value map[string]any) { value["items"] = []any{} }},
		{name: "id", mutate: func(value map[string]any) {
			value["items"].([]any)[0].(map[string]any)["id"] = "video:8"
		}},
		{name: "index", mutate: func(value map[string]any) {
			value["items"].([]any)[0].(map[string]any)["index"] = 1
		}},
		{name: "component count", mutate: func(value map[string]any) {
			value["items"].([]any)[0].(map[string]any)["embedding"] = []float64{1}
		}},
		{name: "non unit", mutate: func(value map[string]any) {
			vector := unitSemanticVector()
			vector[0] = 2
			value["items"].([]any)[0].(map[string]any)["embedding"] = vector
		}},
		{name: "unknown field", mutate: func(value map[string]any) { value["raw"] = "secret" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newSemanticTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				response := map[string]any{
					"model":     domainembedding.SemanticModelName,
					"revision":  domainembedding.SemanticRevision,
					"dimension": domainembedding.SemanticDimension,
					"items": []any{map[string]any{
						"id": "video:7", "index": 0, "embedding": unitSemanticVector(),
					}},
				}
				test.mutate(response)
				_ = json.NewEncoder(writer).Encode(response)
			}))
			var semanticErr *applicationembedding.SemanticError
			_, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
				ID: "video:7", Title: "title",
			}})
			if !errors.As(err, &semanticErr) ||
				semanticErr.Result != applicationembedding.SemanticContract ||
				!semanticErr.Terminal {
				t.Fatalf("embedding error = %#v", err)
			}
		})
	}
	t.Run("non finite", func(t *testing.T) {
		client := newSemanticTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(
				`{"model":"` + domainembedding.SemanticModelName +
					`","revision":"` + domainembedding.SemanticRevision +
					`","dimension":384,"items":[{"id":"video:7","index":0,"embedding":[NaN]}]}`,
			))
		}))
		_, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
			ID: "video:7", Title: "title",
		}})
		assertSemanticResult(t, err, applicationembedding.SemanticContract)
	})
	t.Run("oversized response", func(t *testing.T) {
		client := newSemanticTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"padding":"` + strings.Repeat("x", maxEmbeddingBytes) + `"}`))
		}))
		_, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
			ID: "video:7", Title: "title",
		}})
		assertSemanticResult(t, err, applicationembedding.SemanticContract)
	})
}

func TestClientBoundsConcurrencyTimeoutCancellationAndPayload(t *testing.T) {
	var inFlight atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	client := newSemanticTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		var body embeddingRequest
		_ = json.NewDecoder(request.Body).Decode(&body)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model":     domainembedding.SemanticModelName,
			"revision":  domainembedding.SemanticRevision,
			"dimension": domainembedding.SemanticDimension,
			"items": []any{map[string]any{
				"id": body.Items[0].ID, "index": 0, "embedding": unitSemanticVector(),
			}},
		})
	}))
	client.requestTimeout = 2 * time.Second
	var wait sync.WaitGroup
	errs := make(chan error, 3)
	for index := range 3 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
				ID: "video:" + string(rune('1'+index)), Title: "title",
			}})
			errs <- err
		}()
	}
	<-entered
	<-entered
	select {
	case <-entered:
		t.Fatal("third request exceeded the two-request concurrency bound")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum in-flight requests = %d", maximum.Load())
	}

	timeoutRelease := make(chan struct{})
	timeoutClient := newSemanticTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-timeoutRelease
	}))
	timeoutClient.requestTimeout = 30 * time.Millisecond
	_, err := timeoutClient.Generate(context.Background(), []applicationembedding.SemanticInput{{
		ID: "video:7", Title: "title",
	}})
	close(timeoutRelease)
	assertSemanticResult(t, err, applicationembedding.SemanticTimeout)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = timeoutClient.Generate(canceled, []applicationembedding.SemanticInput{{
		ID: "video:7", Title: "title",
	}})
	assertSemanticResult(t, err, applicationembedding.SemanticCanceled)

	for name, items := range map[string][]applicationembedding.SemanticInput{
		"empty": nil,
		"batch": make([]applicationembedding.SemanticInput, 33),
		"id":    {{ID: " invalid", Title: "title"}},
		"duplicate": {
			{ID: "video:1", Title: "title"},
			{ID: "video:1", Title: "title"},
		},
		"title":      {{ID: "video:1", Title: strings.Repeat("x", 201)}},
		"total text": semanticInputsWithTotalCodepoints(32, 600),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := timeoutClient.Generate(context.Background(), items)
			assertSemanticResult(t, err, applicationembedding.SemanticContract)
		})
	}

	metadataRelease := make(chan struct{})
	metadataClient := newSemanticTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-metadataRelease
	}))
	metadataClient.metadataTimeout = 30 * time.Millisecond
	err = metadataClient.ValidateMetadata(context.Background())
	close(metadataRelease)
	assertSemanticResult(t, err, applicationembedding.SemanticTimeout)
}

func TestClientErrorsRedactTokenPayloadAndResponse(t *testing.T) {
	secretText := "semantic-secret-title"
	var requests atomic.Int32
	client := newSemanticTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(semanticTestToken + secretText))
	}))
	_, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
		ID: "video:7", Title: secretText,
	}})
	if err == nil {
		t.Fatal("authentication rejection was accepted")
	}
	for _, secret := range []string{semanticTestToken, secretText} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed secret %q: %v", secret, err)
		}
	}
	assertSemanticResult(t, err, applicationembedding.SemanticAuth)
	if requests.Load() != 1 {
		t.Fatalf("client retried automatically: requests=%d", requests.Load())
	}
}

func TestLiveSemanticServiceContract(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("FRUX_SEMANTIC_TEST_URL"))
	token := strings.TrimSpace(os.Getenv("FRUX_SEMANTIC_TEST_TOKEN"))
	if baseURL == "" || token == "" {
		t.Skip("set FRUX_SEMANTIC_TEST_URL and FRUX_SEMANTIC_TEST_TOKEN")
	}
	client, err := New(infraconfig.SemanticEmbeddingConfig{
		Enabled: true, BaseURL: baseURL,
		MetadataTimeout: "5s", RequestTimeout: "20s",
	}, token)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.ValidateMetadata(context.Background()); err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
		ID: "video:1", Title: "Frux 城市", Description: "雨后街道",
	}})
	if err != nil || len(vectors) != 1 ||
		len(vectors[0]) != domainembedding.SemanticDimension {
		t.Fatalf("live vectors=%d err=%v", len(vectors), err)
	}
}

func assertSemanticResult(
	t *testing.T,
	err error,
	want applicationembedding.SemanticResult,
) {
	t.Helper()
	var semanticErr *applicationembedding.SemanticError
	if !errors.As(err, &semanticErr) || semanticErr.Result != want {
		t.Fatalf("semantic error = %#v, want %s", err, want)
	}
}

func semanticInputsWithTotalCodepoints(count, codepoints int) []applicationembedding.SemanticInput {
	items := make([]applicationembedding.SemanticInput, count)
	for index := range items {
		items[index] = applicationembedding.SemanticInput{
			ID:          "video:" + string(rune('A'+index)),
			Title:       strings.Repeat("t", 200),
			Description: strings.Repeat("d", codepoints-200),
		}
	}
	return items
}

func TestClientRejectsReorderedOrWrongContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model":     domainembedding.SemanticModelName,
			"revision":  domainembedding.SemanticRevision,
			"dimension": domainembedding.SemanticDimension,
			"items":     []any{},
		})
	}))
	defer server.Close()
	client, err := New(infraconfig.SemanticEmbeddingConfig{
		Enabled: true, BaseURL: server.URL,
		MetadataTimeout: "3s", RequestTimeout: "17s",
	}, "Strong-Internal-Token-For-Semantic-123!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), []applicationembedding.SemanticInput{{
		ID: "video:7", Title: "标题",
	}}); err == nil {
		t.Fatal("partial response was accepted")
	}
}

func TestClientTokenValidationMatchesSharedPythonFixtures(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	content, err := os.ReadFile(filepath.Join(
		filepath.Dir(source),
		"../../../../semantic-embedding/fixtures/strong-token-fixtures.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(content, &fixtures); err != nil {
		t.Fatal(err)
	}
	cfg := infraconfig.SemanticEmbeddingConfig{
		Enabled: true, BaseURL: "http://semantic-embedding:8081",
		MetadataTimeout: "3s", RequestTimeout: "17s",
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			client, err := New(cfg, fixture.Value)
			if fixture.Valid {
				if err != nil || client == nil {
					t.Fatalf("valid token rejected: client=%v err=%v", client, err)
				}
				client.Close()
				return
			}
			if !errors.Is(err, infraconfig.ErrInvalidSemanticEmbeddingConfig) {
				t.Fatalf("invalid token error=%v", err)
			}
		})
	}
}
