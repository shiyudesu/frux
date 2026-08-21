package infraacceptance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPIClientRunsBoundedProductWorkflow(t *testing.T) {
	var server *httptest.Server
	requests := map[string]int{}
	server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests[request.Method+" "+request.URL.Path]++
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/api/sessions" || request.URL.Path == "/api/admin/auth/login":
			_, _ = writer.Write([]byte(`{"access_token":"bounded-token"}`))
		case request.URL.Path == "/api/upload-sessions" && request.Method == http.MethodPost:
			if request.Header.Get("Idempotency-Key") == "" {
				t.Error("missing upload idempotency key")
			}
			_, _ = writer.Write([]byte(`{"mode":"direct","id":"session-1","upload":{"url":"` + server.URL + `/signed-upload","method":"PUT","headers":{"X-Test":"ok"}}}`))
		case request.URL.Path == "/signed-upload":
			if request.Header.Get("X-Test") != "ok" {
				t.Error("missing signed upload header")
			}
			if request.ContentLength != int64(len("fixture")) {
				t.Errorf("content length=%d", request.ContentLength)
			}
			content, _ := io.ReadAll(request.Body)
			if string(content) != "fixture" {
				t.Errorf("content=%q", content)
			}
			writer.WriteHeader(http.StatusNoContent)
		case request.URL.Path == "/api/upload-sessions/session-1/complete":
			_, _ = writer.Write([]byte(`{"asset":{"id":21,"kind":"video"}}`))
		case request.URL.Path == "/api/videos" && request.Method == http.MethodPost:
			_, _ = writer.Write([]byte(`{"id":13,"media_asset_id":21,"cover_asset_id":22}`))
		case request.URL.Path == "/api/admin/review/cases/7/claim":
			_, _ = writer.Write([]byte(`{"lease_token":"lease","case":{"version":2}}`))
		case request.URL.Path == "/api/admin/review/cases/7/decision":
			writer.WriteHeader(http.StatusOK)
		case request.URL.Path == "/api/videos/13/similar":
			_, _ = writer.Write([]byte(`{"semantic_available":true,"items":[{"id":14}]}`))
		case request.URL.Path == "/api/search/videos":
			_, _ = writer.Write([]byte(`{"items":[{"id":14},{"id":13}]}`))
		case request.URL.Path == "/api/videos/13" && request.Method == http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	httpClient := acceptanceTestHTTPClient(t, server, 1<<20)
	client, err := NewAPIClient(httpClient, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userToken, err := client.Login(ctx, false, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	adminToken, err := client.Login(ctx, true, "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	asset, err := client.UploadFixture(ctx, userToken, "video", path, "upload-key")
	if err != nil || asset.ID != 21 {
		t.Fatalf("asset=%#v err=%v", asset, err)
	}
	video, err := client.CreateVideo(ctx, userToken, 21, 22, "title", "description", "video-key")
	if err != nil || video.ID != 13 {
		t.Fatalf("video=%#v err=%v", video, err)
	}
	lease, err := client.ClaimReview(ctx, adminToken, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApproveReview(ctx, adminToken, 7, 1, lease, "decision-key"); err != nil {
		t.Fatal(err)
	}
	similar, err := client.Similar(ctx, 13)
	if err != nil || !similar.Available || len(similar.VideoIDs) != 1 || similar.VideoIDs[0] != 14 {
		t.Fatalf("similar=%#v err=%v", similar, err)
	}
	hybrid, err := client.Hybrid(ctx, "雨夜 城市")
	if err != nil || len(hybrid) != 2 || hybrid[0] != 14 {
		t.Fatalf("hybrid=%v err=%v", hybrid, err)
	}
	if err := client.DeleteVideo(ctx, userToken, 13); err != nil {
		t.Fatal(err)
	}
	if requests["PUT /signed-upload"] != 1 || requests["DELETE /api/videos/13"] != 1 {
		t.Fatalf("requests=%v", requests)
	}
}

func TestAPIClientRejectsMultipartFallbackAndDoesNotLeakUploadURL(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"mode":"multipart"}`))
	}))
	defer server.Close()
	client, err := NewAPIClient(acceptanceTestHTTPClient(t, server, 1<<20), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = client.UploadFixture(context.Background(), "token", "video", path, "key")
	if !IsHTTPError(err, HTTPFailureDecode) || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("error=%v", err)
	}
}

func TestAPIClientRequestPayloadsRemainJSON(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var value map[string]any
		if err := json.NewDecoder(request.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		if value["title"] != "title" {
			t.Fatalf("value=%v", value)
		}
		_, _ = writer.Write([]byte(`{"id":1,"media_asset_id":2,"cover_asset_id":3}`))
	}))
	defer server.Close()
	client, _ := NewAPIClient(acceptanceTestHTTPClient(t, server, 1<<20), server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.CreateVideo(ctx, "token", 2, 3, "title", "description", "key"); err != nil {
		t.Fatal(err)
	}
}
