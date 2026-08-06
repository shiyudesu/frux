package domainreview

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AssignmentEventClaimed    = "claimed"
	AssignmentEventRenewed    = "renewed"
	AssignmentEventReleased   = "released"
	AssignmentEventExpired    = "expired"
	AssignmentEventDecided    = "decided"
	AssignmentEventCancelled  = "cancelled"
	AssignmentEventSuperseded = "superseded"

	NotificationStatePending   = "pending"
	NotificationStateDelivered = "delivered"
	NotificationStateTerminal  = "terminal"

	ReasonApproveCompliant     = "content_compliant"
	ReasonApproveFalsePositive = "false_positive"
	ReasonRejectSexualContent  = "sexual_content"
	ReasonRejectViolence       = "graphic_violence"
	ReasonRejectHate           = "hate"
	ReasonRejectHarassment     = "harassment"
	ReasonRejectSelfHarm       = "self_harm"
	ReasonRejectIllegal        = "illegal_activity"
	ReasonRejectSpam           = "spam"
	ReasonRejectOther          = "other_policy_violation"

	MaxHumanNoteLength        = 1000
	MaxDecisionKeyLength      = 128
	MinHumanLeaseDuration     = time.Minute
	MaxHumanLeaseDuration     = 30 * time.Minute
	DefaultHumanLeaseDuration = 10 * time.Minute
)

type ReasonCode struct {
	Code         string
	Outcome      string
	RequiresNote bool
}

var humanReasonCodes = map[string]ReasonCode{
	ReasonApproveCompliant:     {Code: ReasonApproveCompliant, Outcome: OutcomeApprove},
	ReasonApproveFalsePositive: {Code: ReasonApproveFalsePositive, Outcome: OutcomeApprove},
	ReasonRejectSexualContent:  {Code: ReasonRejectSexualContent, Outcome: OutcomeReject},
	ReasonRejectViolence:       {Code: ReasonRejectViolence, Outcome: OutcomeReject},
	ReasonRejectHate:           {Code: ReasonRejectHate, Outcome: OutcomeReject},
	ReasonRejectHarassment:     {Code: ReasonRejectHarassment, Outcome: OutcomeReject},
	ReasonRejectSelfHarm:       {Code: ReasonRejectSelfHarm, Outcome: OutcomeReject},
	ReasonRejectIllegal:        {Code: ReasonRejectIllegal, Outcome: OutcomeReject},
	ReasonRejectSpam:           {Code: ReasonRejectSpam, Outcome: OutcomeReject},
	ReasonRejectOther:          {Code: ReasonRejectOther, Outcome: OutcomeReject, RequiresNote: true},
}

func RegisteredReasonCode(outcome, code string) (ReasonCode, bool) {
	reason, ok := humanReasonCodes[normalizeToken(code)]
	return reason, ok && reason.Outcome == normalizeToken(outcome)
}

type ReviewerAssignment struct {
	ID          int64
	CaseID      int64
	ReviewerID  int64
	Event       string
	CaseVersion int
	LeaseUntil  *time.Time
	CreatedAt   time.Time
}

type HumanDecisionInput struct {
	CaseID              int64
	ReviewerID          int64
	Outcome             string
	ReasonCode          string
	Note                string
	ReviewVersion       int
	ExpectedCaseVersion int
	IdempotencyKey      string
	DecidedAt           time.Time
}

type HumanDecision struct {
	ID                 int64
	CaseID             int64
	ReviewerID         int64
	Outcome            string
	ReasonCode         string
	Note               string
	ReviewVersion      int
	CaseVersion        int
	IdempotencyKeyHash string
	PayloadHash        string
	CreatedAt          time.Time
}

