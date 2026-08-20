package inframedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

func TestFFmpegMultimodalMediaPreparerIsDeterministicBoundedAndCleansUp(t *testing.T) {
	requireMediaTools(t)
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.mp4")
	coverPath := filepath.Join(root, "cover.jpg")
	command := exec.Command(
		"ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=12",
		"-t", "4", "-c:v", "libx264", "-pix_fmt", "yuv420p", sourcePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate source media: %v: %s", err, output)
	}
	command = exec.Command(
		"ffmpeg", "-v", "error", "-y", "-ss", "1", "-i", sourcePath,
		"-frames:v", "1", "-vf", "scale=320:320:force_original_aspect_ratio=decrease", coverPath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate cover: %v: %s", err, output)
	}
	store := putMultimodalObject(t, root, sourcePath, "uploads/1/source.mp4", "video/mp4")
	putMultimodalObjectInto(t, store, coverPath, "uploads/1/cover.jpg", "image/jpeg")
	workRoot := filepath.Join(root, "work")
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	preparer := NewFFmpegMultimodalMediaPreparer(store, workRoot)
	var _ applicationembedding.MultimodalMediaPreparer = preparer
	request := applicationembedding.MultimodalMediaPreparationRequest{
		VideoObjectKey: "uploads/1/source.mp4", CoverObjectKey: "uploads/1/cover.jpg",
		FrameSamplingPolicy:      domainembedding.MultimodalFrameSamplingPolicyV1,
		ImagePreprocessingPolicy: domainembedding.MultimodalImagePreprocessingV1,
		MaxImages:                4, MaxBytesEach: 2 * 1024 * 1024, MaxTotalBytes: 6 * 1024 * 1024,
		MaxPixelsEach: 512 * 512, AllowedMIMETypes: []string{"image/jpeg"},
	}
	first, err := preparer.PrepareMultimodalMedia(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparer.PrepareMultimodalMedia(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Images) == 0 || len(first.Images) > request.MaxImages || len(second.Images) != len(first.Images) {
		t.Fatalf("prepared image counts = %d/%d", len(first.Images), len(second.Images))
	}
	var total int
	for index := range first.Images {
		left, right := first.Images[index], second.Images[index]
		if left.MIMEType != "image/jpeg" || left.Width <= 0 || left.Height <= 0 ||
			int64(left.Width)*int64(left.Height) > request.MaxPixelsEach ||
			len(left.Content) > request.MaxBytesEach || left.Digest != right.Digest ||
			!bytes.Equal(left.Content, right.Content) {
			t.Fatalf("prepared images differ or exceed bounds: left=%#v right=%#v", left, right)
		}
		total += len(left.Content)
	}
	if total > request.MaxTotalBytes {
		t.Fatalf("prepared bytes = %d", total)
	}
	entries, err := os.ReadDir(workRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary media was not cleaned up: entries=%v err=%v", entries, err)
	}
}

func TestFFmpegMultimodalMediaPreparerRejectsInvalidPolicyAndBounds(t *testing.T) {
	preparer := NewFFmpegMultimodalMediaPreparer(nil, "")
	_, err := preparer.PrepareMultimodalMedia(context.Background(), applicationembedding.MultimodalMediaPreparationRequest{})
	if !errors.Is(err, applicationembedding.ErrInvalidMultimodalMediaPreparation) {
		t.Fatalf("invalid preparation error = %v", err)
	}
	if got := multimodalFrameTimestamps(1000, 4); !reflect.DeepEqual(got, []int64{125, 375, 625, 875}) {
		t.Fatalf("frame timestamps = %v", got)
	}
}

func putMultimodalObject(t testing.TB, root, path, key, contentType string) *LocalStore {
	t.Helper()
	store, err := NewLocalStore(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	putMultimodalObjectInto(t, store, path, key, contentType)
	return store
}

func putMultimodalObjectInto(t testing.TB, store *LocalStore, path, key, contentType string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if _, err := store.Put(
		context.Background(), key, bytes.NewReader(content), int64(len(content)),
		contentType, hex.EncodeToString(sum[:]),
	); err != nil {
		t.Fatal(err)
	}
}
