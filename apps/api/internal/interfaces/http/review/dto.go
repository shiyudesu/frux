package interfaceshttpreview

import "time"

type machineSignalRequest struct {
	Label        string   `json:"label"`
	Confidence   float64  `json:"confidence"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type machineResultRequest struct {
	VideoID       int64                  `json:"video_id"`
	ReviewVersion int                    `json:"review_version"`
	Provider      string                 `json:"provider"`
	ModelVersion  string                 `json:"model_version"`
	SourceKind    string                 `json:"source_kind"`
	GeneratedAt   time.Time              `json:"generated_at"`
	RolloutMode   string                 `json:"rollout_mode"`
	PolicyVersion int                    `json:"policy_version"`
	Signals       []machineSignalRequest `json:"signals"`
}

type machineResultResponse struct {
	CaseID        int64  `json:"case_id"`
	Status        string `json:"status"`
	Outcome       string `json:"outcome"`
	PolicyVersion int    `json:"policy_version"`
	RolloutMode   string `json:"rollout_mode"`
	Duplicate     bool   `json:"duplicate"`
}

type humanClaimRequest struct {
	ExpectedCaseVersion int `json:"expected_case_version"`
}

type humanLeaseRequest struct {
	LeaseToken          string `json:"lease_token"`
	ExpectedCaseVersion int    `json:"expected_case_version"`
}

type humanDecisionRequest struct {
	LeaseToken          string `json:"lease_token"`
	ExpectedCaseVersion int    `json:"expected_case_version"`
	ReviewVersion       int    `json:"review_version"`
	Outcome             string `json:"outcome"`
	ReasonCode          string `json:"reason_code"`
	Note                string `json:"note"`
}

type humanCaseResponse struct {
	ID                 int64      `json:"id"`
	VideoID            int64      `json:"video_id"`
	ReviewVersion      int        `json:"review_version"`
	Status             string     `json:"status"`
	PolicyVersion      int        `json:"policy_version"`
	Priority           int        `json:"priority"`
	Version            int        `json:"version"`
	AssignedReviewerID int64      `json:"assigned_reviewer_id,omitempty"`
	LeaseExpiresAt     *time.Time `json:"lease_expires_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
}

type humanQueueItemResponse struct {
	Case     humanCaseResponse `json:"case"`
	AuthorID int64             `json:"author_id"`
	Title    string            `json:"title"`
	MediaURL string            `json:"media_url"`
	CoverURL string            `json:"cover_url"`
}

type humanQueueResponse struct {
	Items      []humanQueueItemResponse `json:"items"`
	NextCursor string                   `json:"next_cursor"`
	HasMore    bool                     `json:"has_more"`
	Scope      string                   `json:"scope"`
}

type humanLeaseResponse struct {
	Case       humanCaseResponse `json:"case"`
	LeaseToken string            `json:"lease_token"`
	ServerTime time.Time         `json:"server_time"`
}

type humanPreviewResponse struct {
	MediaURL   string    `json:"media_url"`
	CoverURL   string    `json:"cover_url"`
	ExpiresAt  time.Time `json:"expires_at"`
	ServerTime time.Time `json:"server_time"`
}

type humanSubjectResponse struct {
	VideoID       int64  `json:"video_id"`
	AuthorID      int64  `json:"author_id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	MediaURL      string `json:"media_url"`
	CoverURL      string `json:"cover_url"`
	ReviewVersion int    `json:"review_version"`
}

type evidenceSignalResponse struct {
	ID            int64     `json:"id"`
	ResultID      string    `json:"result_id"`
	Label         string    `json:"label"`
	Confidence    float64   `json:"confidence"`
	EvidenceRefs  []string  `json:"evidence_refs"`
	Provider      string    `json:"provider"`
	ModelVersion  string    `json:"model_version"`
	PolicyVersion int       `json:"policy_version"`
	SourceKind    string    `json:"source_kind"`
	GeneratedAt   time.Time `json:"generated_at"`
	CreatedAt     time.Time `json:"created_at"`
}

type automatedDecisionResponse struct {
	ID            int64     `json:"id"`
	ResultID      string    `json:"result_id"`
	Outcome       string    `json:"outcome"`
	PolicyVersion int       `json:"policy_version"`
	RolloutMode   string    `json:"rollout_mode"`
	CreatedAt     time.Time `json:"created_at"`
}

type assignmentResponse struct {
	ID          int64      `json:"id"`
	ReviewerID  int64      `json:"reviewer_id"`
	Event       string     `json:"event"`
	CaseVersion int        `json:"case_version"`
	LeaseUntil  *time.Time `json:"lease_until,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type humanDecisionResponse struct {
	ID            int64     `json:"id"`
	ReviewerID    int64     `json:"reviewer_id"`
	Outcome       string    `json:"outcome"`
	ReasonCode    string    `json:"reason_code"`
	Note          string    `json:"note"`
	ReviewVersion int       `json:"review_version"`
	CaseVersion   int       `json:"case_version"`
	CreatedAt     time.Time `json:"created_at"`
}

type humanHistoryResponse struct {
	Signals            []evidenceSignalResponse    `json:"signals"`
	AutomatedDecisions []automatedDecisionResponse `json:"automated_decisions"`
	Assignments        []assignmentResponse        `json:"assignments"`
	HumanDecisions     []humanDecisionResponse     `json:"human_decisions"`
}

type humanCaseDetailResponse struct {
	Case    humanCaseResponse    `json:"case"`
	Subject humanSubjectResponse `json:"subject"`
	History humanHistoryResponse `json:"history"`
}

type humanDecisionResultResponse struct {
	Case      humanCaseResponse     `json:"case"`
	Decision  humanDecisionResponse `json:"decision"`
	Duplicate bool                  `json:"duplicate"`
}
