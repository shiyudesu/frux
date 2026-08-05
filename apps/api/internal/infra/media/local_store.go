package inframedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

type LocalStore struct {
	root string
}

func NewLocalStore(root string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, err
	}
	return &LocalStore{root: absolute}, nil
}

func (s *LocalStore) Put(_ context.Context, key string, body io.Reader, sizeBytes int64, contentType, checksumSHA256 string) (*domainmedia.ObjectMetadata, error) {
	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	if sizeBytes <= 0 {
		return nil, domainmedia.ErrInvalidSize
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".frux-upload-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(body, sizeBytes+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written != sizeBytes {
		return nil, domainmedia.ErrInvalidSize
	}
	actualChecksum := hex.EncodeToString(hash.Sum(nil))
	if checksumSHA256 != "" && !strings.EqualFold(actualChecksum, strings.TrimSpace(checksumSHA256)) {
		return nil, domainmedia.ErrObjectChecksumMismatch
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = mime.TypeByExtension(filepath.Ext(path))
	}
	return &domainmedia.ObjectMetadata{
		Key: key, ContentType: contentType, SizeBytes: sizeBytes, ChecksumSHA256: actualChecksum,
		ETag: actualChecksum, LastModified: info.ModTime().UTC(),
	}, nil
}

func (s *LocalStore) Open(ctx context.Context, key string) (io.ReadCloser, *domainmedia.ObjectMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	metadata, err := s.Head(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	path, err := s.resolve(key)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, domainmedia.ErrObjectNotFound
		}
		return nil, nil, err
	}
	return file, metadata, nil
}

func (s *LocalStore) Head(_ context.Context, key string) (*domainmedia.ObjectMetadata, error) {
	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, domainmedia.ErrObjectNotFound
		}
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	return &domainmedia.ObjectMetadata{
		Key: key, ContentType: mime.TypeByExtension(filepath.Ext(path)), SizeBytes: info.Size(),
		ChecksumSHA256: checksum, ETag: checksum, LastModified: info.ModTime().UTC(),
	}, nil
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *LocalStore) List(ctx context.Context, prefix string) ([]domainmedia.ObjectMetadata, error) {
	prefix = strings.TrimSpace(strings.TrimPrefix(prefix, "/"))
	if prefix != "" && !domainmedia.ValidObjectKey(prefix) {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	root := s.root
	if prefix != "" {
		resolved, err := s.resolve(prefix)
		if err != nil {
			return nil, err
		}
		root = resolved
	}
	result := []domainmedia.ObjectMetadata{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		metadata, err := s.Head(ctx, key)
		if err != nil {
			return err
		}
		result = append(result, *metadata)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	return result, err
}

func (s *LocalStore) PresignPut(context.Context, string, string, string, int64, time.Duration) (*domainmedia.PresignedRequest, error) {
	return nil, domainmedia.ErrPresignUnsupported
}

func (s *LocalStore) PresignGet(context.Context, string, time.Duration) (*domainmedia.PresignedRequest, error) {
	return nil, domainmedia.ErrPresignUnsupported
}

func (s *LocalStore) Path(key string) (string, error) {
	return s.resolve(key)
}

func (s *LocalStore) resolve(key string) (string, error) {
	key = strings.TrimSpace(key)
	if !domainmedia.ValidObjectKey(key) {
		return "", domainmedia.ErrInvalidObjectKey
	}
	path := filepath.Join(s.root, filepath.FromSlash(key))
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", domainmedia.ErrInvalidObjectKey
	}
	return path, nil
}
