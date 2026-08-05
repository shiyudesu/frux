package applicationadminaudit

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
)

const (
	auditCursorVersion   = 1
	maxAuditCursorLength = 2048
)

type cursorPayload struct {
	Version    int    `json:"v"`
	FilterHash string `json:"f"`
	CreatedAt  string `json:"t"`
	EventID    int64  `json:"i"`
}

type queryFilterPayload struct {
	ActorID    int64  `json:"actor_id"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	Outcome    string `json:"outcome"`
	From       string `json:"from"`
	To         string `json:"to"`
}

func encodeCursor(filterKey string, cursor *domainadminaudit.Cursor) string {
	if filterKey == "" || cursor == nil || cursor.EventID <= 0 || cursor.CreatedAt.IsZero() {
		return ""
	}
	encoded, err := json.Marshal(cursorPayload{
		Version: auditCursorVersion, FilterHash: filterKey,
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano), EventID: cursor.EventID,
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCursor(value, filterKey string) (*domainadminaudit.Cursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if len(value) > maxAuditCursorLength {
		return nil, domainadminaudit.ErrInvalidCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, domainadminaudit.ErrInvalidCursor
	}
	var payload cursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, domainadminaudit.ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.CreatedAt))
	if err != nil || payload.Version != auditCursorVersion || payload.FilterHash != filterKey ||
		payload.EventID <= 0 {
		return nil, domainadminaudit.ErrInvalidCursor
	}
	return &domainadminaudit.Cursor{CreatedAt: createdAt.UTC(), EventID: payload.EventID}, nil
}

func queryFilterKey(query domainadminaudit.Query) string {
	encoded, err := json.Marshal(queryFilterPayload{
		ActorID: query.ActorID, Action: string(query.Action),
		TargetType: string(query.TargetType), Outcome: string(query.Outcome),
		From: query.From.UTC().Format(time.RFC3339Nano),
		To:   query.To.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
