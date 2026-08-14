package inframedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const defaultMediaCommandTimeout = 360 * time.Minute
const defaultMediaMaxDuration = 180 * time.Minute
const defaultFFmpegPreset = "veryfast"

var ErrMediaCommandTimeout = errors.New("media command timed out")

type FFmpegProcessor struct {
	store          domainmedia.MediaObjectStore
	commandTimeout time.Duration
	maxDuration    time.Duration
	ffmpegPreset   string
	tempRoot       string
}

type FFmpegProcessorOption func(*FFmpegProcessor)

func WithFFmpegCommandTimeout(timeout time.Duration) FFmpegProcessorOption {
	return func(processor *FFmpegProcessor) {
		if timeout > 0 {
			processor.commandTimeout = timeout
		}
	}
}

func WithFFmpegMaxDuration(maxDuration time.Duration) FFmpegProcessorOption {
	return func(processor *FFmpegProcessor) {
		if maxDuration > 0 {
			processor.maxDuration = maxDuration
		}
	}
}

func WithFFmpegPreset(preset string) FFmpegProcessorOption {
	return func(processor *FFmpegProcessor) {
		if preset = strings.TrimSpace(preset); preset != "" {
			processor.ffmpegPreset = preset
		}
	}
}

func NewFFmpegProcessor(
	store domainmedia.MediaObjectStore,
	options ...FFmpegProcessorOption,
) *FFmpegProcessor {
	processor := &FFmpegProcessor{
		store:          store,
		commandTimeout: defaultMediaCommandTimeout,
		maxDuration:    defaultMediaMaxDuration,
		ffmpegPreset:   defaultFFmpegPreset,
	}
	for _, option := range options {
		if option != nil {
			option(processor)
		}
	}
	return processor
}

func (p *FFmpegProcessor) Process(ctx context.Context, asset *domainmedia.MediaAsset, job *domainmedia.MediaProcessingJob) (*applicationmedia.ProcessResult, error) {
	if p == nil || p.store == nil || asset == nil || job == nil {
		return nil, &applicationmedia.ProcessError{Code: "invalid_input", Terminal: true, Err: errors.New("invalid media processing input")}
	}
	workDir, err := os.MkdirTemp(p.tempRoot, "frux-media-*")
	if err != nil {
		return nil, &applicationmedia.ProcessError{Code: "temp_dir", Err: err}
	}
	defer os.RemoveAll(workDir)

	sourcePath := filepath.Join(workDir, "source"+sourceExtension(asset.ContentType, asset.ObjectKey))
	if err := p.downloadSource(ctx, asset, sourcePath); err != nil {
		return nil, &applicationmedia.ProcessError{Code: "source_download", Err: err}
	}
	probeStart := time.Now()
	probe, err := p.probe(ctx, sourcePath)
	inframetrics.ObserveVideoProcessing("media_probe", time.Since(probeStart), err)
	if err != nil {
		return nil, err
	}
	if _, err := selectProcessingProfile(job.ProfileVersion); err != nil {
		return nil, err
	}

	outputDir := filepath.Join(workDir, "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, &applicationmedia.ProcessError{Code: "output_dir", Err: err}
	}
	mode := baselineModeFor(probe)
	outputPath := filepath.Join(outputDir, "source.mp4")
	start := time.Now()
	outputWidth, outputHeight, audioCodec, err := p.writeBaselineMP4(
		ctx, sourcePath, outputPath, probe, mode,
	)
	inframetrics.ObserveVideoProcessing("media_"+string(mode), time.Since(start), err)
	if err != nil {
		return nil, err
	}
	objectKey, checksum, size, err := p.publishFile(
		ctx, asset.ID, job.ID, job.ProfileVersion, outputPath, "video/mp4",
	)
	if err != nil {
		return nil, err
	}
	variant := &domainmedia.MediaVariant{
		AssetID: asset.ID, ProfileVersion: job.ProfileVersion,
		SourceType: domainmedia.SourceTypeMP4, Format: "mp4", Codec: "h264",
		AudioCodec: audioCodec, Width: outputWidth, Height: outputHeight,
		Quality: fmt.Sprintf("%dp", outputHeight), ObjectKey: objectKey,
		Role: domainmedia.VariantRoleBaseline, SortOrder: 10, State: domainmedia.VariantStateReady,
		ChecksumSHA256: checksum, SizeBytes: size, Public: false,
	}

	return &applicationmedia.ProcessResult{
		Width: outputWidth, Height: outputHeight, DurationMS: probe.DurationMS,
		VideoCodec: "h264", AudioCodec: audioCodec,
		Variants: []*domainmedia.MediaVariant{variant},
	}, nil
}

