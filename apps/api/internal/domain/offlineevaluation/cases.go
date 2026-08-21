package domainofflineevaluation

import (
	"errors"
	"sort"
	"time"
)

var ErrInvalidEvaluationDataset = errors.New("invalid offline evaluation dataset")

type FeedbackClass string

const (
	FeedbackPositive  FeedbackClass = "positive"
	FeedbackQuickSkip FeedbackClass = "quick_skip"
	FeedbackNeutral   FeedbackClass = "neutral"
	FeedbackMissing   FeedbackClass = "missing"
)

type CaseProfile struct {
	Version           string
	PositiveThreshold float64
	QuickSkipMaximum  float64
	MinimumHistory    int
	SessionLimit      int
}

func DefaultCaseProfile() CaseProfile {
	return CaseProfile{
		Version: SessionProfileV1, PositiveThreshold: 0.8, QuickSkipMaximum: 0.2,
		MinimumHistory: 3, SessionLimit: 20,
	}
}

func (p CaseProfile) Valid() bool {
	return p.Version == SessionProfileV1 && p.PositiveThreshold > p.QuickSkipMaximum &&
		p.PositiveThreshold <= 100 && p.QuickSkipMaximum >= 0 &&
		p.MinimumHistory >= 1 && p.MinimumHistory <= 1000 &&
		p.SessionLimit >= 1 && p.SessionLimit <= 1000
}

func (p CaseProfile) Classify(ratio *float64) FeedbackClass {
	if ratio == nil {
		return FeedbackMissing
	}
	if *ratio >= p.PositiveThreshold {
		return FeedbackPositive
	}
	if *ratio <= p.QuickSkipMaximum {
		return FeedbackQuickSkip
	}
	return FeedbackNeutral
}

type EvaluationCase struct {
	Ordinal       int
	UserKey       string
	TargetItemKey string
	Cutoff        time.Time
	History       []Interaction
	Session       []Interaction
	CandidateKeys []string
}

type CaseBuildResult struct {
	Cases      []EvaluationCase
	Exclusions map[ExclusionCode]int
	Users      int
	Neutral    int
	Missing    int
}

func BuildCases(dataset *Dataset, profile CaseProfile, maxCases int) (CaseBuildResult, error) {
	result := CaseBuildResult{Exclusions: make(map[ExclusionCode]int)}
	if dataset == nil || !ValidDatasetKind(dataset.Kind) || len(dataset.Items) == 0 ||
		len(dataset.Interactions) == 0 || !profile.Valid() || maxCases < 1 || maxCases > 1_000_000 {
		return result, ErrInvalidEvaluationDataset
	}
	byUser := make(map[string][]Interaction)
	for _, interaction := range dataset.Interactions {
		if interaction.UserKey == "" || interaction.ItemKey == "" || interaction.OccurredAt.IsZero() {
			return result, ErrInvalidEvaluationDataset
		}
		if _, exists := dataset.Items[interaction.ItemKey]; !exists {
			return result, ErrInvalidEvaluationDataset
		}
		byUser[interaction.UserKey] = append(byUser[interaction.UserKey], interaction)
		switch profile.Classify(interaction.WatchRatio) {
		case FeedbackNeutral:
			result.Neutral++
		case FeedbackMissing:
			result.Missing++
		}
	}
	users := make([]string, 0, len(byUser))
	for user := range byUser {
		users = append(users, user)
	}
	sort.Strings(users)
	result.Users = len(users)
	itemKeys := make([]string, 0, len(dataset.Items))
	for itemKey := range dataset.Items {
		itemKeys = append(itemKeys, itemKey)
	}
	sort.Strings(itemKeys)
	for _, user := range users {
		interactions := byUser[user]
		sort.SliceStable(interactions, func(i, j int) bool {
			if !interactions[i].OccurredAt.Equal(interactions[j].OccurredAt) {
				return interactions[i].OccurredAt.Before(interactions[j].OccurredAt)
			}
			if interactions[i].SourceOrder != interactions[j].SourceOrder {
				return interactions[i].SourceOrder < interactions[j].SourceOrder
			}
			return interactions[i].ItemKey < interactions[j].ItemKey
		})
		targetIndex := -1
		for index := len(interactions) - 1; index >= 0; index-- {
			if profile.Classify(interactions[index].WatchRatio) == FeedbackPositive {
				targetIndex = index
				break
			}
		}
		if targetIndex < 0 || targetIndex < profile.MinimumHistory {
			result.Exclusions[ExclusionInsufficientHistory]++
			continue
		}
		history := append([]Interaction(nil), interactions[:targetIndex]...)
		prior := make(map[string]struct{}, len(history))
		for _, interaction := range history {
			prior[interaction.ItemKey] = struct{}{}
		}
		target := interactions[targetIndex]
		candidates := make([]string, 0, len(itemKeys)-len(prior))
		for _, itemKey := range itemKeys {
			if _, seen := prior[itemKey]; seen {
				continue
			}
			candidates = append(candidates, itemKey)
		}
		if !containsString(candidates, target.ItemKey) {
			result.Exclusions[ExclusionMissingTarget]++
			continue
		}
		sessionStart := max(0, len(history)-profile.SessionLimit)
		result.Cases = append(result.Cases, EvaluationCase{
			Ordinal: len(result.Cases) + 1, UserKey: user, TargetItemKey: target.ItemKey,
			Cutoff: target.OccurredAt.UTC(), History: history,
			Session: append([]Interaction(nil), history[sessionStart:]...), CandidateKeys: candidates,
		})
		if len(result.Cases) > maxCases {
			return CaseBuildResult{}, ErrInvalidEvaluationDataset
		}
	}
	return result, nil
}

func containsString(values []string, target string) bool {
	index := sort.SearchStrings(values, target)
	return index < len(values) && values[index] == target
}
