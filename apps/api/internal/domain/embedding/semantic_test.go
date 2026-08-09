package domainembedding

import (
	"math"
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
