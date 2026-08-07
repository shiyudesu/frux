package infrareview

import "time"

type CaseModel struct {
	ID                 int64      `gorm:"column:id;primaryKey;autoIncrement;index:idx_review_case_human_queue,priority:4;index:idx_review_case_reviewer_work,priority:5"`
	VideoID            int64      `gorm:"column:video_id;not null;uniqueIndex:uk_review_case_video_version,priority:1;index:idx_review_case_status_created,priority:2"`
	ReviewVersion      int        `gorm:"column:review_version;not null;uniqueIndex:uk_review_case_video_version,priority:2"`
	Status             string     `gorm:"column:status;size:32;not null;index:idx_review_case_status_created,priority:1;index:idx_review_case_human_queue,priority:1;index:idx_review_case_reviewer_work,priority:1"`
	PolicyVersion      int        `gorm:"column:policy_version;not null"`
	Priority           int        `gorm:"column:priority;not null;default:0;index:idx_review_case_human_queue,priority:2,sort:desc;index:idx_review_case_reviewer_work,priority:4,sort:desc"`
	Version            int        `gorm:"column:version;not null;default:1"`
	AssignedReviewerID int64      `gorm:"column:assigned_reviewer_id;not null;default:0;index:idx_review_case_lease,priority:1;index:idx_review_case_reviewer_work,priority:2"`
	LeaseTokenHash     string     `gorm:"column:lease_token_hash;size:64;not null;default:''"`
	LeaseExpiresAt     *time.Time `gorm:"column:lease_expires_at;index:idx_review_case_lease,priority:2;index:idx_review_case_reviewer_work,priority:3"`
	CreatedAt          time.Time  `gorm:"column:created_at;not null;autoCreateTime;index:idx_review_case_status_created,priority:3;index:idx_review_case_human_queue,priority:3"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;not null;autoUpdateTime"`
	ClosedAt           *time.Time `gorm:"column:closed_at"`
}

func (CaseModel) TableName() string { return "review_case" }

type ResultModel struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	CaseID        int64     `gorm:"column:case_id;not null;index:idx_review_result_case"`
	VideoID       int64     `gorm:"column:video_id;not null"`
	ReviewVersion int       `gorm:"column:review_version;not null"`
	Provider      string    `gorm:"column:provider;size:64;not null;uniqueIndex:uk_review_result_provider_identity,priority:1"`
	ResultID      string    `gorm:"column:result_id;size:128;not null;uniqueIndex:uk_review_result_provider_identity,priority:2"`
	PayloadHash   string    `gorm:"column:payload_hash;size:64;not null"`
	ModelVersion  string    `gorm:"column:model_version;size:128;not null"`
	PolicyVersion int       `gorm:"column:policy_version;not null"`
	Outcome       string    `gorm:"column:outcome;size:16;not null"`
	DecisionID    int64     `gorm:"column:decision_id;not null;default:0"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;autoCreateTime"`
}

func (ResultModel) TableName() string { return "review_machine_result" }

type SignalModel struct {
	ID               int64     `gorm:"column:id;primaryKey;autoIncrement"`
	CaseID           int64     `gorm:"column:case_id;not null;index:idx_review_signal_case_created,priority:1"`
	ResultReceiptID  int64     `gorm:"column:result_receipt_id;not null;index:idx_review_signal_result"`
	Label            string    `gorm:"column:label;size:64;not null"`
	Confidence       float64   `gorm:"column:confidence;type:double precision;not null"`
	EvidenceRefsJSON string    `gorm:"column:evidence_refs_json;type:jsonb;not null"`
	Provider         string    `gorm:"column:provider;size:64;not null"`
	ModelVersion     string    `gorm:"column:model_version;size:128;not null"`
	PolicyVersion    int       `gorm:"column:policy_version;not null"`
	CreatedAt        time.Time `gorm:"column:created_at;not null;autoCreateTime;index:idx_review_signal_case_created,priority:2"`
}

func (SignalModel) TableName() string { return "review_signal" }

type DecisionModel struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	CaseID          int64     `gorm:"column:case_id;not null;index:idx_review_decision_case_created,priority:1"`
	ResultReceiptID int64     `gorm:"column:result_receipt_id;not null;uniqueIndex:uk_review_decision_result"`
	Outcome         string    `gorm:"column:outcome;size:16;not null"`
	PolicyVersion   int       `gorm:"column:policy_version;not null"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;autoCreateTime;index:idx_review_decision_case_created,priority:2"`
}

func (DecisionModel) TableName() string { return "review_decision" }

