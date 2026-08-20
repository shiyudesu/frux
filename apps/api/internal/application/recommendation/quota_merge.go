package applicationrecommendation

import (
	"errors"
	"math"
	"sort"
	"strings"

	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
)

var errInvalidQuotaMerge = errors.New("invalid recommendation provider quota merge")

type quotaProviderStats struct {
	LocalUnique  int
	Readable     int
	Reserved     int
	FillSelected int
	Represented  int
	Underfill    int
	Exhausted    bool
}

type quotaMergeResult struct {
	Candidates []*domainrecommendation.Candidate
	Providers  map[string]quotaProviderStats
	Overlap    int
}

func normalizeProviderCandidates(provider string, candidates []*domainrecommendation.Candidate, budget int) []*domainrecommendation.Candidate {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || budget <= 0 || len(candidates) == 0 {
		return []*domainrecommendation.Candidate{}
	}
	unique := make(map[int64]*domainrecommendation.Candidate, minInt(len(candidates), budget))
	for _, candidate := range candidates {
		score, ok := providerCandidateScore(candidate, provider)
		if !ok || candidate.VideoID <= 0 || candidate.PublishedAt.IsZero() {
			continue
		}
		existing := unique[candidate.VideoID]
		if existing != nil {
			currentScore := existing.SourceScores[provider]
			if score < currentScore || (score == currentScore && !providerCandidateBefore(candidate, existing, score, currentScore)) {
				continue
			}
		}
		cloned := candidate.Clone()
		cloned.RecallReasons = []domainrecommendation.RecallReason{{Provider: provider, Score: score}}
		cloned.SourceScores = map[string]float64{provider: score}
		unique[cloned.VideoID] = cloned
	}
	output := make([]*domainrecommendation.Candidate, 0, len(unique))
	for _, candidate := range unique {
		output = append(output, candidate)
	}
	sort.SliceStable(output, func(i, j int) bool {
		leftScore := output[i].SourceScores[provider]
		rightScore := output[j].SourceScores[provider]
		return providerCandidateBefore(output[i], output[j], leftScore, rightScore)
	})
	if len(output) > budget {
		output = output[:budget]
	}
	return output
}

func providerCandidateScore(candidate *domainrecommendation.Candidate, provider string) (float64, bool) {
	if candidate == nil {
		return 0, false
	}
	score, found := candidate.SourceScores[provider]
	if !found {
		for _, reason := range candidate.RecallReasons {
			if strings.ToLower(strings.TrimSpace(reason.Provider)) != provider ||
				math.IsNaN(reason.Score) || math.IsInf(reason.Score, 0) {
				continue
			}
			if !found || reason.Score > score {
				score = reason.Score
				found = true
			}
		}
	}
	return score, found && !math.IsNaN(score) && !math.IsInf(score, 0)
}

func providerCandidateBefore(left, right *domainrecommendation.Candidate, leftScore, rightScore float64) bool {
	if leftScore != rightScore {
		return leftScore > rightScore
	}
	if !left.PublishedAt.Equal(right.PublishedAt) {
		return left.PublishedAt.After(right.PublishedAt)
	}
	return left.VideoID > right.VideoID
}

