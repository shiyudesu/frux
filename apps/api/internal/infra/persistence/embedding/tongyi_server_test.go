package infraembedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
)

type tongyiEmbeddingClientStub struct {
	mutex      sync.Mutex
	probeErr   error
	queryErr   error
	videoErr   error
	probeCalls int
	queryCalls int
	videoCalls int
	query      string
	videoText  string
	images     []applicationembedding.PreparedMultimodalImage
}

func (s *tongyiEmbeddingClientStub) Probe(context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.probeCalls++
	return s.probeErr
}

func (s *tongyiEmbeddingClientStub) EmbedQuery(_ context.Context, query string) (*TongyiEmbedding, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.queryCalls++
	s.query = query
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return &TongyiEmbedding{Values: tongyiUnitVector()}, nil
}

func (s *tongyiEmbeddingClientStub) EmbedVideo(
	_ context.Context,
	text string,
	images []applicationembedding.PreparedMultimodalImage,
) (*TongyiEmbedding, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.videoCalls++
	s.videoText = text
	s.images = make([]applicationembedding.PreparedMultimodalImage, len(images))
	for index := range images {
		s.images[index] = images[index].Clone()
	}
	if s.videoErr != nil {
		return nil, s.videoErr
	}
	return &TongyiEmbedding{Values: tongyiUnitVector()}, nil
}

