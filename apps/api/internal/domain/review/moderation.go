package domainreview

import "strings"

const (
	MachineSourceProductionProvider = "production_provider"
	MachineSourceTestSeed           = "test_seed"
	MachineSourceRecovery           = "recovery"
	MachineSourceLegacyUnknown      = "legacy_unknown"

	ModerationModeDisabled    = "disabled"
	ModerationModeObserve     = "observe"
	ModerationModeApproveOnly = "approve_only"
	ModerationModeEnforce     = "enforce"
)

func ValidMachineSourceKind(source string) bool {
	switch normalizeToken(source) {
	case MachineSourceProductionProvider, MachineSourceTestSeed,
		MachineSourceRecovery, MachineSourceLegacyUnknown:
		return true
	default:
		return false
	}
}

func ValidModerationMode(mode string) bool {
	switch normalizeToken(mode) {
	case ModerationModeDisabled, ModerationModeObserve,
		ModerationModeApproveOnly, ModerationModeEnforce:
		return true
	default:
		return false
	}
}

func RestrictAutomatedOutcome(
	mode string,
	policyOutcome string,
	priority int,
) (string, int, error) {
	mode = normalizeToken(mode)
	policyOutcome = normalizeToken(policyOutcome)
	if !ValidModerationMode(mode) {
		return "", 0, ErrInvalidModerationMode
	}
	if !ValidOutcome(policyOutcome) {
		return "", 0, ErrInvalidDecisionOutcome
	}
	switch mode {
	case ModerationModeEnforce:
		return policyOutcome, priority, nil
	case ModerationModeApproveOnly:
		if policyOutcome == OutcomeApprove {
			return OutcomeApprove, priority, nil
		}
	case ModerationModeObserve, ModerationModeDisabled:
	}
	if priority < 1 {
		priority = 1
	}
	return OutcomeHuman, priority, nil
}

func ValidModerationProfileVersion(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxModerationProfileVersionLength {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func ValidProviderIdentifier(value string) bool {
	return validModerationIdentifier(value, MaxProviderLength, false)
}

func ValidModelVersion(value string) bool {
	return validModerationIdentifier(value, MaxModelVersionLength, true)
}

func validModerationIdentifier(value string, maxLength int, allowSlash bool) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxLength {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '_' || char == '-' || char == ':' || char == '@' ||
			char == '+' || (allowSlash && char == '/') {
			continue
		}
		return false
	}
	return true
}
