package domainofflineevaluation

import "testing"

func TestRegistriesAreClosedAndKIsSorted(t *testing.T) {
	if !ValidTrack(TrackPublicDataset) || ValidTrack("online") ||
		!ValidDatasetKind(DatasetKuaiRec) || ValidDatasetKind("mixed") ||
		len(Baselines()) != 7 || !ValidBaseline(BaselineMultimodalSession) || ValidBaseline("trained") ||
		!ValidK([]int{1, 5, 10}) || ValidK([]int{5, 1}) || ValidK([]int{1, 1}) || ValidK([]int{0}) {
		t.Fatal("closed offline evaluation registry validation failed")
	}
}

func TestManifestValidationSeparatesDatasetProfiles(t *testing.T) {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	kuai := Manifest{
		Version: ManifestVersion, Dataset: DatasetKuaiRec, Release: "2.0",
		SourceURL: "https://github.com/chongminggao/KuaiRec", Citation: "KuaiRec CIKM 2022",
		LicenseID: "CC-BY-SA-4.0", LicenseStatus: LicenseOperatorReview, Schema: KuaiRecSchemaV2,
		Files: []ManifestFile{
			{Role: RoleInteractions, Path: "small_matrix.csv", Schema: "kuairec-interactions-v2", SHA256: digest, Rows: 2},
			{Role: RoleCategories, Path: "item_categories.csv", Schema: "kuairec-categories-v2", SHA256: digest, Rows: 2},
		},
	}
	if _, err := kuai.Validate(); err != nil {
		t.Fatal(err)
	}
	micro := kuai
	micro.Dataset = DatasetMicroLens
	micro.Schema = MicroLensCanonicalV1
	micro.NormalizationRecipe = "frux-microlens-normalizer-v1"
	micro.Files[1].Role = RoleItems
	if _, err := micro.Validate(); err != nil {
		t.Fatal(err)
	}
	micro.NormalizationRecipe = ""
	if _, err := micro.Validate(); err == nil {
		t.Fatal("expected MicroLens normalization recipe rejection")
	}
	kuai.Files[0].Path = "../small_matrix.csv"
	if _, err := kuai.Validate(); err == nil {
		t.Fatal("expected escaping path rejection")
	}
}