func TestTongyiAdapterEndToEndWithFruxProviderClient(t *testing.T) {
	upstream := &tongyiEmbeddingClientStub{}
	adapter := newTongyiTestAdapter(t, upstream)
	server := httptest.NewServer(adapter.Handler())
	defer server.Close()
	provider := newMultimodalHTTPTestProvider(t, server.URL, TongyiMultimodalContract(), nil)

	if err := provider.CheckReady(context.Background(), MultimodalProviderCapabilityQuery); err == nil {
		t.Fatal("adapter reported ready before upstream probe")
	}
	if err := adapter.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := provider.CheckReady(context.Background(), MultimodalProviderCapabilityVideo); err != nil {
		t.Fatal(err)
	}
	queryRequest, err := applicationembedding.NewMultimodalQueryEmbeddingRequest(
		TongyiMultimodalContract(), "雨夜城市", 64,
	)
	if err != nil {
		t.Fatal(err)
	}
	queryResult, err := provider.EmbedQueryText(context.Background(), queryRequest)
	if err != nil || len(queryResult.Vector) != TongyiDimension {
		t.Fatalf("query result=%#v err=%v", queryResult, err)
	}
	imageContent := []byte("frame")
	digest := sha256.Sum256(imageContent)
	videoRequest, err := applicationembedding.NewMultimodalVideoEmbeddingRequest(
		TongyiMultimodalContract(),
		applicationembedding.MultimodalPublicVideoContent{
			Title: "下班以后", Description: "雨夜街道",
			Published: true, Public: true, MediaReady: true, SourceCurrent: true,
		},
		128,
		[]applicationembedding.PreparedMultimodalImage{{
			MIMEType: "image/jpeg", Width: 32, Height: 32,
			Digest: hex.EncodeToString(digest[:]), Content: imageContent,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	videoResult, err := provider.EmbedVideoContent(context.Background(), videoRequest)
	if err != nil || len(videoResult.Vector) != TongyiDimension {
		t.Fatalf("video result=%#v err=%v", videoResult, err)
	}
	upstream.mutex.Lock()
	defer upstream.mutex.Unlock()
	if upstream.probeCalls != 1 || upstream.queryCalls != 1 || upstream.videoCalls != 1 ||
		upstream.query != "雨夜城市" || upstream.videoText != "下班以后\n雨夜街道" ||
		len(upstream.images) != 1 || !bytes.Equal(upstream.images[0].Content, imageContent) {
		t.Fatalf("upstream=%#v", upstream)
	}
}

func TestTongyiAdapterAuthenticatesTimestampAndReplay(t *testing.T) {
	upstream := &tongyiEmbeddingClientStub{}
	adapter := newTongyiTestAdapter(t, upstream)
	if err := adapter.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(adapter.Handler())
	defer server.Close()
	contract := TongyiMultimodalContract()
	operationID := strings.Repeat("a", 32)
	payload := multimodalReadyRequest{
		ProtocolVersion: MultimodalProviderProtocolV1, OperationID: operationID,
		RequiredCapability: MultimodalProviderCapabilityQuery,
		Contract:           multimodalContractToEnvelope(contract),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	stale := sendTongyiSignedRequest(t, server.URL+multimodalReadyPath, operationID, body, time.Now().Add(-10*time.Minute), multimodalTestSecret)
	if stale.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale status=%d", stale.StatusCode)
	}
	_ = stale.Body.Close()
	first := sendTongyiSignedRequest(t, server.URL+multimodalReadyPath, operationID, body, time.Now(), multimodalTestSecret)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status=%d", first.StatusCode)
	}
	_ = first.Body.Close()
	second := sendTongyiSignedRequest(t, server.URL+multimodalReadyPath, operationID, body, time.Now(), multimodalTestSecret)
	if second.StatusCode != http.StatusConflict || second.Header.Get("X-Frux-Response-Signature") == "" {
		t.Fatalf("replay status=%d headers=%v", second.StatusCode, second.Header)
	}
	_ = second.Body.Close()

	badSignatureRequest, err := http.NewRequest(http.MethodPost, server.URL+multimodalReadyPath, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	badSignatureRequest.Header.Set("X-Frux-Multimodal-Protocol", MultimodalProviderProtocolV1)
	badSignatureRequest.Header.Set("Content-Type", "application/json")
	badSignatureRequest.Header.Set("X-Frux-Operation-ID", strings.Repeat("b", 32))
	badSignatureRequest.Header.Set("X-Frux-Timestamp", time.Now().UTC().Truncate(time.Second).Format(time.RFC3339))
	badSignatureRequest.Header.Set("X-Frux-Signature", strings.Repeat("0", 64))
	badSignature, err := http.DefaultClient.Do(badSignatureRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer badSignature.Body.Close()
	if badSignature.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad signature status=%d", badSignature.StatusCode)
	}
}

func TestTongyiAdapterRejectsContractSourceAndImageBeforeUpstream(t *testing.T) {
	upstream := &tongyiEmbeddingClientStub{}
	adapter := newTongyiTestAdapter(t, upstream)
	if err := adapter.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(adapter.Handler())
	defer server.Close()
	contract := TongyiMultimodalContract()
	imageContent := []byte("frame")
	digest := sha256.Sum256(imageContent)
	base := multimodalVideoRequest{
		ProtocolVersion: MultimodalProviderProtocolV1,
		Contract:        multimodalContractToEnvelope(contract),
		SourceHash:      strings.Repeat("1", 64), Text: "public",
		Images: []multimodalImageEnvelope{{
			MIMEType: "image/jpeg", Width: 32, Height: 32,
			Digest: hex.EncodeToString(digest[:]), ContentBase64: base64.StdEncoding.EncodeToString(imageContent),
		}},
	}
	for _, test := range []struct {
		name   string
		status int
		mutate func(*multimodalVideoRequest)
	}{
		{name: "contract", status: http.StatusConflict, mutate: func(r *multimodalVideoRequest) { r.Contract.RevisionAlias = "other" }},
		{name: "source", status: http.StatusBadRequest, mutate: func(r *multimodalVideoRequest) { r.SourceHash = "bad" }},
		{name: "image digest", status: http.StatusBadRequest, mutate: func(r *multimodalVideoRequest) { r.Images[0].Digest = strings.Repeat("0", 64) }},
		{name: "image encoding", status: http.StatusBadRequest, mutate: func(r *multimodalVideoRequest) { r.Images[0].ContentBase64 = "%%%" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := base
			request.Images = append([]multimodalImageEnvelope(nil), base.Images...)
			test.mutate(&request)
			operationID := randomTongyiOperationID(t)
			request.OperationID = operationID
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			response := sendTongyiSignedRequest(t, server.URL+multimodalVideoPath, operationID, body, time.Now(), multimodalTestSecret)
			defer response.Body.Close()
			if response.StatusCode != test.status || response.Header.Get("X-Frux-Response-Signature") == "" {
				t.Fatalf("status=%d headers=%v", response.StatusCode, response.Header)
			}
		})
	}
	upstream.mutex.Lock()
	defer upstream.mutex.Unlock()
	if upstream.videoCalls != 0 {
		t.Fatalf("video calls=%d", upstream.videoCalls)
	}
}

func TestTongyiAdapterMapsUpstreamFailuresToSignedClosedErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		upstream   error
		status     int
		code       string
		retryAfter string
	}{
		{name: "capacity", upstream: &TongyiUpstreamError{Retryable: true, RetryAfter: 3 * time.Second, Code: "rate_limit"}, status: http.StatusTooManyRequests, code: "capacity", retryAfter: "3"},
		{name: "unavailable", upstream: &TongyiUpstreamError{Retryable: true, Code: "transport"}, status: http.StatusServiceUnavailable, code: "unavailable"},
		{name: "terminal", upstream: &TongyiUpstreamError{Code: "response_invalid"}, status: http.StatusUnprocessableEntity, code: "internal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := &tongyiEmbeddingClientStub{queryErr: test.upstream}
			adapter := newTongyiTestAdapter(t, upstream)
			if err := adapter.Probe(context.Background()); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(adapter.Handler())
			defer server.Close()
			provider := newMultimodalHTTPTestProvider(t, server.URL, TongyiMultimodalContract(), nil)
			request, err := applicationembedding.NewMultimodalQueryEmbeddingRequest(
				TongyiMultimodalContract(), "query", 64,
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.EmbedQueryText(context.Background(), request)
			var providerError *applicationembedding.MultimodalProviderError
			if !errors.As(err, &providerError) || providerError.Retryable != (test.status >= 500 || test.status == 429) {
				t.Fatalf("error=%v", err)
			}
			if test.retryAfter != "" && providerError.RetryAfter != 3*time.Second {
				t.Fatalf("retry after=%v", providerError.RetryAfter)
			}
		})
	}
}

func TestTongyiAdapterHealthContainsNoProviderDetails(t *testing.T) {
	adapter := newTongyiTestAdapter(t, &tongyiEmbeddingClientStub{})
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "tongyi") ||
		strings.Contains(response.Body.String(), "model") || strings.Contains(response.Body.String(), "key") {
		t.Fatalf("health status=%d body=%q", response.Code, response.Body.String())
	}
}