type probeMetadata struct {
	Width      int
	Height     int
	DurationMS int64
	VideoCodec string
	AudioCodec string
	HasAudio   bool
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (p *FFmpegProcessor) probe(ctx context.Context, path string) (*probeMetadata, error) {
	output, err := p.runCommand(ctx, "ffprobe",
		"-v", "error", "-show_entries", "stream=codec_type,codec_name,width,height:format=duration",
		"-of", "json", path,
	)
	if err != nil {
		if errors.Is(err, ErrMediaCommandTimeout) {
			return nil, &applicationmedia.ProcessError{Code: "probe_timeout", Err: err}
		}
		return nil, &applicationmedia.ProcessError{Code: "probe_failed", Terminal: true, Err: err}
	}
	var decoded ffprobeOutput
	if err := json.Unmarshal(output, &decoded); err != nil {
		return nil, &applicationmedia.ProcessError{Code: "probe_invalid", Terminal: true, Err: err}
	}
	if len(decoded.Streams) == 0 || len(decoded.Streams) > 8 {
		return nil, &applicationmedia.ProcessError{Code: "stream_count", Terminal: true, Err: errors.New("unsupported media stream count")}
	}
	result := &probeMetadata{}
	for _, stream := range decoded.Streams {
		switch strings.ToLower(stream.CodecType) {
		case "video":
			if result.Width == 0 {
				result.Width = stream.Width
				result.Height = stream.Height
				result.VideoCodec = strings.ToLower(stream.CodecName)
			}
		case "audio":
			result.HasAudio = true
			if result.AudioCodec == "" {
				result.AudioCodec = strings.ToLower(stream.CodecName)
			}
		}
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(decoded.Format.Duration), 64)
	if err != nil || duration <= 0 || result.Width <= 0 || result.Height <= 0 {
		return nil, &applicationmedia.ProcessError{Code: "probe_metadata", Terminal: true, Err: errors.New("invalid media metadata")}
	}
	result.DurationMS = int64(math.Round(duration * 1000))
	if time.Duration(result.DurationMS)*time.Millisecond > p.maxDuration {
		return nil, &applicationmedia.ProcessError{
			Code:     "duration_limit",
			Terminal: true,
			Err:      fmt.Errorf("media duration exceeds %s limit", p.maxDuration),
		}
	}
	if result.Width > 3840 || result.Height > 3840 {
		return nil, &applicationmedia.ProcessError{Code: "dimension_limit", Terminal: true, Err: errors.New("media dimensions exceed limit")}
	}
	if !supportedVideoCodec(result.VideoCodec) || (result.AudioCodec != "" && !supportedAudioCodec(result.AudioCodec)) {
		return nil, &applicationmedia.ProcessError{Code: "codec_unsupported", Terminal: true, Err: errors.New("media codec is unsupported")}
	}
	return result, nil
}

func supportedVideoCodec(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "h264", "h265", "hevc", "vp8", "vp9", "av1":
		return true
	default:
		return false
	}
}

func supportedAudioCodec(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "aac", "mp3", "opus", "vorbis":
		return true
	default:
		return false
	}
}

type baselineMode string

const (
	baselineModeRemux          baselineMode = "remux"
	baselineModeNormalizeAudio baselineMode = "normalize_audio"
	baselineModeTranscode      baselineMode = "transcode"
)

func baselineModeFor(probe *probeMetadata) baselineMode {
	if probe != nil && probe.VideoCodec == "h264" {
		if !probe.HasAudio || probe.AudioCodec == "aac" {
			return baselineModeRemux
		}
		return baselineModeNormalizeAudio
	}
	return baselineModeTranscode
}

func (p *FFmpegProcessor) writeBaselineMP4(
	ctx context.Context,
	sourcePath string,
	outputPath string,
	probe *probeMetadata,
	mode baselineMode,
) (int, int, string, error) {
	if probe == nil || probe.Width <= 0 || probe.Height <= 0 {
		return 0, 0, "", &applicationmedia.ProcessError{
			Code: "invalid_dimensions", Terminal: true, Err: errors.New("invalid source dimensions"),
		}
	}
	width, height := probe.Width, probe.Height
	args := []string{
		"-y", "-i", sourcePath,
		"-map", "0:v:0", "-map", "0:a:0?",
	}
	switch mode {
	case baselineModeRemux:
		args = append(args, "-c", "copy")
	case baselineModeNormalizeAudio:
		args = append(args, "-c:v", "copy", "-c:a", "aac", "-b:a", "128k")
	default:
		width, height = evenDimensions(width, height)
		if width != probe.Width || height != probe.Height {
			args = append(args, "-vf", fmt.Sprintf("scale=%d:%d", width, height))
		}
		args = append(args,
			"-c:v", "libx264", "-preset", p.ffmpegPreset,
			"-crf", "23", "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "128k",
		)
	}
	args = append(args,
		"-map_metadata", "-1", "-map_chapters", "-1",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart", outputPath,
	)
	if _, err := p.runCommand(ctx, "ffmpeg", args...); err != nil {
		code := string(mode) + "_failed"
		if errors.Is(err, ErrMediaCommandTimeout) {
			code = string(mode) + "_timeout"
		}
		return 0, 0, "", &applicationmedia.ProcessError{Code: code, Err: err}
	}
	audioCodec := ""
	if probe.HasAudio {
		audioCodec = "aac"
	}
	return width, height, audioCodec, nil
}

