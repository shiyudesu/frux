package domainreview

import (
	"math"
	"sort"
	"time"
)

const (
	LabelSexualContent   = "sexual_content"
	LabelGraphicViolence = "graphic_violence"
	LabelHate            = "hate"
	LabelHarassment      = "harassment"
	LabelSelfHarm        = "self_harm"
	LabelIllegalActivity = "illegal_activity"
	LabelSpam            = "spam"
	LabelSafe            = "safe"
)

type LabelRule struct {
	Label           string   `json:"label"`
	HumanThreshold  *float64 `json:"human_threshold,omitempty"`
	RejectThreshold *float64 `json:"reject_threshold,omitempty"`
}

type PolicyConfiguration struct {
	Rules          []LabelRule `json:"rules"`
	DefaultOutcome string      `json:"default_outcome"`
}

type Policy struct {
	ID        int64
	Version   int
	Enabled   bool
	Config    PolicyConfiguration
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewPolicy(version int, enabled bool, config PolicyConfiguration, now time.Time) (*Policy, error) {
	if version <= 0 {
		return nil, ErrInvalidPolicyVersion
	}
	if now.IsZero() {
		return nil, ErrInvalidPolicy
	}
	normalized, err := normalizePolicy(config)
	if err != nil {
		return nil, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	return &Policy{Version: version, Enabled: enabled, Config: normalized, CreatedAt: now, UpdatedAt: now}, nil
}

func RestorePolicy(id int64, version int, enabled bool, config PolicyConfiguration, createdAt, updatedAt time.Time) *Policy {
	normalized, err := normalizePolicy(config)
	if err != nil {
		return nil
	}
	return &Policy{ID: id, Version: version, Enabled: enabled, Config: normalized, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC()}
}

func InitialPolicy(now time.Time) (*Policy, error) {
	return NewPolicy(1, true, PolicyConfiguration{
		DefaultOutcome: OutcomeHuman,
		Rules: []LabelRule{
			{Label: LabelSexualContent, HumanThreshold: floatPtr(0)},
			{Label: LabelGraphicViolence, HumanThreshold: floatPtr(0)},
			{Label: LabelHate, HumanThreshold: floatPtr(0)},
			{Label: LabelHarassment, HumanThreshold: floatPtr(0)},
			{Label: LabelSelfHarm, HumanThreshold: floatPtr(0)},
			{Label: LabelIllegalActivity, HumanThreshold: floatPtr(0)},
			{Label: LabelSpam, HumanThreshold: floatPtr(0)},
			{Label: LabelSafe},
		},
	}, now)
}

func (p *Policy) Route(signals []MachineSignal) (string, error) {
	outcome, _, err := p.RouteWithPriority(signals)
	return outcome, err
}

func (p *Policy) RouteWithPriority(signals []MachineSignal) (string, int, error) {
	if p == nil {
		return "", 0, ErrInvalidPolicy
	}
	config, err := normalizePolicy(p.Config)
	if err != nil {
		return "", 0, err
	}
	rules := make(map[string]LabelRule, len(config.Rules))
	for _, rule := range config.Rules {
		rules[rule.Label] = rule
	}
	human := false
	priority := 0
	for _, signal := range signals {
		rule, known := rules[signal.Label]
		if !known || !KnownLabel(signal.Label) {
			human = true
			priority = max(priority, humanSignalPriority(signal.Confidence))
			continue
		}
		if rule.RejectThreshold != nil && signal.Confidence >= *rule.RejectThreshold {
			return OutcomeReject, 0, nil
		}
		if rule.HumanThreshold != nil && signal.Confidence >= *rule.HumanThreshold {
			human = true
			priority = max(priority, humanSignalPriority(signal.Confidence))
		}
	}
	if human {
		return OutcomeHuman, max(priority, 1), nil
	}
	if config.DefaultOutcome == OutcomeHuman {
		return OutcomeHuman, 1, nil
	}
	return config.DefaultOutcome, 0, nil
}

func KnownLabel(label string) bool {
	switch NormalizeLabel(label) {
	case LabelSexualContent, LabelGraphicViolence, LabelHate, LabelHarassment,
		LabelSelfHarm, LabelIllegalActivity, LabelSpam, LabelSafe:
		return true
	default:
		return false
	}
}

func normalizePolicy(config PolicyConfiguration) (PolicyConfiguration, error) {
	defaultOutcome := normalizeToken(config.DefaultOutcome)
	if !ValidOutcome(defaultOutcome) {
		return PolicyConfiguration{}, ErrInvalidDecisionOutcome
	}
	seen := make(map[string]struct{}, len(config.Rules))
	rules := make([]LabelRule, 0, len(config.Rules))
	for _, rule := range config.Rules {
		label := NormalizeLabel(rule.Label)
		if !KnownLabel(label) {
			return PolicyConfiguration{}, ErrUnknownPolicyLabel
		}
		if _, exists := seen[label]; exists {
			return PolicyConfiguration{}, ErrDuplicatePolicyLabel
		}
		seen[label] = struct{}{}
		if !validThreshold(rule.HumanThreshold) || !validThreshold(rule.RejectThreshold) {
			return PolicyConfiguration{}, ErrInvalidPolicyThreshold
		}
		if rule.HumanThreshold != nil && rule.RejectThreshold != nil && *rule.HumanThreshold > *rule.RejectThreshold {
			return PolicyConfiguration{}, ErrInvalidPolicyThreshold
		}
		rules = append(rules, LabelRule{
			Label: label, HumanThreshold: cloneFloat(rule.HumanThreshold), RejectThreshold: cloneFloat(rule.RejectThreshold),
		})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Label < rules[j].Label })
	return PolicyConfiguration{Rules: rules, DefaultOutcome: defaultOutcome}, nil
}

func validThreshold(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 1)
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func humanSignalPriority(confidence float64) int {
	return min(100, max(1, int(math.Ceil(confidence*100))))
}

func floatPtr(value float64) *float64 { return &value }