type PolicyModel struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Version    int       `gorm:"column:version;not null;uniqueIndex:uk_review_policy_version"`
	Enabled    bool      `gorm:"column:enabled;not null;default:false;index:idx_review_policy_enabled_version,priority:1"`
	ConfigJSON string    `gorm:"column:config_json;type:jsonb;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null;autoUpdateTime;index:idx_review_policy_enabled_version,priority:2"`
}

func (PolicyModel) TableName() string { return "review_policy" }

type AssignmentModel struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	CaseID      int64      `gorm:"column:case_id;not null;index:idx_review_assignment_case_created,priority:1"`
	ReviewerID  int64      `gorm:"column:reviewer_id;not null;index:idx_review_assignment_reviewer_created,priority:1"`
	Event       string     `gorm:"column:event;size:16;not null"`
	CaseVersion int        `gorm:"column:case_version;not null"`
	LeaseUntil  *time.Time `gorm:"column:lease_until"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null;index:idx_review_assignment_case_created,priority:2;index:idx_review_assignment_reviewer_created,priority:2"`
}

func (AssignmentModel) TableName() string { return "review_assignment_history" }

type HumanDecisionModel struct {
	ID            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	CaseID        int64     `gorm:"column:case_id;not null;uniqueIndex:uk_review_human_decision_case;index:idx_review_human_decision_case_created,priority:1"`
	ReviewerID    int64     `gorm:"column:reviewer_id;not null;index:idx_review_human_decision_reviewer_created,priority:1"`
	Outcome       string    `gorm:"column:outcome;size:16;not null"`
	ReasonCode    string    `gorm:"column:reason_code;size:64;not null"`
	Note          string    `gorm:"column:note;size:4000;not null;default:''"`
	ReviewVersion int       `gorm:"column:review_version;not null"`
	CaseVersion   int       `gorm:"column:case_version;not null"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;index:idx_review_human_decision_case_created,priority:2;index:idx_review_human_decision_reviewer_created,priority:2,sort:desc"`
}

func (HumanDecisionModel) TableName() string { return "review_human_decision" }

type HumanDecisionIdempotencyModel struct {
	ID                 int64     `gorm:"column:id;primaryKey;autoIncrement"`
	CaseID             int64     `gorm:"column:case_id;not null;uniqueIndex:uk_review_human_decision_idempotency,priority:1"`
	ReviewerID         int64     `gorm:"column:reviewer_id;not null;uniqueIndex:uk_review_human_decision_idempotency,priority:2"`
	IdempotencyKeyHash string    `gorm:"column:idempotency_key_hash;size:64;not null;uniqueIndex:uk_review_human_decision_idempotency,priority:3"`
	PayloadHash        string    `gorm:"column:payload_hash;size:64;not null"`
	DecisionID         int64     `gorm:"column:decision_id;not null"`
	CreatedAt          time.Time `gorm:"column:created_at;not null"`
}

func (HumanDecisionIdempotencyModel) TableName() string {
	return "review_human_decision_idempotency"
}

type NotificationOutboxModel struct {
	EventID       string     `gorm:"column:event_id;size:64;primaryKey"`
	RecipientID   int64      `gorm:"column:recipient_id;not null"`
	VideoID       int64      `gorm:"column:video_id;not null"`
	Outcome       string     `gorm:"column:outcome;size:16;not null"`
	ReviewVersion int        `gorm:"column:review_version;not null;default:0"`
	Stage         string     `gorm:"column:stage;size:32;not null;default:''"`
	Result        string     `gorm:"column:result;size:32;not null;default:''"`
	ReasonCode    string     `gorm:"column:reason_code;size:64;not null;default:''"`
	OccurredAt    *time.Time `gorm:"column:occurred_at"`
	State         string     `gorm:"column:state;size:16;not null;default:'pending';index:idx_review_notification_outbox_pending,priority:1"`
	Attempts      int        `gorm:"column:attempts;not null;default:0"`
	AvailableAt   time.Time  `gorm:"column:available_at;not null;index:idx_review_notification_outbox_pending,priority:2"`
	LeaseOwner    string     `gorm:"column:lease_owner;size:128;not null;default:''"`
	LeaseUntil    *time.Time `gorm:"column:lease_until;index:idx_review_notification_outbox_pending,priority:3"`
	LastError     string     `gorm:"column:last_error;size:1024;not null;default:''"`
	DeliveredAt   *time.Time `gorm:"column:delivered_at"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;not null"`
}

func (NotificationOutboxModel) TableName() string { return "review_notification_outbox" }
