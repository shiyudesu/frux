package infrarecommendation

import (
	domainexposure "GCFeed/internal/domain/exposure"
	"testing"
)

func TestProgressEventWeightUsesBoundedPlaybackProgress(t *testing.T) {
	duration := 60_000
	early := eventWeight(domainexposure.EventTypeProgress, 5_000, 4_000, &duration, false)
	late := eventWeight(domainexposure.EventTypeProgress, 45_000, 40_000, &duration, false)
	if early != 0.25 {
		t.Fatalf("early progress weight = %v, want floor 0.25", early)
	}
	if late <= early || late > 1.5 {
		t.Fatalf("late progress weight is not bounded and increasing: early=%v late=%v", early, late)
	}
	if complete := eventWeight(domainexposure.EventTypeComplete, 60_000, 55_000, &duration, true); complete <= late {
		t.Fatalf("complete weight %v should exceed progress weight %v", complete, late)
	}
}
