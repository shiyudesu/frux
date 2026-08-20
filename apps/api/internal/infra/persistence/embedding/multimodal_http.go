package infraembedding

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const (
	MultimodalProviderProtocolV1 = "frux-multimodal-v1"

	MultimodalProviderCapabilityVideo = "video"
	MultimodalProviderCapabilityQuery = "query"

	multimodalReadyPath = "/v1/ready"
	multimodalVideoPath = "/v1/embed/video"
	multimodalQueryPath = "/v1/embed/query"

	maxMultimodalClockSkew = 5 * time.Minute
)

var ErrInvalidMultimodalHTTPProvider = errors.New("invalid multimodal HTTP provider")
var ErrMultimodalProviderNotReady = errors.New("multimodal provider not ready")

type MultimodalHTTPProviderConfig struct {
	Endpoint           string
	HMACSecret         string
	ProtocolVersion    string
	AllowInsecureLocal bool
	Timeout            time.Duration
	MaxRequestBytes    int64
	MaxResponseBytes   int64
	MaxIdleConnections int
	MaxVideoTextRunes  int
	MaxQueryRunes      int
	MaxImages          int
	MaxImageBytes      int
	MaxTotalImageBytes int
	MaxImagePixels     int64
	AllowedMIMETypes   []string
	Observer           MultimodalHTTPObserver
}

type MultimodalHTTPObserver interface {
	ObserveMultimodalHTTP(operation, result string, duration time.Duration)
}

type multimodalHTTPMetricsObserver struct{}

func (multimodalHTTPMetricsObserver) ObserveMultimodalHTTP(operation, result string, duration time.Duration) {
	inframetrics.ObserveMultimodalProviderTransport(operation, result, duration)
}

type HTTPMultimodalProvider struct {
	endpoint           string
	secret             []byte
	protocolVersion    string
	contract           domainembedding.MultimodalContractIdentity
	client             *http.Client
	maxRequestBytes    int64
	maxResponseBytes   int64
	maxVideoTextRunes  int
	maxQueryRunes      int
	maxImages          int
	maxImageBytes      int
	maxTotalImageBytes int
	maxImagePixels     int64
	allowedMIMETypes   map[string]struct{}
	observer           MultimodalHTTPObserver
	now                func() time.Time
}

type multimodalContractEnvelope struct {
	ProviderAlias            string `json:"provider_alias"`
	ModelAlias               string `json:"model_alias"`
	RevisionAlias            string `json:"revision_alias"`
	Dimension                int    `json:"dimension"`
	TextCanonicalizer        string `json:"text_canonicalizer"`
	FrameSamplingPolicy      string `json:"frame_sampling_policy"`
	ImagePreprocessingPolicy string `json:"image_preprocessing_policy"`
	FusionPolicy             string `json:"fusion_policy"`
}

type multimodalReadyRequest struct {
	ProtocolVersion    string                     `json:"protocol_version"`
	OperationID        string                     `json:"operation_id"`
	RequiredCapability string                     `json:"required_capability"`
	Contract           multimodalContractEnvelope `json:"contract"`
}

type multimodalReadyResponse struct {
	ProtocolVersion string                     `json:"protocol_version"`
	OperationID     string                     `json:"operation_id"`
	Ready           bool                       `json:"ready"`
	Capabilities    []string                   `json:"capabilities"`
	Contract        multimodalContractEnvelope `json:"contract"`
}

type multimodalVideoRequest struct {
	ProtocolVersion string                     `json:"protocol_version"`
	OperationID     string                     `json:"operation_id"`
	Contract        multimodalContractEnvelope `json:"contract"`
	SourceHash      string                     `json:"source_hash"`
	Text            string                     `json:"text"`
	Images          []multimodalImageEnvelope  `json:"images"`
}