func NewHumanDecision(input HumanDecisionInput) (*HumanDecision, error) {
	if input.CaseID <= 0 {
		return nil, ErrInvalidCaseID
	}
	if input.ReviewerID <= 0 {
		return nil, ErrInvalidReviewerID
	}
	outcome := normalizeToken(input.Outcome)
	if outcome != OutcomeApprove && outcome != OutcomeReject {
		return nil, ErrInvalidDecisionOutcome
	}
	reasonCode := normalizeToken(input.ReasonCode)
	reason, ok := RegisteredReasonCode(outcome, reasonCode)
	if !ok {
		return nil, ErrInvalidReasonCode
	}
	note := strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(note) > MaxHumanNoteLength {
		return nil, ErrReviewNoteTooLong
	}
	if reason.RequiresNote && note == "" {
		return nil, ErrReviewNoteRequired
	}
	if input.ReviewVersion <= 0 {
		return nil, ErrInvalidReviewVersion
	}
	if input.ExpectedCaseVersion <= 0 {
		return nil, ErrInvalidCaseVersion
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" || len(key) > MaxDecisionKeyLength {
		return nil, ErrInvalidIdempotencyKey
	}
	decidedAt := input.DecidedAt
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	decision := &HumanDecision{
		CaseID: input.CaseID, ReviewerID: input.ReviewerID, Outcome: outcome,
		ReasonCode: reasonCode, Note: note, ReviewVersion: input.ReviewVersion,
		CaseVersion: input.ExpectedCaseVersion, CreatedAt: decidedAt.UTC().Truncate(time.Microsecond),
	}
	keySum := sha256.Sum256([]byte(key))
	decision.IdempotencyKeyHash = hex.EncodeToString(keySum[:])
	payload, err := json.Marshal(struct {
		CaseID        int64  `json:"case_id"`
		ReviewerID    int64  `json:"reviewer_id"`
		Outcome       string `json:"outcome"`
		ReasonCode    string `json:"reason_code"`
		Note          string `json:"note"`
		ReviewVersion int    `json:"review_version"`
		CaseVersion   int    `json:"case_version"`
	}{
		decision.CaseID, decision.ReviewerID, decision.Outcome, decision.ReasonCode,
		decision.Note, decision.ReviewVersion, decision.CaseVersion,
	})
	if err != nil {
		return nil, err
	}
	payloadSum := sha256.Sum256(payload)
	decision.PayloadHash = hex.EncodeToString(payloadSum[:])
	return decision, nil
}

func ValidAssignmentEvent(event string) bool {
	switch normalizeToken(event) {
	case AssignmentEventClaimed, AssignmentEventRenewed, AssignmentEventReleased,
		AssignmentEventExpired, AssignmentEventDecided, AssignmentEventCancelled,
		AssignmentEventSuperseded:
		return true
	default:
		return false
	}
}

func ValidPriority(priority int) bool { return priority >= 0 && priority <= 100 }

func ValidLeaseDuration(duration time.Duration) bool {
	return duration >= MinHumanLeaseDuration && duration <= MaxHumanLeaseDuration
}

func (c *ReviewCase) Claim(reviewerID int64, tokenHash string, expectedVersion int, now time.Time, duration time.Duration) error {
	if c == nil || c.Status != CaseStatusPendingHuman {
		return ErrReviewCaseNotHuman
	}
	if reviewerID <= 0 {
		return ErrInvalidReviewerID
	}
	if !validTokenHash(tokenHash) {
		return ErrInvalidLeaseToken
	}
	if now.IsZero() || !ValidLeaseDuration(duration) {
		return ErrInvalidLeaseDuration
	}
	now = now.UTC()
	if c.AssignedReviewerID > 0 && c.LeaseExpiresAt != nil && now.Before(c.LeaseExpiresAt.UTC()) {
		return ErrReviewCaseClaimed
	}
	if expectedVersion <= 0 || c.Version != expectedVersion {
		return ErrReviewCaseVersion
	}
	expiresAt := now.Add(duration).Truncate(time.Microsecond)
	c.AssignedReviewerID = reviewerID
	c.LeaseTokenHash = tokenHash
	c.LeaseExpiresAt = &expiresAt
	c.Version++
	c.UpdatedAt = now.Truncate(time.Microsecond)
	return nil
}

func (c *ReviewCase) Renew(reviewerID int64, tokenHash string, expectedVersion int, now time.Time, duration time.Duration) error {
	if err := c.validateLease(reviewerID, tokenHash, expectedVersion, now); err != nil {
		return err
	}
	if !ValidLeaseDuration(duration) {
		return ErrInvalidLeaseDuration
	}
	expiresAt := now.UTC().Add(duration).Truncate(time.Microsecond)
	c.LeaseExpiresAt = &expiresAt
	c.Version++
	c.UpdatedAt = now.UTC().Truncate(time.Microsecond)
	return nil
}

