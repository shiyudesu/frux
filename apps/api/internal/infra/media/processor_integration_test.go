package inframedia

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	domainmedia "GCFeed/internal/domain/media"
)

func TestFFmpegProcessorGeneratesBaselineAndDASH(t *testing.T) {
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
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100",
		"-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
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

	processor := NewFFmpegProcessor(store)
	result, err := processor.Process(
		context.Background(),
		&domainmedia.MediaAsset{
			ID: 1, OwnerID: 1, Kind: domainmedia.AssetKindVideo, StorageBackend: domainmedia.StorageBackendLocal,
			ObjectKey: "uploads/1/source.mp4", ContentType: "video/mp4", SizeBytes: size,
			ChecksumSHA256: checksum, State: domainmedia.AssetStateUploaded,
		},
		&domainmedia.MediaProcessingJob{ID: 1, AssetID: 1, ProfileVersion: "v1", MaxAttempts: 3},
	)
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
