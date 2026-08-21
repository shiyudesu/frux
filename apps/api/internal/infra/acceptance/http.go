package infraacceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxAcceptanceJSONRequestBytes = 1 << 20

type HTTPFailureCode string

const (
	HTTPFailureRequest   HTTPFailureCode = "request"
	HTTPFailureTransport HTTPFailureCode = "transport"
	HTTPFailureStatus    HTTPFailureCode = "status"
	HTTPFailureRead      HTTPFailureCode = "read"
	HTTPFailureOversize  HTTPFailureCode = "oversize"
	HTTPFailureDecode    HTTPFailureCode = "decode"
)

type HTTPError struct {
	Code      HTTPFailureCode
	Retryable bool
	Status    int
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "acceptance HTTP failure"
	}
	return "acceptance HTTP failure: " + string(e.Code)
}

type HTTPClient struct {
	client           *http.Client
	maxResponseBytes int64
}

func NewHTTPClient(timeout time.Duration, maxResponseBytes int64, base *http.Client) (*HTTPClient, error) {
	if timeout < time.Second || timeout > 2*time.Minute || maxResponseBytes < 64<<10 || maxResponseBytes > 8<<20 {
		return nil, ErrInvalidAcceptanceConfig
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if base != nil && base.Transport != nil {
		configured, ok := base.Transport.(*http.Transport)
		if !ok {
			return nil, ErrInvalidAcceptanceConfig
		}
		transport = configured.Clone()
	}
	transport.MaxIdleConns = 8
	transport.MaxIdleConnsPerHost = 4
	transport.MaxConnsPerHost = 8
	return &HTTPClient{
		client: &http.Client{
			Transport: transport, Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		maxResponseBytes: maxResponseBytes,
	}, nil
}

func (c *HTTPClient) JSON(
	ctx context.Context,
	method string,
	endpoint string,
	bearer string,
	input any,
	output any,
) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil || len(encoded) > maxAcceptanceJSONRequestBytes {
			return &HTTPError{Code: HTTPFailureRequest}
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return &HTTPError{Code: HTTPFailureRequest}
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(bearer) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
	}
	responseBody, err := c.do(request)
	if err != nil {
		return err
	}
	if output == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return &HTTPError{Code: HTTPFailureDecode}
	}
	return nil
}

func (c *HTTPClient) Text(ctx context.Context, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &HTTPError{Code: HTTPFailureRequest}
	}
	return c.do(request)
}

func (c *HTTPClient) CheckHealth(ctx context.Context, baseEndpoint string) error {
	baseEndpoint = strings.TrimRight(strings.TrimSpace(baseEndpoint), "/")
	if baseEndpoint == "" {
		return &HTTPError{Code: HTTPFailureRequest}
	}
	var response map[string]any
	if err := c.JSON(ctx, http.MethodGet, baseEndpoint+"/health", "", nil, &response); err != nil {
		return err
	}
	if len(response) == 0 {
		return &HTTPError{Code: HTTPFailureDecode}
	}
	return nil
}

func (c *HTTPClient) CollectMetrics(ctx context.Context, endpoint string) (MetricSnapshot, error) {
	content, err := c.Text(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return ParseMetricSnapshot(content)
}

func (c *HTTPClient) do(request *http.Request) ([]byte, error) {
	if c == nil || c.client == nil || request == nil {
		return nil, &HTTPError{Code: HTTPFailureRequest}
	}
	response, err := c.client.Do(request)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		return nil, &HTTPError{Code: HTTPFailureTransport, Retryable: true}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return nil, &HTTPError{Code: HTTPFailureRead, Retryable: true}
	}
	if int64(len(body)) > c.maxResponseBytes {
		return nil, &HTTPError{Code: HTTPFailureOversize}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		retryable := response.StatusCode == http.StatusRequestTimeout ||
			response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError
		return nil, &HTTPError{Code: HTTPFailureStatus, Status: response.StatusCode, Retryable: retryable}
	}
	return body, nil
}

func IsHTTPError(err error, code HTTPFailureCode) bool {
	var failure *HTTPError
	return errors.As(err, &failure) && failure.Code == code
}
