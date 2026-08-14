package inframedia

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEvenDimensionsOnlyFloorsOddValues(t *testing.T) {
	tests := []struct {
		width      int
		height     int
		wantWidth  int
		wantHeight int
	}{
		{width: 1920, height: 1080, wantWidth: 1920, wantHeight: 1080},
		{width: 853, height: 481, wantWidth: 852, wantHeight: 480},
		{width: 1, height: 1, wantWidth: 2, wantHeight: 2},
	}
	for _, test := range tests {
		width, height := evenDimensions(test.width, test.height)
		if width != test.wantWidth || height != test.wantHeight {
			t.Fatalf("%dx%d became %dx%d, want %dx%d",
				test.width, test.height, width, height, test.wantWidth, test.wantHeight)
		}
	}
}

func TestSelectProcessingProfileSupportsLegacyAndCurrent(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		profile, err := selectProcessingProfile(version)
		if err != nil || profile.Version != version {
			t.Fatalf("profile %q = %+v, err=%v", version, profile, err)
		}
	}
	if _, err := selectProcessingProfile("v3"); err == nil {
		t.Fatal("unsupported profile was accepted")
	}
}

func TestBaselineModePrefersStreamCopy(t *testing.T) {
	tests := []struct {
		probe *probeMetadata
		want  baselineMode
	}{
		{
			probe: &probeMetadata{VideoCodec: "h264", AudioCodec: "aac", HasAudio: true},
			want:  baselineModeRemux,
		},
		{
			probe: &probeMetadata{VideoCodec: "h264"},
			want:  baselineModeRemux,
		},
		{
			probe: &probeMetadata{VideoCodec: "h264", AudioCodec: "opus", HasAudio: true},
			want:  baselineModeNormalizeAudio,
		},
		{
			probe: &probeMetadata{VideoCodec: "vp9", AudioCodec: "opus", HasAudio: true},
			want:  baselineModeTranscode,
		},
	}
	for _, test := range tests {
		if got := baselineModeFor(test.probe); got != test.want {
			t.Fatalf("probe=%+v mode=%q want=%q", test.probe, got, test.want)
		}
	}
}

func TestFFmpegProcessorCommandTimeoutAndCancellationAreDistinct(t *testing.T) {
	processor := NewFFmpegProcessor(
		nil,
		WithFFmpegCommandTimeout(20*time.Millisecond),
	)
	if _, err := processor.runCommand(
		context.Background(), "sh", "-c", "sleep 1",
	); !errors.Is(err, ErrMediaCommandTimeout) {
		t.Fatalf("timeout error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := processor.runCommand(
		ctx, "sh", "-c", "sleep 1",
	); !errors.Is(err, context.Canceled) || errors.Is(err, ErrMediaCommandTimeout) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestTailTextKeepsActionableSuffix(t *testing.T) {
	if got := tailText("prefix-actionable-tail", 15); got != "actionable-tail" {
		t.Fatalf("tail = %q", got)
	}
}
