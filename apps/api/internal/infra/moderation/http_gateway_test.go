package inframoderation

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	applicationreview "github.com/shiyudesu/frux/internal/application/review"
	domainreview "github.com/shiyudesu/frux/internal/domain/review"
)

func TestHTTPGatewaySignsAndValidatesCanonicalExchange(t *testing.T) {
	secret := strings.Repeat("s", 32)
	generatedAt := time.Now().UTC().Truncate(time.Microsecond)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		requestID := request.Header.Get("X-Frux-Request-ID")
		if request.Header.Get("X-Frux-Signature") != requestSignature(
			[]byte(secret), request.Header.Get("X-Frux-Timestamp"), requestID, body,
		) {
			t.Fatal("request signature mismatch")
		}
		var payload gatewayRequest
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		responseBody, _ := json.Marshal(gatewayResponse{
			RequestID: requestID, Provider: "fixture", ModelVersion: "v1",
			GeneratedAt: generatedAt,
			Signals: []gatewayResponseSignal{{
				Label: domainreview.LabelSafe, Confidence: 0.99,
				FrameTimestampsMS: []int64{payload.Frames[0].TimestampMS},
			}},
		})
		writer.Header().Set(
			"X-Frux-Response-Signature",
			responseSignature([]byte(secret), requestID, responseBody),
		)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(responseBody)
	}))
	defer server.Close()

	gateway, err := NewHTTPGateway(server.URL, secret, time.Second, WithInsecureLocalGateway())
	if err != nil {
		t.Fatal(err)
	}
	result, err := gateway.Evaluate(t.Context(), validProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "fixture" || len(result.Signals) != 1 ||
		result.Signals[0].FrameTimestampsMS[0] != 500 {
		t.Fatalf("result = %#v", result)
	}
	replayed, err := gateway.Evaluate(t.Context(), validProviderRequest())
	if err != nil || !replayed.GeneratedAt.Equal(result.GeneratedAt) {
		t.Fatalf("replayed result = %#v err=%v", replayed, err)
	}
}

func TestHTTPGatewayClassifiesRetryableAndMalformedResponses(t *testing.T) {
	secret := strings.Repeat("s", 32)
	for _, test := range []struct {
		name      string
		status    int
		body      string
		retryable bool
		sign      bool
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, retryable: true},
		{name: "server error", status: http.StatusServiceUnavailable, retryable: true},
		{name: "client rejection", status: http.StatusBadRequest, retryable: false},
		{name: "malformed", status: http.StatusOK, body: `{"request_id":`, retryable: true, sign: true},
		{name: "unsigned", status: http.StatusOK, body: `{}`, retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body := []byte(test.body)
				if test.sign {
					writer.Header().Set(
						"X-Frux-Response-Signature",
						responseSignature([]byte(secret), request.Header.Get("X-Frux-Request-ID"), body),
					)
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write(body)
			}))
			defer server.Close()
			gateway, err := NewHTTPGateway(server.URL, secret, time.Second, WithInsecureLocalGateway())
			if err != nil {
				t.Fatal(err)
			}
			_, err = gateway.Evaluate(t.Context(), validProviderRequest())
			providerErr, ok := err.(*applicationreview.ModerationProviderError)
			if !ok || providerErr.Retryable != test.retryable {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestHTTPGatewayTimeoutIsRetryable(t *testing.T) {
	secret := strings.Repeat("s", 32)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()
	gateway, err := NewHTTPGateway(
		server.URL, secret, 20*time.Millisecond, WithInsecureLocalGateway(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = gateway.Evaluate(t.Context(), validProviderRequest())
	providerErr, ok := err.(*applicationreview.ModerationProviderError)
	if !ok || !providerErr.Retryable || providerErr.Code != "transport" {
		t.Fatalf("timeout error = %#v", err)
	}
}

func TestGatewayRejectsInsecureOrUnreachableSampleURLs(t *testing.T) {
	request := validProviderRequest()
	request.Frames[0].URL = "http://media.example.com/frame.jpg"
	if _, _, err := validateAndBuildRequest(request, false); err == nil {
		t.Fatal("accepted insecure remote sample URL")
	}
	request.Frames[0].URL = "http://127.0.0.1:9000/frame.jpg"
	if _, _, err := validateAndBuildRequest(request, false); err == nil {
		t.Fatal("accepted loopback sample URL without local exception")
	}
	if _, _, err := validateAndBuildRequest(request, true); err != nil {
		t.Fatalf("local fixture sample URL rejected: %v", err)
	}
}

func validProviderRequest() applicationreview.ModerationProviderRequest {
	return applicationreview.ModerationProviderRequest{
		JobID: 1, CaseID: 2, VideoID: 3, ReviewVersion: 1,
		RequestedPolicyVersion: 1, RequestID: "moderation-request:2:1:1",
		Title: "title", Description: "description",
		Frames: []domainreview.ModerationFrameAccess{{
			TimestampMS: 500, SHA256: strings.Repeat("a", 64),
			URL: "https://media.example/frame.jpg", ExpiresAt: time.Now().Add(time.Minute),
		}},
	}
}
