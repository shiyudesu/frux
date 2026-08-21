package infraofflineevaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

type InputFailure string

const (
	FailureManifest InputFailure = "manifest"
	FailurePath     InputFailure = "path"
	FailureFile     InputFailure = "file"
	FailureSize     InputFailure = "size"
	FailureHash     InputFailure = "hash"
	FailureRows     InputFailure = "rows"
	FailureCSV      InputFailure = "csv"
)

type InputError struct {
	Code InputFailure
	Role string
}

func (e *InputError) Error() string {
	if e == nil {
		return "offline evaluation input failed"
	}
	if e.Role == "" {
		return "offline evaluation input failed: " + string(e.Code)
	}
	return "offline evaluation input failed: " + string(e.Code) + ":" + e.Role
}

type ManifestLimits struct {
	MaxManifestBytes int64
	MaxFileBytes     int64
	MaxRows          int64
	MaxColumns       int
}

func DefaultManifestLimits() ManifestLimits {
	return ManifestLimits{
		MaxManifestBytes: 1 << 20,
		MaxFileBytes:     8 << 30,
		MaxRows:          100_000_000,
		MaxColumns:       64,
	}
}

type FileEvidence struct {
	Role   string `json:"role"`
	Schema string `json:"schema"`
	SHA256 string `json:"sha256"`
	Rows   int64  `json:"rows"`
	Bytes  int64  `json:"bytes"`
}

type ManifestEvidence struct {
	ManifestSHA256 string         `json:"manifest_sha256"`
	Dataset        string         `json:"dataset"`
	Release        string         `json:"release"`
	Schema         string         `json:"schema"`
	SourceURL      string         `json:"source_url"`
	Citation       string         `json:"citation"`
	LicenseID      string         `json:"license_id"`
	LicenseStatus  string         `json:"license_status"`
	Files          []FileEvidence `json:"files"`
}

type LoadedManifest struct {
	Root     string
	Manifest domainofflineevaluation.Manifest
	Evidence ManifestEvidence
	Files    map[string]string
}

func LoadManifest(root, manifestPath string, limits ManifestLimits) (*LoadedManifest, error) {
	if !validManifestLimits(limits) {
		return nil, &InputError{Code: FailureManifest}
	}
	rootPath, err := secureRoot(root)
	if err != nil {
		return nil, err
	}
	resolvedManifest, err := secureRelativeFile(rootPath, manifestPath, limits.MaxManifestBytes, "manifest")
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(resolvedManifest)
	if err != nil || int64(len(content)) > limits.MaxManifestBytes {
		return nil, &InputError{Code: FailureManifest}
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest domainofflineevaluation.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, &InputError{Code: FailureManifest}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, &InputError{Code: FailureManifest}
	}
	manifest, err = manifest.Validate()
	if err != nil {
		return nil, &InputError{Code: FailureManifest}
	}
	manifestDigest := sha256.Sum256(content)
	loaded := &LoadedManifest{
		Root: rootPath, Manifest: manifest, Files: make(map[string]string, len(manifest.Files)),
		Evidence: ManifestEvidence{
			ManifestSHA256: hex.EncodeToString(manifestDigest[:]), Dataset: string(manifest.Dataset),
			Release: manifest.Release, Schema: manifest.Schema, SourceURL: manifest.SourceURL,
			Citation: manifest.Citation, LicenseID: manifest.LicenseID,
			LicenseStatus: manifest.LicenseStatus, Files: make([]FileEvidence, 0, len(manifest.Files)),
		},
	}
	for _, declared := range manifest.Files {
		resolved, err := secureRelativeFile(rootPath, declared.Path, limits.MaxFileBytes, declared.Role)
		if err != nil {
			return nil, err
		}
		evidence, err := inspectCSV(resolved, declared.Role, declared.Schema, limits)
		if err != nil {
			return nil, err
		}
		if evidence.SHA256 != declared.SHA256 {
			return nil, &InputError{Code: FailureHash, Role: declared.Role}
		}
		if evidence.Rows != declared.Rows {
			return nil, &InputError{Code: FailureRows, Role: declared.Role}
		}
		loaded.Files[declared.Role] = resolved
		loaded.Evidence.Files = append(loaded.Evidence.Files, evidence)
	}
	sort.Slice(loaded.Evidence.Files, func(i, j int) bool {
		return loaded.Evidence.Files[i].Role < loaded.Evidence.Files[j].Role
	})
	return loaded, nil
}

func inspectCSV(filePath, role, schema string, limits ManifestLimits) (FileEvidence, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return FileEvidence{}, &InputError{Code: FailureFile, Role: role}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > limits.MaxFileBytes {
		return FileEvidence{}, &InputError{Code: FailureSize, Role: role}
	}
	hasher := sha256.New()
	reader := csv.NewReader(io.TeeReader(file, hasher))
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil || len(header) == 0 || len(header) > limits.MaxColumns {
		return FileEvidence{}, &InputError{Code: FailureCSV, Role: role}
	}
	rows := int64(0)
	for {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || len(record) == 0 || len(record) > limits.MaxColumns {
			return FileEvidence{}, &InputError{Code: FailureCSV, Role: role}
		}
		rows++
		if rows > limits.MaxRows {
			return FileEvidence{}, &InputError{Code: FailureRows, Role: role}
		}
	}
	return FileEvidence{
		Role: role, Schema: schema, SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Rows: rows, Bytes: info.Size(),
	}, nil
}

func secureRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", &InputError{Code: FailurePath}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", &InputError{Code: FailurePath}
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", &InputError{Code: FailurePath}
	}
	return filepath.Clean(absolute), nil
}

func secureRelativeFile(root, relative string, maximum int64, role string) (string, error) {
	relative = strings.TrimSpace(relative)
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative ||
		relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", &InputError{Code: FailurePath, Role: role}
	}
	resolved := filepath.Join(root, relative)
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &InputError{Code: FailurePath, Role: role}
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", &InputError{Code: FailureFile, Role: role}
	}
	if info.Size() <= 0 || info.Size() > maximum {
		return "", &InputError{Code: FailureSize, Role: role}
	}
	return resolved, nil
}

func validManifestLimits(limits ManifestLimits) bool {
	return limits.MaxManifestBytes >= 1024 && limits.MaxManifestBytes <= 8<<20 &&
		limits.MaxFileBytes >= 1024 && limits.MaxFileBytes <= 16<<30 &&
		limits.MaxRows >= 1 && limits.MaxRows <= 100_000_000 &&
		limits.MaxColumns >= 2 && limits.MaxColumns <= 256
}
