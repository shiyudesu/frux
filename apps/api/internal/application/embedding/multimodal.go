package applicationembedding

import (
	"context"
	"strconv"
	"strings"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

type MultimodalEmbeddingProvider interface {
	EmbedVideoContent(context.Context, MultimodalVideoEmbeddingRequest) (*MultimodalEmbeddingResult, error)
	EmbedQueryText(context.Context, MultimodalQueryEmbeddingRequest) (*MultimodalEmbeddingResult, error)
}

type MultimodalVideoEmbeddingRequest struct {
	Contract   domainembedding.MultimodalContractIdentity
	SourceHash string
	Text       string
	Images     []PreparedMultimodalImage
}

func NewMultimodalVideoEmbeddingRequest(
	contract domainembedding.MultimodalContractIdentity,
	title string,
	description string,
	maxTextRunes int,
	images []PreparedMultimodalImage,
) (MultimodalVideoEmbeddingRequest, error) {
	text, err := domainembedding.CanonicalizePublicVideoText(title, description, maxTextRunes)
	if err != nil {
		return MultimodalVideoEmbeddingRequest{}, err
	}
	clonedImages := make([]PreparedMultimodalImage, len(images))
	parts := make([][]byte, 0, len(images)*5+2)
	parts = append(parts, []byte(contract.Canonical()), []byte(text))
	for index := range images {
		clonedImages[index] = images[index].Clone()
		image := clonedImages[index]
		parts = append(parts,
			[]byte(image.MIMEType),
			[]byte(strconv.Itoa(image.Width)),
			[]byte(strconv.Itoa(image.Height)),
			[]byte(image.Digest),
			image.Content,
		)
	}
	return MultimodalVideoEmbeddingRequest{
		Contract: contract, SourceHash: domainembedding.MultimodalSourceHash(parts...),
		Text: text, Images: clonedImages,
	}, nil
}

func (r MultimodalVideoEmbeddingRequest) Clone() MultimodalVideoEmbeddingRequest {
	cloned := r
	cloned.Images = make([]PreparedMultimodalImage, len(r.Images))
	for index := range r.Images {
		cloned.Images[index] = r.Images[index].Clone()
	}
	return cloned
}

type MultimodalQueryEmbeddingRequest struct {
	Contract   domainembedding.MultimodalContractIdentity
	SourceHash string
	Query      string
}

func NewMultimodalQueryEmbeddingRequest(
	contract domainembedding.MultimodalContractIdentity,
	query string,
	maxRunes int,
) (MultimodalQueryEmbeddingRequest, error) {
	query, err := domainembedding.CanonicalizePublicQuery(query, maxRunes)
	if err != nil {
		return MultimodalQueryEmbeddingRequest{}, err
	}
	return MultimodalQueryEmbeddingRequest{
		Contract: contract,
		SourceHash: domainembedding.MultimodalSourceHash(
			[]byte(contract.Canonical()), []byte(query),
		),
		Query: query,
	}, nil
}

type PreparedMultimodalImage struct {
	MIMEType string
	Width    int
	Height   int
	Digest   string
	Content  []byte
}

func (i PreparedMultimodalImage) Clone() PreparedMultimodalImage {
	cloned := i
	cloned.MIMEType = strings.ToLower(strings.TrimSpace(i.MIMEType))
	cloned.Digest = strings.ToLower(strings.TrimSpace(i.Digest))
	cloned.Content = append([]byte(nil), i.Content...)
	return cloned
}

type MultimodalEmbeddingResult struct {
	Identity domainembedding.MultimodalVectorIdentity
	Vector   []float64
}

func (r *MultimodalEmbeddingResult) Clone() *MultimodalEmbeddingResult {
	if r == nil {
		return nil
	}
	cloned := *r
	cloned.Vector = append([]float64(nil), r.Vector...)
	return &cloned
}

func ValidateMultimodalEmbeddingResult(
	expectedContract domainembedding.MultimodalContractIdentity,
	expectedSourceHash string,
	result *MultimodalEmbeddingResult,
) (*domainembedding.MultimodalVector, error) {
	if result == nil {
		return nil, &domainembedding.MultimodalValidationError{
			Code: domainembedding.MultimodalValidationMissingResult,
		}
	}
	return domainembedding.ValidateMultimodalVector(
		expectedContract,
		expectedSourceHash,
		result.Identity,
		result.Vector,
	)
}