func newTongyiTestAdapter(t testing.TB, client tongyiEmbeddingClient) *TongyiAdapter {
	t.Helper()
	adapter, err := NewTongyiAdapter(tongyiTestConfig("https://workspace.example.com/multimodal", nil), client)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func sendTongyiSignedRequest(
	t testing.TB,
	endpoint string,
	operationID string,
	body []byte,
	timestamp time.Time,
	secret string,
) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	timestampRaw := timestamp.UTC().Truncate(time.Second).Format(time.RFC3339)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Frux-Multimodal-Protocol", MultimodalProviderProtocolV1)
	request.Header.Set("X-Frux-Operation-ID", operationID)
	request.Header.Set("X-Frux-Timestamp", timestampRaw)
	request.Header.Set("X-Frux-Signature", multimodalRequestSignature(
		[]byte(secret), MultimodalProviderProtocolV1, http.MethodPost,
		request.URL.EscapedPath(), timestampRaw, operationID, body,
	))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func randomTongyiOperationID(t testing.TB) string {
	t.Helper()
	operationID, err := newMultimodalOperationID()
	if err != nil {
		t.Fatal(err)
	}
	return operationID
}

func tongyiUnitVector() []float64 {
	vector := make([]float64, TongyiDimension)
	vector[0] = 1
	return vector
}

func decodeTongyiError(t testing.TB, response *http.Response) multimodalErrorResponse {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var output multimodalErrorResponse
	if err := json.Unmarshal(body, &output); err != nil {
		t.Fatal(err)
	}
	return output
}
