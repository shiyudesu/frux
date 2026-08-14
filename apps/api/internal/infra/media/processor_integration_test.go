package inframedia

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestFFmpegProcessorProducesOneSourceResolutionBaselineForV1AndV2(t *testing.T) {
	for _, profileVersion := range []string{"v1", "v2"} {
		t.Run(profileVersion, func(t *testing.T) {
			store, asset, job := ffmpegProcessorFixture(
				t,
				fixtureOptions{
					dimensions: "608x1080", duration: "1",
					profileVersion: profileVersion, videoCodec: "libx264",
					audioCodec: "aac", extension: ".mp4", contentType: "video/mp4",
				},
			)
			result, err := NewFFmpegProcessor(store).Process(context.Background(), asset, job)
			if err != nil {
				t.Fatalf("process source media: %v", err)
			}
			if result.Width != 608 || result.Height != 1080 ||
				result.VideoCodec != "h264" || result.AudioCodec != "aac" ||
				len(result.Variants) != 1 {
				t.Fatalf("unexpected result: %+v", result)
			}
			baseline := result.Variants[0]
			if baseline.Role != domainmedia.VariantRoleBaseline ||
				baseline.SourceType != domainmedia.SourceTypeMP4 ||
				baseline.ProfileVersion != profileVersion ||
				baseline.Width != 608 || baseline.Height != 1080 {
				t.Fatalf("unexpected baseline: %+v", baseline)
			}
			if _, err := store.Head(context.Background(), baseline.ObjectKey); err != nil {
				t.Fatalf("baseline object missing: %v", err)
			}
		})
	}
}

func TestFFmpegProcessorNormalizesVP9OnceAtSourceResolution(t *testing.T) {
	store, asset, job := ffmpegProcessorFixture(
		t,
		fixtureOptions{
			dimensions: "640x360", duration: "1", profileVersion: "v2",
			videoCodec: "libvpx-vp9", extension: ".webm", contentType: "video/webm",
		},
	)
	result, err := NewFFmpegProcessor(store).Process(context.Background(), asset, job)
	if err != nil {
		t.Fatalf("normalize VP9 media: %v", err)
	}
	if len(result.Variants) != 1 ||
		result.Width != 640 || result.Height != 360 ||
		result.VideoCodec != "h264" || result.AudioCodec != "" ||
		result.Variants[0].Codec != "h264" {
		t.Fatalf("unexpected normalized result: %+v", result)
	}
}

func TestFFmpegProcessorHonorsConfiguredDurationLimit(t *testing.T) {
	store, asset, job := ffmpegProcessorFixture(
		t,
		fixtureOptions{
			dimensions: "640x360", duration: "1", profileVersion: "v2",
			videoCodec: "libx264", audioCodec: "aac",
			extension: ".mp4", contentType: "video/mp4",
		},
	)
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

type fixtureOptions struct {
	dimensions     string
	duration       string
	profileVersion string
	videoCodec     string
	audioCodec     string
	extension      string
	contentType    string
}

func ffmpegProcessorFixture(
	t *testing.T,
	options fixtureOptions,
) (*LocalStore, *domainmedia.MediaAsset, *domainmedia.MediaProcessingJob) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source"+options.extension)
	args := []string{
		"-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=" + options.dimensions + ":rate=24",
	}
	if options.audioCodec != "" {
		args = append(args,
			"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100",
		)
	}
	args = append(args, "-t", options.duration, "-c:v", options.videoCodec)
	if options.videoCodec == "libx264" {
		args = append(args, "-preset", "ultrafast", "-pix_fmt", "yuv420p")
	} else if options.videoCodec == "libvpx-vp9" {
		args = append(args, "-deadline", "realtime", "-cpu-used", "8")
	}
	if options.audioCodec != "" {
		args = append(args, "-c:a", options.audioCodec)
	}
	args = append(args, sourcePath)
	command := exec.Command("ffmpeg", args...)
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
	objectKey := "uploads/1/source" + options.extension
	if _, err := store.Put(context.Background(), objectKey, source, size, options.contentType, checksum); err != nil {
		_ = source.Close()
		t.Fatalf("store source media: %v", err)
	}
	_ = source.Close()

	return store, &domainmedia.MediaAsset{
			ID: 1, OwnerID: 1, Kind: domainmedia.AssetKindVideo,
			StorageBackend: domainmedia.StorageBackendLocal,
			ObjectKey:      objectKey, ContentType: options.contentType, SizeBytes: size,
			ChecksumSHA256: checksum, State: domainmedia.AssetStateUploaded,
		}, &domainmedia.MediaProcessingJob{
			ID: 1, AssetID: 1, ProfileVersion: options.profileVersion, MaxAttempts: 3,
		}
}
