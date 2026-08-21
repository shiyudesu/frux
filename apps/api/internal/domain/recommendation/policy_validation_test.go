package domainrecommendation

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestValidatePolicyConfigurationMatchesNewPolicyAndReturnsDeepClone(t *testing.T) {
	config := InitialRecommendationPolicyConfiguration()
	normalized, err := ValidatePolicyConfiguration(config)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPolicy("recommend", 7, true, config, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized, policy.Config) {
		t.Fatalf("normalized=%#v policy=%#v", normalized, policy.Config)
	}
	config.FeatureWeights[FeatureHotness] = 99
	config.RecallBudgets[RecallProviderFresh] = 1
	if reflect.DeepEqual(normalized, config) || normalized.FeatureWeights[FeatureHotness] == 99 ||
		normalized.RecallBudgets[RecallProviderFresh] == 1 {
		t.Fatal("normalized configuration aliases caller maps")
	}
}

func TestValidatePolicyConfigurationRetainsProductionRejections(t *testing.T) {
	config := InitialRecommendationPolicyConfiguration()
	config.FeatureWeights["unknown"] = 1
	_, validationErr := ValidatePolicyConfiguration(config)
	_, policyErr := NewPolicy("recommend", 1, true, config, time.Unix(1, 0).UTC())
	if !errors.Is(validationErr, ErrUnknownPolicyFeature) || !errors.Is(policyErr, ErrUnknownPolicyFeature) {
		t.Fatalf("validation=%v policy=%v", validationErr, policyErr)
	}
}
