package inframedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

type ModerationInputPreparer struct {
	store     domainmedia.MediaObjectStore
	resolver  domainmedia.MediaURLResolver
	processor *FFmpegProcessor
	cleanup   applicationreview.ModerationSampleCleanup
	retention time.Duration
	tempRoot  string
}

func NewModerationInputPreparer(
	store domainmedia.MediaObjectStore,
	resolver domainmedia.MediaURLResolver,
	cleanup applicationreview.ModerationSampleCleanup,
	retention time.Duration,
) *ModerationInputPreparer {
	return &ModerationInputPreparer{
		store: store, resolver: resolver, processor: NewFFmpegProcessor(store),
		cleanup: cleanup, retention: retention,
	}
}

func (p *ModerationInputPreparer) Prepare(
	ctx context.Context,
	subject *domainreview.ModerationSubject,
	job *domainreview.ModerationJob,
) (*domainreview.ModerationInputManifest, error) {
	if p == nil || p.store == nil || p.processor == nil || subject == nil || job == nil ||
		subject.CaseID != job.CaseID || subject.VideoID != job.VideoID ||
		subject.ReviewVersion != job.ReviewVersion ||
		strings.TrimSpace(subject.SourceObjectKey) == "" {
		return nil, moderationInputError("invalid_subject", true, domainreview.ErrInvalidModerationInput)
	}
	metadata, err := p.store.Head(ctx, subject.SourceObjectKey)
	if err != nil {
		return nil, moderationInputError("source_head", false, err)
	}
	workDir, err := os.MkdirTemp(p.tempRoot, "frux-moderation-*")
	if err != nil {
		return nil, moderationInputError("temp_dir", false, err)
	}
	defer os.RemoveAll(workDir)
	sourcePath := filepath.Join(workDir, "source"+sourceExtension(metadata.ContentType, metadata.Key))
	asset := &domainmedia.MediaAsset{
		ObjectKey: metadata.Key, ContentType: metadata.ContentType,
		SizeBytes: metadata.SizeBytes, ChecksumSHA256: metadata.ChecksumSHA256,
	}
	if err := p.processor.downloadSource(ctx, asset, sourcePath, "moderation_source"); err != nil {
		return nil, moderationInputError("source_download", false, err)
	}
	probe, err := p.processor.probe(ctx, sourcePath)
	if err != nil {
		return nil, moderationInputError("probe_failed", true, err)
	}
	timestamps := moderationFrameTimestamps(probe.DurationMS)
	outputDir := filepath.Join(workDir, "frames")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, moderationInputError("frame_dir", false, err)
	}
	frames, err := p.extractFrames(ctx, sourcePath, outputDir, timestamps)
	if err != nil {
		return nil, err
	}
	manifest := &domainreview.ModerationInputManifest{
		ProfileVersion: job.InputProfileVersion,
		DurationMS:     probe.DurationMS, PreparedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	uploadedKeys := make([]string, 0, len(frames))
	for _, frame := range frames {
		objectKey := fmt.Sprintf(
			"moderation/%d/%d/%d/%s/attempt-%03d/%012d-%s.jpg",
			job.CaseID, job.ReviewVersion, job.ProviderConfigVersion, job.InputProfileVersion,
			job.Attempts, frame.timestampMS, frame.checksum[:12],
		)
		if p.cleanup != nil {
			if p.retention <= 0 {
				return nil, moderationInputError(
					"cleanup_config", true, domainreview.ErrInvalidModerationInput,
					uploadedKeys...,
				)
			}
			if err := p.cleanup.ScheduleModerationSampleCleanup(
				ctx, []string{objectKey}, time.Now().UTC().Add(p.retention),
			); err != nil {
				return nil, moderationInputError("cleanup_schedule", false, err, uploadedKeys...)
			}
		}
		uploadedKeys = append(uploadedKeys, objectKey)
		file, err := os.Open(frame.path)
		if err != nil {
			return nil, moderationInputError("frame_open", false, err, uploadedKeys...)
		}
		_, putErr := p.store.Put(
			ctx, objectKey, file, frame.size, "image/jpeg", frame.checksum,
		)
		closeErr := file.Close()
		if putErr != nil {
			return nil, moderationInputError("frame_store", false, putErr, uploadedKeys...)
		}
		if closeErr != nil {
			return nil, moderationInputError("frame_close", false, closeErr, uploadedKeys...)
		}
		manifest.Frames = append(manifest.Frames, domainreview.ModerationFrameSample{
			TimestampMS: frame.timestampMS, SHA256: frame.checksum,
			ObjectKey: objectKey, SizeBytes: frame.size,
			Width: frame.width, Height: frame.height,
		})
	}
	if err := domainreview.ValidateModerationInputManifest(manifest); err != nil {
		return nil, moderationInputError("manifest_invalid", true, err, uploadedKeys...)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) > domainreview.MaxModerationManifestBytes {
		return nil, moderationInputError("manifest_oversized", true, err, uploadedKeys...)
	}
	return manifest, nil
}

