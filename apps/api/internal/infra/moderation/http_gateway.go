package inframoderation

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

const (
	maxGatewayResponseBytes = 256 << 10
	maxGatewayClockSkew     = 5 * time.Minute
)

type HTTPGateway struct {
	endpoint           string
	secret             []byte
	client             *http.Client
	now                func() time.Time
	allowInsecureLocal bool
}

type gatewayOptions struct {
	allowInsecureLocal bool
}

type GatewayOption func(*gatewayOptions)

func WithInsecureLocalGateway() GatewayOption {
	return func(options *gatewayOptions) {
		options.allowInsecureLocal = true
	}
}

type gatewayRequest struct {
	JobID                  int64           `json:"job_id"`
	CaseID                 int64           `json:"case_id"`
	VideoID                int64           `json:"video_id"`
	ReviewVersion          int             `json:"review_version"`
	RequestedPolicyVersion int             `json:"requested_policy_version"`
	RequestID              string          `json:"request_id"`
	Metadata               gatewayMetadata `json:"metadata"`
	Frames                 []gatewayFrame  `json:"frames"`
}

type gatewayMetadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type gatewayFrame struct {
	TimestampMS int64  `json:"timestamp_ms"`
	SHA256      string `json:"sha256"`
	URL         string `json:"url"`
}

type gatewayResponse struct {
	RequestID    string                  `json:"request_id"`
	Provider     string                  `json:"provider"`
	ModelVersion string                  `json:"model_version"`
	GeneratedAt  time.Time               `json:"generated_at"`
	Signals      []gatewayResponseSignal `json:"signals"`
}

type gatewayResponseSignal struct {
	Label             string  `json:"label"`
	Confidence        float64 `json:"confidence"`
	FrameTimestampsMS []int64 `json:"frame_timestamps_ms"`
}

func NewHTTPGateway(
	endpoint, secret string,
	timeout time.Duration,
	options ...GatewayOption,
) (*HTTPGateway, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	secret = strings.TrimSpace(secret)
	if endpoint == "" || len(secret) < 32 || timeout <= 0 || timeout > 30*time.Second {
		return nil, errors.New("invalid moderation gateway configuration")
	}
	settings := gatewayOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid moderation gateway configuration")
	}
	if parsed.Scheme != "https" &&
		(parsed.Scheme != "http" || !settings.allowInsecureLocal ||
			!localGatewayHost(parsed.Hostname())) {
		return nil, errors.New("invalid moderation gateway transport")
	}
	return &HTTPGateway{
		endpoint: endpoint, secret: []byte(secret),
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now:                func() time.Time { return time.Now().UTC() },
		allowInsecureLocal: settings.allowInsecureLocal,
	}, nil
}

func localGatewayHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}

func (g *HTTPGateway) Evaluate(
	ctx context.Context,
	input applicationreview.ModerationProviderRequest,
) (*applicationreview.ModerationProviderResult, error) {
	requestPayload, frameTimestamps, err := validateAndBuildRequest(
		input, g.allowInsecureLocal,
	)
	if err != nil {
		return nil, providerError("invalid_request", false, err)
	}
	body, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, providerError("request_encode", false, err)
	}
	timestamp := g.now().UTC().Truncate(time.Second).Format(time.RFC3339)
	signature := requestSignature(g.secret, timestamp, input.RequestID, body)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, providerError("request_create", false, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Frux-Request-ID", input.RequestID)
	request.Header.Set("X-Frux-Timestamp", timestamp)
	request.Header.Set("X-Frux-Signature", signature)

	response, err := g.client.Do(request)
	if err != nil {
		return nil, providerError("transport", true, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayResponseBytes+1))
	if err != nil {
		return nil, providerError("response_read", true, err)
	}
	if len(responseBody) > maxGatewayResponseBytes {
		return nil, providerError("response_oversized", true, errors.New("moderation response exceeds limit"))
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, providerError("provider_retryable_status", true, fmt.Errorf("status %d", response.StatusCode))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, providerError("provider_rejected", false, fmt.Errorf("status %d", response.StatusCode))
	}
	if !verifyResponseSignature(
		g.secret, input.RequestID, responseBody,
		response.Header.Get("X-Frux-Response-Signature"),
	) {
		return nil, providerError("response_signature", true, errors.New("invalid moderation response signature"))
	}
	var decoded gatewayResponse
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, providerError("response_invalid", true, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, providerError("response_invalid", true, err)
	}
	result, err := validateGatewayResponse(decoded, input.RequestID, frameTimestamps, g.now())
	if err != nil {
		return nil, providerError("response_invalid", true, err)
	}
	return result, nil
}

