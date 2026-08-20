package applicationrecommendation

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

func quotaCandidate(id int64, provider string, score float64, publishedAt time.Time) *domainrecommendation.Candidate {
	return annotateCandidate(recallCandidate(id, id, 0, publishedAt), provider, score)
}

func quotaConfig(limit int, order []string, reservations map[string]int) domainrecommendation.PolicyConfiguration {
	return domainrecommendation.PolicyConfiguration{
		PreRankPoolLimit:           limit,
		RecallProviderOrder:        append([]string(nil), order...),
		RecallProviderReservations: reservations,
	}
}

func TestNormalizeProviderCandidatesIsStableBoundedAndDefensive(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	provider := domainrecommendation.RecallProviderFresh
	bestDuplicate := quotaCandidate(1, provider, 3, now.Add(-time.Hour))
	bestDuplicate.RecallReasons = append(bestDuplicate.RecallReasons, domainrecommendation.RecallReason{Provider: domainrecommendation.RecallProviderHot, Score: 99})
	bestDuplicate.SourceScores[domainrecommendation.RecallProviderHot] = 99
	input := []*domainrecommendation.Candidate{
		quotaCandidate(2, provider, 2, now),
		quotaCandidate(1, provider, 1, now.Add(-time.Minute)),
		nil,
		bestDuplicate,
		quotaCandidate(3, provider, math.NaN(), now),
		recallCandidate(4, 4, 0, now),
	}
	got := normalizeProviderCandidates(provider, input, 2)
	if ids := candidateIDs(got); !reflect.DeepEqual(ids, []int64{1, 2}) {
		t.Fatalf("normalized IDs = %v, want [1 2]", ids)
	}
	if got[0] == bestDuplicate || len(got[0].RecallReasons) != 1 ||
		got[0].RecallReasons[0].Provider != provider || len(got[0].SourceScores) != 1 ||
		got[0].SourceScores[provider] != 3 {
		t.Fatalf("provider evidence was not canonical and defensive: %#v", got[0])
	}
	reversed := []*domainrecommendation.Candidate{input[5], input[4], input[3], input[2], input[1], input[0]}
	if other := normalizeProviderCandidates(provider, reversed, 2); !reflect.DeepEqual(candidateIDs(other), candidateIDs(got)) {
		t.Fatalf("raw input order changed normalization: %v != %v", candidateIDs(other), candidateIDs(got))
	}
	bestDuplicate.SourceScores[provider] = 0
	if got[0].SourceScores[provider] != 3 {
		t.Fatal("normalized candidate aliased input")
	}
}

func TestNormalizeProviderCandidatesUsesDeterministicTies(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	provider := domainrecommendation.RecallProviderFresh
	input := []*domainrecommendation.Candidate{
		quotaCandidate(1, provider, 2, now.Add(-time.Minute)),
		quotaCandidate(2, provider, 2, now),
		quotaCandidate(3, provider, 2, now),
		quotaCandidate(4, provider, 3, now.Add(-time.Hour)),
	}
	if got := candidateIDs(normalizeProviderCandidates(provider, input, 3)); !reflect.DeepEqual(got, []int64{4, 3, 2}) {
		t.Fatalf("tie order = %v, want [4 3 2]", got)
	}
}

func TestQuotaMergeReservationsOverlapAndFill(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	config := quotaConfig(50, []string{fresh, hot}, map[string]int{fresh: 2, hot: 2})
	providers := map[string][]*domainrecommendation.Candidate{
		fresh: {
			quotaCandidate(1, fresh, 3, now),
			quotaCandidate(2, fresh, 2, now.Add(-time.Minute)),
			quotaCandidate(3, fresh, 1, now.Add(-2*time.Minute)),
		},
		hot: {
			quotaCandidate(1, hot, 3, now),
			quotaCandidate(4, hot, 2, now.Add(-time.Minute)),
			quotaCandidate(5, hot, 1, now.Add(-2*time.Minute)),
		},
	}
	result, err := mixQuotaCandidates(config, providers)
	if err != nil {
		t.Fatal(err)
	}
	if ids := candidateIDs(result.Candidates); !reflect.DeepEqual(ids, []int64{1, 2, 4, 3, 5}) {
		t.Fatalf("mixed IDs = %v, want [1 2 4 3 5]", ids)
	}
	if result.Providers[fresh].Represented != 3 || result.Providers[hot].Represented != 3 ||
		result.Providers[fresh].Underfill != 0 || result.Providers[hot].Underfill != 0 ||
		result.Overlap != 1 {
		t.Fatalf("unexpected quota stats: %#v overlap=%d", result.Providers, result.Overlap)
	}
	if len(result.Candidates[0].RecallReasons) != 2 {
		t.Fatalf("overlap reasons were lost: %#v", result.Candidates[0].RecallReasons)
	}
}

func TestQuotaMergeUnderfillReturnsCapacityToRoundRobin(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	config := quotaConfig(50, []string{fresh, hot}, map[string]int{fresh: 3, hot: 0})
	providers := map[string][]*domainrecommendation.Candidate{
		fresh: {quotaCandidate(1, fresh, 1, now)},
		hot: {
			quotaCandidate(2, hot, 5, now),
			quotaCandidate(3, hot, 4, now),
			quotaCandidate(4, hot, 3, now),
			quotaCandidate(5, hot, 2, now),
		},
	}
	result, err := mixQuotaCandidates(config, providers)
	if err != nil {
		t.Fatal(err)
	}
	if ids := candidateIDs(result.Candidates); !reflect.DeepEqual(ids, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("underfill did not release fill capacity: %v", ids)
	}
	if result.Providers[fresh].Underfill != 2 || !result.Providers[fresh].Exhausted ||
		result.Providers[hot].FillSelected != 4 {
		t.Fatalf("unexpected underfill stats: %#v", result.Providers)
	}
}

