package domainembedding

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func testMultimodalContract(t testing.TB, dimension int) MultimodalContractIdentity {
	t.Helper()
	contract, err := NewMultimodalContractIdentity(
		" Provider-A ",
		" Model-A ",
		" Revision-1 ",
		dimension,
		MultimodalTextCanonicalizerV1,
		MultimodalFrameSamplingPolicyV1,
		MultimodalImagePreprocessingV1,
		MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func TestMultimodalContractIdentityIsCanonicalAndIsolated(t *testing.T) {
	contract := testMultimodalContract(t, MinMultimodalDimension)
	if contract.ProviderAlias != "provider-a" || contract.ModelAlias != "model-a" ||
		contract.RevisionAlias != "revision-1" || contract.Key() == "" {
		t.Fatalf("contract was not canonical: %#v", contract)
	}
	same := testMultimodalContract(t, MinMultimodalDimension)
	if !contract.Equal(same) || contract.Canonical() != same.Canonical() || contract.Key() != same.Key() {
		t.Fatal("equivalent contracts did not share identity")
	}
	different := testMultimodalContract(t, MinMultimodalDimension+1)
	if contract.Equal(different) || contract.Key() == different.Key() {
		t.Fatal("different dimensions shared a contract identity")
	}
	if _, err := NewMultimodalContractIdentity(
		"provider a", "model", "revision", MinMultimodalDimension,
		MultimodalTextCanonicalizerV1, MultimodalFrameSamplingPolicyV1,
		MultimodalImagePreprocessingV1, MultimodalFusionPolicyV1,
	); MultimodalFailureCode(err) != MultimodalValidationInvalidContract {
		t.Fatalf("invalid alias error = %v", err)
	}
}

func TestValidateMultimodalVectorRequiresExactIdentityAndDefensiveValues(t *testing.T) {
	contract := testMultimodalContract(t, MinMultimodalDimension)
	values := make([]float64, contract.Dimension)
	values[0] = 1
	sourceHash := MultimodalSourceHash([]byte("public text"), []byte("frame digest"))
	identity := MultimodalVectorIdentity{
		Contract: contract, SourceHash: sourceHash, VectorDigest: MultimodalVectorDigest(values),
	}
	validated, err := ValidateMultimodalVector(contract, sourceHash, identity, values)
	if err != nil {
		t.Fatal(err)
	}
	values[0] = 0
	if validated.Values[0] != 1 || validated.Identity != identity {
		t.Fatalf("validated vector aliased input or changed identity: %#v", validated)
	}

	tests := []struct {
		name       string
		contract   MultimodalContractIdentity
		sourceHash string
		identity   MultimodalVectorIdentity
		values     []float64
		want       MultimodalValidationCode
	}{
		{name: "contract", contract: contract, sourceHash: sourceHash, identity: func() MultimodalVectorIdentity {
			changed := identity
			changed.Contract = testMultimodalContract(t, contract.Dimension+1)
			return changed
		}(), values: validated.Values, want: MultimodalValidationContractMismatch},
		{name: "source hash", contract: contract, sourceHash: MultimodalSourceHash([]byte("other")), identity: identity, values: validated.Values, want: MultimodalValidationInputHash},
		{name: "dimension", contract: contract, sourceHash: sourceHash, identity: identity, values: validated.Values[:contract.Dimension-1], want: MultimodalValidationDimension},
		{name: "non finite", contract: contract, sourceHash: sourceHash, identity: identity, values: func() []float64 {
			changed := append([]float64(nil), validated.Values...)
			changed[1] = math.NaN()
			return changed
		}(), want: MultimodalValidationNonFinite},
		{name: "norm", contract: contract, sourceHash: sourceHash, identity: identity, values: make([]float64, contract.Dimension), want: MultimodalValidationNorm},
		{name: "digest", contract: contract, sourceHash: sourceHash, identity: func() MultimodalVectorIdentity {
			changed := identity
			changed.VectorDigest = strings.Repeat("0", MultimodalDigestHexLength)
			return changed
		}(), values: validated.Values, want: MultimodalValidationDigest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateMultimodalVector(test.contract, test.sourceHash, test.identity, test.values)
			if MultimodalFailureCode(err) != test.want {
				t.Fatalf("error = %v code=%q, want %q", err, MultimodalFailureCode(err), test.want)
			}
		})
	}
}

func TestMultimodalHashesAreLengthDelimitedAndDeterministic(t *testing.T) {
	first := MultimodalSourceHash([]byte("ab"), []byte("c"))
	second := MultimodalSourceHash([]byte("a"), []byte("bc"))
	if first == second || first != MultimodalSourceHash([]byte("ab"), []byte("c")) {
		t.Fatalf("source hashes were ambiguous or unstable: %q %q", first, second)
	}
	if MultimodalVectorDigest([]float64{1, 0}) == MultimodalVectorDigest([]float64{0, 1}) {
		t.Fatal("different vectors shared a digest")
	}
}

func TestCanonicalizeMultimodalPublicTextAndQuery(t *testing.T) {
	text, err := CanonicalizePublicVideoText("  Ｆｒｕｘ\t视频 ", " 第一行\n第二行 ", 64)
	if err != nil || text != "Frux 视频\n第一行 第二行" {
		t.Fatalf("canonical video text = %q err=%v", text, err)
	}
	query, err := CanonicalizePublicQuery("  猫咪\n 视频  ", 16)
	if err != nil || query != "猫咪 视频" {
		t.Fatalf("canonical query = %q err=%v", query, err)
	}
	for _, invalid := range []func() error{
		func() error { _, err := CanonicalizePublicVideoText("", "", 64); return err },
		func() error { _, err := CanonicalizePublicVideoText("bad\x00", "", 64); return err },
		func() error { _, err := CanonicalizePublicQuery("", 16); return err },
		func() error { _, err := CanonicalizePublicQuery("12345", 4); return err },
	} {
		if err := invalid(); MultimodalFailureCode(err) != MultimodalValidationInvalidText {
			t.Fatalf("invalid text error = %v", err)
		}
	}
}

func TestMultimodalValidationErrorSupportsErrorsAs(t *testing.T) {
	err := validationError(MultimodalValidationDigest)
	var validation *MultimodalValidationError
	if !errors.As(err, &validation) || !reflect.DeepEqual(validation.Code, MultimodalValidationDigest) {
		t.Fatalf("validation error was not typed: %v", err)
	}
}
