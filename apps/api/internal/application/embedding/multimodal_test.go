package applicationembedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"slices"
	"testing"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type replaceableMultimodalProvider struct {
	videoRequest MultimodalVideoEmbeddingRequest
	queryRequest MultimodalQueryEmbeddingRequest
	result       *MultimodalEmbeddingResult
}

type alternateMultimodalProvider struct {
	result *MultimodalEmbeddingResult
}

func (p alternateMultimodalProvider) EmbedVideoContent(context.Context, MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	return p.result.Clone(), nil
}

func (p alternateMultimodalProvider) EmbedQueryText(context.Context, MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	return p.result.Clone(), nil
}

func (p *replaceableMultimodalProvider) EmbedVideoContent(_ context.Context, request MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	p.videoRequest = request.Clone()
	return p.result.Clone(), nil
}

func (p *replaceableMultimodalProvider) EmbedQueryText(_ context.Context, request MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error) {
	p.queryRequest = request
	return p.result.Clone(), nil
}

func TestMultimodalProviderOperationsShareOneValidatedContract(t *testing.T) {
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", "revision", domainembedding.MinMultimodalDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]float64, contract.Dimension)
	values[0] = 1
	provider := &replaceableMultimodalProvider{}
	var _ MultimodalEmbeddingProvider = provider
	imageContent := []byte{1, 2}
	imageDigest := sha256.Sum256(imageContent)
	videoRequest, err := NewMultimodalVideoEmbeddingRequest(
		contract,
		MultimodalPublicVideoContent{
			Title: " public ", Description: " video ",
			Published: true, Public: true, MediaReady: true, SourceCurrent: true,
		},
		64,
		[]PreparedMultimodalImage{{
			MIMEType: " IMAGE/JPEG ", Width: 2, Height: 2,
			Digest: hex.EncodeToString(imageDigest[:]), Content: imageContent,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	queryRequest, err := NewMultimodalQueryEmbeddingRequest(contract, " public   query ", 64)
	if err != nil {
		t.Fatal(err)
	}
	videoHash := videoRequest.SourceHash
	queryHash := queryRequest.SourceHash

	provider.result = &MultimodalEmbeddingResult{Identity: domainembedding.MultimodalVectorIdentity{
		Contract: contract, SourceHash: videoHash, VectorDigest: domainembedding.MultimodalVectorDigest(values),
	}, Vector: values}
	videoResult, err := provider.EmbedVideoContent(context.Background(), videoRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateMultimodalEmbeddingResult(contract, videoHash, videoResult); err != nil {
		t.Fatal(err)
	}
	provider.videoRequest.Images[0].Content[0] = 9
	if videoResult.Vector[0] != 1 || videoRequest.Images[0].Content[0] != 1 {
		t.Fatal("provider request/result cloning aliased mutable input")
	}

	provider.result = &MultimodalEmbeddingResult{Identity: domainembedding.MultimodalVectorIdentity{
		Contract: contract, SourceHash: queryHash, VectorDigest: domainembedding.MultimodalVectorDigest(values),
	}, Vector: values}
	queryResult, err := provider.EmbedQueryText(context.Background(), queryRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateMultimodalEmbeddingResult(contract, queryHash, queryResult); err != nil {
		t.Fatal(err)
	}
	if provider.videoRequest.Text != "public\nvideo" || provider.queryRequest.Query != "public query" ||
		provider.videoRequest.Images[0].MIMEType != "image/jpeg" {
		t.Fatalf("provider contract lost bounded inputs: video=%#v query=%#v", provider.videoRequest, provider.queryRequest)
	}
}

func TestMultimodalProviderRequestsExposeOnlyBoundedPublicInputs(t *testing.T) {
	videoFields := fieldNames(reflect.TypeOf(MultimodalVideoEmbeddingRequest{}))
	queryFields := fieldNames(reflect.TypeOf(MultimodalQueryEmbeddingRequest{}))
	if !reflect.DeepEqual(videoFields, []string{"Contract", "Images", "SourceHash", "Text"}) ||
		!reflect.DeepEqual(queryFields, []string{"Contract", "Query", "SourceHash"}) {
		t.Fatalf("provider request boundary changed: video=%v query=%v", videoFields, queryFields)
	}
}

func TestIneligibleVideoCannotBuildProviderRequest(t *testing.T) {
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", "revision", domainembedding.MinMultimodalDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	states := []MultimodalPublicVideoContent{
		{Title: "private", Published: true, Public: false, MediaReady: true, SourceCurrent: true},
		{Title: "draft", Published: false, Public: true, MediaReady: true, SourceCurrent: true},
		{Title: "processing", Published: true, Public: true, MediaReady: false, SourceCurrent: true},
		{Title: "stale", Published: true, Public: true, MediaReady: true, SourceCurrent: false},
	}
	for _, content := range states {
		if _, err := NewMultimodalVideoEmbeddingRequest(contract, content, 64, nil); !errors.Is(err, ErrIneligibleMultimodalContent) {
			t.Fatalf("ineligible content %#v error = %v", content, err)
		}
	}
}

func fieldNames(value reflect.Type) []string {
	fields := make([]string, 0, value.NumField())
	for index := 0; index < value.NumField(); index++ {
		fields = append(fields, value.Field(index).Name)
	}
	slices.Sort(fields)
	return fields
}

func TestValidateMultimodalEmbeddingResultRejectsMissingResult(t *testing.T) {
	_, err := ValidateMultimodalEmbeddingResult(domainembedding.MultimodalContractIdentity{}, "", nil)
	if domainembedding.MultimodalFailureCode(err) != domainembedding.MultimodalValidationMissingResult {
		t.Fatalf("missing result error = %v", err)
	}
}

func TestMultimodalProviderImplementationIsReplaceableUnderSameContract(t *testing.T) {
	contract, err := domainembedding.NewMultimodalContractIdentity(
		"provider", "model", "revision", domainembedding.MinMultimodalDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewMultimodalQueryEmbeddingRequest(contract, "query", 32)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]float64, contract.Dimension)
	values[0] = 1
	result := &MultimodalEmbeddingResult{
		Identity: domainembedding.MultimodalVectorIdentity{
			Contract: contract, SourceHash: request.SourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(values),
		},
		Vector: values,
	}
	providers := []MultimodalEmbeddingProvider{
		&replaceableMultimodalProvider{result: result},
		alternateMultimodalProvider{result: result},
	}
	for _, provider := range providers {
		provided, err := provider.EmbedQueryText(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateMultimodalEmbeddingResult(contract, request.SourceHash, provided); err != nil {
			t.Fatalf("replaceable provider result failed the shared contract: %v", err)
		}
	}
}
