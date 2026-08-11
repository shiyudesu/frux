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
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const defaultMediaCommandTimeout = 15 * time.Minute

type generatedMP4 struct {
	path      string
	height    int
	width     int
	bitrate   int
	role      string
	sortOrder int
}

type FFmpegProcessor struct {
	store          domainmedia.MediaObjectStore
	commandTimeout time.Duration
	tempRoot       string
}

func NewFFmpegProcessor(store domainmedia.MediaObjectStore) *FFmpegProcessor {
	return &FFmpegProcessor{store: store, commandTimeout: defaultMediaCommandTimeout}
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
	profile, err := selectProcessingProfile(job.ProfileVersion)
	if err != nil {
		return nil, err
	}

	outputDir := filepath.Join(workDir, "outputs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, &applicationmedia.ProcessError{Code: "output_dir", Err: err}
	}
	heights := renditionHeights(probe.Height, profile.Heights)
	baselineHeight := min(probe.Height, profile.BaselineMaxHeight)
	if baselineHeight <= 0 {
		return nil, &applicationmedia.ProcessError{Code: "invalid_dimensions", Terminal: true, Err: errors.New("invalid source dimensions")}
	}
	heights = appendUniqueHeight([]int{baselineHeight}, heights...)
	generated := make([]generatedMP4, 0, len(heights))
	for index, height := range heights {
		width := scaledEvenWidth(probe.Width, probe.Height, height)
		role := domainmedia.VariantRoleRendition
		sortOrder := 20 + index
		if height == baselineHeight {
			role = domainmedia.VariantRoleBaseline
			sortOrder = 10
		}
		bitrate := profile.bitrateForHeight(height)
		path := filepath.Join(outputDir, fmt.Sprintf("%dp.mp4", height))
		start := time.Now()
		err := p.transcodeMP4(ctx, sourcePath, path, width, height, bitrate)
		inframetrics.ObserveVideoProcessing("media_mp4_"+strconv.Itoa(height), time.Since(start), err)
		if err != nil {
			return nil, err
		}
		generated = append(generated, generatedMP4{
			path: path, height: height, width: width, bitrate: bitrate, role: role, sortOrder: sortOrder,
		})
	}
	sort.SliceStable(generated, func(i, j int) bool {
		return generated[i].sortOrder < generated[j].sortOrder
	})

	variants := make([]*domainmedia.MediaVariant, 0, len(generated)+8)
	for _, output := range generated {
		objectKey, checksum, size, err := p.publishFile(
			ctx, asset.ID, job.ID, job.ProfileVersion, output.path, "video/mp4",
		)
		if err != nil {
			return nil, err
		}
		variants = append(variants, &domainmedia.MediaVariant{
			AssetID: asset.ID, ProfileVersion: job.ProfileVersion,
			SourceType: domainmedia.SourceTypeMP4, Format: "mp4", Codec: "h264", AudioCodec: "aac",
			Width: output.width, Height: output.height, Bitrate: output.bitrate,
			Quality: fmt.Sprintf("%dp", output.height), ObjectKey: objectKey, Role: output.role,
			SortOrder: output.sortOrder, State: domainmedia.VariantStateReady,
			ChecksumSHA256: checksum, SizeBytes: size, Public: false,
		})
	}

	dashDir := filepath.Join(outputDir, "dash")
	if err := os.MkdirAll(dashDir, 0o755); err != nil {
		return nil, &applicationmedia.ProcessError{Code: "dash_dir", Err: err}
	}
	manifestPath := filepath.Join(dashDir, "manifest.mpd")
	start := time.Now()
	err = p.generateDASH(ctx, generatedPaths(generated), manifestPath, probe.HasAudio, profile.DASHSegmentSeconds)
	inframetrics.ObserveVideoProcessing("media_dash", time.Since(start), err)
	if err != nil {
		return nil, err
	}
	dashFiles, err := os.ReadDir(dashDir)
	if err != nil {
		return nil, &applicationmedia.ProcessError{Code: "dash_read", Err: err}
	}
	publishedDash, err := p.publishBundle(ctx, asset.ID, job.ID, job.ProfileVersion, dashDir, dashFiles)
	if err != nil {
		return nil, err
	}
	for _, file := range dashFiles {
		if file.IsDir() {
			continue
		}
		path := filepath.Join(dashDir, file.Name())
		published := publishedDash[file.Name()]
		role := domainmedia.VariantRoleSegment
		sortOrder := 0
		if file.Name() == filepath.Base(manifestPath) {
			role = domainmedia.VariantRoleManifest
			sortOrder = 100
		}
		variants = append(variants, &domainmedia.MediaVariant{
			AssetID: asset.ID, ProfileVersion: job.ProfileVersion,
			SourceType: domainmedia.SourceTypeDASH, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
			ObjectKey: published.objectKey, Role: role, SortOrder: sortOrder, State: domainmedia.VariantStateReady,
			ChecksumSHA256: published.checksum, SizeBytes: published.size, Public: false,
		})
	}

	return &applicationmedia.ProcessResult{
		Width: probe.Width, Height: probe.Height, DurationMS: probe.DurationMS,
		VideoCodec: probe.VideoCodec, AudioCodec: probe.AudioCodec, Variants: variants,
	}, nil
}

