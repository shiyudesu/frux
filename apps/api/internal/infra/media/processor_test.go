package inframedia

import "testing"

func TestRenditionHeightsNeverUpscales(t *testing.T) {
	tests := []struct {
		source int
		want   []int
	}{
		{source: 360, want: []int{}},
		{source: 480, want: []int{480}},
		{source: 900, want: []int{480, 720}},
		{source: 2160, want: []int{480, 720, 1080}},
	}
	for _, test := range tests {
		got := renditionHeights(test.source, []int{480, 720, 1080})
		if len(got) != len(test.want) {
			t.Fatalf("source %d: got %v want %v", test.source, got, test.want)
		}
		for index := range got {
			if got[index] != test.want[index] || got[index] > test.source {
				t.Fatalf("source %d: got %v want %v", test.source, got, test.want)
			}
		}
	}
}

func TestScaledEvenWidth(t *testing.T) {
	if got := scaledEvenWidth(1920, 1080, 720); got != 1280 {
		t.Fatalf("unexpected scaled width %d", got)
	}
	if got := scaledEvenWidth(853, 480, 360); got%2 != 0 {
		t.Fatalf("expected even width, got %d", got)
	}
}
