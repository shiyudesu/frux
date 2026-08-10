package domainembedding

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanonicalVideoTextAndSemanticVectorContract(t *testing.T) {
	title, description, text, err := CanonicalVideoText(
		"  Ｆｒｕｘ\t城市  ",
		" 雨后\n街道 ",
	)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Frux 城市" || description != "雨后 街道" ||
		text != "Frux 城市\n雨后 街道" {
		t.Fatalf("canonical text = %q %q %q", title, description, text)
	}
	vector := make([]float64, SemanticDimension)
	value := 1 / math.Sqrt(float64(SemanticDimension))
	for index := range vector {
		vector[index] = value
	}
	normalized, err := NormalizeSemanticVector(vector)
	if err != nil || len(normalized) != SemanticDimension {
		t.Fatalf("normalized = %d err=%v", len(normalized), err)
	}
	vector[0] = math.Inf(1)
	if _, err := NormalizeSemanticVector(vector); err == nil {
		t.Fatal("non-finite semantic vector was accepted")
	}
}

func TestSemanticVectorConstructionAndSerializationAreBounded(t *testing.T) {
	value := 1 / math.Sqrt(float64(SemanticDimension))
	input := make([]float64, SemanticDimension)
	for index := range input {
		input[index] = value * (1 + 5e-6)
	}
	normalized, err := NormalizeSemanticVector(input)
	if err != nil {
		t.Fatal(err)
	}
	norm := 0.0
	for _, component := range normalized {
		norm += component * component
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-12 {
		t.Fatalf("normalized norm = %.16f", math.Sqrt(norm))
	}
	content, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > 16*1024 {
		t.Fatalf("semantic JSON is unbounded: %d bytes", len(content))
	}
	var roundTrip []float64
	if err := json.Unmarshal(content, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(roundTrip) != SemanticDimension {
		t.Fatalf("round-trip dimension = %d", len(roundTrip))
	}
	embedding := NewVideoEmbedding(1, SemanticModelKey, normalized, "hash", string(content))
	normalized[0] = 0
	if embedding.Embedding[0] == 0 {
		t.Fatal("embedding retained caller-owned vector storage")
	}
	for name, vector := range map[string][]float64{
		"short":    make([]float64, SemanticDimension-1),
		"long":     make([]float64, SemanticDimension+1),
		"zero":     make([]float64, SemanticDimension),
		"non-unit": append([]float64{2}, make([]float64, SemanticDimension-1)...),
		"nan":      append([]float64{math.NaN()}, make([]float64, SemanticDimension-1)...),
		"infinite": append([]float64{math.Inf(1)}, make([]float64, SemanticDimension-1)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeSemanticVector(vector); err == nil {
				t.Fatal("invalid semantic vector was accepted")
			}
		})
	}
}

func TestSharedGoPythonCanonicalizationFixtures(t *testing.T) {
	path := filepath.Join(
		"..", "..", "..", "..", "semantic-embedding", "fixtures",
		"canonicalization-fixtures.json",
	)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Title                string `json:"title"`
		Description          string `json:"description"`
		CanonicalTitle       string `json:"canonical_title"`
		CanonicalDescription string `json:"canonical_description"`
		Text                 string `json:"text"`
		SHA256               string `json:"sha256"`
	}
	if err := json.Unmarshal(content, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		title, description, text, err := CanonicalVideoText(
			fixture.Title, fixture.Description,
		)
		if err != nil {
			t.Fatal(err)
		}
		if title != fixture.CanonicalTitle ||
			description != fixture.CanonicalDescription ||
			text != fixture.Text ||
			TextHash(text) != fixture.SHA256 {
			t.Fatalf("fixture mismatch for %q", fixture.Title)
		}
	}
}

func TestSemanticRetryDelayCapsAtThirtyMinutes(t *testing.T) {
	want := []time.Duration{
		5 * time.Second, 30 * time.Second, 2 * time.Minute,
		10 * time.Minute, 30 * time.Minute, 30 * time.Minute,
	}
	for index, expected := range want {
		if got := SemanticRetryDelay(index + 1); got != expected {
			t.Fatalf("attempt %d delay = %v, want %v", index+1, got, expected)
		}
	}
}
