package applicationsearch

import (
	domainsearch "GCFeed/internal/domain/search"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const (
	searchCursorVersion   = 1
	maxSearchCursorLength = 2048
)

type cursorPayload struct {
	Version   int    `json:"v"`
	Category  string `json:"c"`
	Query     string `json:"q"`
	Relevance int    `json:"r"`
	Time      string `json:"t"`
	ID        int64  `json:"i"`
}

func EncodeVideoCursor(query string, cursor *domainsearch.VideoCursor) string {
	if cursor == nil || !domainsearch.ValidVideoRelevance(cursor.Relevance) ||
		cursor.PublishedAt.IsZero() || cursor.VideoID <= 0 {
		return ""
	}
	query, err := domainsearch.NormalizeQuery(query)
	if err != nil {
		return ""
	}
	return encodeCursor(cursorPayload{
		Version: searchCursorVersion, Category: domainsearch.CategoryVideos, Query: query,
		Relevance: cursor.Relevance, Time: cursor.PublishedAt.UTC().Format(time.RFC3339Nano), ID: cursor.VideoID,
	})
}

func DecodeVideoCursor(value, query string) (*domainsearch.VideoCursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	query, err := domainsearch.NormalizeQuery(query)
	if err != nil {
		return nil, err
	}
	payload, parsedTime, err := decodeCursor(value, domainsearch.CategoryVideos, query)
	if err != nil || !domainsearch.ValidVideoRelevance(payload.Relevance) {
		return nil, domainsearch.ErrInvalidCursor
	}
	return &domainsearch.VideoCursor{
		Relevance: payload.Relevance, PublishedAt: parsedTime, VideoID: payload.ID,
	}, nil
}

func EncodeUserCursor(query string, cursor *domainsearch.UserCursor) string {
	if cursor == nil || !domainsearch.ValidUserRelevance(cursor.Relevance) ||
		cursor.UpdatedAt.IsZero() || cursor.UserID <= 0 {
		return ""
	}
	query, err := domainsearch.NormalizeQuery(query)
	if err != nil {
		return ""
	}
	return encodeCursor(cursorPayload{
		Version: searchCursorVersion, Category: domainsearch.CategoryUsers, Query: query,
		Relevance: cursor.Relevance, Time: cursor.UpdatedAt.UTC().Format(time.RFC3339Nano), ID: cursor.UserID,
	})
}

func DecodeUserCursor(value, query string) (*domainsearch.UserCursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	query, err := domainsearch.NormalizeQuery(query)
	if err != nil {
		return nil, err
	}
	payload, parsedTime, err := decodeCursor(value, domainsearch.CategoryUsers, query)
	if err != nil || !domainsearch.ValidUserRelevance(payload.Relevance) {
		return nil, domainsearch.ErrInvalidCursor
	}
	return &domainsearch.UserCursor{
		Relevance: payload.Relevance, UpdatedAt: parsedTime, UserID: payload.ID,
	}, nil
}

func encodeCursor(payload cursorPayload) string {
	content, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(content)
}

func decodeCursor(value, category, query string) (cursorPayload, time.Time, error) {
	var payload cursorPayload
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxSearchCursorLength {
		return payload, time.Time{}, domainsearch.ErrInvalidCursor
	}
	content, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return payload, time.Time{}, domainsearch.ErrInvalidCursor
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return payload, time.Time{}, domainsearch.ErrInvalidCursor
	}
	parsedTime, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.Time))
	if err != nil || payload.Version != searchCursorVersion || payload.Category != category ||
		payload.Query != query || payload.ID <= 0 {
		return payload, time.Time{}, domainsearch.ErrInvalidCursor
	}
	return payload, parsedTime.UTC(), nil
}
