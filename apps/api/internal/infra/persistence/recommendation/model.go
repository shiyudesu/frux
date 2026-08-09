package infrarecommendation

import "time"

type PolicyModel struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Scene      string    `gorm:"column:scene;size:32;not null;uniqueIndex:uk_recommendation_policy_scene_version,priority:1;index:idx_recommendation_policy_scene_enabled_version,priority:1"`
	Version    int       `gorm:"column:version;not null;uniqueIndex:uk_recommendation_policy_scene_version,priority:2;index:idx_recommendation_policy_scene_enabled_version,priority:3"`
	Enabled    bool      `gorm:"column:enabled;not null;default:false;index:idx_recommendation_policy_scene_enabled_version,priority:2"`
	ConfigJSON string    `gorm:"column:config_json;type:jsonb;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (PolicyModel) TableName() string {
	return "recommendation_policy"
}

type UserInterestProfileModel struct {
	UserID                       int64     `gorm:"column:user_id;primaryKey"`
	LongTermVectorJSON           string    `gorm:"column:long_term_vector_json;type:jsonb;not null"`
	RecentVectorJSON             string    `gorm:"column:recent_vector_json;type:jsonb;not null"`
	AuthorAffinitiesJSON         string    `gorm:"column:author_affinities_json;type:jsonb;not null"`
	NegativeTopicVectorJSON      string    `gorm:"column:negative_topic_vector_json;type:jsonb;not null"`
	NegativeAuthorAffinitiesJSON string    `gorm:"column:negative_author_affinities_json;type:jsonb;not null"`
	Version                      int64     `gorm:"column:version;not null;default:0"`
	UpdatedAt                    time.Time `gorm:"column:updated_at;not null;index:idx_recommendation_profile_updated"`
}

func (UserInterestProfileModel) TableName() string {
	return "user_interest_profile"
}

type AppliedProfileEventModel struct {
	UserID        int64     `gorm:"column:user_id;primaryKey;priority:1"`
	SourceEventID string    `gorm:"column:source_event_id;size:128;primaryKey;priority:2"`
	PayloadHash   string    `gorm:"column:payload_hash;size:64;not null"`
	AppliedAt     time.Time `gorm:"column:applied_at;not null;autoCreateTime;index:idx_recommendation_applied_profile_event_time"`
}

func (AppliedProfileEventModel) TableName() string {
	return "recommendation_applied_profile_event"
}

type BehaviorEventModel struct {
	EventID             string     `gorm:"column:event_id;size:128;primaryKey;priority:2"`
	ViewEventID         int64      `gorm:"column:view_event_id;not null;uniqueIndex:uk_recommendation_behavior_view_event"`
	UserID              int64      `gorm:"column:user_id;not null;primaryKey;priority:1;index:idx_recommendation_behavior_user_occurred,priority:1"`
	VideoID             int64      `gorm:"column:video_id;not null"`
	Scene               string     `gorm:"column:scene;size:32;not null;default:''"`
	RequestID           string     `gorm:"column:request_id;size:64;not null;default:''"`
	EventType           string     `gorm:"column:event_type;size:32;not null"`
	PlaybackSessionID   *string    `gorm:"column:playback_session_id;size:128"`
	Sequence            *int64     `gorm:"column:sequence"`
	PositionMs          int        `gorm:"column:position_ms;not null;default:0"`
	WatchMs             int        `gorm:"column:watch_ms;not null;default:0"`
	DurationMs          *int       `gorm:"column:duration_ms"`
	Completed           bool       `gorm:"column:completed;not null;default:false"`
	ExposureCount       int        `gorm:"column:exposure_count_snapshot;not null;default:0"`
	OccurredAt          time.Time  `gorm:"column:occurred_at;not null;index:idx_recommendation_behavior_user_occurred,priority:2"`
	RecordedAt          time.Time  `gorm:"column:recorded_at;not null;default:CURRENT_TIMESTAMP"`
	ProfileDispatchedAt *time.Time `gorm:"column:profile_dispatched_at;index:idx_recommendation_behavior_profile_pending,priority:1"`
	ProfileAvailableAt  time.Time  `gorm:"column:profile_available_at;not null;default:CURRENT_TIMESTAMP;index:idx_recommendation_behavior_profile_pending,priority:2"`
	ProfileLeasedUntil  *time.Time `gorm:"column:profile_leased_until;index:idx_recommendation_behavior_profile_pending,priority:3"`
	ProfileAttempts     int        `gorm:"column:profile_attempts;not null;default:0"`
	ProfileLastError    string     `gorm:"column:profile_last_error;size:1024;not null;default:''"`
	CreatedAt           time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (BehaviorEventModel) TableName() string {
	return "recommendation_behavior_event"
}

type FeedbackModel struct {
	ID                   int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID               int64     `gorm:"column:user_id;not null;uniqueIndex:uk_recommendation_feedback_user_key,priority:1;index:idx_recommendation_feedback_user_created,priority:1"`
	VideoID              int64     `gorm:"column:video_id;not null;index:idx_recommendation_feedback_video_created,priority:1"`
	RequestID            string    `gorm:"column:request_id;size:64;not null;index:idx_recommendation_feedback_request"`
	FeedbackType         string    `gorm:"column:feedback_type;size:32;not null"`
	IdempotencyKey       string    `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uk_recommendation_feedback_user_key,priority:2"`
	SuppressionScope     string    `gorm:"column:suppression_scope;size:16;not null;default:'video'"`
	SuppressionScopeID   int64     `gorm:"column:suppression_scope_id;not null;default:0;index:idx_recommendation_feedback_suppression,priority:2"`
	SuppressionExpiresAt time.Time `gorm:"column:suppression_expires_at;not null;index:idx_recommendation_feedback_suppression,priority:1"`
	CreatedAt            time.Time `gorm:"column:created_at;not null;autoCreateTime;index:idx_recommendation_feedback_user_created,priority:2;index:idx_recommendation_feedback_video_created,priority:2"`
}

