package infraembedding

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	maxTongyiAdapterVideoTextRunes  = 8192
	maxTongyiAdapterQueryRunes      = 512
	maxTongyiAdapterImages          = 16
	maxTongyiAdapterImageBytes      = 20 << 20
	maxTongyiAdapterTotalImageBytes = 64 << 20
	maxTongyiAdapterImagePixels     = 16_000_000
	maxTongyiReplayEntries          = 10_000
)

type tongyiEmbeddingClient interface {
	Probe(context.Context) error
	EmbedQuery(context.Context, string) (*TongyiEmbedding, error)
	EmbedVideo(context.Context, string, []applicationembedding.PreparedMultimodalImage) (*TongyiEmbedding, error)
}

type TongyiAdapter struct {
	secret          []byte
	client          tongyiEmbeddingClient
	contract        domainembedding.MultimodalContractIdentity
	maxRequestBytes int64
	now             func() time.Time
	ready           atomic.Bool
	replay          *tongyiReplayGuard
}

type tongyiReplayGuard struct {
	mutex   sync.Mutex
	entries map[string]time.Time
	limit   int
	ttl     time.Duration
}

func NewTongyiAdapter(config TongyiAdapterConfig, client tongyiEmbeddingClient) (*TongyiAdapter, error) {
	if err := validateTongyiAdapterConfig(config); err != nil || client == nil {
		return nil, ErrInvalidTongyiAdapterConfig
	}
	return &TongyiAdapter{
		secret: []byte(strings.TrimSpace(config.FruxHMACSecret)),
		client: client, contract: TongyiMultimodalContract(),
		maxRequestBytes: config.MaxInboundRequestBytes,
		now:             func() time.Time { return time.Now().UTC() },
		replay: &tongyiReplayGuard{
			entries: make(map[string]time.Time), limit: maxTongyiReplayEntries,
			ttl: maxMultimodalClockSkew,
		},
	}, nil
}

func (a *TongyiAdapter) Probe(ctx context.Context) error {
	if a == nil || a.client == nil {
		return ErrInvalidTongyiAdapterConfig
	}
	if err := a.client.Probe(ctx); err != nil {
		return err
	}
	a.ready.Store(true)
	return nil
}

func (a *TongyiAdapter) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST "+multimodalReadyPath, a.handleReady)
	mux.HandleFunc("POST "+multimodalVideoPath, a.handleVideo)
	mux.HandleFunc("POST "+multimodalQueryPath, a.handleQuery)
	return mux
}

func (a *TongyiAdapter) handleReady(writer http.ResponseWriter, request *http.Request) {
	operationID, body, ok := a.authenticate(writer, request)
	if !ok {
		return
	}
	var input multimodalReadyRequest
	if !a.decodeRequest(writer, operationID, body, &input) {
		return
	}
	if input.ProtocolVersion != MultimodalProviderProtocolV1 || input.OperationID != operationID ||
		(input.RequiredCapability != MultimodalProviderCapabilityVideo && input.RequiredCapability != MultimodalProviderCapabilityQuery) {
		a.writeError(writer, http.StatusBadRequest, operationID, "invalid_request", 0)
		return
	}
	contract, err := multimodalContractFromEnvelope(input.Contract)
	if err != nil || !contract.Equal(a.contract) {
		a.writeError(writer, http.StatusConflict, operationID, "unsupported_contract", 0)
		return
	}
	if !a.ready.Load() {
		a.writeError(writer, http.StatusServiceUnavailable, operationID, "unavailable", time.Second)
		return
	}
	a.writeJSON(writer, http.StatusOK, operationID, multimodalReadyResponse{
		ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID, Ready: true,
		Capabilities: []string{MultimodalProviderCapabilityVideo, MultimodalProviderCapabilityQuery},
		Contract:     multimodalContractToEnvelope(a.contract),
	})
}

func (a *TongyiAdapter) handleQuery(writer http.ResponseWriter, request *http.Request) {
	operationID, body, ok := a.authenticate(writer, request)
	if !ok {
		return
	}
	if !a.ready.Load() {
		a.writeError(writer, http.StatusServiceUnavailable, operationID, "unavailable", time.Second)
		return
	}
	var input multimodalQueryRequest
	if !a.decodeRequest(writer, operationID, body, &input) {
		return
	}
	if status, code := a.validateEnvelope(input.ProtocolVersion, input.OperationID, operationID, input.Contract, input.SourceHash); code != "" {
		a.writeError(writer, status, operationID, code, 0)
		return
	}
	canonical, err := domainembedding.CanonicalizePublicQuery(input.Query, maxTongyiAdapterQueryRunes)
	if err != nil || canonical != input.Query {
		a.writeError(writer, http.StatusBadRequest, operationID, "invalid_request", 0)
		return
	}
	result, err := a.client.EmbedQuery(request.Context(), input.Query)
	if err != nil {
		a.writeUpstreamError(writer, operationID, err)
		return
	}
	a.writeEmbedding(writer, operationID, input.SourceHash, result)
}

