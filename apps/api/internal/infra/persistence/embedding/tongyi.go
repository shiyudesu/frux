package infraembedding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

const (
	TongyiUpstreamModel        = "tongyi-embedding-vision-flash-2026-03-06"
	TongyiProviderAlias        = "alibaba-bailian"
	TongyiModelAlias           = "tongyi-embedding-vision-flash"
	TongyiRevisionAlias        = "2026-03-06-res1"
	TongyiDimension            = 768
	TongyiResolutionLevel      = 1
	TongyiDefaultListenAddr    = "127.0.0.1:8099"
	TongyiDefaultTimeout       = 20 * time.Second
	TongyiDefaultShutdown      = 10 * time.Second
	TongyiDefaultRequestBytes  = 24 << 20
	TongyiDefaultResponseBytes = 2 << 20
)

var ErrInvalidTongyiAdapterConfig = errors.New("invalid Tongyi adapter configuration")

type TongyiAdapterConfig struct {
	ListenAddress            string
	FruxHMACSecret           string
	DashScopeEndpoint        string
	DashScopeAPIKey          string
	UpstreamTimeout          time.Duration
	MaxInboundRequestBytes   int64
	MaxUpstreamResponseBytes int64
	ShutdownTimeout          time.Duration
	HTTPClient               *http.Client
}

func LoadTongyiAdapterConfigFromEnv() (TongyiAdapterConfig, error) {
	config := TongyiAdapterConfig{
		ListenAddress:     strings.TrimSpace(os.Getenv("FRUX_MULTIMODAL_PROVIDER_LISTEN_ADDR")),
		FruxHMACSecret:    strings.TrimSpace(os.Getenv("FRUX_MULTIMODAL_HMAC_SECRET")),
		DashScopeEndpoint: strings.TrimSpace(os.Getenv("DASHSCOPE_MULTIMODAL_ENDPOINT")),
		DashScopeAPIKey:   strings.TrimSpace(os.Getenv("DASHSCOPE_API_KEY")),
	}
	if config.ListenAddress == "" {
		config.ListenAddress = TongyiDefaultListenAddr
	}
	var err error
	if config.UpstreamTimeout, err = tongyiEnvDuration("FRUX_TONGYI_UPSTREAM_TIMEOUT", TongyiDefaultTimeout); err != nil {
		return TongyiAdapterConfig{}, err
	}
	if config.ShutdownTimeout, err = tongyiEnvDuration("FRUX_TONGYI_SHUTDOWN_TIMEOUT", TongyiDefaultShutdown); err != nil {
		return TongyiAdapterConfig{}, err
	}
	if config.MaxInboundRequestBytes, err = tongyiEnvInt64("FRUX_TONGYI_MAX_REQUEST_BYTES", TongyiDefaultRequestBytes); err != nil {
		return TongyiAdapterConfig{}, err
	}
	if config.MaxUpstreamResponseBytes, err = tongyiEnvInt64("FRUX_TONGYI_MAX_RESPONSE_BYTES", TongyiDefaultResponseBytes); err != nil {
		return TongyiAdapterConfig{}, err
	}
	if err := validateTongyiAdapterConfig(config); err != nil {
		return TongyiAdapterConfig{}, err
	}
	return config, nil
}

func TongyiMultimodalContract() domainembedding.MultimodalContractIdentity {
	contract, err := domainembedding.NewMultimodalContractIdentity(
		TongyiProviderAlias,
		TongyiModelAlias,
		TongyiRevisionAlias,
		TongyiDimension,
		domainembedding.MultimodalTextCanonicalizerV1,
		domainembedding.MultimodalFrameSamplingPolicyV1,
		domainembedding.MultimodalImagePreprocessingV1,
		domainembedding.MultimodalFusionPolicyV1,
	)
	if err != nil {
		panic(err)
	}
	return contract
}

type TongyiUsage struct {
	InputTokens  int64
	ImageTokens  int64
	TextTokens   int64
	OutputTokens int64
	TotalTokens  int64
}

type TongyiEmbedding struct {
	Values []float64
	Usage  TongyiUsage
}

type TongyiUpstreamError struct {
	Retryable  bool
	RetryAfter time.Duration
	Code       string
}

func (e *TongyiUpstreamError) Error() string {
	if e == nil || strings.TrimSpace(e.Code) == "" {
		return "Tongyi upstream failed"
	}
	return "Tongyi upstream failed: " + e.Code
}

type TongyiClient struct {
	endpoint         string
	apiKey           string
	client           *http.Client
	maxRequestBytes  int64
	maxResponseBytes int64
	now              func() time.Time
}