func (c *ReviewCase) Release(reviewerID int64, tokenHash string, expectedVersion int, now time.Time) error {
	if err := c.validateLease(reviewerID, tokenHash, expectedVersion, now); err != nil {
		return err
	}
	c.clearLease()
	c.Version++
	c.UpdatedAt = now.UTC().Truncate(time.Microsecond)
	return nil
}

func (c *ReviewCase) Expire(now time.Time) bool {
	if c == nil || c.AssignedReviewerID <= 0 || c.LeaseExpiresAt == nil || now.IsZero() ||
		now.UTC().Before(c.LeaseExpiresAt.UTC()) {
		return false
	}
	c.clearLease()
	c.Version++
	c.UpdatedAt = now.UTC().Truncate(time.Microsecond)
	return true
}

func (c *ReviewCase) ValidateDecision(reviewerID int64, tokenHash string, expectedVersion, reviewVersion int, now time.Time) error {
	if c == nil || c.Status != CaseStatusPendingHuman {
		return ErrReviewCaseNotHuman
	}
	if c.ReviewVersion != reviewVersion {
		return ErrReviewSubjectStale
	}
	return c.validateLease(reviewerID, tokenHash, expectedVersion, now)
}

func (c *ReviewCase) validateLease(reviewerID int64, tokenHash string, expectedVersion int, now time.Time) error {
	if c == nil || c.Status != CaseStatusPendingHuman {
		return ErrReviewCaseNotHuman
	}
	if expectedVersion <= 0 || c.Version != expectedVersion {
		return ErrReviewCaseVersion
	}
	if reviewerID <= 0 || c.AssignedReviewerID != reviewerID {
		return ErrReviewLeaseNotOwned
	}
	if !validTokenHash(tokenHash) || !secureTokenHashEqual(c.LeaseTokenHash, tokenHash) {
		return ErrReviewLeaseNotOwned
	}
	if now.IsZero() || c.LeaseExpiresAt == nil || !now.UTC().Before(c.LeaseExpiresAt.UTC()) {
		return ErrReviewLeaseExpired
	}
	return nil
}

func (c *ReviewCase) clearLease() {
	c.AssignedReviewerID = 0
	c.LeaseTokenHash = ""
	c.LeaseExpiresAt = nil
}

func validTokenHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func secureTokenHashEqual(expected, actual string) bool {
	return len(expected) == len(actual) &&
		subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

type QueueCursor struct {
	Priority  int
	CreatedAt time.Time
	CaseID    int64
}

type HumanQueueFilter struct {
	MinPriority int
	MaxPriority int
	Cursor      *QueueCursor
	Limit       int
}

type HumanQueueItem struct {
	Case     *ReviewCase
	Title    string
	AuthorID int64
	MediaURL string
	CoverURL string
}

type ReviewSubject struct {
	VideoID       int64
	AuthorID      int64
	Title         string
	Description   string
	MediaURL      string
	CoverURL      string
	ReviewVersion int
}

type CaseHistory struct {
	Signals            []*EvidenceSignal
	AutomatedDecisions []*AutomatedDecision
	Assignments        []*ReviewerAssignment
	HumanDecisions     []*HumanDecision
}

type EvidenceSignal struct {
	ID            int64
	ResultID      string
	Label         string
	Confidence    float64
	EvidenceRefs  []string
	Provider      string
	ModelVersion  string
	PolicyVersion int
	CreatedAt     time.Time
}

type HumanCaseDetail struct {
	Case    *ReviewCase
	Subject ReviewSubject
	History CaseHistory
}

type LeaseResult struct {
	Case       *ReviewCase
	LeaseToken string
	Duplicate  bool
}

type HumanDecisionResult struct {
	Case             *ReviewCase
	Decision         *HumanDecision
	Duplicate        bool
	ApplySideEffects bool
	MediaAssetID     int64
	CoverAssetID     int64
}

type ReviewNotification struct {
	EventID     string
	RecipientID int64
	VideoID     int64
	Outcome     string
	State       string
	Attempts    int
	AvailableAt time.Time
	LeaseOwner  string
	LeaseUntil  *time.Time
	LastError   string
	DeliveredAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