func (a *TongyiAdapter) handleVideo(writer http.ResponseWriter, request *http.Request) {
	operationID, body, ok := a.authenticate(writer, request)
	if !ok {
		return
	}
	if !a.ready.Load() {
		a.writeError(writer, http.StatusServiceUnavailable, operationID, "unavailable", time.Second)
		return
	}
	var input multimodalVideoRequest
	if !a.decodeRequest(writer, operationID, body, &input) {
		return
	}
	if status, code := a.validateEnvelope(input.ProtocolVersion, input.OperationID, operationID, input.Contract, input.SourceHash); code != "" {
		a.writeError(writer, status, operationID, code, 0)
		return
	}
	if !validTongyiVideoText(input.Text) || len(input.Images) == 0 || len(input.Images) > maxTongyiAdapterImages {
		a.writeError(writer, http.StatusBadRequest, operationID, "invalid_request", 0)
		return
	}
	images := make([]applicationembedding.PreparedMultimodalImage, len(input.Images))
	totalBytes := 0
	for index, image := range input.Images {
		content, err := base64.StdEncoding.DecodeString(image.ContentBase64)
		if err != nil || len(content) == 0 || len(content) > maxTongyiAdapterImageBytes ||
			totalBytes > maxTongyiAdapterTotalImageBytes-len(content) ||
			image.Width <= 0 || image.Height <= 0 ||
			int64(image.Width) > maxTongyiAdapterImagePixels/int64(image.Height) ||
			!supportedMultimodalImageType(image.MIMEType) {
			a.writeError(writer, http.StatusBadRequest, operationID, "invalid_request", 0)
			return
		}
		digest := sha256.Sum256(content)
		if !hmac.Equal(
			[]byte(strings.ToLower(strings.TrimSpace(image.Digest))),
			[]byte(hex.EncodeToString(digest[:])),
		) {
			a.writeError(writer, http.StatusBadRequest, operationID, "invalid_request", 0)
			return
		}
		totalBytes += len(content)
		images[index] = applicationembedding.PreparedMultimodalImage{
			MIMEType: strings.ToLower(strings.TrimSpace(image.MIMEType)),
			Width:    image.Width, Height: image.Height,
			Digest: strings.ToLower(strings.TrimSpace(image.Digest)), Content: content,
		}
	}
	result, err := a.client.EmbedVideo(request.Context(), input.Text, images)
	if err != nil {
		a.writeUpstreamError(writer, operationID, err)
		return
	}
	a.writeEmbedding(writer, operationID, input.SourceHash, result)
}

func (a *TongyiAdapter) authenticate(
	writer http.ResponseWriter,
	request *http.Request,
) (string, []byte, bool) {
	operationID := strings.TrimSpace(request.Header.Get("X-Frux-Operation-ID"))
	protocol := strings.TrimSpace(request.Header.Get("X-Frux-Multimodal-Protocol"))
	timestampRaw := strings.TrimSpace(request.Header.Get("X-Frux-Timestamp"))
	body, err := io.ReadAll(io.LimitReader(request.Body, a.maxRequestBytes+1))
	if err != nil || int64(len(body)) > a.maxRequestBytes {
		a.writeError(writer, http.StatusRequestEntityTooLarge, operationID, "invalid_request", 0)
		return operationID, nil, false
	}
	mediaType, _, mediaTypeErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if mediaTypeErr != nil || mediaType != "application/json" {
		a.writeError(writer, http.StatusUnsupportedMediaType, operationID, "invalid_request", 0)
		return operationID, nil, false
	}
	timestamp, timestampErr := time.Parse(time.RFC3339, timestampRaw)
	if protocol != MultimodalProviderProtocolV1 || !validTongyiOperationID(operationID) ||
		timestampErr != nil || a.now().Sub(timestamp.UTC()).Abs() > maxMultimodalClockSkew {
		a.writeError(writer, http.StatusUnauthorized, operationID, "invalid_request", 0)
		return operationID, nil, false
	}
	expected := multimodalRequestSignature(
		a.secret, protocol, request.Method, request.URL.EscapedPath(), timestampRaw, operationID, body,
	)
	if !secureMultimodalSignatureEqual(request.Header.Get("X-Frux-Signature"), expected) {
		a.writeError(writer, http.StatusUnauthorized, operationID, "invalid_request", 0)
		return operationID, nil, false
	}
	if !a.replay.Accept(operationID, a.now()) {
		a.writeError(writer, http.StatusConflict, operationID, "invalid_request", 0)
		return operationID, nil, false
	}
	return operationID, body, true
}

