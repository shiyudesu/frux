package applicationrecommendation

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"io"
	"math"
	"strings"
	"time"
)

const (
	snapshotCursorVersion    = "r1"
	maxSnapshotCandidates    = 500
	maxSnapshotCursorLength  = 2048
	maxSnapshotPayloadLength = 1024
)

var ErrSnapshotUnavailable = errors.New("recommendation snapshot is unavailable")

// SnapshotStore persists only bounded, internal recommendation state. It is
// optional: callers must retain the legacy pagination path when it is absent.
type SnapshotStore interface {
	// CreateSnapshot atomically creates a snapshot or returns the extant
	// snapshot for the same request identity.
	CreateSnapshot(ctx context.Context, snapshot *Snapshot, ttl time.Duration) (*Snapshot, bool, error)
	LoadSnapshot(ctx context.Context, snapshotID string) (*Snapshot, bool, error)
	LoadSnapshotForRequest(ctx context.Context, userID int64, scene string, requestID string) (*Snapshot, bool, error)
}

// Snapshot is the server-side ordered candidate set for one recommendation
// session. Candidates retain their internal recall and ranking metadata.
type Snapshot struct {
	ID                string
	UserID            int64
	Scene             string
	RequestID         string
	PolicyVersion     int
	ExpiresAt         time.Time
	Candidates        []*domainrecommendation.Candidate
	Degraded          bool                  `json:"degraded"`
	DegradedProviders []ProviderDegradation `json:"degraded_providers,omitempty"`
}

func (s *Snapshot) Clone() *Snapshot {
	if s == nil {
		return nil
	}
	cloned := *s
	cloned.Candidates = cloneCandidates(s.Candidates)
	cloned.DegradedProviders = append([]ProviderDegradation(nil), s.DegradedProviders...)
	return &cloned
}

type snapshotCursorPayload struct {
	Version       string         `json:"v"`
	SnapshotID    string         `json:"s"`
	UserID        int64          `json:"u"`
	Scene         string         `json:"c"`
	RequestID     string         `json:"r"`
	PolicyVersion int            `json:"p"`
	Offset        int            `json:"o"`
	ExpiresAt     int64          `json:"e"`
	Fallback      *cursorPayload `json:"f,omitempty"`
}

// SnapshotCursorSigner creates and validates opaque, versioned cursors.
type SnapshotCursorSigner interface {
	SignSnapshotCursor(payload snapshotCursorPayload) (string, error)
	VerifySnapshotCursor(raw string, userID int64, scene string, requestID string, now time.Time) (*snapshotCursorPayload, error)
}

type HMACSnapshotCursorSigner struct {
	secret []byte
}

func NewHMACSnapshotCursorSigner(secret string) (*HMACSnapshotCursorSigner, error) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 16 {
		return nil, ErrSnapshotUnavailable
	}
	return &HMACSnapshotCursorSigner{secret: []byte(secret)}, nil
}

func (s *HMACSnapshotCursorSigner) SignSnapshotCursor(payload snapshotCursorPayload) (string, error) {
	if s == nil || len(s.secret) == 0 || !validSnapshotCursorPayload(payload) {
		return "", domainrecommendation.ErrInvalidCursor
	}
	content, err := json.Marshal(payload)
	if err != nil || len(content) > maxSnapshotPayloadLength {
		return "", domainrecommendation.ErrInvalidCursor
	}
	encoded := base64.RawURLEncoding.EncodeToString(content)
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return snapshotCursorVersion + "." + encoded + "." + signature, nil
}

func (s *HMACSnapshotCursorSigner) VerifySnapshotCursor(raw string, userID int64, scene string, requestID string, now time.Time) (*snapshotCursorPayload, error) {
	if s == nil || len(s.secret) == 0 || len(raw) == 0 || len(raw) > maxSnapshotCursorLength {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != snapshotCursorVersion {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	content, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(content) == 0 || len(content) > maxSnapshotPayloadLength {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(providedSignature) != sha256.Size {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(parts[1]))
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	var payload snapshotCursorPayload
	if err := json.Unmarshal(content, &payload); err != nil || !validSnapshotCursorPayload(payload) {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	if payload.UserID != userID || payload.Scene != strings.ToLower(strings.TrimSpace(scene)) ||
		(requestID != "" && payload.RequestID != requestID) || !now.UTC().Before(time.Unix(0, payload.ExpiresAt).UTC()) {
		return nil, domainrecommendation.ErrInvalidCursor
	}
	return &payload, nil
}

func validSnapshotCursorPayload(payload snapshotCursorPayload) bool {
	if payload.Version != snapshotCursorVersion || payload.SnapshotID == "" || len(payload.SnapshotID) > 128 ||
		payload.UserID <= 0 || payload.Scene == "" || len(payload.Scene) > domainrecommendation.MaxSceneLength ||
		payload.RequestID == "" || len(payload.RequestID) > domainrecommendation.MaxRequestIDLength ||
		payload.PolicyVersion <= 0 || payload.Offset < 0 || payload.Offset > maxSnapshotCandidates ||
		payload.ExpiresAt <= 0 {
		return false
	}
	if payload.Fallback != nil {
		if payload.Fallback.VideoID <= 0 || strings.TrimSpace(payload.Fallback.PublishedAt) == "" ||
			math.IsNaN(payload.Fallback.RankScore) || math.IsInf(payload.Fallback.RankScore, 0) {
			return false
		}
	}
	return true
}

func snapshotID(userID int64, scene string, requestID string, policyVersion int) string {
	value := fmt.Sprintf("%d|%s|%s|%d", userID, scene, requestID, policyVersion)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func generatedRequestID() (string, error) {
	return generatedRequestIDFromReader(rand.Reader)
}

func generatedRequestIDFromReader(reader io.Reader) (string, error) {
	var value [24]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", err
	}
	return "srv-" + hex.EncodeToString(value[:]), nil
}

func cloneCandidates(candidates []*domainrecommendation.Candidate) []*domainrecommendation.Candidate {
	cloned := make([]*domainrecommendation.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil {
			cloned = append(cloned, candidate.Clone())
		}
	}
	return cloned
}