func (p *ModerationInputPreparer) ResolveAccess(
	ctx context.Context,
	manifest *domainreview.ModerationInputManifest,
	expiry time.Duration,
) ([]domainreview.ModerationFrameAccess, error) {
	if p == nil || p.resolver == nil || expiry <= 0 {
		return nil, moderationInputError("access_unavailable", true, domainreview.ErrInvalidModerationInput)
	}
	if err := domainreview.ValidateModerationInputManifest(manifest); err != nil {
		return nil, moderationInputError("manifest_invalid", true, err)
	}
	access := make([]domainreview.ModerationFrameAccess, 0, len(manifest.Frames))
	for _, frame := range manifest.Frames {
		signedURL, expiresAt, err := p.resolver.ProtectedURL(ctx, frame.ObjectKey, expiry)
		if err != nil {
			return nil, moderationInputError("access_sign", false, err)
		}
		access = append(access, domainreview.ModerationFrameAccess{
			TimestampMS: frame.TimestampMS, SHA256: frame.SHA256,
			URL: signedURL, ExpiresAt: expiresAt,
		})
	}
	return access, nil
}

type extractedModerationFrame struct {
	path        string
	timestampMS int64
	checksum    string
	size        int64
	width       int
	height      int
}

func (p *ModerationInputPreparer) extractFrames(
	ctx context.Context,
	sourcePath string,
	outputDir string,
	timestamps []int64,
) ([]extractedModerationFrame, error) {
	qualityLevels := []int{4, 7, 10}
	for _, quality := range qualityLevels {
		frames := make([]extractedModerationFrame, 0, len(timestamps))
		var total int64
		for index, timestamp := range timestamps {
			path := filepath.Join(outputDir, fmt.Sprintf("%02d.jpg", index))
			_ = os.Remove(path)
			if _, err := p.processor.runCommand(
				ctx, "ffmpeg", "-y", "-ss", formatTimestamp(timestamp), "-i", sourcePath,
				"-frames:v", "1", "-vf", "scale=512:512:force_original_aspect_ratio=decrease",
				"-q:v", strconv.Itoa(quality), path,
			); err != nil {
				return nil, moderationInputError("frame_extract", true, err)
			}
			checksum, size, err := fileChecksum(path)
			if err != nil || size <= 0 {
				return nil, moderationInputError("frame_checksum", false, err)
			}
			probe, err := p.probeImage(ctx, path)
			if err != nil {
				return nil, err
			}
			total += size
			frames = append(frames, extractedModerationFrame{
				path: path, timestampMS: timestamp, checksum: checksum,
				size: size, width: probe.Width, height: probe.Height,
			})
		}
		if total <= domainreview.MaxModerationInputBytes {
			return frames, nil
		}
	}
	return nil, moderationInputError("frame_budget", true, domainreview.ErrInvalidModerationInput)
}

func (p *ModerationInputPreparer) probeImage(
	ctx context.Context,
	path string,
) (*probeMetadata, error) {
	output, err := p.processor.runCommand(
		ctx, "ffprobe", "-v", "error",
		"-show_entries", "stream=codec_type,width,height",
		"-of", "json", path,
	)
	if err != nil {
		return nil, moderationInputError("frame_probe", true, err)
	}
	var decoded ffprobeOutput
	if err := json.Unmarshal(output, &decoded); err != nil {
		return nil, moderationInputError("frame_probe_invalid", true, err)
	}
	for _, stream := range decoded.Streams {
		if stream.CodecType == "video" && stream.Width > 0 && stream.Height > 0 &&
			stream.Width <= domainreview.MaxModerationFrameEdge &&
			stream.Height <= domainreview.MaxModerationFrameEdge {
			return &probeMetadata{Width: stream.Width, Height: stream.Height}, nil
		}
	}
	return nil, moderationInputError("frame_dimensions", true, domainreview.ErrInvalidModerationInput)
}

func moderationFrameTimestamps(durationMS int64) []int64 {
	count := int(math.Ceil(float64(durationMS) / 5000))
	if count < 1 {
		count = 1
	}
	if count > domainreview.MaxModerationFrames {
		count = domainreview.MaxModerationFrames
	}
	result := make([]int64, 0, count)
	for index := 0; index < count; index++ {
		timestamp := (int64(2*index+1) * durationMS) / int64(2*count)
		if timestamp >= durationMS {
			timestamp = durationMS - 1
		}
		result = append(result, timestamp)
	}
	return result
}

func formatTimestamp(milliseconds int64) string {
	return fmt.Sprintf("%.3f", float64(milliseconds)/1000)
}

func moderationInputError(
	code string,
	terminal bool,
	err error,
	objectKeys ...string,
) error {
	if err == nil {
		err = errors.New(code)
	}
	return &applicationreview.ModerationInputError{
		Code: code, Terminal: terminal,
		ObjectKeys: append([]string(nil), objectKeys...),
		Err:        err,
	}
}

type LocalModerationURLResolver struct {
	baseURL string
	signer  *LocalProtectedURLSigner
}

func NewLocalModerationURLResolver(
	baseURL string,
	signer *LocalProtectedURLSigner,
) (*LocalModerationURLResolver, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || signer == nil {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	return &LocalModerationURLResolver{
		baseURL: strings.TrimRight(parsed.String(), "/"), signer: signer,
	}, nil
}

func (r *LocalModerationURLResolver) PublicURL(string) (string, error) {
	return "", domainmedia.ErrPresignUnsupported
}

func (r *LocalModerationURLResolver) ProtectedURL(
	_ context.Context,
	objectKey string,
	expiry time.Duration,
) (string, time.Time, error) {
	path, expiresAt, err := r.signer.Sign(objectKey, expiry)
	if err != nil {
		return "", time.Time{}, err
	}
	return r.baseURL + path, expiresAt, nil
}