func mixQuotaCandidates(config domainrecommendation.PolicyConfiguration, providers map[string][]*domainrecommendation.Candidate) (*quotaMergeResult, error) {
	if config.PreRankPoolLimit < domainrecommendation.MinPolicyPreRankCandidates ||
		config.PreRankPoolLimit > domainrecommendation.MaxPolicyPreRankCandidates ||
		len(config.RecallProviderOrder) == 0 ||
		len(config.RecallProviderReservations) != len(config.RecallProviderOrder) {
		return nil, errInvalidQuotaMerge
	}

	order := append([]string(nil), config.RecallProviderOrder...)
	sequences := make(map[string][]*domainrecommendation.Candidate, len(order))
	merged := make(map[int64]*domainrecommendation.Candidate)
	stats := make(map[string]quotaProviderStats, len(order))
	selectedProviders := make(map[string]struct{}, len(order))
	reservationSum := 0
	for _, provider := range order {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			return nil, errInvalidQuotaMerge
		}
		if _, duplicate := selectedProviders[provider]; duplicate {
			return nil, errInvalidQuotaMerge
		}
		selectedProviders[provider] = struct{}{}
		reservation, ok := config.RecallProviderReservations[provider]
		if !ok || reservation < 0 {
			return nil, errInvalidQuotaMerge
		}
		reservationSum += reservation
		sequence := providers[provider]
		sequences[provider] = sequence
		stats[provider] = quotaProviderStats{LocalUnique: len(sequence), Readable: len(sequence)}
		for _, candidate := range sequence {
			mergeRecalledCandidate(merged, candidate)
		}
	}
	if reservationSum > config.PreRankPoolLimit {
		return nil, errInvalidQuotaMerge
	}
	for provider := range config.RecallProviderReservations {
		if _, selected := selectedProviders[provider]; !selected {
			return nil, errInvalidQuotaMerge
		}
	}

	result := &quotaMergeResult{Candidates: make([]*domainrecommendation.Candidate, 0, config.PreRankPoolLimit), Providers: stats}
	selected := make(map[int64]*domainrecommendation.Candidate, config.PreRankPoolLimit)
	represented := make(map[string]map[int64]struct{}, len(order))
	cursors := make(map[string]int, len(order))
	for _, provider := range order {
		represented[provider] = map[int64]struct{}{}
	}

	selectCandidate := func(provider string, candidate *domainrecommendation.Candidate, phase string) bool {
		if candidate == nil {
			return false
		}
		global := merged[candidate.VideoID]
		if global == nil {
			return false
		}
		if selected[global.VideoID] == nil {
			if len(result.Candidates) >= config.PreRankPoolLimit {
				return false
			}
			cloned := global.Clone()
			selected[global.VideoID] = cloned
			result.Candidates = append(result.Candidates, cloned)
			current := result.Providers[provider]
			if phase == "reservation" {
				current.Reserved++
			} else {
				current.FillSelected++
			}
			result.Providers[provider] = current
		}
		for _, reason := range global.RecallReasons {
			reasonProvider := strings.ToLower(strings.TrimSpace(reason.Provider))
			if _, selectedProvider := selectedProviders[reasonProvider]; selectedProvider {
				represented[reasonProvider][global.VideoID] = struct{}{}
			}
		}
		return true
	}

	for len(result.Candidates) < config.PreRankPoolLimit {
		needsReservation := false
		progressed := false
		for _, provider := range order {
			if len(represented[provider]) >= config.RecallProviderReservations[provider] {
				continue
			}
			needsReservation = true
			sequence := sequences[provider]
			if cursors[provider] >= len(sequence) {
				continue
			}
			candidate := sequence[cursors[provider]]
			cursors[provider]++
			progressed = true
			selectCandidate(provider, candidate, "reservation")
			if len(result.Candidates) >= config.PreRankPoolLimit {
				break
			}
		}
		if !needsReservation || !progressed {
			break
		}
	}

	for len(result.Candidates) < config.PreRankPoolLimit {
		progressed := false
		for _, provider := range order {
			sequence := sequences[provider]
			if cursors[provider] >= len(sequence) {
				continue
			}
			candidate := sequence[cursors[provider]]
			cursors[provider]++
			progressed = true
			selectCandidate(provider, candidate, "fill")
			if len(result.Candidates) >= config.PreRankPoolLimit {
				break
			}
		}
		if !progressed {
			break
		}
	}

	for _, provider := range order {
		current := result.Providers[provider]
		current.Represented = len(represented[provider])
		if reservation := config.RecallProviderReservations[provider]; current.Represented < reservation {
			current.Underfill = reservation - current.Represented
		}
		current.Exhausted = cursors[provider] >= len(sequences[provider])
		result.Providers[provider] = current
	}
	for _, candidate := range result.Candidates {
		representedProviders := 0
		for _, reason := range candidate.RecallReasons {
			if _, selectedProvider := selectedProviders[strings.ToLower(strings.TrimSpace(reason.Provider))]; selectedProvider {
				representedProviders++
			}
		}
		if representedProviders > 1 {
			result.Overlap++
		}
	}
	return result, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