type publishedFile struct {
	objectKey string
	checksum  string
	size      int64
}

func (p *FFmpegProcessor) publishBundle(ctx context.Context, assetID, jobID int64, profileVersion, directory string, entries []os.DirEntry) (map[string]publishedFile, error) {
	names := make([]string, 0, len(entries))
	files := make(map[string]publishedFile, len(entries))
	bundleHash := sha256.New()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		checksum, size, err := fileChecksum(filepath.Join(directory, name))
		if err != nil {
			return nil, &applicationmedia.ProcessError{Code: "bundle_checksum_failed", Err: err}
		}
		names = append(names, name)
		files[name] = publishedFile{checksum: checksum, size: size}
	}
	sort.Strings(names)
	for _, name := range names {
		file := files[name]
		_, _ = io.WriteString(bundleHash, name+"\x00"+file.checksum+"\n")
	}
	bundleChecksum := hex.EncodeToString(bundleHash.Sum(nil))
	tempPrefix := fmt.Sprintf("tmp/media/%d/%d/dash", assetID, jobID)
	finalPrefix := fmt.Sprintf("processed/%d/%s/dash-%s", assetID, profileVersion, bundleChecksum)
	for _, name := range names {
		file := files[name]
		path := filepath.Join(directory, name)
		contentType := contentTypeForPath(path)
		tempKey := tempPrefix + "/" + name
		if err := p.putFile(ctx, tempKey, path, file.size, contentType, file.checksum); err != nil {
			return nil, err
		}
		metadata, err := p.store.Head(ctx, tempKey)
		if err != nil || metadata.SizeBytes != file.size || !strings.EqualFold(metadata.ChecksumSHA256, file.checksum) {
			if err == nil {
				err = domainmedia.ErrObjectChecksumMismatch
			}
			return nil, &applicationmedia.ProcessError{Code: "bundle_temp_verify_failed", Err: err}
		}
		finalKey := finalPrefix + "/" + name
		reader, _, err := p.store.Open(ctx, tempKey)
		if err != nil {
			return nil, &applicationmedia.ProcessError{Code: "bundle_temp_open_failed", Err: err}
		}
		_, putErr := p.store.Put(ctx, finalKey, reader, file.size, contentType, file.checksum)
		closeErr := reader.Close()
		if putErr != nil {
			return nil, &applicationmedia.ProcessError{Code: "bundle_publish_failed", Err: putErr}
		}
		if closeErr != nil {
			return nil, &applicationmedia.ProcessError{Code: "bundle_publish_close_failed", Err: closeErr}
		}
		finalMetadata, err := p.store.Head(ctx, finalKey)
		if err != nil || finalMetadata.SizeBytes != file.size || !strings.EqualFold(finalMetadata.ChecksumSHA256, file.checksum) {
			if err == nil {
				err = domainmedia.ErrObjectChecksumMismatch
			}
			return nil, &applicationmedia.ProcessError{Code: "bundle_publish_verify_failed", Err: err}
		}
		if err := p.store.Delete(ctx, tempKey); err != nil {
			return nil, &applicationmedia.ProcessError{Code: "bundle_temp_cleanup_failed", Err: err}
		}
		file.objectKey = finalKey
		files[name] = file
	}
	return files, nil
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
	if result.DurationMS > int64(10*time.Minute/time.Millisecond) {
		return nil, &applicationmedia.ProcessError{Code: "duration_limit", Terminal: true, Err: errors.New("media duration exceeds limit")}
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

func (p *FFmpegProcessor) transcodeMP4(ctx context.Context, sourcePath, outputPath string, width, height, bitrate int) error {
	_, err := p.runCommand(ctx, "ffmpeg",
		"-y", "-i", sourcePath,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-vf", fmt.Sprintf("scale=%d:%d", width, height),
		"-c:v", "libx264", "-preset", "medium", "-pix_fmt", "yuv420p",
		"-b:v", strconv.Itoa(bitrate), "-maxrate", strconv.Itoa(bitrate), "-bufsize", strconv.Itoa(bitrate*2),
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart", outputPath,
	)
	if err != nil {
		return &applicationmedia.ProcessError{Code: "transcode_failed", Err: err}
	}
	return nil
}

func (p *FFmpegProcessor) generateDASH(ctx context.Context, inputs []string, manifestPath string, hasAudio bool, segmentSeconds int) error {
	args := []string{"-y"}
	for _, input := range inputs {
		args = append(args, "-i", input)
	}
	for index := range inputs {
		args = append(args, "-map", fmt.Sprintf("%d:v:0", index))
	}
	adaptationSets := "id=0,streams=v"
	if hasAudio && len(inputs) > 0 {
		args = append(args, "-map", "0:a:0?")
		adaptationSets += " id=1,streams=a"
	}
	args = append(args,
		"-c", "copy", "-f", "dash", "-seg_duration", strconv.Itoa(segmentSeconds),
		"-use_template", "1", "-use_timeline", "1",
		"-init_seg_name", "init-$RepresentationID$.m4s",
		"-media_seg_name", "chunk-$RepresentationID$-$Number%05d$.m4s",
		"-adaptation_sets", adaptationSets, manifestPath,
	)
	if _, err := p.runCommand(ctx, "ffmpeg", args...); err != nil {
		return &applicationmedia.ProcessError{Code: "dash_failed", Err: err}
	}
	return nil
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
		message := strings.TrimSpace(stderr.String())
		if len(message) > 2048 {
			message = message[len(message)-2048:]
		}
		return nil, fmt.Errorf("%s: %w: %s", name, err, message)
	}
	return stdout.Bytes(), nil
}

