package infraofflineevaluation

import (
	"os"
	"path/filepath"
	"testing"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

func TestLoadDatasetNormalizesKuaiRecAndMicroLensFixtures(t *testing.T) {
	for _, fixture := range []struct {
		root      string
		kind      domainofflineevaluation.DatasetKind
		firstItem string
		firstUser string
	}{
		{filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "kuairec-v2"), domainofflineevaluation.DatasetKuaiRec, "101", "1"},
		{filepath.Join("..", "..", "..", "testdata", "recommendation-offline", "microlens-canonical-v1"), domainofflineevaluation.DatasetMicroLens, "video-1", "user-a"},
	} {
		loaded, err := LoadManifest(fixture.root, "manifest.json", DefaultManifestLimits())
		if err != nil {
			t.Fatal(err)
		}
		dataset, err := LoadDataset(loaded, DefaultDatasetLimits())
		if err != nil {
			t.Fatal(err)
		}
		if dataset.Kind != fixture.kind || len(dataset.Interactions) != 8 || len(dataset.Items) != 6 ||
			dataset.Interactions[0].UserKey != domainofflineevaluation.DatasetUserKey(fixture.kind, fixture.firstUser) ||
			dataset.Interactions[0].ItemKey != domainofflineevaluation.DatasetItemKey(fixture.kind, fixture.firstItem) ||
			dataset.FeatureDimensions[domainofflineevaluation.FeatureMultimodal] != 3 {
			t.Fatalf("dataset=%#v", dataset)
		}
	}
}

func TestDatasetParsersRejectRatioMismatchDuplicateAndUnknownRawLayout(t *testing.T) {
	root := t.TempDir()
	mismatch := filepath.Join(root, "mismatch.csv")
	if err := os.WriteFile(mismatch, []byte("user_id,video_id,play_duration,video_duration,time,date,timestamp,watch_ratio\n1,1,9,10,t,d,1,0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataset := &domainofflineevaluation.Dataset{
		Kind: domainofflineevaluation.DatasetKuaiRec,
		Items: map[string]domainofflineevaluation.Item{
			domainofflineevaluation.DatasetItemKey(domainofflineevaluation.DatasetKuaiRec, "1"): {Key: domainofflineevaluation.DatasetItemKey(domainofflineevaluation.DatasetKuaiRec, "1")},
		},
	}
	if err := parseKuaiInteractions(mismatch, dataset, DefaultDatasetLimits()); err == nil {
		t.Fatal("expected watch-ratio mismatch")
	}
	duplicate := filepath.Join(root, "duplicate.csv")
	if err := os.WriteFile(duplicate, []byte("video_id,feat\n1,\"[1,1]\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := parseKuaiCategories(duplicate, &domainofflineevaluation.Dataset{Kind: domainofflineevaluation.DatasetKuaiRec, Items: map[string]domainofflineevaluation.Item{}}, DefaultDatasetLimits()); err == nil {
		t.Fatal("expected duplicate category rejection")
	}
	unknown := filepath.Join(root, "unknown.csv")
	if err := os.WriteFile(unknown, []byte("user,item,time,label\na,b,1,1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := parseMicroInteractions(unknown, &domainofflineevaluation.Dataset{Kind: domainofflineevaluation.DatasetMicroLens, Items: map[string]domainofflineevaluation.Item{}}, DefaultDatasetLimits()); err == nil {
		t.Fatal("expected unknown MicroLens layout rejection")
	}
}
