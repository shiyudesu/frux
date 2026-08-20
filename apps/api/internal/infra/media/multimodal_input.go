package inframedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

type FFmpegMultimodalMediaPreparer struct {
	store     domainmedia.MediaObjectStore
	processor *FFmpegProcessor
	tempRoot  string
}

func NewFFmpegMultimodalMediaPreparer(
	store domainmedia.MediaObjectStore,
	tempRoot string,
) *FFmpegMultimodalMediaPreparer {
	return &FFmpegMultimodalMediaPreparer{
		store: store, processor: NewFFmpegProcessor(store), tempRoot: strings.TrimSpace(tempRoot),
	}
}

func (p *FFmpegMultimodalMediaPreparer) PrepareMultimodalMedia(
	ctx context.Context,
	request applicationembedding.MultimodalMediaPreparationRequest,
) (*applicationembedding.PreparedMultimodalMedia, error) {
	if p == nil || p.store == nil || p.processor == nil ||
		strings.TrimSpace(request.VideoObjectKey) == "" ||
		request.FrameSamplingPolicy != domainembedding.MultimodalFrameSamplingPolicyV1 ||
		request.ImagePreprocessingPolicy != domainembedding.MultimodalImagePreprocessingV1 ||
		request.MaxImages < 1 || request.MaxImages > 16 ||
		request.MaxBytesEach < 64*1024 || request.MaxTotalBytes < request.MaxBytesEach ||
		request.MaxPixelsEach < 10_000 ||
		!containsMIMEType(request.AllowedMIMETypes, "image/jpeg") {
		return nil, applicationembedding.ErrInvalidMultimodalMediaPreparation
	}
	workDir, err := os.MkdirTemp(p.tempRoot, "frux-multimodal-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	videoPath := filepath.Join(workDir, "source-video")
	if err := p.downloadObject(ctx, request.VideoObjectKey, videoPath); err != nil {
		return nil, err
	}
	probe, err := p.processor.probe(ctx, videoPath)
	if err != nil {
		return nil, err
	}

	coverPath := ""
	if strings.TrimSpace(request.CoverObjectKey) != "" {
		coverPath = filepath.Join(workDir, "source-cover")
		if err := p.downloadObject(ctx, request.CoverObjectKey, coverPath); err != nil {
			return nil, err
		}
	}

	edge := min(512, int(math.Floor(math.Sqrt(float64(request.MaxPixelsEach)))))
	if edge < 2 {
		return nil, applicationembedding.ErrInvalidMultimodalMediaPreparation
	}
	outputDir := filepath.Join(workDir, "prepared")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return nil, err
	}

	for _, quality := range []int{3, 6, 9} {
		images, prepareErr := p.prepareImages(
			ctx, videoPath, coverPath, outputDir, probe.DurationMS, edge, quality, request,
		)
		if prepareErr == nil {
			return &applicationembedding.PreparedMultimodalMedia{Images: images}, nil
		}
		if !errors.Is(prepareErr, applicationembedding.ErrInvalidMultimodalMediaPreparation) {
			return nil, prepareErr
		}
	}
	return nil, applicationembedding.ErrInvalidMultimodalMediaPreparation
}

func (p *FFmpegMultimodalMediaPreparer) prepareImages(
	ctx context.Context,
	videoPath string,
	coverPath string,
	outputDir string,
	durationMS int64,
	edge int,
	quality int,
	request applicationembedding.MultimodalMediaPreparationRequest,
) ([]applicationembedding.PreparedMultimodalImage, error) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(outputDir, entry.Name())); err != nil {
			return nil, err
		}
	}

	paths := make([]string, 0, request.MaxImages)
	if coverPath != "" {
		path := filepath.Join(outputDir, "00-cover.jpg")
		if err := p.extractImage(ctx, coverPath, 0, false, path, edge, quality); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	remaining := request.MaxImages - len(paths)
	for index, timestamp := range multimodalFrameTimestamps(durationMS, remaining) {
		path := filepath.Join(outputDir, fmt.Sprintf("%02d-frame-%012d.jpg", index+len(paths), timestamp))
		if err := p.extractImage(ctx, videoPath, timestamp, true, path, edge, quality); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}

	images := make([]applicationembedding.PreparedMultimodalImage, 0, len(paths))
	seenDigests := make(map[string]struct{}, len(paths))
	total := 0
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.Size() <= 0 || info.Size() > int64(request.MaxBytesEach) {
			return nil, applicationembedding.ErrInvalidMultimodalMediaPreparation
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if total > request.MaxTotalBytes-len(content) {
			return nil, applicationembedding.ErrInvalidMultimodalMediaPreparation
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(content))
		if err != nil || config.Width <= 0 || config.Height <= 0 ||
			int64(config.Width)*int64(config.Height) > request.MaxPixelsEach {
			return nil, applicationembedding.ErrInvalidMultimodalMediaPreparation
		}
		sum := sha256.Sum256(content)
		digest := hex.EncodeToString(sum[:])
		if _, duplicate := seenDigests[digest]; duplicate {
			continue
		}
		seenDigests[digest] = struct{}{}
		total += len(content)
		images = append(images, applicationembedding.PreparedMultimodalImage{
			MIMEType: "image/jpeg", Width: config.Width, Height: config.Height,
			Digest: digest, Content: append([]byte(nil), content...),
		})
	}
	if len(images) == 0 {
		return nil, applicationembedding.ErrInvalidMultimodalMediaPreparation
	}
	return images, nil
}

func (p *FFmpegMultimodalMediaPreparer) extractImage(
	ctx context.Context,
	sourcePath string,
	timestampMS int64,
	seek bool,
	outputPath string,
	edge int,
	quality int,
) error {
	args := []string{"-y"}
	if seek {
		args = append(args, "-ss", formatTimestamp(timestampMS))
	}
	args = append(args,
		"-i", sourcePath,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", edge, edge),
		"-q:v", strconv.Itoa(quality),
		outputPath,
	)
	_, err := p.processor.runCommand(ctx, "ffmpeg", args...)
	return err
}

func (p *FFmpegMultimodalMediaPreparer) downloadObject(ctx context.Context, objectKey, path string) error {
	reader, metadata, err := p.store.Open(ctx, strings.TrimSpace(objectKey))
	if err != nil {
		return err
	}
	defer reader.Close()
	if metadata == nil || metadata.SizeBytes <= 0 || metadata.ChecksumSHA256 == "" {
		return applicationembedding.ErrInvalidMultimodalMediaPreparation
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(reader, metadata.SizeBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != metadata.SizeBytes ||
		!strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), metadata.ChecksumSHA256) {
		return domainmedia.ErrObjectChecksumMismatch
	}
	return nil
}

func multimodalFrameTimestamps(durationMS int64, count int) []int64 {
	if durationMS <= 0 || count <= 0 {
		return nil
	}
	timestamps := make([]int64, 0, count)
	for index := 0; index < count; index++ {
		timestamp := int64(2*index+1) * durationMS / int64(2*count)
		if timestamp >= durationMS {
			timestamp = durationMS - 1
		}
		timestamps = append(timestamps, timestamp)
	}
	return timestamps
}

func containsMIMEType(values []string, target string) bool {
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}
