package domainrelation

import "time"

// FollowProjectionOutboxItem is the durable, idempotent follow signal consumed
// by the recommendation profile worker.
type FollowProjectionOutboxItem struct {
	ID                      int64
	Attempts                int
	EventID                 string
	UserID                  int64
	AuthorID                int64
	Active                  bool
	OccurredAt              time.Time
	RecommendationRequestID string
	RecommendationVideoID   int64
}
