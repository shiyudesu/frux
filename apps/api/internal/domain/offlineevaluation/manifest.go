package domainofflineevaluation

import (
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"slices"
	"strings"
)

var ErrInvalidManifest = errors.New("invalid offline evaluation manifest")

const (
	RoleInteractions       = "interactions"
	RoleCategories         = "categories"
	RoleItems              = "items"
	RoleAuthors            = "authors"
	RoleTextFeatures       = "text_features"
	RoleImageFeatures      = "image_features"
	RoleMultimodalFeatures = "multimodal_features"
	RoleThroughput         = "embedding_throughput"

	maxManifestFiles = 8
	maxManifestRows  = int64(100_000_000)
)

type Manifest struct {
	Version             string         `json:"version"`
	Dataset             DatasetKind    `json:"dataset"`
	Release             string         `json:"release"`
	SourceURL           string         `json:"source_url"`
	Citation            string         `json:"citation"`
	LicenseID           string         `json:"license_id"`
	LicenseStatus       string         `json:"license_status"`
	Schema              string         `json:"schema"`
	NormalizationRecipe string         `json:"normalization_recipe,omitempty"`
	Files               []ManifestFile `json:"files"`
}

type ManifestFile struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	Schema string `json:"schema"`
	SHA256 string `json:"sha256"`
	Rows   int64  `json:"rows"`
}

func (m Manifest) Validate() (Manifest, error) {
	m.Version = strings.TrimSpace(m.Version)
	m.Release = strings.TrimSpace(m.Release)
	m.SourceURL = strings.TrimSpace(m.SourceURL)
	m.Citation = strings.TrimSpace(m.Citation)
	m.LicenseID = strings.TrimSpace(m.LicenseID)
	m.LicenseStatus = strings.ToLower(strings.TrimSpace(m.LicenseStatus))
	m.Schema = strings.ToLower(strings.TrimSpace(m.Schema))
	m.NormalizationRecipe = strings.TrimSpace(m.NormalizationRecipe)
	if m.Version != ManifestVersion || !ValidDatasetKind(m.Dataset) ||
		!boundedManifestText(m.Release, 1, 128) || !boundedManifestText(m.Citation, 1, 512) ||
		!boundedManifestText(m.LicenseID, 1, 128) || m.LicenseStatus != LicenseOperatorReview ||
		len(m.Files) < 2 || len(m.Files) > maxManifestFiles || !validSourceURL(m.SourceURL) {
		return Manifest{}, ErrInvalidManifest
	}
	switch m.Dataset {
	case DatasetKuaiRec:
		if m.Schema != KuaiRecSchemaV2 || m.NormalizationRecipe != "" {
			return Manifest{}, ErrInvalidManifest
		}
	case DatasetMicroLens:
		if m.Schema != MicroLensCanonicalV1 || !boundedManifestText(m.NormalizationRecipe, 1, 128) {
			return Manifest{}, ErrInvalidManifest
		}
	default:
		return Manifest{}, ErrInvalidManifest
	}
	roles := make(map[string]struct{}, len(m.Files))
	paths := make(map[string]struct{}, len(m.Files))
	files := make([]ManifestFile, len(m.Files))
	for index, file := range m.Files {
		validated, err := validateManifestFile(file)
		if err != nil || !roleAllowedForDataset(m.Dataset, validated.Role) {
			return Manifest{}, ErrInvalidManifest
		}
		if _, exists := roles[validated.Role]; exists {
			return Manifest{}, ErrInvalidManifest
		}
		if _, exists := paths[validated.Path]; exists {
			return Manifest{}, ErrInvalidManifest
		}
		roles[validated.Role] = struct{}{}
		paths[validated.Path] = struct{}{}
		files[index] = validated
	}
	if _, exists := roles[RoleInteractions]; !exists {
		return Manifest{}, ErrInvalidManifest
	}
	requiredRole := RoleCategories
	if m.Dataset == DatasetMicroLens {
		requiredRole = RoleItems
	}
	if _, exists := roles[requiredRole]; !exists {
		return Manifest{}, ErrInvalidManifest
	}
	m.Files = files
	return m, nil
}

func validateManifestFile(file ManifestFile) (ManifestFile, error) {
	file.Role = strings.ToLower(strings.TrimSpace(file.Role))
	file.Path = strings.TrimSpace(file.Path)
	file.Schema = strings.ToLower(strings.TrimSpace(file.Schema))
	file.SHA256 = strings.ToLower(strings.TrimSpace(file.SHA256))
	if !slices.Contains(manifestRoles(), file.Role) || !safeRelativeManifestPath(file.Path) ||
		!boundedManifestText(file.Schema, 1, 128) || file.Rows < 1 || file.Rows > maxManifestRows ||
		!validManifestDigest(file.SHA256) {
		return ManifestFile{}, ErrInvalidManifest
	}
	return file, nil
}

func manifestRoles() []string {
	return []string{
		RoleInteractions, RoleCategories, RoleItems, RoleAuthors,
		RoleTextFeatures, RoleImageFeatures, RoleMultimodalFeatures, RoleThroughput,
	}
}

func roleAllowedForDataset(dataset DatasetKind, role string) bool {
	if dataset == DatasetKuaiRec {
		return role != RoleItems
	}
	return role != RoleCategories
}

func safeRelativeManifestPath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') ||
		strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func validManifestDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func validSourceURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.Fragment == "" && len(value) <= 512
}

func boundedManifestText(value string, minimum, maximum int) bool {
	return len(value) >= minimum && len(value) <= maximum && !strings.ContainsAny(value, "\r\n\x00")
}
