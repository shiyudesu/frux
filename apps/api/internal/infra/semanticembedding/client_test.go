package infrasemanticembedding

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
)

func TestClientValidatesMetadataAndOrderedEmbedding(t *testing.T) {
	token := "Strong-Internal-Token-For-Semantic-123!"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Internal-Token") != token {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/internal/v1/model" {
			_ = json.NewEncoder(writer).Encode(map[string]any{
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
			})
			return
		}
		vector := make([]float64, domainembedding.SemanticDimension)
		value := 1 / math.Sqrt(float64(domainembedding.SemanticDimension))
		for index := range vector {
			vector[index] = value
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model":     domainembedding.SemanticModelName,
			"revision":  domainembedding.SemanticRevision,
			"dimension": domainembedding.SemanticDimension,
			"items": []any{map[string]any{
				"id": "video:7", "index": 0, "embedding": vector,
			}},
		})
	}))
	defer server.Close()
	client, err := New(infraconfig.SemanticEmbeddingConfig{
		Enabled: true, BaseURL: server.URL,
		MetadataTimeout: "3s", RequestTimeout: "17s",
	}, token)
	if err != nil {
		t.Fatal(err)
	}
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
