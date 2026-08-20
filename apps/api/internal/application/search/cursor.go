package applicationsearch

import (
	"encoding/base64"
	"encoding/json"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	"math"
	"strings"
	"time"
)

const (
	videoCursorVersion       = 1
	hybridVideoCursorVersion = 2
	userCursorVersion        = 2
	maxSearchCursorLength    = 2048
)

type cursorPayload struct {
	Version        int     `json:"v"`
	Category       string  `json:"c"`
	Query          string  `json:"q"`
	Relevance      int     `json:"r"`
	Time           string  `json:"t"`
	ID             int64   `json:"i"`
	Mode           string  `json:"m,omitempty"`
	RankingVersion string  `json:"rv,omitempty"`
	ContractKey    string  `json:"k,omitempty"`
	Score          float64 `json:"s,omitempty"`
	ExpiresAt      int64   `json:"e,omitempty"`
}

const (
	VideoRetrievalModeLexical = "lexical"
	VideoRetrievalModeHybrid  = "hybrid"
)

type HybridVideoCursor struct {
	Mode           string
	RankingVersion string
	ContractKey    string
	Relevance      int
	HybridScore    float64
	PublishedAt    time.Time
	VideoID        int64
	ExpiresAt      time.Time
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
		Version: videoCursorVersion, Category: domainsearch.CategoryVideos, Query: query,
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
	payload, parsedTime, err := decodeCursor(value, videoCursorVersion, domainsearch.CategoryVideos, query)
	if err != nil || !domainsearch.ValidVideoRelevance(payload.Relevance) {
		return nil, domainsearch.ErrInvalidCursor
	}
	return &domainsearch.VideoCursor{
		Relevance: payload.Relevance, PublishedAt: parsedTime, VideoID: payload.ID,
	}, nil
}

func EncodeHybridVideoCursor(query string, cursor *HybridVideoCursor) string {
	if cursor == nil || cursor.VideoID <= 0 || cursor.PublishedAt.IsZero() ||
		cursor.ExpiresAt.IsZero() || strings.TrimSpace(cursor.RankingVersion) == "" {
		return ""
	}
	query, err := domainsearch.NormalizeQuery(query)
	if err != nil {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(cursor.Mode))
	switch mode {
	case VideoRetrievalModeLexical:
		if !domainsearch.ValidVideoRelevance(cursor.Relevance) || cursor.ContractKey != "" {
			return ""
		}
	case VideoRetrievalModeHybrid:
		if math.IsNaN(cursor.HybridScore) || math.IsInf(cursor.HybridScore, 0) ||
			cursor.HybridScore <= 0 || strings.TrimSpace(cursor.ContractKey) == "" {
			return ""
		}
	default:
		return ""
	}
	return encodeCursor(cursorPayload{
		Version: hybridVideoCursorVersion, Category: domainsearch.CategoryVideos, Query: query,
		Relevance: cursor.Relevance, Time: cursor.PublishedAt.UTC().Format(time.RFC3339Nano), ID: cursor.VideoID,
		Mode: mode, RankingVersion: strings.TrimSpace(cursor.RankingVersion),
		ContractKey: strings.TrimSpace(cursor.ContractKey), Score: cursor.HybridScore,
		ExpiresAt: cursor.ExpiresAt.UTC().UnixNano(),
	})
}

func DecodeHybridVideoCursor(value, query, rankingVersion, contractKey string, now time.Time) (*HybridVideoCursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	query, err := domainsearch.NormalizeQuery(query)
	if err != nil {
		return nil, err
	}
	payload, parsedTime, err := decodeCursor(value, hybridVideoCursorVersion, domainsearch.CategoryVideos, query)
	if err != nil || payload.ExpiresAt <= 0 || !now.UTC().Before(time.Unix(0, payload.ExpiresAt).UTC()) ||
		payload.RankingVersion != strings.TrimSpace(rankingVersion) {
		return nil, domainsearch.ErrInvalidCursor
	}
	cursor := &HybridVideoCursor{
		Mode: payload.Mode, RankingVersion: payload.RankingVersion,
		ContractKey: payload.ContractKey, Relevance: payload.Relevance,
		HybridScore: payload.Score, PublishedAt: parsedTime,
		VideoID: payload.ID, ExpiresAt: time.Unix(0, payload.ExpiresAt).UTC(),
	}
	switch cursor.Mode {
	case VideoRetrievalModeLexical:
		if cursor.ContractKey != "" || !domainsearch.ValidVideoRelevance(cursor.Relevance) {
			return nil, domainsearch.ErrInvalidCursor
		}
	case VideoRetrievalModeHybrid:
		if cursor.ContractKey != strings.TrimSpace(contractKey) || math.IsNaN(cursor.HybridScore) ||
			math.IsInf(cursor.HybridScore, 0) || cursor.HybridScore <= 0 {
			return nil, domainsearch.ErrInvalidCursor
		}
	default:
		return nil, domainsearch.ErrInvalidCursor
	}
	return cursor, nil
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
		Version: userCursorVersion, Category: domainsearch.CategoryUsers, Query: query,
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
	payload, parsedTime, err := decodeCursor(value, userCursorVersion, domainsearch.CategoryUsers, query)
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

func decodeCursor(value string, version int, category, query string) (cursorPayload, time.Time, error) {
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
	if err != nil || payload.Version != version || payload.Category != category ||
		payload.Query != query || payload.ID <= 0 {
		return payload, time.Time{}, domainsearch.ErrInvalidCursor
	}
	return payload, parsedTime.UTC(), nil
}