type multimodalImageEnvelope struct {
	MIMEType      string `json:"mime_type"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	Digest        string `json:"digest"`
	ContentBase64 string `json:"content_base64"`
}

type multimodalQueryRequest struct {
	ProtocolVersion string                     `json:"protocol_version"`
	OperationID     string                     `json:"operation_id"`
	Contract        multimodalContractEnvelope `json:"contract"`
	SourceHash      string                     `json:"source_hash"`
	Query           string                     `json:"query"`
}

type multimodalEmbeddingResponse struct {
	ProtocolVersion string                     `json:"protocol_version"`
	OperationID     string                     `json:"operation_id"`
	Contract        multimodalContractEnvelope `json:"contract"`
	SourceHash      string                     `json:"source_hash"`
	VectorDigest    string                     `json:"vector_digest"`
	Vector          []float64                  `json:"vector"`
}

type multimodalErrorResponse struct {
	ProtocolVersion string `json:"protocol_version"`
	OperationID     string `json:"operation_id"`
	Code            string `json:"code"`
}

func NewHTTPMultimodalProvider(
	config MultimodalHTTPProviderConfig,
	contract domainembedding.MultimodalContractIdentity,
) (*HTTPMultimodalProvider, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	secret := strings.TrimSpace(config.HMACSecret)
	protocolVersion := strings.ToLower(strings.TrimSpace(config.ProtocolVersion))
	if protocolVersion == "" {
		protocolVersion = MultimodalProviderProtocolV1
	}
	validatedContract, err := domainembedding.NewMultimodalContractIdentity(
		contract.ProviderAlias, contract.ModelAlias, contract.RevisionAlias, contract.Dimension,
		contract.TextCanonicalizer, contract.FrameSamplingPolicy,
		contract.ImagePreprocessingPolicy, contract.FusionPolicy,
	)
	allowedMIMETypes := make(map[string]struct{}, len(config.AllowedMIMETypes))
	for _, mimeType := range config.AllowedMIMETypes {
		mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		if !supportedMultimodalImageType(mimeType) {
			return nil, ErrInvalidMultimodalHTTPProvider
		}
		allowedMIMETypes[mimeType] = struct{}{}
	}
	minimumEncodedRequestBytes := int64(base64.StdEncoding.EncodedLen(config.MaxTotalImageBytes)) + 64<<10
	if err != nil || !validatedContract.Equal(contract) || endpoint == "" ||
		len(secret) < 32 || len(secret) > 512 || protocolVersion != MultimodalProviderProtocolV1 ||
		config.Timeout < 100*time.Millisecond || config.Timeout > 2*time.Minute ||
		config.MaxRequestBytes < 1<<20 || config.MaxRequestBytes > 96<<20 ||
		config.MaxResponseBytes < 64<<10 || config.MaxResponseBytes > 8<<20 ||
		config.MaxRequestBytes < minimumEncodedRequestBytes ||
		config.MaxVideoTextRunes < 1 || config.MaxVideoTextRunes > 8192 ||
		config.MaxQueryRunes < 1 || config.MaxQueryRunes > 512 ||
		config.MaxImages < 1 || config.MaxImages > 16 ||
		config.MaxImageBytes < 64<<10 || config.MaxImageBytes > 20<<20 ||
		config.MaxTotalImageBytes < config.MaxImageBytes || config.MaxTotalImageBytes > 64<<20 ||
		config.MaxImagePixels < 10_000 || config.MaxImagePixels > 16_000_000 ||
		len(allowedMIMETypes) == 0 {
		return nil, ErrInvalidMultimodalHTTPProvider
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidMultimodalHTTPProvider
	}
	if parsed.Scheme != "https" &&
		(parsed.Scheme != "http" || !config.AllowInsecureLocal || !multimodalLocalHost(parsed.Hostname())) {
		return nil, ErrInvalidMultimodalHTTPProvider
	}
	maxIdleConnections := config.MaxIdleConnections
	if maxIdleConnections == 0 {
		maxIdleConnections = 8
	}
	if maxIdleConnections < 1 || maxIdleConnections > 128 {
		return nil, ErrInvalidMultimodalHTTPProvider
	}
	observer := config.Observer
	if observer == nil {
		observer = multimodalHTTPMetricsObserver{}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = maxIdleConnections
	transport.MaxIdleConnsPerHost = maxIdleConnections
	transport.MaxConnsPerHost = maxIdleConnections
	transport.IdleConnTimeout = 30 * time.Second
	return &HTTPMultimodalProvider{
		endpoint: endpoint, secret: []byte(secret), protocolVersion: protocolVersion,
		contract: contract, maxRequestBytes: config.MaxRequestBytes,
		maxResponseBytes:  config.MaxResponseBytes,
		maxVideoTextRunes: config.MaxVideoTextRunes, maxQueryRunes: config.MaxQueryRunes,
		maxImages: config.MaxImages, maxImageBytes: config.MaxImageBytes,
		maxTotalImageBytes: config.MaxTotalImageBytes, maxImagePixels: config.MaxImagePixels,
		allowedMIMETypes: allowedMIMETypes, observer: observer,
		client: &http.Client{
			Timeout: config.Timeout, Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (p *HTTPMultimodalProvider) Contract() domainembedding.MultimodalContractIdentity {
	if p == nil {
		return domainembedding.MultimodalContractIdentity{}
	}
	return p.contract
}

func (p *HTTPMultimodalProvider) CheckReady(ctx context.Context, requiredCapability string) error {
	started := time.Now()
	result := "success"
	defer func() { p.observe("readiness", result, time.Since(started)) }()
	requiredCapability = strings.ToLower(strings.TrimSpace(requiredCapability))
	if p == nil || (requiredCapability != MultimodalProviderCapabilityVideo && requiredCapability != MultimodalProviderCapabilityQuery) {
		result = "invalid"
		return ErrInvalidMultimodalHTTPProvider
	}
	operationID, err := newMultimodalOperationID()
	if err != nil {
		result = "invalid"
		return providerTransportError(false, "operation_id")
	}
	payload := multimodalReadyRequest{
		ProtocolVersion: p.protocolVersion, OperationID: operationID,
		RequiredCapability: requiredCapability, Contract: multimodalContractToEnvelope(p.contract),
	}
	var response multimodalReadyResponse
	if err := p.post(ctx, multimodalReadyPath, operationID, payload, &response); err != nil {
		result = multimodalHTTPResult(err)
		return err
	}
	contract, err := multimodalContractFromEnvelope(response.Contract)
	if err != nil || response.ProtocolVersion != p.protocolVersion || response.OperationID != operationID ||
		!response.Ready || !contract.Equal(p.contract) || !containsMultimodalCapability(response.Capabilities, requiredCapability) {
		result = "invalid"
		return providerTransportError(false, "readiness_mismatch")
	}
	return nil
}

func (p *HTTPMultimodalProvider) EmbedVideoContent(
	ctx context.Context,
	request applicationembedding.MultimodalVideoEmbeddingRequest,
) (*applicationembedding.MultimodalEmbeddingResult, error) {
	started := time.Now()
	resultLabel := "success"
	defer func() { p.observe("video", resultLabel, time.Since(started)) }()
	if p == nil || !request.Contract.Equal(p.contract) || !validMultimodalSHA256(request.SourceHash) ||
		strings.TrimSpace(request.Text) == "" || utf8.RuneCountInString(request.Text) > p.maxVideoTextRunes ||
		len(request.Images) == 0 || len(request.Images) > p.maxImages {
		resultLabel = "invalid"
		return nil, providerTransportError(false, "invalid_request")
	}
	images := make([]multimodalImageEnvelope, len(request.Images))
	totalImageBytes := 0
	for index, image := range request.Images {
		digest := sha256.Sum256(image.Content)
		if image.Width <= 0 || image.Height <= 0 || len(image.Content) == 0 ||
			len(image.Content) > p.maxImageBytes || totalImageBytes > p.maxTotalImageBytes-len(image.Content) ||
			int64(image.Width) > p.maxImagePixels/int64(image.Height) ||
			!p.supportsImageType(image.MIMEType) ||
			!hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(image.Digest))), []byte(hex.EncodeToString(digest[:]))) {
			resultLabel = "invalid"
			return nil, providerTransportError(false, "invalid_request")
		}
		totalImageBytes += len(image.Content)
		images[index] = multimodalImageEnvelope{
			MIMEType: strings.ToLower(strings.TrimSpace(image.MIMEType)), Width: image.Width,
			Height: image.Height, Digest: strings.ToLower(strings.TrimSpace(image.Digest)),
			ContentBase64: base64.StdEncoding.EncodeToString(image.Content),
		}
	}
	operationID, err := newMultimodalOperationID()
	if err != nil {
		resultLabel = "invalid"
		return nil, providerTransportError(false, "operation_id")
	}
	payload := multimodalVideoRequest{
		ProtocolVersion: p.protocolVersion, OperationID: operationID,
		Contract: multimodalContractToEnvelope(request.Contract), SourceHash: request.SourceHash,
		Text: request.Text, Images: images,
	}
	response, err := p.embed(ctx, multimodalVideoPath, operationID, request.SourceHash, payload)
	if err != nil {
		resultLabel = multimodalHTTPResult(err)
		return nil, err
	}
	return response, nil
}

func (p *HTTPMultimodalProvider) EmbedQueryText(
	ctx context.Context,
	request applicationembedding.MultimodalQueryEmbeddingRequest,
) (*applicationembedding.MultimodalEmbeddingResult, error) {
	started := time.Now()
	resultLabel := "success"
	defer func() { p.observe("query", resultLabel, time.Since(started)) }()
	if p == nil || !request.Contract.Equal(p.contract) || !validMultimodalSHA256(request.SourceHash) ||
		strings.TrimSpace(request.Query) == "" || utf8.RuneCountInString(request.Query) > p.maxQueryRunes {
		resultLabel = "invalid"
		return nil, providerTransportError(false, "invalid_request")
	}
	operationID, err := newMultimodalOperationID()
	if err != nil {
		resultLabel = "invalid"
		return nil, providerTransportError(false, "operation_id")
	}
	payload := multimodalQueryRequest{
		ProtocolVersion: p.protocolVersion, OperationID: operationID,
		Contract: multimodalContractToEnvelope(request.Contract), SourceHash: request.SourceHash,
		Query: request.Query,
	}
	response, err := p.embed(ctx, multimodalQueryPath, operationID, request.SourceHash, payload)
	if err != nil {
		resultLabel = multimodalHTTPResult(err)
		return nil, err
	}
	return response, nil
}

func (p *HTTPMultimodalProvider) embed(
	ctx context.Context,
	path string,
	operationID string,
	sourceHash string,
	payload any,
) (*applicationembedding.MultimodalEmbeddingResult, error) {
	var response multimodalEmbeddingResponse
	if err := p.post(ctx, path, operationID, payload, &response); err != nil {
		return nil, err
	}
	contract, err := multimodalContractFromEnvelope(response.Contract)
	if err != nil || response.ProtocolVersion != p.protocolVersion || response.OperationID != operationID {
		return nil, providerTransportError(false, "response_identity")
	}
	result := &applicationembedding.MultimodalEmbeddingResult{
		Identity: domainembedding.MultimodalVectorIdentity{
			Contract: contract, SourceHash: response.SourceHash, VectorDigest: response.VectorDigest,
		},
		Vector: response.Vector,
	}
	validated, err := applicationembedding.ValidateMultimodalEmbeddingResult(p.contract, sourceHash, result)
	if err != nil {
		return nil, providerTransportError(false, "response_vector")
	}
	return &applicationembedding.MultimodalEmbeddingResult{
		Identity: validated.Identity, Vector: append([]float64(nil), validated.Values...),
	}, nil
}

func (p *HTTPMultimodalProvider) post(
	ctx context.Context,
	path string,
	operationID string,
	payload any,
	responseTarget any,
) error {
	body, err := json.Marshal(payload)
	if err != nil || int64(len(body)) > p.maxRequestBytes {
		return providerTransportError(false, "request_oversized")
	}
	timestamp := p.now().UTC().Truncate(time.Second).Format(time.RFC3339)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return providerTransportError(false, "request_create")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Frux-Multimodal-Protocol", p.protocolVersion)
	request.Header.Set("X-Frux-Operation-ID", operationID)
	request.Header.Set("X-Frux-Timestamp", timestamp)
	request.Header.Set("X-Frux-Signature", multimodalRequestSignature(
		p.secret, p.protocolVersion, request.Method, request.URL.EscapedPath(), timestamp, operationID, body,
	))
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return providerTransportError(true, "timeout")
		}
		return providerTransportError(true, "transport")
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, p.maxResponseBytes+1))
	if err != nil {
		return providerTransportError(true, "response_read")
	}
	if int64(len(responseBody)) > p.maxResponseBytes {
		return providerTransportError(false, "response_oversized")
	}
	if !verifyMultimodalResponseSignature(
		p.secret, p.protocolVersion, response.StatusCode, operationID, responseBody,
		response.Header.Get("X-Frux-Response-Signature"),
	) {
		return providerTransportError(false, "response_signature")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var providerResponse multimodalErrorResponse
		if err := decodeMultimodalJSON(responseBody, &providerResponse); err != nil ||
			providerResponse.ProtocolVersion != p.protocolVersion ||
			providerResponse.OperationID != operationID ||
			!validMultimodalProviderErrorCode(providerResponse.Code) {
			return providerTransportError(false, "response_invalid")
		}
		retryable := response.StatusCode == http.StatusRequestTimeout ||
			response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError
		providerError := providerTransportError(retryable, "provider_status")
		if typed, ok := providerError.(*applicationembedding.MultimodalProviderError); ok && retryable {
			typed.RetryAfter = multimodalRetryAfter(response.Header.Get("Retry-After"), p.now())
		}
		return providerError
	}
	if err := decodeMultimodalJSON(responseBody, responseTarget); err != nil {
		return providerTransportError(false, "response_invalid")
	}
	return nil
}

func (p *HTTPMultimodalProvider) observe(operation, result string, duration time.Duration) {
	if p != nil && p.observer != nil {
		p.observer.ObserveMultimodalHTTP(operation, result, duration)
	}
}

func (p *HTTPMultimodalProvider) supportsImageType(value string) bool {
	if p == nil {
		return false
	}
	_, ok := p.allowedMIMETypes[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func multimodalContractToEnvelope(contract domainembedding.MultimodalContractIdentity) multimodalContractEnvelope {
	return multimodalContractEnvelope{
		ProviderAlias: contract.ProviderAlias, ModelAlias: contract.ModelAlias,
		RevisionAlias: contract.RevisionAlias, Dimension: contract.Dimension,
		TextCanonicalizer:        contract.TextCanonicalizer,
		FrameSamplingPolicy:      contract.FrameSamplingPolicy,
		ImagePreprocessingPolicy: contract.ImagePreprocessingPolicy,
		FusionPolicy:             contract.FusionPolicy,
	}
}

func multimodalContractFromEnvelope(envelope multimodalContractEnvelope) (domainembedding.MultimodalContractIdentity, error) {
	return domainembedding.NewMultimodalContractIdentity(
		envelope.ProviderAlias, envelope.ModelAlias, envelope.RevisionAlias, envelope.Dimension,
		envelope.TextCanonicalizer, envelope.FrameSamplingPolicy,
		envelope.ImagePreprocessingPolicy, envelope.FusionPolicy,
	)
}

func multimodalRequestSignature(
	secret []byte,
	protocolVersion string,
	method string,
	path string,
	timestamp string,
	operationID string,
	body []byte,
) string {
	digest := sha256.Sum256(body)
	message := strings.Join([]string{
		protocolVersion, method, path, timestamp, operationID, hex.EncodeToString(digest[:]),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyMultimodalResponseSignature(
	secret []byte,
	protocolVersion string,
	statusCode int,
	operationID string,
	body []byte,
	provided string,
) bool {
	expected := multimodalResponseSignature(secret, protocolVersion, statusCode, operationID, body)
	return secureMultimodalSignatureEqual(provided, expected)
}

func multimodalResponseSignature(
	secret []byte,
	protocolVersion string,
	statusCode int,
	operationID string,
	body []byte,
) string {
	digest := sha256.Sum256(body)
	message := strings.Join([]string{
		protocolVersion, strconv.Itoa(statusCode), operationID, hex.EncodeToString(digest[:]),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func secureMultimodalSignatureEqual(provided, expected string) bool {
	decoded, err := hex.DecodeString(strings.TrimSpace(provided))
	expectedBytes, expectedErr := hex.DecodeString(expected)
	return err == nil && expectedErr == nil && hmac.Equal(decoded, expectedBytes)
}

func newMultimodalOperationID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

func validMultimodalSHA256(value string) bool {
	decoded, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(value)))
	return err == nil && len(decoded) == sha256.Size
}

func supportedMultimodalImageType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func containsMultimodalCapability(values []string, expected string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != MultimodalProviderCapabilityVideo && value != MultimodalProviderCapabilityQuery {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	_, exists := seen[expected]
	return exists
}

func providerTransportError(retryable bool, code string) error {
	return &applicationembedding.MultimodalProviderError{
		Retryable: retryable, Err: fmt.Errorf("provider transport %s", strings.TrimSpace(code)),
	}
}

func multimodalHTTPResult(err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	var providerError *applicationembedding.MultimodalProviderError
	if errors.As(err, &providerError) {
		if providerError.Retryable {
			return "retryable"
		}
		return "terminal"
	}
	return "invalid"
}

func multimodalRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return min(time.Duration(seconds)*time.Second, 24*time.Hour)
	}
	if deadline, err := http.ParseTime(raw); err == nil {
		return min(max(deadline.Sub(now.UTC()), 0), 24*time.Hour)
	}
	return 0
}

func ensureMultimodalJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON")
	}
	return err
}

func decodeMultimodalJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureMultimodalJSONEOF(decoder)
}

func validMultimodalProviderErrorCode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "invalid_request", "unsupported_contract", "capacity", "unavailable", "internal":
		return true
	default:
		return false
	}
}

func multimodalLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}
