package infraacceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionAcceptanceAPIUsesAuthenticatedProductWorkflow(t *testing.T) {
	var server *httptest.Server
	viewCalls, favoriteCalls, feedCalls := 0, 0, 0
	server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/api/sessions" && request.Header.Get("Authorization") != "Bearer bounded-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/api/sessions":
			_, _ = writer.Write([]byte(`{"access_token":"bounded-token"}`))
		case request.URL.Path == "/api/users/me":
			_, _ = writer.Write([]byte(`{"id":7}`))
		case request.URL.Path == "/api/video-view-events":
			viewCalls++
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body["scene"] != "recommend" || body["request_id"] != "request-1" {
				t.Errorf("view body=%v err=%v", body, err)
			}
			writer.WriteHeader(http.StatusCreated)
		case strings.HasSuffix(request.URL.Path, "/favorite"):
			favoriteCalls++
			if request.Header.Get("Idempotency-Key") == "" || request.Header.Get("X-Recommendation-Request-ID") != "request-1" {
				t.Error("favorite headers missing")
			}
			writer.WriteHeader(http.StatusOK)
		case request.URL.Path == "/api/feed-queries":
			feedCalls++
			if feedCalls == 1 {
				_, _ = writer.Write([]byte(`{"request_id":"request-1","items":[{"video_id":13}],"next_cursor":"signed-secret-cursor","has_more":true}`))
			} else {
				_, _ = writer.Write([]byte(`{"request_id":"request-1","items":[{"video_id":14}],"next_cursor":"","has_more":false}`))
			}
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewAPIClient(acceptanceTestHTTPClient(t, server, 1<<20), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.Login(context.Background(), false, "user", "secret")
	if err != nil {
		t.Fatal(err)
	}
	userID, err := client.Me(context.Background(), token)
	if err != nil || userID != 7 {
		t.Fatalf("user=%d err=%v", userID, err)
	}
	now := time.Now().UTC()
	if err := client.CreateSessionViewEvent(context.Background(), token, 11, "request-1", "complete", "event-1", "playback-1", 1, 100, 90, 100, true, now); err != nil {
		t.Fatal(err)
	}
	if err := client.SetSessionFavorite(context.Background(), token, 11, "request-1", "favorite-1", true); err != nil {
		t.Fatal(err)
	}
	first, err := client.SessionFeed(context.Background(), token, "request-1", "session-1", "", 11, 12, 1)
	if err != nil || !first.HasMore || first.NextCursor == "" || len(first.VideoIDs) != 1 || first.VideoIDs[0] != 13 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := client.SessionFeed(context.Background(), token, "request-1", "session-1", first.NextCursor, 11, 12, 1)
	if err != nil || len(second.VideoIDs) != 1 || second.VideoIDs[0] != 14 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	if err := client.SetSessionFavorite(context.Background(), token, 11, "request-1", "favorite-cleanup-1", false); err != nil {
		t.Fatal(err)
	}
	if viewCalls != 1 || favoriteCalls != 2 || feedCalls != 2 {
		t.Fatalf("view=%d favorite=%d feed=%d", viewCalls, favoriteCalls, feedCalls)
	}
}