func (p *FFmpegProcessor) publishFile(ctx context.Context, assetID, jobID int64, profileVersion, path, contentType string) (string, string, int64, error) {
	checksum, size, err := fileChecksum(path)
	if err != nil {
		return "", "", 0, &applicationmedia.ProcessError{Code: "checksum_failed", Err: err}
	}
	filename := filepath.Base(path)
	tempKey := fmt.Sprintf("tmp/media/%d/%d/%s", assetID, jobID, filename)
	if err := p.putFile(ctx, tempKey, path, size, contentType, checksum); err != nil {
		return "", "", 0, err
	}
	tempMetadata, err := p.store.Head(ctx, tempKey)
	if err != nil || tempMetadata.SizeBytes != size || !strings.EqualFold(tempMetadata.ChecksumSHA256, checksum) {
		if err == nil {
			err = domainmedia.ErrObjectChecksumMismatch
		}
		return "", "", 0, &applicationmedia.ProcessError{Code: "temp_verify_failed", Err: err}
	}
	finalKey := fmt.Sprintf("processed/%d/%s/%s/%s", assetID, profileVersion, checksum, filename)
	reader, _, err := p.store.Open(ctx, tempKey)
	if err != nil {
		return "", "", 0, &applicationmedia.ProcessError{Code: "temp_open_failed", Err: err}
	}
	_, putErr := p.store.Put(ctx, finalKey, reader, size, contentType, checksum)
	closeErr := reader.Close()
	if putErr != nil {
		return "", "", 0, &applicationmedia.ProcessError{Code: "publish_failed", Err: putErr}
	}
	if closeErr != nil {
		return "", "", 0, &applicationmedia.ProcessError{Code: "publish_close_failed", Err: closeErr}
	}
	finalMetadata, err := p.store.Head(ctx, finalKey)
	if err != nil || finalMetadata.SizeBytes != size || !strings.EqualFold(finalMetadata.ChecksumSHA256, checksum) {
		if err == nil {
			err = domainmedia.ErrObjectChecksumMismatch
		}
		return "", "", 0, &applicationmedia.ProcessError{Code: "publish_verify_failed", Err: err}
	}
	if err := p.store.Delete(ctx, tempKey); err != nil {
		return "", "", 0, &applicationmedia.ProcessError{Code: "temp_cleanup_failed", Err: err}
	}
	return finalKey, checksum, size, nil
}

func (p *FFmpegProcessor) putFile(ctx context.Context, key, path string, size int64, contentType, checksum string) error {
	file, err := os.Open(path)
	if err != nil {
		return &applicationmedia.ProcessError{Code: "output_open_failed", Err: err}
	}
	defer file.Close()
	if _, err := p.store.Put(ctx, key, file, size, contentType, checksum); err != nil {
		return &applicationmedia.ProcessError{Code: "object_put_failed", Err: err}
	}
	return nil
}

func (p *FFmpegProcessor) downloadSource(ctx context.Context, asset *domainmedia.MediaAsset, path string) error {
	reader, metadata, err := p.store.Open(ctx, asset.ObjectKey)
	if err != nil {
		return err
	}
	defer reader.Close()
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), reader)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	if written != asset.SizeBytes || metadata.SizeBytes != asset.SizeBytes || !strings.EqualFold(checksum, asset.ChecksumSHA256) {
		return domainmedia.ErrObjectChecksumMismatch
	}
	return nil
}

func (p *FFmpegProcessor) runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, p.commandTimeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := tailText(strings.TrimSpace(stderr.String()), 2048)
		if ctx.Err() != nil {
			return nil, commandError(name, ctx.Err(), message)
		}
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return nil, commandError(
				name,
				fmt.Errorf("%w after %s", ErrMediaCommandTimeout, p.commandTimeout),
				message,
			)
		}
		return nil, commandError(name, err, message)
	}
	return stdout.Bytes(), nil
}

func commandError(name string, err error, diagnostic string) error {
	if diagnostic == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w: %s", name, err, diagnostic)
}

func tailText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

type processingProfile struct {
	Version string
}

func selectProcessingProfile(version string) (*processingProfile, error) {
	switch strings.TrimSpace(version) {
	case "v1", "v2":
		return &processingProfile{Version: strings.TrimSpace(version)}, nil
	default:
		return nil, &applicationmedia.ProcessError{Code: "unsupported_profile", Terminal: true, Err: errors.New("unsupported media processing profile")}
	}
}

func evenDimensions(width, height int) (int, int) {
	if width%2 != 0 {
		width--
	}
	if height%2 != 0 {
		height--
	}
	return max(width, 2), max(height, 2)
}

func fileChecksum(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func sourceExtension(contentType, objectKey string) string {
	if extension := filepath.Ext(objectKey); extension != "" && len(extension) <= 8 {
		return extension
	}
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	default:
		return ".mp4"
	}
}