func TestQuotaMergeProviderOrderControlsDeterministicFill(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	makeCandidates := func(start int64, provider string) []*domainrecommendation.Candidate {
		output := make([]*domainrecommendation.Candidate, 0, 60)
		for index := range 60 {
			output = append(output, quotaCandidate(start+int64(index), provider, float64(60-index), now.Add(-time.Duration(index)*time.Second)))
		}
		return output
	}
	providers := map[string][]*domainrecommendation.Candidate{
		fresh: makeCandidates(1, fresh),
		hot:   makeCandidates(1001, hot),
	}
	first, err := mixQuotaCandidates(quotaConfig(50, []string{fresh, hot}, map[string]int{fresh: 0, hot: 0}), providers)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mixQuotaCandidates(quotaConfig(50, []string{hot, fresh}, map[string]int{fresh: 0, hot: 0}), providers)
	if err != nil {
		t.Fatal(err)
	}
	if first.Candidates[0].VideoID != 1 || second.Candidates[0].VideoID != 1001 ||
		len(first.Candidates) != 50 || len(second.Candidates) != 50 {
		t.Fatalf("provider order was not explicit: first=%v second=%v", candidateIDs(first.Candidates[:2]), candidateIDs(second.Candidates[:2]))
	}
	for index := 0; index < 50; index += 2 {
		if first.Candidates[index].VideoID >= 1000 || first.Candidates[index+1].VideoID < 1000 {
			t.Fatalf("round-robin order diverged at %d: %v", index, candidateIDs(first.Candidates[index:index+2]))
		}
	}
}

func TestQuotaMergeDuplicateTurnsStillFillExactPool(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	freshCandidates := make([]*domainrecommendation.Candidate, 0, 50)
	hotCandidates := make([]*domainrecommendation.Candidate, 0, 50)
	for id := int64(1); id <= 50; id++ {
		freshCandidates = append(freshCandidates, quotaCandidate(id, fresh, float64(51-id), now.Add(-time.Duration(id)*time.Second)))
		hotID := id
		if id > 25 {
			hotID = id + 50
		}
		hotCandidates = append(hotCandidates, quotaCandidate(hotID, hot, float64(51-id), now.Add(-time.Duration(id)*time.Second)))
	}
	result, err := mixQuotaCandidates(
		quotaConfig(50, []string{fresh, hot}, map[string]int{fresh: 20, hot: 20}),
		map[string][]*domainrecommendation.Candidate{fresh: freshCandidates, hot: hotCandidates},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 50 {
		t.Fatalf("mixed pool = %d, want 50", len(result.Candidates))
	}
	seen := make(map[int64]struct{}, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if _, duplicate := seen[candidate.VideoID]; duplicate {
			t.Fatalf("duplicate video %d consumed a second slot", candidate.VideoID)
		}
		seen[candidate.VideoID] = struct{}{}
	}
	if result.Providers[fresh].Represented < 20 || result.Providers[hot].Represented < 20 || result.Overlap == 0 {
		t.Fatalf("reservations or overlap lost: providers=%#v overlap=%d", result.Providers, result.Overlap)
	}
}

func TestQuotaMergeEmptyProviderUnderfillsAndSingleRemainderFills(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	hotCandidates := make([]*domainrecommendation.Candidate, 0, 60)
	for id := int64(1); id <= 60; id++ {
		hotCandidates = append(hotCandidates, quotaCandidate(id, hot, float64(61-id), now.Add(-time.Duration(id)*time.Second)))
	}
	result, err := mixQuotaCandidates(
		quotaConfig(50, []string{fresh, hot}, map[string]int{fresh: 10, hot: 10}),
		map[string][]*domainrecommendation.Candidate{fresh: {}, hot: hotCandidates},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 50 || result.Providers[fresh].Underfill != 10 ||
		!result.Providers[fresh].Exhausted || result.Providers[hot].Represented != 50 {
		t.Fatalf("single-provider remainder was not reallocated: candidates=%d providers=%#v", len(result.Candidates), result.Providers)
	}
}

func TestQuotaMergeRejectsInvalidContract(t *testing.T) {
	fresh := domainrecommendation.RecallProviderFresh
	hot := domainrecommendation.RecallProviderHot
	tests := []struct {
		name   string
		config domainrecommendation.PolicyConfiguration
	}{
		{name: "pool below bound", config: quotaConfig(49, []string{fresh}, map[string]int{fresh: 0})},
		{name: "pool above bound", config: quotaConfig(501, []string{fresh}, map[string]int{fresh: 0})},
		{name: "duplicate provider", config: quotaConfig(50, []string{fresh, fresh}, map[string]int{fresh: 0, hot: 0})},
		{name: "missing reservation", config: quotaConfig(50, []string{fresh, hot}, map[string]int{fresh: 0, "unknown": 0})},
		{name: "negative reservation", config: quotaConfig(50, []string{fresh}, map[string]int{fresh: -1})},
		{name: "reservation sum", config: quotaConfig(50, []string{fresh, hot}, map[string]int{fresh: 30, hot: 30})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mixQuotaCandidates(test.config, nil); !errors.Is(err, errInvalidQuotaMerge) {
				t.Fatalf("error = %v, want %v", err, errInvalidQuotaMerge)
			}
		})
	}
}
