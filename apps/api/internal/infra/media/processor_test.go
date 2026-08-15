package inframedia

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
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

func TestPublishFileUsesOneFinalPutAndIdempotentReuse(t *testing.T) {
	store := newPublishTestStore()
	processor := NewFFmpegProcessor(store)
	path := writePublishTestFile(t, "processed output")

	key, checksum, size, err := processor.publishFile(
		context.Background(), 7, "v2", path, "video/mp4",
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.puts != 1 || store.opens != 0 {
		t.Fatalf("puts=%d opens=%d", store.puts, store.opens)
	}
	if !strings.HasPrefix(key, "processed/7/v2/"+checksum+"/") || size != 16 {
		t.Fatalf("key=%q checksum=%q size=%d", key, checksum, size)
	}

	reusedKey, reusedChecksum, reusedSize, err := processor.publishFile(
		context.Background(), 7, "v2", path, "video/mp4",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reusedKey != key || reusedChecksum != checksum || reusedSize != size ||
		store.puts != 1 || store.opens != 0 {
		t.Fatalf(
			"reuse key=%q checksum=%q size=%d puts=%d opens=%d",
			reusedKey, reusedChecksum, reusedSize, store.puts, store.opens,
		)
	}
}

func TestPublishFileRejectsConflictWithoutOverwrite(t *testing.T) {
	store := newPublishTestStore()
	processor := NewFFmpegProcessor(store)
	path := writePublishTestFile(t, "processed output")
	checksum, _, err := fileChecksum(path)
	if err != nil {
		t.Fatal(err)
	}
	key := "processed/7/v2/" + checksum + "/" + filepath.Base(path)
	store.objects[key] = domainmedia.ObjectMetadata{
		Key: key, SizeBytes: 99, ChecksumSHA256: strings.Repeat("f", 64),
	}

	_, _, _, err = processor.publishFile(
		context.Background(), 7, "v2", path, "video/mp4",
	)
	var processErr *applicationmedia.ProcessError
	if !errors.As(err, &processErr) || processErr.Code != "output_conflict" || store.puts != 0 {
		t.Fatalf("error=%v puts=%d", err, store.puts)
	}
}

func TestPublishFileHonorsCancellationDuringFinalPut(t *testing.T) {
	store := newPublishTestStore()
	processor := NewFFmpegProcessor(store)
	path := writePublishTestFile(t, strings.Repeat("x", 1024))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, _, err := processor.publishFile(ctx, 7, "v2", path, "video/mp4")
	var processErr *applicationmedia.ProcessError
	if !errors.As(err, &processErr) ||
		processErr.Code != "object_put_failed" ||
		!errors.Is(processErr.Err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

type publishTestStore struct {
	objects map[string]domainmedia.ObjectMetadata
	puts    int
	opens   int
}

func newPublishTestStore() *publishTestStore {
	return &publishTestStore{objects: map[string]domainmedia.ObjectMetadata{}}
}

func (s *publishTestStore) Put(
	_ context.Context,
	key string,
	body io.Reader,
	sizeBytes int64,
	contentType, checksumSHA256 string,
) (*domainmedia.ObjectMetadata, error) {
	s.puts++
	content, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != sizeBytes {
		return nil, domainmedia.ErrInvalidSize
	}
	metadata := domainmedia.ObjectMetadata{
		Key: key, ContentType: contentType, SizeBytes: sizeBytes,
		ChecksumSHA256: checksumSHA256,
	}
	s.objects[key] = metadata
	return &metadata, nil
}

func (s *publishTestStore) Open(
	_ context.Context,
	key string,
) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	s.opens++
	metadata, ok := s.objects[key]
	if !ok {
		return nil, nil, domainmedia.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(make([]byte, metadata.SizeBytes))), &metadata, nil
}

func (s *publishTestStore) Head(
	_ context.Context,
	key string,
) (*domainmedia.ObjectMetadata, error) {
	metadata, ok := s.objects[key]
	if !ok {
		return nil, domainmedia.ErrObjectNotFound
	}
	return &metadata, nil
}

func (s *publishTestStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func (s *publishTestStore) List(context.Context, string) ([]domainmedia.ObjectMetadata, error) {
	result := make([]domainmedia.ObjectMetadata, 0, len(s.objects))
	for _, metadata := range s.objects {
		result = append(result, metadata)
	}
	return result, nil
}

func (*publishTestStore) PresignPut(
	context.Context,
	string,
	string,
	string,
	int64,
	time.Duration,
) (*domainmedia.PresignedRequest, error) {
	return nil, nil
}

func (*publishTestStore) PresignGet(
	context.Context,
	string,
	time.Duration,
) (*domainmedia.PresignedRequest, error) {
	return nil, nil
}

func writePublishTestFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
