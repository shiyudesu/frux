package domainofflineevaluation

import (
	"testing"
	"time"
)

func TestBuildCasesUsesOnlyEarlierHistoryAndExcludesPriorItems(t *testing.T) {
	dataset := testDataset()
	result, err := BuildCases(dataset, DefaultCaseProfile(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Cases) != 1 || result.Cases[0].TargetItemKey != "kuairec:item:4" ||
		len(result.Cases[0].History) != 3 || len(result.Cases[0].Session) != 3 ||
		containsString(result.Cases[0].CandidateKeys, "kuairec:item:1") ||
		!containsString(result.Cases[0].CandidateKeys, "kuairec:item:4") {
		t.Fatalf("result=%#v", result)
	}
	for _, interaction := range result.Cases[0].History {
		if !interaction.OccurredAt.Before(result.Cases[0].Cutoff) {
			t.Fatal("future interaction leaked into history")
		}
	}
	profile := DefaultCaseProfile()
	neutral := 0.5
	if profile.Classify(nil) != FeedbackMissing || profile.Classify(&neutral) != FeedbackNeutral {
		t.Fatal("feedback classification mismatch")
	}
}

func TestBaselinesAreDeterministicAndSessionInterestUsesNegativeSignal(t *testing.T) {
	dataset := testDataset()
	result, err := BuildCases(dataset, DefaultCaseProfile(), 10)
	if err != nil {
		t.Fatal(err)
	}
	fixture := result.Cases[0]
	for _, baseline := range Baselines() {
		first := Rank(dataset, fixture, DefaultCaseProfile(), baseline)
		second := Rank(dataset, fixture, DefaultCaseProfile(), baseline)
		if !first.Available || len(first.Items) != len(fixture.CandidateKeys) || len(second.Items) != len(first.Items) {
			t.Fatalf("baseline=%s first=%#v", baseline, first)
		}
		for index := range first.Items {
			if first.Items[index] != second.Items[index] {
				t.Fatalf("baseline=%s nondeterministic", baseline)
			}
		}
	}
	session := Rank(dataset, fixture, DefaultCaseProfile(), BaselineMultimodalSession)
	if session.Items[0].ItemKey != fixture.TargetItemKey {
		t.Fatalf("ranking=%#v", session.Items)
	}
	missing := dataset.Items["kuairec:item:5"]
	delete(missing.Features, FeatureImage)
	dataset.Items[missing.Key] = missing
	if ranking := Rank(dataset, fixture, DefaultCaseProfile(), BaselineImage); ranking.Available || ranking.Reason != ExclusionMissingFeature {
		t.Fatalf("ranking=%#v", ranking)
	}
}

func testDataset() *Dataset {
	base := time.Unix(100, 0).UTC()
	positive := 0.9
	negative := 0.1
	neutral := 0.5
	items := map[string]Item{}
	features := [][]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {0.95, 0.05, 0}, {0, 0.2, 0.8}}
	for index := 1; index <= 5; index++ {
		key := "kuairec:item:" + string(rune('0'+index))
		items[key] = Item{
			Key: key, AuthorKey: "author", Categories: []string{"category-" + string(rune('0'+index%2))},
			Features: map[FeatureChannel][]float64{
				FeatureText: append([]float64(nil), features[index-1]...), FeatureImage: append([]float64(nil), features[index-1]...),
				FeatureMultimodal: append([]float64(nil), features[index-1]...),
			},
		}
	}
	return &Dataset{
		Kind: DatasetKuaiRec, Release: "fixture", Schema: KuaiRecSchemaV2, Items: items,
		FeatureDimensions: map[FeatureChannel]int{FeatureText: 3, FeatureImage: 3, FeatureMultimodal: 3},
		Interactions: []Interaction{
			{UserKey: "kuairec:user:1", ItemKey: "kuairec:item:1", OccurredAt: base, WatchRatio: &positive, SourceOrder: 1},
			{UserKey: "kuairec:user:1", ItemKey: "kuairec:item:2", OccurredAt: base.Add(time.Second), WatchRatio: &negative, SourceOrder: 2},
			{UserKey: "kuairec:user:1", ItemKey: "kuairec:item:3", OccurredAt: base.Add(2 * time.Second), WatchRatio: &neutral, SourceOrder: 3},
			{UserKey: "kuairec:user:1", ItemKey: "kuairec:item:4", OccurredAt: base.Add(3 * time.Second), WatchRatio: &positive, SourceOrder: 4},
		},
	}
}
