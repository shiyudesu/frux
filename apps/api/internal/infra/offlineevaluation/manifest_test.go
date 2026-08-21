package infraofflineevaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

func TestLoadManifestValidatesHashesCountsAndRedactsPaths(t *testing.T) {
	root := t.TempDir()
	interaction := []byte("user_id,video_id,play_duration,video_duration,timestamp,watch_ratio\n1,2,8,10,100,0.8\n")
	categories := []byte("video_id,feat\n2,[1]\n")
	writeFixture(t, root, "small_matrix.csv", interaction)
	writeFixture(t, root, "item_categories.csv", categories)
	manifest := fmt.Sprintf(`{
  "version": %q,
  "dataset": "kuairec",
  "release": "2.0",
  "source_url": "https://github.com/chongminggao/KuaiRec",
  "citation": "KuaiRec CIKM 2022",
  "license_id": "CC-BY-SA-4.0",
  "license_status": "operator_reviewed",
  "schema": "kuairec-v2",
  "files": [
    {"role":"interactions","path":"small_matrix.csv","schema":"kuairec-interactions-v2","sha256":%q,"rows":1},
    {"role":"categories","path":"item_categories.csv","schema":"kuairec-categories-v2","sha256":%q,"rows":1}
  ]
}`, domainofflineevaluation.ManifestVersion, digest(interaction), digest(categories))
	writeFixture(t, root, "manifest.json", []byte(manifest))
	loaded, err := LoadManifest(root, "manifest.json", DefaultManifestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.Dataset != domainofflineevaluation.DatasetKuaiRec ||
		len(loaded.Evidence.Files) != 2 || loaded.Evidence.Files[0].Role != domainofflineevaluation.RoleCategories ||
		loaded.Evidence.ManifestSHA256 == "" || loaded.Files[domainofflineevaluation.RoleInteractions] == "" {
		t.Fatalf("loaded=%#v", loaded)
	}
	if value := fmt.Sprintf("%+v", loaded.Evidence); filepath.IsAbs(value) || contains(value, root) {
		t.Fatalf("evidence leaked path: %s", value)
	}
}

func TestLoadManifestRejectsMismatchAndSymlink(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "small_matrix.csv", []byte("user_id,video_id\n1,2\n"))
	writeFixture(t, root, "item_categories.csv", []byte("video_id,feat\n2,[1]\n"))
	manifest := fmt.Sprintf(`{"version":%q,"dataset":"kuairec","release":"2.0","source_url":"https://example.com/source","citation":"citation","license_id":"reviewed","license_status":"operator_reviewed","schema":"kuairec-v2","files":[{"role":"interactions","path":"small_matrix.csv","schema":"v1","sha256":%q,"rows":1},{"role":"categories","path":"item_categories.csv","schema":"v1","sha256":%q,"rows":1}]}`,
		domainofflineevaluation.ManifestVersion,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		digest([]byte("video_id,feat\n2,[1]\n")),
	)
	writeFixture(t, root, "manifest.json", []byte(manifest))
	if _, err := LoadManifest(root, "manifest.json", DefaultManifestLimits()); err == nil {
		t.Fatal("expected hash mismatch")
	}
	if err := os.Remove(filepath.Join(root, "small_matrix.csv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "item_categories.csv"), filepath.Join(root, "small_matrix.csv")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(root, "manifest.json", DefaultManifestLimits()); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func writeFixture(t testing.TB, root, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func contains(value, target string) bool {
	return len(target) > 0 && len(value) >= len(target) && (value == target || filepath.Clean(value) == target || stringContains(value, target))
}

func stringContains(value, target string) bool {
	for index := 0; index+len(target) <= len(value); index++ {
		if value[index:index+len(target)] == target {
			return true
		}
	}
	return false
}