type tongyiRequest struct {
	Model      string           `json:"model"`
	Input      tongyiInput      `json:"input"`
	Parameters tongyiParameters `json:"parameters"`
}

type tongyiInput struct {
	Contents []tongyiContent `json:"contents"`
}

type tongyiContent struct {
	Text        string   `json:"text,omitempty"`
	MultiImages []string `json:"multi_images,omitempty"`
}

type tongyiParameters struct {
	OutputType string `json:"output_type"`
	Dimension  int    `json:"dimension"`
	ResLevel   int    `json:"res_level,omitempty"`
}

type tongyiResponse struct {
	Output struct {
		Embeddings []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
			Type      string    `json:"type"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		InputTokens       int64 `json:"input_tokens"`
		InputTokensDetail struct {
			ImageTokens int64 `json:"image_tokens"`
			TextTokens  int64 `json:"text_tokens"`
		} `json:"input_tokens_details"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

func NewTongyiClient(config TongyiAdapterConfig) (*TongyiClient, error) {
	if err := validateTongyiAdapterConfig(config); err != nil {
		return nil, err
	}
	baseClient := config.HTTPClient
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if baseClient != nil && baseClient.Transport != nil {
		configuredTransport, ok := baseClient.Transport.(*http.Transport)
		if !ok {
			return nil, ErrInvalidTongyiAdapterConfig
		}
		transport = configuredTransport.Clone()
	}
	transport.MaxIdleConns = 8
	transport.MaxIdleConnsPerHost = 8
	transport.MaxConnsPerHost = 8
	transport.IdleConnTimeout = 30 * time.Second
	return &TongyiClient{
		endpoint: strings.TrimSpace(config.DashScopeEndpoint),
		apiKey:   strings.TrimSpace(config.DashScopeAPIKey),
		client: &http.Client{
			Transport: transport, Timeout: config.UpstreamTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		maxRequestBytes:  config.MaxInboundRequestBytes,
		maxResponseBytes: config.MaxUpstreamResponseBytes,
		now:              func() time.Time { return time.Now().UTC() },
	}, nil
}

func (c *TongyiClient) EmbedQuery(ctx context.Context, query string) (*TongyiEmbedding, error) {
	query = strings.TrimSpace(query)
	if c == nil || query == "" {
		return nil, tongyiError(false, "invalid_request", 0)
	}
	return c.embed(ctx, "query", "text", []tongyiContent{{Text: query}})
}

func (c *TongyiClient) Probe(ctx context.Context) error {
	if c == nil {
		return ErrInvalidTongyiAdapterConfig
	}
	_, err := c.embed(
		ctx, "startup", "text",
		[]tongyiContent{{Text: "frux multimodal provider readiness"}},
	)
	return err
}

func (c *TongyiClient) EmbedVideo(
	ctx context.Context,
	text string,
	images []applicationembedding.PreparedMultimodalImage,
) (*TongyiEmbedding, error) {
	text = strings.TrimSpace(text)
	if c == nil || text == "" || len(images) == 0 || len(images) > 16 {
		return nil, tongyiError(false, "invalid_request", 0)
	}
	dataURIs := make([]string, len(images))
	for index, image := range images {
		if !supportedMultimodalImageType(image.MIMEType) || len(image.Content) == 0 {
			return nil, tongyiError(false, "invalid_request", 0)
		}
		dataURIs[index] = "data:" + strings.ToLower(strings.TrimSpace(image.MIMEType)) + ";base64," +
			base64.StdEncoding.EncodeToString(image.Content)
	}
	return c.embed(ctx, "video", "fused", []tongyiContent{{Text: text, MultiImages: dataURIs}})
}

func (c *TongyiClient) embed(
	ctx context.Context,
	operation string,
	expectedType string,
	contents []tongyiContent,
) (*TongyiEmbedding, error) {
	started := time.Now()
	resultLabel := "success"
	defer func() { inframetrics.ObserveTongyiProvider(operation, resultLabel, time.Since(started)) }()
	payload := tongyiRequest{
		Model:      TongyiUpstreamModel,
		Input:      tongyiInput{Contents: contents},
		Parameters: tongyiParameters{OutputType: "dense", Dimension: TongyiDimension},
	}
	if operation == "video" {
		payload.Parameters.ResLevel = TongyiResolutionLevel
	}
	body, err := json.Marshal(payload)
	if err != nil || int64(len(body)) > c.maxRequestBytes {
		resultLabel = "terminal"
		return nil, tongyiError(false, "request_oversized", 0)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		resultLabel = "terminal"
		return nil, tongyiError(false, "request_create", 0)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			resultLabel = "cancelled"
			return nil, ctx.Err()
		}
		resultLabel = "retryable"
		return nil, tongyiError(true, "transport", 0)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		resultLabel = "retryable"
		return nil, tongyiError(true, "response_read", 0)
	}
	if int64(len(responseBody)) > c.maxResponseBytes {
		resultLabel = "terminal"
		return nil, tongyiError(false, "response_oversized", 0)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		retryable := response.StatusCode == http.StatusRequestTimeout ||
			response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError
		if retryable {
			resultLabel = "retryable"
		} else {
			resultLabel = "terminal"
		}
		code := "upstream_status"
		if response.StatusCode == http.StatusTooManyRequests {
			code = "rate_limit"
		}
		return nil, tongyiError(retryable, code, multimodalRetryAfter(response.Header.Get("Retry-After"), c.now()))
	}
	var decoded tongyiResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		resultLabel = "terminal"
		return nil, tongyiError(false, "response_invalid", 0)
	}
	embedding, err := validateTongyiResponse(decoded, expectedType)
	if err != nil {
		resultLabel = "terminal"
		return nil, err
	}
	inframetrics.ObserveTongyiUsage(operation, embedding.Usage.InputTokens, embedding.Usage.ImageTokens, embedding.Usage.TextTokens, embedding.Usage.OutputTokens)
	return embedding, nil
}

func validateTongyiResponse(response tongyiResponse, expectedType string) (*TongyiEmbedding, error) {
	if len(response.Output.Embeddings) != 1 {
		return nil, tongyiError(false, "response_count", 0)
	}
	item := response.Output.Embeddings[0]
	if item.Index != 0 || strings.ToLower(strings.TrimSpace(item.Type)) != expectedType ||
		len(item.Embedding) != TongyiDimension {
		return nil, tongyiError(false, "response_shape", 0)
	}
	var norm float64
	for _, value := range item.Embedding {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, tongyiError(false, "response_vector", 0)
		}
		norm = math.Hypot(norm, value)
	}
	if norm == 0 {
		return nil, tongyiError(false, "response_vector", 0)
	}
	values := make([]float64, len(item.Embedding))
	for index, value := range item.Embedding {
		values[index] = value / norm
	}
	usage := TongyiUsage{
		InputTokens:  response.Usage.InputTokens,
		ImageTokens:  response.Usage.InputTokensDetail.ImageTokens,
		TextTokens:   response.Usage.InputTokensDetail.TextTokens,
		OutputTokens: response.Usage.OutputTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
	if usage.InputTokens <= 0 || usage.ImageTokens < 0 || usage.TextTokens < 0 || usage.OutputTokens < 0 ||
		usage.TotalTokens < usage.InputTokens || usage.InputTokens > 1_000_000_000 ||
		usage.ImageTokens > usage.InputTokens || usage.TextTokens > usage.InputTokens {
		return nil, tongyiError(false, "response_usage", 0)
	}
	return &TongyiEmbedding{Values: values, Usage: usage}, nil
}

func validateTongyiAdapterConfig(config TongyiAdapterConfig) error {
	if strings.TrimSpace(config.ListenAddress) == "" || len(strings.TrimSpace(config.FruxHMACSecret)) < 32 ||
		len(strings.TrimSpace(config.FruxHMACSecret)) > 512 || strings.TrimSpace(config.DashScopeAPIKey) == "" ||
		len(strings.TrimSpace(config.DashScopeAPIKey)) > 4096 ||
		config.UpstreamTimeout < 100*time.Millisecond || config.UpstreamTimeout > 2*time.Minute ||
		config.MaxInboundRequestBytes < 1<<20 || config.MaxInboundRequestBytes > 96<<20 ||
		config.MaxUpstreamResponseBytes < 64<<10 || config.MaxUpstreamResponseBytes > 8<<20 ||
		config.ShutdownTimeout < time.Second || config.ShutdownTimeout > time.Minute {
		return ErrInvalidTongyiAdapterConfig
	}
	if _, _, err := net.SplitHostPort(config.ListenAddress); err != nil {
		return ErrInvalidTongyiAdapterConfig
	}
	endpoint, err := url.Parse(strings.TrimSpace(config.DashScopeEndpoint))
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return ErrInvalidTongyiAdapterConfig
	}
	return nil
}

func tongyiEnvDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, ErrInvalidTongyiAdapterConfig)
	}
	return value, nil
}

func tongyiEnvInt64(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, ErrInvalidTongyiAdapterConfig)
	}
	return value, nil
}

func tongyiError(retryable bool, code string, retryAfter time.Duration) error {
	return &TongyiUpstreamError{
		Retryable: retryable, RetryAfter: retryAfter, Code: strings.ToLower(strings.TrimSpace(code)),
	}
}
