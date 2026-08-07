package inframedia

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

func TestModerationInputPreparerIsDeterministicAndBounded(t *testing.T) {
	requireMediaTools(t)
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.mp4")
	command := exec.Command(
		"ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=960x540:rate=12",
		"-t", "6", "-c:v", "libx264", "-pix_fmt", "yuv420p", sourcePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate source media: %v: %s", err, output)
	}
	store := putModerationSource(t, root, sourcePath, "uploads/1/source.mp4")
	signer, err := NewLocalProtectedURLSigner(
		"/moderation-media", strings.Repeat("s", 32), time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewLocalModerationURLResolver("http://127.0.0.1:8080", signer)
	if err != nil {
		t.Fatal(err)
	}
	preparer := NewModerationInputPreparer(store, resolver, nil, time.Hour)
	job, subject := moderationInputFixture(t, "uploads/1/source.mp4")

	first, err := preparer.Prepare(context.Background(), subject, job)
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparer.Prepare(context.Background(), subject, job)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Frames) != 2 || len(second.Frames) != len(first.Frames) {
		t.Fatalf("frame counts first=%d second=%d", len(first.Frames), len(second.Frames))
	}
	var total int64
	for index := range first.Frames {
		left, right := first.Frames[index], second.Frames[index]
		if left.TimestampMS != right.TimestampMS || left.SHA256 != right.SHA256 ||
			left.ObjectKey != right.ObjectKey ||
			left.Width > domainreview.MaxModerationFrameEdge ||
			left.Height > domainreview.MaxModerationFrameEdge {
			t.Fatalf("frames differ left=%#v right=%#v", left, right)
		}
		total += left.SizeBytes
	}
	if total > domainreview.MaxModerationInputBytes {
		t.Fatalf("total sample bytes = %d", total)
	}
	access, err := preparer.ResolveAccess(context.Background(), first, 30*time.Second)
	if err != nil || len(access) != len(first.Frames) {
		t.Fatalf("access = %#v err=%v", access, err)
	}
	parsed, err := url.Parse(access[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if !signer.Verify(
		first.Frames[0].ObjectKey,
		parsed.Query().Get("expires"),
		parsed.Query().Get("signature"),
	) {
		t.Fatal("signed sample URL did not verify")
	}
}

func TestModerationInputPreparerRejectsCorruptMedia(t *testing.T) {
	requireMediaTools(t)
	root := t.TempDir()
	sourcePath := filepath.Join(root, "corrupt.mp4")
	if err := os.WriteFile(sourcePath, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := putModerationSource(t, root, sourcePath, "uploads/1/corrupt.mp4")
	preparer := NewModerationInputPreparer(store, nil, nil, time.Hour)
	job, subject := moderationInputFixture(t, "uploads/1/corrupt.mp4")
	_, err := preparer.Prepare(context.Background(), subject, job)
	var inputErr *applicationreview.ModerationInputError
	if !errors.As(err, &inputErr) || !inputErr.Terminal {
		t.Fatalf("corrupt media error = %#v", err)
	}
}

func TestModerationFrameTimestampsAreBoundedForShortAndLongVideos(t *testing.T) {
	short := moderationFrameTimestamps(500)
	if len(short) != 1 || short[0] != 250 {
		t.Fatalf("short timestamps = %v", short)
	}
	longFirst := moderationFrameTimestamps(int64(10 * time.Minute / time.Millisecond))
	longSecond := moderationFrameTimestamps(int64(10 * time.Minute / time.Millisecond))
	if len(longFirst) != domainreview.MaxModerationFrames ||
		len(longSecond) != len(longFirst) {
		t.Fatalf("long timestamp counts = %d/%d", len(longFirst), len(longSecond))
	}
	for index := range longFirst {
		if longFirst[index] != longSecond[index] ||
			(index > 0 && longFirst[index] <= longFirst[index-1]) {
			t.Fatalf("long timestamps = %v / %v", longFirst, longSecond)
		}
	}
}

func moderationInputFixture(
	t *testing.T,
	objectKey string,
) (*domainreview.ModerationJob, *domainreview.ModerationSubject) {
	t.Helper()
	job, err := domainreview.NewModerationJob(
		1, 2, 1,
		domainreview.ModerationJobConfig{
			Mode: domainreview.ModerationModeObserve, ProviderConfigVersion: 1,
			InputProfileVersion: "frames-v1", MaxAttempts: 3,
		},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	job.ID = 1
	return job, &domainreview.ModerationSubject{
		CaseID: 1, VideoID: 2, ReviewVersion: 1, PolicyVersion: 1,
		Title: "title", Description: "description", SourceObjectKey: objectKey,
	}
}

func putModerationSource(
	t *testing.T,
	root string,
	sourcePath string,
	objectKey string,
) *LocalStore {
	t.Helper()
	checksum, size, err := fileChecksum(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewLocalStore(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := store.Put(
		context.Background(), objectKey, source, size, "video/mp4", checksum,
	); err != nil {
		t.Fatal(err)
	}
	return store
}

func requireMediaTools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
}
