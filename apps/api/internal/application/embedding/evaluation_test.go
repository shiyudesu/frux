package applicationembedding

import (
	"math"
	"testing"
)

func TestEvaluateMultimodalGoldenSetIsDeterministic(t *testing.T) {
	golden := MultimodalGoldenSet{
		Version: MultimodalGoldenSetVersionV1, ModelContract: "fixture-contract-v1",
		MergeVersion: "hybrid-rank-v1", Cutoff: 3,
		Cases: []MultimodalGoldenCase{
			{
				QueryID: "q-1", Relevant: map[int64]int{1: 3, 2: 1},
				Baselines: map[string]MultimodalGoldenRanking{
					"lexical":    {RankedVideoIDs: []int64{2, 9, 1}, LatencyMS: 2},
					"image":      {RankedVideoIDs: []int64{1, 8, 7}, LatencyMS: 4},
					"multimodal": {RankedVideoIDs: []int64{1, 2, 9}, LatencyMS: 6},
				},
			},
			{
				QueryID: "q-2", Relevant: map[int64]int{3: 3},
				Baselines: map[string]MultimodalGoldenRanking{
					"lexical":    {RankedVideoIDs: []int64{9, 3}, LatencyMS: 3},
					"multimodal": {RankedVideoIDs: []int64{3, 9}, LatencyMS: 8},
				},
			},
		},
	}
	first, err := EvaluateMultimodalGoldenSet(golden)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EvaluateMultimodalGoldenSet(golden)
	if err != nil {
		t.Fatal(err)
	}
	if first.QueryCount != 2 || first.RelevantCount != 3 ||
		first.Baselines["multimodal"].RecallAtK != 1 ||
		first.Baselines["multimodal"].MRR != 1 ||
		first.Baselines["multimodal"].LatencyP95MS != 8 ||
		math.Abs(first.Baselines["lexical"].MRR-0.75) > 1e-9 ||
		first.Baselines["multimodal"] != second.Baselines["multimodal"] {
		t.Fatalf("unexpected report: %#v %#v", first, second)
	}
}

func TestEvaluateMultimodalGoldenSetRejectsInvalidInputs(t *testing.T) {
	if _, err := EvaluateMultimodalGoldenSet(MultimodalGoldenSet{}); err != ErrInvalidMultimodalGoldenSet {
		t.Fatalf("empty golden set error=%v", err)
	}
	invalid := MultimodalGoldenSet{
		Version: MultimodalGoldenSetVersionV1, ModelContract: "contract", MergeVersion: "merge", Cutoff: 1,
		Cases: []MultimodalGoldenCase{{
			QueryID: "q", Relevant: map[int64]int{1: 4},
			Baselines: map[string]MultimodalGoldenRanking{"lexical": {RankedVideoIDs: []int64{1}}},
		}},
	}
	if _, err := EvaluateMultimodalGoldenSet(invalid); err != ErrInvalidMultimodalGoldenSet {
		t.Fatalf("invalid grade error=%v", err)
	}
}
