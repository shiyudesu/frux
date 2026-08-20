package domainembedding

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	unicodeNorm "golang.org/x/text/unicode/norm"
)

const (
	MultimodalTextCanonicalizerV1   = "public-content-v1"
	MultimodalFrameSamplingPolicyV1 = "representative-frames-v1"
	MultimodalImagePreprocessingV1  = "rgb-fit-v1"
	MultimodalFusionPolicyV1        = "provider-fusion-v1"
	MultimodalHybridMergeVersionV1  = "hybrid-rank-v1"
	MultimodalExactRankingVersionV1 = "exact-cosine-v1"
	MultimodalLexicalFallback       = "lexical"
	MinMultimodalDimension          = 32
	MaxMultimodalDimension          = 8192
	MultimodalVectorNormTolerance   = 0.001
	MultimodalDigestHexLength       = sha256.Size * 2
)

type MultimodalValidationCode string

const (
	MultimodalValidationInvalidContract  MultimodalValidationCode = "invalid_contract"
	MultimodalValidationMissingResult    MultimodalValidationCode = "missing_result"
	MultimodalValidationContractMismatch MultimodalValidationCode = "contract_mismatch"
	MultimodalValidationInputHash        MultimodalValidationCode = "input_hash_mismatch"
	MultimodalValidationDimension        MultimodalValidationCode = "dimension_mismatch"
	MultimodalValidationNonFinite        MultimodalValidationCode = "non_finite_component"
	MultimodalValidationNorm             MultimodalValidationCode = "norm_mismatch"
	MultimodalValidationDigest           MultimodalValidationCode = "digest_mismatch"
	MultimodalValidationInvalidText      MultimodalValidationCode = "invalid_text"
)

type MultimodalValidationError struct {
	Code MultimodalValidationCode
}

func (e *MultimodalValidationError) Error() string {
	if e == nil {
		return "invalid multimodal embedding"
	}
	return "invalid multimodal embedding: " + string(e.Code)
}

func MultimodalFailureCode(err error) MultimodalValidationCode {
	if err == nil {
		return ""
	}
	var validationError *MultimodalValidationError
	if errors.As(err, &validationError) {
		return validationError.Code
	}
	return MultimodalValidationInvalidContract
}

type MultimodalContractIdentity struct {
	ProviderAlias            string
	ModelAlias               string
	RevisionAlias            string
	Dimension                int
	TextCanonicalizer        string
	FrameSamplingPolicy      string
	ImagePreprocessingPolicy string
	FusionPolicy             string
}

func NewMultimodalContractIdentity(
	providerAlias string,
	modelAlias string,
	revisionAlias string,
	dimension int,
	textCanonicalizer string,
	frameSamplingPolicy string,
	imagePreprocessingPolicy string,
	fusionPolicy string,
) (MultimodalContractIdentity, error) {
	identity := MultimodalContractIdentity{
		ProviderAlias:            normalizeMultimodalToken(providerAlias),
		ModelAlias:               normalizeMultimodalToken(modelAlias),
		RevisionAlias:            normalizeMultimodalToken(revisionAlias),
		Dimension:                dimension,
		TextCanonicalizer:        normalizeMultimodalToken(textCanonicalizer),
		FrameSamplingPolicy:      normalizeMultimodalToken(frameSamplingPolicy),
		ImagePreprocessingPolicy: normalizeMultimodalToken(imagePreprocessingPolicy),
		FusionPolicy:             normalizeMultimodalToken(fusionPolicy),
	}
	if !validMultimodalToken(identity.ProviderAlias) ||
		!validMultimodalToken(identity.ModelAlias) ||
		!validMultimodalToken(identity.RevisionAlias) ||
		!validMultimodalToken(identity.TextCanonicalizer) ||
		!validMultimodalToken(identity.FrameSamplingPolicy) ||
		!validMultimodalToken(identity.ImagePreprocessingPolicy) ||
		!validMultimodalToken(identity.FusionPolicy) ||
		dimension < MinMultimodalDimension || dimension > MaxMultimodalDimension {
		return MultimodalContractIdentity{}, validationError(MultimodalValidationInvalidContract)
	}
	return identity, nil
}

func (i MultimodalContractIdentity) Equal(other MultimodalContractIdentity) bool {
	return i == other
}

func (i MultimodalContractIdentity) Canonical() string {
	values := []string{
		i.ProviderAlias,
		i.ModelAlias,
		i.RevisionAlias,
		strconv.Itoa(i.Dimension),
		i.TextCanonicalizer,
		i.FrameSamplingPolicy,
		i.ImagePreprocessingPolicy,
		i.FusionPolicy,
	}
	var builder strings.Builder
	for _, value := range values {
		builder.WriteString(strconv.Itoa(len(value)))
		builder.WriteByte(':')
		builder.WriteString(value)
	}
	return builder.String()
}

func (i MultimodalContractIdentity) Key() string {
	sum := sha256.Sum256([]byte(i.Canonical()))
	return hex.EncodeToString(sum[:])
}

