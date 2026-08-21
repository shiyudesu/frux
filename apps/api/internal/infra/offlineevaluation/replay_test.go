package infraofflineevaluation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	applicationofflineevaluation "github.com/shiyudesu/frux/internal/application/offlineevaluation"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func TestLoadNamedPolicyStrictlyNormalizesAndHashes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	content, err := json.Marshal(namedPolicyFile{Name: "baseline", Configuration: domainrecommendation.InitialRecommendationPolicyConfiguration()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadNamedPolicy(path)
	if err != nil || policy.Name != "baseline" || policy.InputSHA256 == "" || policy.NormalizedSHA256 == "" {
		t.Fatalf("policy=%#v err=%v", policy, err)
	}
	if err := os.WriteFile(path, append(content, []byte(`{"trailing":true}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNamedPolicy(path); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func TestLoadReplayBundleRejectsUnknownAndNonCanonicalTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replay.json")
	bundle := `{"version":"linear-replay-v1","scope":"full_pool_fixture","cases":[{"name":"case","expected_order":[1],"candidates":[{"video_id":1,"author_key":"a","published_at":"1970-01-01T00:01:40Z","recall_providers":["fresh"],"score_components":{"content_similarity":1,"session_similarity":0,"semantic_similarity":0,"hotness":0,"freshness":0,"author_affinity":0,"follow_relation":0,"negative_penalty":0,"exposure_penalty":0}}]}]}`
	if err := os.WriteFile(path, []byte(bundle), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReplayBundle(path)
	if err != nil || loaded.Bundle.Version != applicationofflineevaluation.ReplayVersion || loaded.SHA256 == "" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	bad := []byte(`{"version":"linear-replay-v1","scope":"full_pool_fixture","unknown":true,"cases":[]}`)
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReplayBundle(path); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}
