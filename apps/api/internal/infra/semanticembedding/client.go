package infrasemanticembedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
)

var semanticItemIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

const (
	maxMetadataBytes  = 16 << 10
	maxEmbeddingBytes = 1 << 20
	maxRequestBytes   = 128 << 10
)

type Client struct {
	baseURL         string
	token           string
	metadataTimeout time.Duration
	requestTimeout  time.Duration
	httpClient      *http.Client
	slots           chan struct{}
}

type modelMetadata struct {
	Model             string `json:"model"`
	Revision          string `json:"revision"`
	Dimension         int    `json:"dimension"`
	MaxSequenceTokens int    `json:"max_sequence_tokens"`
	DType             string `json:"dtype"`
	Normalized        bool   `json:"normalized"`
	Device            string `json:"device"`
	Limits            struct {
		MaxBatchSize             int `json:"max_batch_size"`
		MaxTitleCodepoints       int `json:"max_title_codepoints"`
		MaxDescriptionCodepoints int `json:"max_description_codepoints"`
		MaxTotalCodepoints       int `json:"max_total_codepoints"`
		MaxRequestBytes          int `json:"max_request_bytes"`
	} `json:"limits"`
}

type embeddingRequest struct {
	Items []embeddingRequestItem `json:"items"`
}

type embeddingRequestItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type embeddingResponse struct {
	Model     string `json:"model"`
	Revision  string `json:"revision"`
	Dimension int    `json:"dimension"`
	Items     []struct {
		ID        string    `json:"id"`
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"items"`
}

func New(cfg infraconfig.SemanticEmbeddingConfig, token string) (*Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	baseURL := strings.TrimSpace(strings.TrimSuffix(cfg.BaseURL, "/"))
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") ||
		!infraconfig.ValidStrongInternalToken(token) {
		return nil, infraconfig.ErrInvalidSemanticEmbeddingConfig
	}
	metadataTimeout, err := time.ParseDuration(cfg.MetadataTimeout)
	if err != nil {
		return nil, infraconfig.ErrInvalidSemanticEmbeddingConfig
	}
	requestTimeout, err := time.ParseDuration(cfg.RequestTimeout)
	if err != nil {
		return nil, infraconfig.ErrInvalidSemanticEmbeddingConfig
	}
	if metadataTimeout < 500*time.Millisecond || metadataTimeout > 5*time.Second ||
		requestTimeout < time.Second || requestTimeout > 20*time.Second {
		return nil, infraconfig.ErrInvalidSemanticEmbeddingConfig
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: time.Second, KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: time.Second,
		DisableCompression:  true,
		MaxConnsPerHost:     2, MaxIdleConnsPerHost: 2, MaxIdleConns: 2,
		IdleConnTimeout: 30 * time.Second,
	}
	return &Client{
		baseURL: baseURL, token: strings.Trim(token, " "),
		metadataTimeout: metadataTimeout, requestTimeout: requestTimeout,
		httpClient: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		slots: make(chan struct{}, 2),
	}, nil
}

func (c *Client) ValidateMetadata(ctx context.Context) error {
	start := time.Now()
	result := applicationembedding.SemanticInternal
	defer func() {
		inframetrics.ObserveSemanticClient("metadata", string(result), time.Since(start))
	}()
	if c == nil {
		result = applicationembedding.SemanticUnavailable
		return semanticError(result, false, errors.New("semantic client unavailable"))
	}
	requestContext, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	response, err := c.do(requestContext, http.MethodGet, "/internal/v1/model", nil)
	if err != nil {
		result = semanticResult(err)
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result = statusResult(response.StatusCode)
		return semanticError(result, result == applicationembedding.SemanticContract, errors.New("metadata rejected"))
	}
	var metadata modelMetadata
	if err := decodeBounded(response.Body, maxMetadataBytes, &metadata); err != nil ||
		!validMetadata(metadata) {
		result = applicationembedding.SemanticContract
		return semanticError(result, true, errors.New("metadata contract mismatch"))
	}
	result = applicationembedding.SemanticSuccess
	return nil
}

func (c *Client) Generate(
	ctx context.Context,
	items []applicationembedding.SemanticInput,
) ([][]float64, error) {
	start := time.Now()
	result := applicationembedding.SemanticInternal
	defer func() {
		inframetrics.ObserveSemanticClient("embed", string(result), time.Since(start))
	}()
	if c == nil || len(items) == 0 || len(items) > 32 {
		result = applicationembedding.SemanticContract
		return nil, semanticError(result, true, errors.New("invalid semantic batch"))
	}
	requestBody := embeddingRequest{Items: make([]embeddingRequestItem, 0, len(items))}
	seen := make(map[string]struct{}, len(items))
	totalCodepoints := 0
	for _, item := range items {
		if !semanticItemIDPattern.MatchString(item.ID) {
			result = applicationembedding.SemanticContract
			return nil, semanticError(result, true, errors.New("invalid semantic item"))
		}
		if _, exists := seen[item.ID]; exists {
			result = applicationembedding.SemanticContract
			return nil, semanticError(result, true, errors.New("duplicate semantic item"))
		}
		seen[item.ID] = struct{}{}
		title, description, _, err := domainembedding.CanonicalVideoText(
			item.Title, item.Description,
		)
		if err != nil {
			result = applicationembedding.SemanticContract
			return nil, semanticError(result, true, err)
		}
		totalCodepoints += len([]rune(title)) + len([]rune(description))
		if totalCodepoints > 16384 {
			result = applicationembedding.SemanticContract
			return nil, semanticError(result, true, errors.New("semantic batch text too large"))
		}
		requestBody.Items = append(requestBody.Items, embeddingRequestItem{
			ID: item.ID, Title: title, Description: description,
		})
	}
	body, err := json.Marshal(requestBody)
	if err != nil || len(body) > maxRequestBytes {
		result = applicationembedding.SemanticContract
		return nil, semanticError(result, true, errors.New("semantic request too large"))
	}
	requestContext, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	response, err := c.do(
		requestContext, http.MethodPost, "/internal/v1/embeddings", bytes.NewReader(body),
	)
	if err != nil {
		result = semanticResult(err)
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result = statusResult(response.StatusCode)
		return nil, semanticError(result, false, errors.New("semantic request rejected"))
	}
	var decoded embeddingResponse
	if err := decodeBounded(response.Body, maxEmbeddingBytes, &decoded); err != nil ||
		decoded.Model != domainembedding.SemanticModelName ||
		decoded.Revision != domainembedding.SemanticRevision ||
		decoded.Dimension != domainembedding.SemanticDimension ||
		len(decoded.Items) != len(items) {
		result = applicationembedding.SemanticContract
		return nil, semanticError(result, true, errors.New("semantic response contract mismatch"))
	}
	vectors := make([][]float64, len(items))
	for index, output := range decoded.Items {
		if output.ID != items[index].ID || output.Index != index {
			result = applicationembedding.SemanticContract
			return nil, semanticError(result, true, errors.New("semantic response order mismatch"))
		}
		vector, err := domainembedding.NormalizeSemanticVector(output.Embedding)
		if err != nil {
			result = applicationembedding.SemanticContract
			return nil, semanticError(result, true, err)
		}
		vectors[index] = vector
	}
	result = applicationembedding.SemanticSuccess
	return vectors, nil
}

func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
) (*http.Response, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, semanticError(semanticResult(ctx.Err()), false, ctx.Err())
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, semanticError(applicationembedding.SemanticInternal, false, err)
	}
	request.Header.Set("X-Internal-Token", c.token)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, semanticError(semanticResult(err), false, err)
	}
	return response, nil
}

func (c *Client) Close() {
	if c == nil || c.httpClient == nil {
		return
	}
	c.httpClient.CloseIdleConnections()
}

func decodeBounded(reader io.Reader, limit int64, target any) error {
	limited := &io.LimitedReader{R: reader, N: limit + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if limited.N <= 0 {
		return errors.New("semantic response too large")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing semantic response")
		}
		return err
	}
	return nil
}

func validMetadata(metadata modelMetadata) bool {
	return metadata.Model == domainembedding.SemanticModelName &&
		metadata.Revision == domainembedding.SemanticRevision &&
		metadata.Dimension == domainembedding.SemanticDimension &&
		metadata.MaxSequenceTokens == 128 &&
		metadata.DType == "float32" && metadata.Normalized &&
		strings.EqualFold(metadata.Device, "cpu") &&
		metadata.Limits.MaxBatchSize == 32 &&
		metadata.Limits.MaxTitleCodepoints == 200 &&
		metadata.Limits.MaxDescriptionCodepoints == 2000 &&
		metadata.Limits.MaxTotalCodepoints == 16384 &&
		metadata.Limits.MaxRequestBytes == maxRequestBytes
}

func statusResult(status int) applicationembedding.SemanticResult {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return applicationembedding.SemanticAuth
	case http.StatusTooManyRequests:
		return applicationembedding.SemanticOverCapacity
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return applicationembedding.SemanticTimeout
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		return applicationembedding.SemanticUnavailable
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return applicationembedding.SemanticContract
	default:
		return applicationembedding.SemanticInternal
	}
}

func semanticResult(err error) applicationembedding.SemanticResult {
	var semanticErr *applicationembedding.SemanticError
	if errors.As(err, &semanticErr) {
		return semanticErr.Result
	}
	if errors.Is(err, context.Canceled) {
		return applicationembedding.SemanticCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return applicationembedding.SemanticTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return applicationembedding.SemanticTimeout
	}
	return applicationembedding.SemanticUnavailable
}

func semanticError(
	result applicationembedding.SemanticResult,
	terminal bool,
	err error,
) error {
	return &applicationembedding.SemanticError{Result: result, Terminal: terminal, Err: err}
}

var _ applicationembedding.SemanticGenerator = (*Client)(nil)