type MultimodalVectorIdentity struct {
	Contract     MultimodalContractIdentity
	SourceHash   string
	VectorDigest string
}

type MultimodalVector struct {
	Identity MultimodalVectorIdentity
	Values   []float64
}

type MultimodalQueryVector struct {
	Contract MultimodalContractIdentity
	Values   []float64
}

func (v *MultimodalQueryVector) Clone() *MultimodalQueryVector {
	if v == nil {
		return nil
	}
	cloned := *v
	cloned.Values = append([]float64(nil), v.Values...)
	return &cloned
}

func ValidateMultimodalVector(
	expectedContract MultimodalContractIdentity,
	expectedSourceHash string,
	actualIdentity MultimodalVectorIdentity,
	values []float64,
) (*MultimodalVector, error) {
	if _, err := NewMultimodalContractIdentity(
		expectedContract.ProviderAlias,
		expectedContract.ModelAlias,
		expectedContract.RevisionAlias,
		expectedContract.Dimension,
		expectedContract.TextCanonicalizer,
		expectedContract.FrameSamplingPolicy,
		expectedContract.ImagePreprocessingPolicy,
		expectedContract.FusionPolicy,
	); err != nil {
		return nil, err
	}
	if !expectedContract.Equal(actualIdentity.Contract) {
		return nil, validationError(MultimodalValidationContractMismatch)
	}
	expectedSourceHash = strings.ToLower(strings.TrimSpace(expectedSourceHash))
	if !validSHA256Hex(expectedSourceHash) ||
		strings.ToLower(strings.TrimSpace(actualIdentity.SourceHash)) != expectedSourceHash {
		return nil, validationError(MultimodalValidationInputHash)
	}
	validatedValues, err := ValidateMultimodalQueryVector(expectedContract, values)
	if err != nil {
		return nil, err
	}
	digest := MultimodalVectorDigest(validatedValues)
	if !validSHA256Hex(actualIdentity.VectorDigest) ||
		strings.ToLower(strings.TrimSpace(actualIdentity.VectorDigest)) != digest {
		return nil, validationError(MultimodalValidationDigest)
	}
	return &MultimodalVector{
		Identity: MultimodalVectorIdentity{
			Contract: expectedContract, SourceHash: expectedSourceHash, VectorDigest: digest,
		},
		Values: validatedValues,
	}, nil
}

func ValidateMultimodalQueryVector(
	contract MultimodalContractIdentity,
	values []float64,
) ([]float64, error) {
	validatedContract, err := NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	if err != nil || !validatedContract.Equal(contract) {
		return nil, validationError(MultimodalValidationInvalidContract)
	}
	if len(values) != contract.Dimension {
		return nil, validationError(MultimodalValidationDimension)
	}
	var normSquared float64
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, validationError(MultimodalValidationNonFinite)
		}
		normSquared += value * value
	}
	if math.Abs(math.Sqrt(normSquared)-1) > MultimodalVectorNormTolerance {
		return nil, validationError(MultimodalValidationNorm)
	}
	return append([]float64(nil), values...), nil
}

func MultimodalSourceHash(parts ...[]byte) string {
	hasher := sha256.New()
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write(part)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func MultimodalVectorDigest(values []float64) string {
	hasher := sha256.New()
	var valueBytes [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(valueBytes[:], math.Float64bits(value))
		_, _ = hasher.Write(valueBytes[:])
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func CanonicalizePublicVideoText(title, description string, maxRunes int) (string, error) {
	title, err := canonicalizeMultimodalText(title)
	if err != nil {
		return "", err
	}
	description, err = canonicalizeMultimodalText(description)
	if err != nil {
		return "", err
	}
	text := BuildVideoText(title, description)
	if text == "" || maxRunes <= 0 || utf8.RuneCountInString(text) > maxRunes {
		return "", validationError(MultimodalValidationInvalidText)
	}
	return text, nil
}

func CanonicalizePublicQuery(query string, maxRunes int) (string, error) {
	query, err := canonicalizeMultimodalText(query)
	if err != nil || query == "" || maxRunes <= 0 || utf8.RuneCountInString(query) > maxRunes {
		return "", validationError(MultimodalValidationInvalidText)
	}
	return query, nil
}

func canonicalizeMultimodalText(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", validationError(MultimodalValidationInvalidText)
	}
	for _, character := range value {
		if unicode.IsControl(character) && !unicode.IsSpace(character) {
			return "", validationError(MultimodalValidationInvalidText)
		}
	}
	value = unicodeNorm.NFKC.String(value)
	return strings.Join(strings.Fields(value), " "), nil
}

func validationError(code MultimodalValidationCode) error {
	return &MultimodalValidationError{Code: code}
}

func normalizeMultimodalToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validMultimodalToken(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') ||
			(index > 0 && (character == '-' || character == '_' || character == '.')) {
			continue
		}
		return false
	}
	return true
}

func validSHA256Hex(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != MultimodalDigestHexLength {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