func validateAndBuildRequest(
	input applicationreview.ModerationProviderRequest,
	allowInsecureLocal bool,
) (gatewayRequest, map[int64]struct{}, error) {
	if input.JobID <= 0 || input.CaseID <= 0 || input.VideoID <= 0 ||
		input.ReviewVersion <= 0 || input.RequestedPolicyVersion <= 0 ||
		strings.TrimSpace(input.RequestID) == "" ||
		len(input.RequestID) > domainreview.MaxResultIdentityLength ||
		len(input.Title) > 128 || len(input.Description) > 512 ||
		len(input.Frames) == 0 || len(input.Frames) > domainreview.MaxModerationFrames {
		return gatewayRequest{}, nil, domainreview.ErrInvalidModerationInput
	}
	timestamps := make(map[int64]struct{}, len(input.Frames))
	frames := make([]gatewayFrame, 0, len(input.Frames))
	for _, frame := range input.Frames {
		frameURL, err := url.Parse(strings.TrimSpace(frame.URL))
		if err != nil || frameURL.Host == "" || frameURL.User != nil ||
			frameURL.Fragment != "" ||
			(frameURL.Scheme != "https" &&
				(frameURL.Scheme != "http" || !allowInsecureLocal ||
					!localGatewayHost(frameURL.Hostname()))) ||
			frame.TimestampMS < 0 || len(frame.SHA256) != 64 ||
			frame.ExpiresAt.IsZero() {
			return gatewayRequest{}, nil, domainreview.ErrInvalidModerationInput
		}
		if _, duplicate := timestamps[frame.TimestampMS]; duplicate {
			return gatewayRequest{}, nil, domainreview.ErrInvalidModerationInput
		}
		timestamps[frame.TimestampMS] = struct{}{}
		frames = append(frames, gatewayFrame{
			TimestampMS: frame.TimestampMS, SHA256: frame.SHA256, URL: frame.URL,
		})
	}
	return gatewayRequest{
		JobID: input.JobID, CaseID: input.CaseID, VideoID: input.VideoID,
		ReviewVersion:          input.ReviewVersion,
		RequestedPolicyVersion: input.RequestedPolicyVersion,
		RequestID:              strings.TrimSpace(input.RequestID),
		Metadata:               gatewayMetadata{Title: input.Title, Description: input.Description},
		Frames:                 frames,
	}, timestamps, nil
}

func validateGatewayResponse(
	response gatewayResponse,
	requestID string,
	frameTimestamps map[int64]struct{},
	now time.Time,
) (*applicationreview.ModerationProviderResult, error) {
	response.RequestID = strings.TrimSpace(response.RequestID)
	response.Provider = strings.ToLower(strings.TrimSpace(response.Provider))
	response.ModelVersion = strings.TrimSpace(response.ModelVersion)
	if response.RequestID != requestID ||
		!domainreview.ValidProviderIdentifier(response.Provider) ||
		!domainreview.ValidModelVersion(response.ModelVersion) ||
		response.GeneratedAt.IsZero() ||
		response.GeneratedAt.After(now.UTC().Add(maxGatewayClockSkew)) ||
		len(response.Signals) == 0 || len(response.Signals) > domainreview.MaxSignalsPerResult {
		return nil, errors.New("invalid moderation response envelope")
	}
	result := &applicationreview.ModerationProviderResult{
		Provider: response.Provider, ModelVersion: response.ModelVersion,
		GeneratedAt: response.GeneratedAt.UTC().Truncate(time.Microsecond),
	}
	for _, signal := range response.Signals {
		label := domainreview.NormalizeLabel(signal.Label)
		if label == "" || len(label) > domainreview.MaxSignalLabelLength ||
			math.IsNaN(signal.Confidence) || math.IsInf(signal.Confidence, 0) ||
			signal.Confidence < 0 || signal.Confidence > 1 ||
			len(signal.FrameTimestampsMS) > domainreview.MaxEvidenceRefs {
			return nil, errors.New("invalid moderation signal")
		}
		seen := make(map[int64]struct{}, len(signal.FrameTimestampsMS))
		timestamps := make([]int64, 0, len(signal.FrameTimestampsMS))
		for _, timestamp := range signal.FrameTimestampsMS {
			if _, allowed := frameTimestamps[timestamp]; !allowed {
				return nil, errors.New("unknown moderation evidence timestamp")
			}
			if _, duplicate := seen[timestamp]; duplicate {
				return nil, errors.New("duplicate moderation evidence timestamp")
			}
			seen[timestamp] = struct{}{}
			timestamps = append(timestamps, timestamp)
		}
		result.Signals = append(result.Signals, applicationreview.ModerationProviderSignal{
			Label: label, Confidence: signal.Confidence, FrameTimestampsMS: timestamps,
		})
	}
	return result, nil
}

func requestSignature(secret []byte, timestamp, requestID string, body []byte) string {
	sum := sha256.Sum256(body)
	return sign(secret, timestamp+"\n"+requestID+"\n"+hex.EncodeToString(sum[:]))
}

func responseSignature(secret []byte, requestID string, body []byte) string {
	sum := sha256.Sum256(body)
	return sign(secret, requestID+"\n"+hex.EncodeToString(sum[:]))
}

func verifyResponseSignature(secret []byte, requestID string, body []byte, signature string) bool {
	expected := responseSignature(secret, requestID, body)
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}

func sign(secret []byte, value string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func providerError(code string, retryable bool, err error) error {
	return &applicationreview.ModerationProviderError{
		Code: code, Retryable: retryable, Err: err,
	}
}
