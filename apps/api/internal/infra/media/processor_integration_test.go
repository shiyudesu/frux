package inframedia

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestFFmpegProcessorGeneratesBaselineAndDASH(t *testing.T) {
	store, asset, job := ffmpegProcessorFixture(t, "640x360", "1")
	processor := NewFFmpegProcessor(store)
	result, err := processor.Process(context.Background(), asset, job)
	if err != nil {
		t.Fatalf("process source media: %v", err)
	}
	var baseline, manifest *domainmedia.MediaVariant
	for _, variant := range result.Variants {
		switch variant.Role {
		case domainmedia.VariantRoleBaseline:
			baseline = variant
		case domainmedia.VariantRoleManifest:
			manifest = variant
		}
		if variant.Height > 360 {
			t.Fatalf("processor upscaled source: %+v", variant)
		}
	}
	if baseline == nil || manifest == nil {
		t.Fatalf("missing baseline or DASH manifest: %+v", result.Variants)
	}
	if _, err := store.Head(context.Background(), baseline.ObjectKey); err != nil {
		t.Fatalf("baseline object missing: %v", err)
	}
	if _, err := store.Head(context.Background(), manifest.ObjectKey); err != nil {
		t.Fatalf("manifest object missing: %v", err)
	}
}

func TestFFmpegProcessorGeneratesMultiRenditionDASHWithAudio(t *testing.T) {
	store, asset, job := ffmpegProcessorFixture(t, "608x1080", "1")
	result, err := NewFFmpegProcessor(store).Process(context.Background(), asset, job)
	if err != nil {
		t.Fatalf("process portrait source: %v", err)
	}
	mp4Count := 0
	segmentCount := 0
	var manifest *domainmedia.MediaVariant
	for _, variant := range result.Variants {
		switch variant.SourceType {
		case domainmedia.SourceTypeMP4:
			mp4Count++
		case domainmedia.SourceTypeDASH:
			if variant.Role == domainmedia.VariantRoleManifest {
				manifest = variant
			} else if variant.Role == domainmedia.VariantRoleSegment {
				segmentCount++
			}
		}
	}
	if mp4Count != 3 || manifest == nil || segmentCount == 0 {
		t.Fatalf("unexpected multi-rendition outputs: mp4=%d segments=%d manifest=%+v",
			mp4Count, segmentCount, manifest)
	}
	reader, _, err := store.Open(context.Background(), manifest.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read manifest: read=%v close=%v", readErr, closeErr)
	}
	if !strings.Contains(string(body), "<AdaptationSet") {
		t.Fatalf("invalid DASH manifest: %s", body)
	}
}

func TestFFmpegProcessorHonorsConfiguredDurationLimit(t *testing.T) {
	store, asset, job := ffmpegProcessorFixture(t, "640x360", "1")
	_, err := NewFFmpegProcessor(
		store,
		WithFFmpegMaxDuration(500*time.Millisecond),
	).Process(context.Background(), asset, job)
	var processErr *applicationmedia.ProcessError
	if !errors.As(err, &processErr) ||
		processErr.Code != "duration_limit" ||
		!processErr.Terminal {
		t.Fatalf("duration error = %#v", err)
	}
}

func ffmpegProcessorFixture(
	t *testing.T,
	dimensions string,
	duration string,
) (*LocalStore, *domainmedia.MediaAsset, *domainmedia.MediaProcessingJob) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.mp4")
	command := exec.Command(
		"ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size="+dimensions+":rate=24",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100",
		"-t", duration, "-c:v", "libx264", "-preset", "ultrafast",
		"-pix_fmt", "yuv420p", "-c:a", "aac",
		sourcePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate source media: %v: %s", err, output)
	}
	checksum, size, err := fileChecksum(sourcePath)
	if err != nil {
		t.Fatalf("checksum source media: %v", err)
	}
	store, err := NewLocalStore(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("open source media: %v", err)
	}
	if _, err := store.Put(context.Background(), "uploads/1/source.mp4", source, size, "video/mp4", checksum); err != nil {
		_ = source.Close()
		t.Fatalf("store source media: %v", err)
	}
	_ = source.Close()

	return store, &domainmedia.MediaAsset{
			ID: 1, OwnerID: 1, Kind: domainmedia.AssetKindVideo,
			StorageBackend: domainmedia.StorageBackendLocal,
			ObjectKey:      "uploads/1/source.mp4", ContentType: "video/mp4", SizeBytes: size,
			ChecksumSHA256: checksum, State: domainmedia.AssetStateUploaded,
		}, &domainmedia.MediaProcessingJob{
			ID: 1, AssetID: 1, ProfileVersion: "v1", MaxAttempts: 3,
		}
}