func (a *TongyiAdapter) decodeRequest(writer http.ResponseWriter, operationID string, body []byte, target any) bool {
	if err := decodeMultimodalJSON(body, target); err != nil {
		a.writeError(writer, http.StatusBadRequest, operationID, "invalid_request", 0)
		return false
	}
	return true
}

func (a *TongyiAdapter) validateEnvelope(
	protocolVersion string,
	bodyOperationID string,
	headerOperationID string,
	contractEnvelope multimodalContractEnvelope,
	sourceHash string,
) (int, string) {
	if protocolVersion != MultimodalProviderProtocolV1 || bodyOperationID != headerOperationID ||
		!validMultimodalSHA256(sourceHash) {
		return http.StatusBadRequest, "invalid_request"
	}
	contract, err := multimodalContractFromEnvelope(contractEnvelope)
	if err != nil || !contract.Equal(a.contract) {
		return http.StatusConflict, "unsupported_contract"
	}
	return 0, ""
}

func (a *TongyiAdapter) writeEmbedding(
	writer http.ResponseWriter,
	operationID string,
	sourceHash string,
	result *TongyiEmbedding,
) {
	if result == nil {
		a.writeError(writer, http.StatusUnprocessableEntity, operationID, "internal", 0)
		return
	}
	values, err := domainembedding.ValidateMultimodalQueryVector(a.contract, result.Values)
	if err != nil {
		a.writeError(writer, http.StatusUnprocessableEntity, operationID, "internal", 0)
		return
	}
	a.writeJSON(writer, http.StatusOK, operationID, multimodalEmbeddingResponse{
		ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID,
		Contract: multimodalContractToEnvelope(a.contract), SourceHash: sourceHash,
		VectorDigest: domainembedding.MultimodalVectorDigest(values), Vector: values,
	})
}

func (a *TongyiAdapter) writeUpstreamError(writer http.ResponseWriter, operationID string, err error) {
	var upstreamError *TongyiUpstreamError
	if errors.As(err, &upstreamError) {
		if upstreamError.Retryable {
			status := http.StatusServiceUnavailable
			code := "unavailable"
			if upstreamError.Code == "rate_limit" {
				status = http.StatusTooManyRequests
				code = "capacity"
			}
			a.writeError(writer, status, operationID, code, upstreamError.RetryAfter)
			return
		}
		a.writeError(writer, http.StatusUnprocessableEntity, operationID, "internal", 0)
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		a.writeError(writer, http.StatusServiceUnavailable, operationID, "unavailable", 0)
		return
	}
	a.writeError(writer, http.StatusUnprocessableEntity, operationID, "internal", 0)
}

func (a *TongyiAdapter) writeError(
	writer http.ResponseWriter,
	status int,
	operationID string,
	code string,
	retryAfter time.Duration,
) {
	if retryAfter > 0 {
		seconds := int64((retryAfter + time.Second - 1) / time.Second)
		writer.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	}
	a.writeJSON(writer, status, operationID, multimodalErrorResponse{
		ProtocolVersion: MultimodalProviderProtocolV1,
		OperationID:     operationID,
		Code:            code,
	})
}

func (a *TongyiAdapter) writeJSON(writer http.ResponseWriter, status int, operationID string, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Frux-Response-Signature", multimodalResponseSignature(
		a.secret, MultimodalProviderProtocolV1, status, operationID, body,
	))
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func (g *tongyiReplayGuard) Accept(operationID string, now time.Time) bool {
	if g == nil {
		return false
	}
	g.mutex.Lock()
	defer g.mutex.Unlock()
	for key, expiresAt := range g.entries {
		if !now.Before(expiresAt) {
			delete(g.entries, key)
		}
	}
	if _, duplicate := g.entries[operationID]; duplicate || len(g.entries) >= g.limit {
		return false
	}
	g.entries[operationID] = now.Add(g.ttl)
	return true
}

func validTongyiOperationID(value string) bool {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == 16
}

func validTongyiVideoText(value string) bool {
	if value == "" || strings.TrimSpace(value) == "" || !utf8.ValidString(value) ||
		utf8.RuneCountInString(value) > maxTongyiAdapterVideoTextRunes {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) && char != '\n' && char != '\t' {
			return false
		}
	}
	return true
}