type processingProfile struct {
	Version            string
	BaselineMaxHeight  int
	Heights            []int
	Bitrates           map[int]int
	DASHSegmentSeconds int
}

func selectProcessingProfile(version string) (*processingProfile, error) {
	switch strings.TrimSpace(version) {
	case "v1":
		return &processingProfile{
			Version: "v1", BaselineMaxHeight: 720, Heights: []int{480, 720, 1080},
			Bitrates:           map[int]int{480: 1_200_000, 720: 2_500_000, 1080: 5_000_000},
			DASHSegmentSeconds: 4,
		}, nil
	default:
		return nil, &applicationmedia.ProcessError{Code: "unsupported_profile", Terminal: true, Err: errors.New("unsupported media processing profile")}
	}
}

func renditionHeights(sourceHeight int, configured []int) []int {
	result := make([]int, 0, len(configured))
	for _, height := range configured {
		if height <= sourceHeight {
			result = append(result, height)
		}
	}
	return result
}

func appendUniqueHeight(values []int, candidates ...int) []int {
	seen := make(map[int]struct{}, len(values)+len(candidates))
	result := make([]int, 0, len(values)+len(candidates))
	for _, value := range append(values, candidates...) {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func scaledEvenWidth(sourceWidth, sourceHeight, targetHeight int) int {
	if sourceWidth <= 0 || sourceHeight <= 0 || targetHeight <= 0 {
		return 2
	}
	width := int(math.Round(float64(sourceWidth) * float64(targetHeight) / float64(sourceHeight)))
	if width%2 != 0 {
		width--
	}
	return max(width, 2)
}

func (p *processingProfile) bitrateForHeight(height int) int {
	if value := p.Bitrates[height]; value > 0 {
		return value
	}
	for _, configuredHeight := range p.Heights {
		if height <= configuredHeight && p.Bitrates[configuredHeight] > 0 {
			return p.Bitrates[configuredHeight]
		}
	}
	return 5_000_000
}

func generatedPaths(generated []generatedMP4) []string {
	paths := make([]string, 0, len(generated))
	for _, output := range generated {
		paths = append(paths, output.path)
	}
	return paths
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

func contentTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mpd":
		return "application/dash+xml"
	case ".m4s":
		return "video/iso.segment"
	default:
		if value := mime.TypeByExtension(filepath.Ext(path)); value != "" {
			return value
		}
		return "application/octet-stream"
	}
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
