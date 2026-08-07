package domainreview

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestModerationJobHasDeterministicIdentityAndValidatedBounds(t *testing.T) {
	config := ModerationJobConfig{
		Mode: ModerationModeObserve, ProviderConfigVersion: 3,
		InputProfileVersion: "frames-v1", MaxAttempts: 5,
	}
	first, err := NewModerationJob(10, 20, 2, config, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewModerationJob(10, 20, 2, config, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first.ResultID != second.ResultID || first.RequestID != second.RequestID ||
		first.ResultID != "moderation-result:10:2:3" ||
		first.Status != ModerationJobPending {
		t.Fatalf("jobs first=%#v second=%#v", first, second)
	}
	config.MaxAttempts = 11
	if _, err := NewModerationJob(10, 20, 2, config, time.Now()); !errors.Is(err, ErrInvalidModerationJob) {
		t.Fatalf("invalid attempts error = %v", err)
	}
}

func TestModerationInputManifestBounds(t *testing.T) {
	manifest := &ModerationInputManifest{
		ProfileVersion: "frames-v1", DurationMS: 1000, PreparedAt: time.Now(),
		Frames: []ModerationFrameSample{{
			TimestampMS: 500, SHA256: strings.Repeat("a", 64),
			ObjectKey: "moderation/1/frame.jpg", SizeBytes: 100,
			Width: 512, Height: 288,
		}},
	}
	if err := ValidateModerationInputManifest(manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Frames[0].Width = 513
	if err := ValidateModerationInputManifest(manifest); !errors.Is(err, ErrInvalidModerationInput) {
		t.Fatalf("oversized frame error = %v", err)
	}
}
