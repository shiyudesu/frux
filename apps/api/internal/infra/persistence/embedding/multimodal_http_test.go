package infraembedding

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
)

const multimodalTestSecret = "multimodal-http-test-secret-value-123"

type multimodalHTTPObservation struct {
	operation string
	result    string
}

type multimodalHTTPObserverSpy struct {
	mutex        sync.Mutex
	observations []multimodalHTTPObservation
}

func (s *multimodalHTTPObserverSpy) ObserveMultimodalHTTP(operation, result string, _ time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.observations = append(s.observations, multimodalHTTPObservation{operation: operation, result: result})
}

type multimodalTestResponse struct {
	status  int
	body    []byte
	signed  bool
	headers map[string]string
	delay   time.Duration
}

type multimodalTestResponder func(path, operationID string, body []byte) multimodalTestResponse

func TestHTTPMultimodalProviderConformance(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	var capturedVideo []byte
	var capturedQuery []byte
	server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(path, operationID string, body []byte) multimodalTestResponse {
		switch path {
		case multimodalReadyPath:
			return signedMultimodalTestJSON(http.StatusOK, multimodalReadyResponse{
				ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID, Ready: true,
				Capabilities: []string{MultimodalProviderCapabilityVideo, MultimodalProviderCapabilityQuery},
				Contract:     multimodalContractToEnvelope(contract),
			})
		case multimodalVideoPath:
			capturedVideo = append([]byte(nil), body...)
		case multimodalQueryPath:
			capturedQuery = append([]byte(nil), body...)
		default:
			t.Fatalf("unexpected path %q", path)
		}
		var request struct {
			SourceHash string `json:"source_hash"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		vector := multimodalHTTPTestVector(contract.Dimension)
		return signedMultimodalTestJSON(http.StatusOK, multimodalEmbeddingResponse{
			ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID,
			Contract: multimodalContractToEnvelope(contract), SourceHash: request.SourceHash,
			VectorDigest: domainembedding.MultimodalVectorDigest(vector), Vector: vector,
		})
	})
	defer server.Close()

	observer := &multimodalHTTPObserverSpy{}
	provider := newMultimodalHTTPTestProvider(t, server.URL, contract, observer)
	if err := provider.CheckReady(context.Background(), MultimodalProviderCapabilityVideo); err != nil {
		t.Fatal(err)
	}
	if err := provider.CheckReady(context.Background(), MultimodalProviderCapabilityQuery); err != nil {
		t.Fatal(err)
	}
	imageContent := []byte("prepared-public-image")
	imageDigest := sha256.Sum256(imageContent)
	videoRequest, err := applicationembedding.NewMultimodalVideoEmbeddingRequest(
		contract,
		applicationembedding.MultimodalPublicVideoContent{
			Title: "public title", Description: "public description",
			Published: true, Public: true, MediaReady: true, SourceCurrent: true,
		},
		128,
		[]applicationembedding.PreparedMultimodalImage{{
			MIMEType: "image/jpeg", Width: 32, Height: 32,
			Digest: hex.EncodeToString(imageDigest[:]), Content: imageContent,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	videoResult, err := provider.EmbedVideoContent(context.Background(), videoRequest)
	if err != nil || videoResult.Identity.SourceHash != videoRequest.SourceHash || len(videoResult.Vector) != contract.Dimension {
		t.Fatalf("video result=%#v err=%v", videoResult, err)
	}
	queryRequest, err := applicationembedding.NewMultimodalQueryEmbeddingRequest(contract, "public query", 64)
	if err != nil {
		t.Fatal(err)
	}
	queryResult, err := provider.EmbedQueryText(context.Background(), queryRequest)
	if err != nil || queryResult.Identity.SourceHash != queryRequest.SourceHash || len(queryResult.Vector) != contract.Dimension {
		t.Fatalf("query result=%#v err=%v", queryResult, err)
	}

	videoText := string(capturedVideo)
	for _, forbidden := range []string{"storage_url", "signed_url", "credential", "user_id", "session_id", "behavior"} {
		if strings.Contains(videoText, forbidden) {
			t.Fatalf("video payload contains forbidden field %q: %s", forbidden, videoText)
		}
	}
	if !strings.Contains(videoText, `"content_base64"`) || !strings.Contains(videoText, `"text":"public title\npublic description"`) {
		t.Fatalf("video payload missing bounded content: %s", videoText)
	}
	queryText := string(capturedQuery)
	if !strings.Contains(queryText, `"query":"public query"`) {
		t.Fatalf("query payload=%s", queryText)
	}
	for _, forbidden := range []string{"user_id", "session_id", "request_id", "credential", "token"} {
		if strings.Contains(queryText, forbidden) {
			t.Fatalf("query payload contains forbidden field %q: %s", forbidden, queryText)
		}
	}
	observer.mutex.Lock()
	defer observer.mutex.Unlock()
	wantObservations := []multimodalHTTPObservation{
		{operation: "readiness", result: "success"},
		{operation: "readiness", result: "success"},
		{operation: "video", result: "success"},
		{operation: "query", result: "success"},
	}
	if len(observer.observations) != len(wantObservations) {
		t.Fatalf("observations=%v", observer.observations)
	}
	for index := range wantObservations {
		if observer.observations[index] != wantObservations[index] {
			t.Fatalf("observation[%d]=%v want=%v", index, observer.observations[index], wantObservations[index])
		}
	}
}

func TestHTTPMultimodalProviderRejectsInvalidConfiguration(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	base := MultimodalHTTPProviderConfig{
		Endpoint: "https://provider.example.com", HMACSecret: multimodalTestSecret,
		ProtocolVersion: MultimodalProviderProtocolV1, Timeout: time.Second,
		MaxRequestBytes: 2 << 20, MaxResponseBytes: 1 << 20,
		MaxVideoTextRunes: 128, MaxQueryRunes: 64, MaxImages: 4,
		MaxImageBytes: 64 << 10, MaxTotalImageBytes: 64 << 10, MaxImagePixels: 4_000_000,
		AllowedMIMETypes: []string{"image/jpeg"},
	}
	tests := []struct {
		name   string
		mutate func(*MultimodalHTTPProviderConfig)
	}{
		{name: "remote http", mutate: func(c *MultimodalHTTPProviderConfig) {
			c.Endpoint = "http://provider.example.com"
			c.AllowInsecureLocal = true
		}},
		{name: "endpoint userinfo", mutate: func(c *MultimodalHTTPProviderConfig) { c.Endpoint = "https://user@provider.example.com" }},
		{name: "endpoint query", mutate: func(c *MultimodalHTTPProviderConfig) { c.Endpoint = "https://provider.example.com?token=x" }},
		{name: "short secret", mutate: func(c *MultimodalHTTPProviderConfig) { c.HMACSecret = "short" }},
		{name: "unknown protocol", mutate: func(c *MultimodalHTTPProviderConfig) { c.ProtocolVersion = "v2" }},
		{name: "short timeout", mutate: func(c *MultimodalHTTPProviderConfig) { c.Timeout = 10 * time.Millisecond }},
		{name: "small request", mutate: func(c *MultimodalHTTPProviderConfig) { c.MaxRequestBytes = 100 }},
		{name: "small response", mutate: func(c *MultimodalHTTPProviderConfig) { c.MaxResponseBytes = 100 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			test.mutate(&config)
			if _, err := NewHTTPMultimodalProvider(config, contract); !errors.Is(err, ErrInvalidMultimodalHTTPProvider) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestHTTPMultimodalProviderReadinessRejectsMismatch(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	tests := []struct {
		name   string
		mutate func(*multimodalReadyResponse)
	}{
		{name: "not ready", mutate: func(r *multimodalReadyResponse) { r.Ready = false }},
		{name: "protocol", mutate: func(r *multimodalReadyResponse) { r.ProtocolVersion = "v2" }},
		{name: "operation id", mutate: func(r *multimodalReadyResponse) { r.OperationID = "other" }},
		{name: "capability", mutate: func(r *multimodalReadyResponse) { r.Capabilities = []string{MultimodalProviderCapabilityVideo} }},
		{name: "contract", mutate: func(r *multimodalReadyResponse) { r.Contract.RevisionAlias = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, operationID string, _ []byte) multimodalTestResponse {
				response := multimodalReadyResponse{
					ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID, Ready: true,
					Capabilities: []string{MultimodalProviderCapabilityVideo, MultimodalProviderCapabilityQuery},
					Contract:     multimodalContractToEnvelope(contract),
				}
				test.mutate(&response)
				return signedMultimodalTestJSON(http.StatusOK, response)
			})
			defer server.Close()
			provider := newMultimodalHTTPTestProvider(t, server.URL, contract, nil)
			if err := provider.CheckReady(context.Background(), MultimodalProviderCapabilityQuery); err == nil {
				t.Fatal("expected readiness error")
			}
		})
	}
}

func TestHTTPMultimodalProviderRejectsInvalidEmbeddingResponses(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	queryRequest, err := applicationembedding.NewMultimodalQueryEmbeddingRequest(contract, "private marker 93bf", 64)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*multimodalEmbeddingResponse)
		raw    []byte
		signed bool
	}{
		{name: "operation id", mutate: func(r *multimodalEmbeddingResponse) { r.OperationID = "other" }, signed: true},
		{name: "source hash", mutate: func(r *multimodalEmbeddingResponse) { r.SourceHash = strings.Repeat("0", 64) }, signed: true},
		{name: "contract", mutate: func(r *multimodalEmbeddingResponse) { r.Contract.ModelAlias = "other" }, signed: true},
		{name: "digest", mutate: func(r *multimodalEmbeddingResponse) { r.VectorDigest = strings.Repeat("0", 64) }, signed: true},
		{name: "dimension", mutate: func(r *multimodalEmbeddingResponse) { r.Vector = r.Vector[:len(r.Vector)-1] }, signed: true},
		{name: "norm", mutate: func(r *multimodalEmbeddingResponse) { r.Vector[0] = 0.5 }, signed: true},
		{name: "unknown field", raw: []byte(`{"protocol_version":"frux-multimodal-v1","unknown":true}`), signed: true},
		{name: "malformed", raw: []byte(`{"protocol_version":`), signed: true},
		{name: "signature", signed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, operationID string, _ []byte) multimodalTestResponse {
				vector := multimodalHTTPTestVector(contract.Dimension)
				response := multimodalEmbeddingResponse{
					ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID,
					Contract: multimodalContractToEnvelope(contract), SourceHash: queryRequest.SourceHash,
					VectorDigest: domainembedding.MultimodalVectorDigest(vector), Vector: vector,
				}
				if test.mutate != nil {
					test.mutate(&response)
				}
				if test.raw != nil {
					return multimodalTestResponse{status: http.StatusOK, body: test.raw, signed: test.signed}
				}
				output := signedMultimodalTestJSON(http.StatusOK, response)
				output.signed = test.signed
				return output
			})
			defer server.Close()
			provider := newMultimodalHTTPTestProvider(t, server.URL, contract, nil)
			if _, err := provider.EmbedQueryText(context.Background(), queryRequest); err == nil {
				t.Fatal("expected invalid response error")
			}
		})
	}
}

func TestHTTPMultimodalProviderRejectsOutOfBoundsInputsBeforeNetwork(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, _ string, _ []byte) multimodalTestResponse {
		t.Error("invalid input reached provider")
		return multimodalTestResponse{}
	})
	defer server.Close()
	provider := newMultimodalHTTPTestProvider(t, server.URL, contract, nil)
	content := []byte("image")
	digest := sha256.Sum256(content)
	base := applicationembedding.MultimodalVideoEmbeddingRequest{
		Contract: contract, SourceHash: domainembedding.MultimodalSourceHash([]byte("source")), Text: "public",
		Images: []applicationembedding.PreparedMultimodalImage{{
			MIMEType: "image/jpeg", Width: 32, Height: 32,
			Digest: hex.EncodeToString(digest[:]), Content: content,
		}},
	}
	for _, test := range []struct {
		name   string
		mutate func(*applicationembedding.MultimodalVideoEmbeddingRequest)
	}{
		{name: "text", mutate: func(r *applicationembedding.MultimodalVideoEmbeddingRequest) { r.Text = strings.Repeat("字", 129) }},
		{name: "count", mutate: func(r *applicationembedding.MultimodalVideoEmbeddingRequest) {
			r.Images = append(r.Images, r.Images[0], r.Images[0], r.Images[0], r.Images[0])
		}},
		{name: "bytes", mutate: func(r *applicationembedding.MultimodalVideoEmbeddingRequest) {
			r.Images[0].Content = bytes.Repeat([]byte{1}, (64<<10)+1)
			sum := sha256.Sum256(r.Images[0].Content)
			r.Images[0].Digest = hex.EncodeToString(sum[:])
		}},
		{name: "pixels", mutate: func(r *applicationembedding.MultimodalVideoEmbeddingRequest) { r.Images[0].Width = 5_000_000 }},
		{name: "mime", mutate: func(r *applicationembedding.MultimodalVideoEmbeddingRequest) { r.Images[0].MIMEType = "image/png" }},
		{name: "digest", mutate: func(r *applicationembedding.MultimodalVideoEmbeddingRequest) {
			r.Images[0].Digest = strings.Repeat("0", 64)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := base.Clone()
			test.mutate(&request)
			if _, err := provider.EmbedVideoContent(context.Background(), request); err == nil {
				t.Fatal("expected invalid input error")
			}
		})
	}
	queryRequest, err := applicationembedding.NewMultimodalQueryEmbeddingRequest(contract, strings.Repeat("q", 65), 128)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.EmbedQueryText(context.Background(), queryRequest); err == nil {
		t.Fatal("expected oversized query error")
	}
}

func TestHTTPMultimodalProviderSignsConfiguredBasePath(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(path, operationID string, _ []byte) multimodalTestResponse {
		if path != "/models/frux/v1/ready" {
			t.Errorf("path=%q", path)
		}
		return signedMultimodalTestJSON(http.StatusOK, multimodalReadyResponse{
			ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID, Ready: true,
			Capabilities: []string{MultimodalProviderCapabilityQuery}, Contract: multimodalContractToEnvelope(contract),
		})
	})
	defer server.Close()
	provider := newMultimodalHTTPTestProvider(t, server.URL+"/models/frux", contract, nil)
	if err := provider.CheckReady(context.Background(), MultimodalProviderCapabilityQuery); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPMultimodalProviderMapsTransportFailures(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	queryRequest, err := applicationembedding.NewMultimodalQueryEmbeddingRequest(contract, "query", 64)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		status     int
		retryable  bool
		retryAfter string
		redirect   bool
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, retryable: true, retryAfter: "7"},
		{name: "server unavailable", status: http.StatusServiceUnavailable, retryable: true},
		{name: "terminal rejection", status: http.StatusUnprocessableEntity, retryable: false},
		{name: "redirect", status: http.StatusTemporaryRedirect, retryable: false, redirect: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, operationID string, _ []byte) multimodalTestResponse {
				if test.redirect {
					return multimodalTestResponse{
						status: test.status, body: []byte("redirect"), signed: true,
						headers: map[string]string{"Location": "/elsewhere"},
					}
				}
				body, _ := json.Marshal(multimodalErrorResponse{
					ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID, Code: "capacity",
				})
				return multimodalTestResponse{
					status: test.status, body: body, signed: true,
					headers: map[string]string{"Retry-After": test.retryAfter},
				}
			})
			defer server.Close()
			provider := newMultimodalHTTPTestProvider(t, server.URL, contract, nil)
			_, callErr := provider.EmbedQueryText(context.Background(), queryRequest)
			var providerErr *applicationembedding.MultimodalProviderError
			if !errors.As(callErr, &providerErr) || providerErr.Retryable != test.retryable {
				t.Fatalf("error=%v retryable=%v", callErr, providerErr)
			}
			if test.retryAfter != "" && providerErr.RetryAfter != 7*time.Second {
				t.Fatalf("retry after=%v", providerErr.RetryAfter)
			}
		})
	}
	t.Run("unreachable endpoint", func(t *testing.T) {
		provider := newMultimodalHTTPTestProvider(t, "http://127.0.0.1:1", contract, nil)
		_, callErr := provider.EmbedQueryText(context.Background(), queryRequest)
		var providerErr *applicationembedding.MultimodalProviderError
		if !errors.As(callErr, &providerErr) || !providerErr.Retryable {
			t.Fatalf("error=%v", callErr)
		}
	})
}

func TestHTTPMultimodalProviderBoundsCancellationAndPrivacy(t *testing.T) {
	contract := multimodalHTTPTestContract(t)
	queryRequest, err := applicationembedding.NewMultimodalQueryEmbeddingRequest(contract, "secret-looking-query", 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("timeout", func(t *testing.T) {
		server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, operationID string, _ []byte) multimodalTestResponse {
			vector := multimodalHTTPTestVector(contract.Dimension)
			response := signedMultimodalTestJSON(http.StatusOK, multimodalEmbeddingResponse{
				ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID,
				Contract: multimodalContractToEnvelope(contract), SourceHash: queryRequest.SourceHash,
				VectorDigest: domainembedding.MultimodalVectorDigest(vector), Vector: vector,
			})
			response.delay = 200 * time.Millisecond
			return response
		})
		defer server.Close()
		provider := newMultimodalHTTPTestProviderWithLimits(t, server.URL, contract, nil, 100*time.Millisecond, 2<<20, 1<<20)
		_, callErr := provider.EmbedQueryText(context.Background(), queryRequest)
		var providerErr *applicationembedding.MultimodalProviderError
		if !errors.As(callErr, &providerErr) || !providerErr.Retryable || strings.Contains(callErr.Error(), queryRequest.Query) {
			t.Fatalf("error=%v", callErr)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, operationID string, _ []byte) multimodalTestResponse {
			return signedMultimodalTestJSON(http.StatusOK, multimodalReadyResponse{
				ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID, Ready: true,
				Capabilities: []string{MultimodalProviderCapabilityQuery}, Contract: multimodalContractToEnvelope(contract),
			})
		})
		defer server.Close()
		provider := newMultimodalHTTPTestProvider(t, server.URL, contract, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, callErr := provider.EmbedQueryText(ctx, queryRequest)
		if !errors.Is(callErr, context.Canceled) {
			t.Fatalf("error=%v", callErr)
		}
	})
	t.Run("oversized request", func(t *testing.T) {
		server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, _ string, _ []byte) multimodalTestResponse {
			t.Fatal("oversized request reached provider")
			return multimodalTestResponse{}
		})
		defer server.Close()
		provider := newMultimodalHTTPTestProviderWithLimits(t, server.URL, contract, nil, time.Second, 1<<20, 1<<20)
		content := bytes.Repeat([]byte{1}, 800<<10)
		digest := sha256.Sum256(content)
		request := applicationembedding.MultimodalVideoEmbeddingRequest{
			Contract: contract, SourceHash: domainembedding.MultimodalSourceHash([]byte("source")), Text: "public",
			Images: []applicationembedding.PreparedMultimodalImage{{
				MIMEType: "image/jpeg", Width: 32, Height: 32,
				Digest: hex.EncodeToString(digest[:]), Content: content,
			}},
		}
		if _, callErr := provider.EmbedVideoContent(context.Background(), request); callErr == nil {
			t.Fatal("expected oversized request error")
		}
	})
	t.Run("oversized response", func(t *testing.T) {
		server := newMultimodalHTTPTestServer(t, multimodalTestSecret, func(_ string, _ string, _ []byte) multimodalTestResponse {
			return multimodalTestResponse{status: http.StatusOK, body: bytes.Repeat([]byte("x"), (64<<10)+1), signed: true}
		})
		defer server.Close()
		provider := newMultimodalHTTPTestProviderWithLimits(t, server.URL, contract, nil, time.Second, 2<<20, 64<<10)
		if _, callErr := provider.EmbedQueryText(context.Background(), queryRequest); callErr == nil {
			t.Fatal("expected oversized response error")
		}
	})
}

func multimodalHTTPTestContract(t testing.TB) domainembedding.MultimodalContractIdentity {
	t.Helper()
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
	return contract
}

func multimodalHTTPTestVector(dimension int) []float64 {
	vector := make([]float64, dimension)
	vector[0] = 1
	return vector
}

func newMultimodalHTTPTestProvider(
	t testing.TB,
	endpoint string,
	contract domainembedding.MultimodalContractIdentity,
	observer MultimodalHTTPObserver,
) *HTTPMultimodalProvider {
	t.Helper()
	return newMultimodalHTTPTestProviderWithLimits(t, endpoint, contract, observer, time.Second, 2<<20, 1<<20)
}

func newMultimodalHTTPTestProviderWithLimits(
	t testing.TB,
	endpoint string,
	contract domainembedding.MultimodalContractIdentity,
	observer MultimodalHTTPObserver,
	timeout time.Duration,
	maxRequestBytes int64,
	maxResponseBytes int64,
) *HTTPMultimodalProvider {
	t.Helper()
	provider, err := NewHTTPMultimodalProvider(MultimodalHTTPProviderConfig{
		Endpoint: endpoint, HMACSecret: multimodalTestSecret,
		ProtocolVersion: MultimodalProviderProtocolV1, AllowInsecureLocal: true,
		Timeout: timeout, MaxRequestBytes: maxRequestBytes, MaxResponseBytes: maxResponseBytes,
		MaxVideoTextRunes: 128, MaxQueryRunes: 64, MaxImages: 4,
		MaxImageBytes: 64 << 10, MaxTotalImageBytes: 64 << 10, MaxImagePixels: 4_000_000,
		AllowedMIMETypes: []string{"image/jpeg"},
		Observer:         observer,
	}, contract)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func newMultimodalHTTPTestServer(t testing.TB, secret string, responder multimodalTestResponder) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		operationID := request.Header.Get("X-Frux-Operation-ID")
		protocol := request.Header.Get("X-Frux-Multimodal-Protocol")
		timestamp := request.Header.Get("X-Frux-Timestamp")
		parsedTimestamp, timestampErr := time.Parse(time.RFC3339, timestamp)
		if request.Method != http.MethodPost || protocol != MultimodalProviderProtocolV1 ||
			len(operationID) != 32 || timestampErr != nil ||
			time.Since(parsedTimestamp).Abs() > maxMultimodalClockSkew ||
			!hmac.Equal(
				[]byte(request.Header.Get("X-Frux-Signature")),
				[]byte(multimodalTestRequestSignature([]byte(secret), protocol, request.Method, request.URL.Path, timestamp, operationID, body)),
			) {
			t.Errorf("invalid signed request headers path=%s", request.URL.Path)
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		response := responder(request.URL.Path, operationID, body)
		if response.delay > 0 {
			select {
			case <-request.Context().Done():
				return
			case <-time.After(response.delay):
			}
		}
		for key, value := range response.headers {
			writer.Header().Set(key, value)
		}
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		if response.signed {
			writer.Header().Set("X-Frux-Response-Signature", multimodalTestResponseSignature(
				[]byte(secret), MultimodalProviderProtocolV1, status, operationID, response.body,
			))
		}
		writer.WriteHeader(status)
		_, _ = writer.Write(response.body)
	}))
}

func signedMultimodalTestJSON(status int, value any) multimodalTestResponse {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return multimodalTestResponse{status: status, body: body, signed: true}
}

func multimodalTestRequestSignature(
	secret []byte,
	protocolVersion string,
	method string,
	path string,
	timestamp string,
	operationID string,
	body []byte,
) string {
	digest := sha256.Sum256(body)
	message := protocolVersion + "\n" + method + "\n" + path + "\n" + timestamp + "\n" + operationID + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func multimodalTestResponseSignature(
	secret []byte,
	protocolVersion string,
	status int,
	operationID string,
	body []byte,
) string {
	digest := sha256.Sum256(body)
	message := protocolVersion + "\n" + strconv.Itoa(status) + "\n" + operationID + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
