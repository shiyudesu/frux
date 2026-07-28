package domainrecommendation

import (
	"strings"
	"time"
)

const (
	FeedbackTypeNotInterested = "not_interested"
	FeedbackTypeReduceAuthor  = "reduce_author"
	FeedbackTypeAlreadySeen   = "already_seen"
	MaxIdempotencyKeyLength   = 128
)

type Feedback struct {
	ID                   int64
	UserID               int64
	VideoID              int64
	RequestID            string
	FeedbackType         string
	IdempotencyKey       string
	SuppressionScope     string
	SuppressionScopeID   int64
	SuppressionExpiresAt time.Time
	CreatedAt            time.Time
}

const (
	SuppressionScopeVideo  = "video"
	SuppressionScopeAuthor = "author"
)

// FeedbackProjectionOutboxItem wraps a durable feedback fact for asynchronous
// recommendation-profile projection.
type FeedbackProjectionOutboxItem struct {
	ID       int64
	Attempts int
	Feedback *Feedback
}

func NewFeedback(userID int64, videoID int64, requestID string, feedbackType string, idempotencyKey string, createdAt time.Time) (*Feedback, error) {
	if userID <= 0 {
		return nil, ErrInvalidUserID
	}
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	requestID = strings.TrimSpace(requestID)
	feedbackType = strings.ToLower(strings.TrimSpace(feedbackType))
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if requestID == "" {
		return nil, ErrEmptyRequestID
	}
	if len(requestID) > MaxRequestIDLength {
		return nil, ErrRequestIDTooLong
	}
	if !ValidFeedbackType(feedbackType) {
		return nil, ErrInvalidFeedbackType
	}
	if idempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	if len(idempotencyKey) > MaxIdempotencyKeyLength {
		return nil, ErrIdempotencyKeyTooLong
	}
	if createdAt.IsZero() {
		return nil, ErrInvalidCreatedAt
	}
	return &Feedback{
		UserID:         userID,
		VideoID:        videoID,
		RequestID:      requestID,
		FeedbackType:   feedbackType,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      createdAt.UTC(),
	}, nil
}

func RestoreFeedback(id int64, userID int64, videoID int64, requestID string, feedbackType string, idempotencyKey string, createdAt time.Time) *Feedback {
	return &Feedback{
		ID:             id,
		UserID:         userID,
		VideoID:        videoID,
		RequestID:      strings.TrimSpace(requestID),
		FeedbackType:   strings.ToLower(strings.TrimSpace(feedbackType)),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		CreatedAt:      createdAt.UTC(),
	}
}

func (f *Feedback) SetSuppression(scope string, scopeID int64, expiresAt time.Time) error {
	if f == nil || scopeID <= 0 || expiresAt.IsZero() {
		return ErrInvalidFeedbackType
	}
	scope = strings.ToLower(strings.TrimSpace(scope))
	if (f.FeedbackType == FeedbackTypeReduceAuthor && scope != SuppressionScopeAuthor) ||
		(f.FeedbackType != FeedbackTypeReduceAuthor && scope != SuppressionScopeVideo) {
		return ErrInvalidFeedbackType
	}
	f.SuppressionScope, f.SuppressionScopeID, f.SuppressionExpiresAt = scope, scopeID, expiresAt.UTC()
	return nil
}

func ValidFeedbackType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case FeedbackTypeNotInterested, FeedbackTypeReduceAuthor, FeedbackTypeAlreadySeen:
		return true
	default:
		return false
	}
}

func (f *Feedback) SameNormalizedPayload(other *Feedback) bool {
	return f != nil &&
		other != nil &&
		f.UserID == other.UserID &&
		f.VideoID == other.VideoID &&
		f.RequestID == other.RequestID &&
		f.FeedbackType == other.FeedbackType
}
