package applicationembedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

var ErrIneligibleMultimodalContent = errors.New("ineligible multimodal content")
var ErrInvalidMultimodalMediaPreparation = errors.New("invalid multimodal media preparation")

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

type MultimodalPublicVideoContent struct {
	Title         string
	Description   string
	Published     bool
	Public        bool
	MediaReady    bool
	SourceCurrent bool
}

func NewMultimodalVideoEmbeddingRequest(
	contract domainembedding.MultimodalContractIdentity,
	content MultimodalPublicVideoContent,
	maxTextRunes int,
	images []PreparedMultimodalImage,
) (MultimodalVideoEmbeddingRequest, error) {
	if !content.Published || !content.Public || !content.MediaReady || !content.SourceCurrent {
		return MultimodalVideoEmbeddingRequest{}, ErrIneligibleMultimodalContent
	}
	text, err := domainembedding.CanonicalizePublicVideoText(content.Title, content.Description, maxTextRunes)
	if err != nil {
		return MultimodalVideoEmbeddingRequest{}, err
	}
	if len(images) == 0 || len(images) > 16 {
		return MultimodalVideoEmbeddingRequest{}, ErrInvalidMultimodalMediaPreparation
	}
	clonedImages := make([]PreparedMultimodalImage, len(images))
	parts := make([][]byte, 0, len(images)*5+2)
	parts = append(parts, []byte(contract.Canonical()), []byte(text))
	for index := range images {
		clonedImages[index] = images[index].Clone()
		image := clonedImages[index]
		digest := sha256.Sum256(image.Content)
		if image.Width <= 0 || image.Height <= 0 || len(image.Content) == 0 ||
			(image.MIMEType != "image/jpeg" && image.MIMEType != "image/png" && image.MIMEType != "image/webp") ||
			image.Digest != hex.EncodeToString(digest[:]) {
			return MultimodalVideoEmbeddingRequest{}, ErrInvalidMultimodalMediaPreparation
		}
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

func NewMultimodalVideoEmbeddingRequestForSource(
	contract domainembedding.MultimodalContractIdentity,
	sourceHash string,
	content MultimodalPublicVideoContent,
	maxTextRunes int,
	images []PreparedMultimodalImage,
) (MultimodalVideoEmbeddingRequest, error) {
	request, err := NewMultimodalVideoEmbeddingRequest(contract, content, maxTextRunes, images)
	if err != nil {
		return MultimodalVideoEmbeddingRequest{}, err
	}
	sourceHash = strings.ToLower(strings.TrimSpace(sourceHash))
	decoded, decodeErr := hex.DecodeString(sourceHash)
	if decodeErr != nil || len(decoded) != sha256.Size {
		return MultimodalVideoEmbeddingRequest{}, ErrInvalidMultimodalMediaPreparation
	}
	request.SourceHash = sourceHash
	return request, nil
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

func MultimodalVideoSourceHash(
	contract domainembedding.MultimodalContractIdentity,
	canonicalText string,
	mediaURL string,
	coverURL string,
	mediaAssetID int64,
	coverAssetID int64,
	mediaProfileVersion string,
	videoVersion int,
) string {
	return domainembedding.MultimodalSourceHash(
		[]byte(contract.Canonical()),
		[]byte(canonicalText),
		[]byte(strings.TrimSpace(mediaURL)),
		[]byte(strings.TrimSpace(coverURL)),
		[]byte(strconv.FormatInt(mediaAssetID, 10)),
		[]byte(strconv.FormatInt(coverAssetID, 10)),
		[]byte(strings.TrimSpace(mediaProfileVersion)),
		[]byte(strconv.Itoa(videoVersion)),
	)
}

type PreparedMultimodalImage struct {
	MIMEType string
	Width    int
	Height   int
	Digest   string
	Content  []byte
}

type MultimodalMediaPreparationRequest struct {
	VideoObjectKey           string
	CoverObjectKey           string
	FrameSamplingPolicy      string
	ImagePreprocessingPolicy string
	MaxImages                int
	MaxBytesEach             int
	MaxTotalBytes            int
	MaxPixelsEach            int64
	AllowedMIMETypes         []string
}

type PreparedMultimodalMedia struct {
	Images []PreparedMultimodalImage
}

func (m *PreparedMultimodalMedia) Clone() *PreparedMultimodalMedia {
	if m == nil {
		return nil
	}
	cloned := &PreparedMultimodalMedia{Images: make([]PreparedMultimodalImage, len(m.Images))}
	for index := range m.Images {
		cloned.Images[index] = m.Images[index].Clone()
	}
	return cloned
}

type MultimodalMediaPreparer interface {
	PrepareMultimodalMedia(context.Context, MultimodalMediaPreparationRequest) (*PreparedMultimodalMedia, error)
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