type OutcomeModel struct {
	ID          string    `gorm:"column:id;size:128;primaryKey"`
	RequestID   string    `gorm:"column:request_id;size:64;not null;index:idx_recommendation_outcome_request,priority:1"`
	UserID      int64     `gorm:"column:user_id;not null;index:idx_recommendation_outcome_request,priority:2"`
	VideoID     int64     `gorm:"column:video_id;not null"`
	OutcomeType string    `gorm:"column:outcome_type;size:32;not null"`
	OccurredAt  time.Time `gorm:"column:occurred_at;not null;index:idx_recommendation_outcome_request,priority:3"`
	RecordedAt  time.Time `gorm:"column:recorded_at;not null;default:CURRENT_TIMESTAMP"`
}

func (OutcomeModel) TableName() string { return "recommendation_outcome" }

func (FeedbackModel) TableName() string {
	return "recommendation_feedback"
}

// FeedbackProfileOutboxModel is committed with recommendation_feedback and
// held until asynchronous profile projection succeeds.
type FeedbackProfileOutboxModel struct {
	ID           int64      `gorm:"column:id;primaryKey;autoIncrement"`
	FeedbackID   int64      `gorm:"column:feedback_id;not null;uniqueIndex:uk_recommendation_feedback_profile_outbox_feedback"`
	AvailableAt  time.Time  `gorm:"column:available_at;not null;index:idx_recommendation_feedback_profile_outbox_pending,priority:2"`
	LeasedUntil  *time.Time `gorm:"column:leased_until;index:idx_recommendation_feedback_profile_outbox_pending,priority:3"`
	DispatchedAt *time.Time `gorm:"column:dispatched_at;index:idx_recommendation_feedback_profile_outbox_pending,priority:1"`
	Attempts     int        `gorm:"column:attempts;not null;default:0"`
	LastError    string     `gorm:"column:last_error;size:1024;not null;default:''"`
	CreatedAt    time.Time  `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
}

func (FeedbackProfileOutboxModel) TableName() string {
	return "recommendation_feedback_profile_outbox"
}

type RequestLogModel struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	RequestID     string    `gorm:"column:request_id;size:64;not null;uniqueIndex:uk_recommendation_request_log_user_request,priority:2"`
	UserID        int64     `gorm:"column:user_id;not null;uniqueIndex:uk_recommendation_request_log_user_request,priority:1;index:idx_recommendation_request_log_user_created,priority:1"`
	Scene         string    `gorm:"column:scene;size:32;not null;index:idx_recommendation_request_log_scene_created,priority:1"`
	PolicyVersion int       `gorm:"column:policy_version;not null;index:idx_recommendation_request_log_scene_created,priority:2"`
	PayloadJSON   string    `gorm:"column:payload_json;type:jsonb;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;autoCreateTime;index:idx_recommendation_request_log_user_created,priority:2;index:idx_recommendation_request_log_scene_created,priority:3;index:idx_recommendation_request_log_created"`
}

func (RequestLogModel) TableName() string {
	return "recommendation_request_log"
}

// ServedCandidateEvidenceModel stores only the bounded, server-issued
// membership needed to validate recommendation feedback and attribution.
type ServedCandidateEvidenceModel struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID        int64     `gorm:"column:user_id;not null;uniqueIndex:uk_recommendation_served_candidate_identity,priority:1;index:idx_recommendation_served_candidate_request,priority:1"`
	RequestID     string    `gorm:"column:request_id;size:64;not null;uniqueIndex:uk_recommendation_served_candidate_identity,priority:2;index:idx_recommendation_served_candidate_request,priority:2"`
	VideoID       int64     `gorm:"column:video_id;not null;uniqueIndex:uk_recommendation_served_candidate_identity,priority:3"`
	EvidenceKind  string    `gorm:"column:evidence_kind;size:16;not null;default:'first_page';index:idx_recommendation_served_candidate_request,priority:3"`
	PolicyVersion int       `gorm:"column:policy_version;not null"`
	Position      int       `gorm:"column:position;not null"`
	ServedAt      time.Time `gorm:"column:served_at;not null"`
	ExpiresAt     time.Time `gorm:"column:expires_at;not null;index:idx_recommendation_served_candidate_expiry"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;autoCreateTime"`
}

func (ServedCandidateEvidenceModel) TableName() string {
	return "recommendation_served_candidate"
}
