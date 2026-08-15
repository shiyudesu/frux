package domainadminaudit

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
)

const (
	MaxTargetIDLength        = 128
	MaxRequestIDLength       = 128
	MaxIdempotencyKeyLength  = 128
	IdempotencyKeyHashLength = 71
	MaxDetailEntries         = 13
	MaxDetailKeyLength       = 64
	MaxDetailValueLength     = 256
	MaxDetailBytes           = 2048
	MaxQueryLimit            = 100
	MaxQueryRange            = 31 * 24 * time.Hour
)

type Action string

const (
	ActionAuditQuery            Action = "audit.query"
	ActionReviewDecide          Action = "review.decide"
	ActionContentEnforce        Action = "content.enforce"
	ActionContentRestore        Action = "content.restore"
	ActionConfigPublish         Action = "config.publish"
	ActionGovernanceExecute     Action = "governance.execute"
	ActionDeadLetterReplay      Action = "dead_letter.replay"
	ActionKafkaDeadLetterReplay Action = "kafka_dead_letter.replay"
	ActionAccountFreeze         Action = "account.freeze"
	ActionAccountUnfreeze       Action = "account.unfreeze"
	ActionAccountSessionsRevoke Action = "account.sessions_revoke"
	ActionMediaProcessingRetry  Action = "media_processing.retry"
)

type TargetType string

const (
	TargetAuditTrail            TargetType = "audit_trail"
	TargetReviewCase            TargetType = "review_case"
	TargetVideo                 TargetType = "video"
	TargetConfig                TargetType = "config"
	TargetGovernanceControl     TargetType = "governance_control"
	TargetDeadLetterMessage     TargetType = "dead_letter_message"
	TargetKafkaDeadLetterRecord TargetType = "kafka_dead_letter_record"
	TargetUserAccount           TargetType = "user_account"
	TargetMediaProcessingJob    TargetType = "media_processing_job"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeDenied  Outcome = "denied"
	OutcomeFailure Outcome = "failure"
)

var allowedDetailsByAction = map[Action]map[string]struct{}{
	ActionAuditQuery: detailKeys("filter_count", "http_method", "reason_code", "route"),
	ActionReviewDecide: detailKeys(
		"decision", "http_method", "reason_code", "review_version", "route",
	),
	ActionContentEnforce: detailKeys(
		"http_method", "new_status", "previous_status", "reason_code", "route",
	),
	ActionContentRestore: detailKeys(
		"http_method", "new_status", "previous_status", "reason_code", "route",
	),
	ActionConfigPublish: detailKeys(
		"http_method", "new_revision", "previous_revision", "reason_code", "route",
	),
	ActionGovernanceExecute: detailKeys(
		"http_method", "new_revision", "operation", "previous_revision", "reason_code", "route",
	),
	ActionDeadLetterReplay: detailKeys(
		"failure_code", "http_method", "original_event_id", "queue", "reason_code",
		"replay_id", "route",
	),
	ActionKafkaDeadLetterReplay: detailKeys(
		"consumer_group", "failure_code", "http_method", "offset", "original_event_id",
		"partition", "reason_code", "replay_id", "route", "source_offset",
		"source_partition", "source_topic", "topic",
	),
	ActionAccountFreeze: detailKeys(
		"http_method", "new_status", "new_version", "previous_status", "previous_version",
		"reason_code", "revoked_session_count", "route",
	),
	ActionAccountUnfreeze: detailKeys(
		"http_method", "new_status", "new_version", "previous_status", "previous_version",
		"reason_code", "revoked_session_count", "route",
	),
	ActionAccountSessionsRevoke: detailKeys(
		"http_method", "new_status", "new_version", "previous_status", "previous_version",
		"reason_code", "revoked_session_count", "route",
	),
	ActionMediaProcessingRetry: detailKeys(
		"http_method", "new_state", "previous_attempts", "previous_state",
		"reason_code", "route", "video_id",
	),
}

type actionSchema struct {
	permission     domainaccount.AdminPermission
	targetType     TargetType
	route          string
	method         string
	successKeys    map[string]struct{}
	successReasons map[string]struct{}
}

var schemasByAction = map[Action]actionSchema{
	ActionAuditQuery: {
		permission:  domainaccount.PermissionAuditRead,
		targetType:  TargetAuditTrail,
		route:       "/api/admin/audit-events",
		method:      "GET",
		successKeys: detailKeys("filter_count", "http_method", "route"),
	},
	ActionReviewDecide: {
		permission:  domainaccount.PermissionReviewDecide,
		targetType:  TargetReviewCase,
		route:       "/api/admin/review/cases/:caseId/decision",
		method:      "POST",
		successKeys: detailKeys("decision", "http_method", "reason_code", "review_version", "route"),
		successReasons: detailKeys(
			"content_compliant", "false_positive", "sexual_content", "graphic_violence",
			"hate", "harassment", "self_harm", "illegal_activity", "spam", "other_policy_violation",
		),
	},
	ActionContentEnforce: {
		permission:     domainaccount.PermissionContentEnforce,
		targetType:     TargetVideo,
		route:          "/api/admin/videos/:videoId/enforcement",
		method:         "POST",
		successKeys:    detailKeys("http_method", "new_status", "previous_status", "reason_code", "route"),
		successReasons: detailKeys("manual_enforcement", "policy_violation"),
	},
	ActionContentRestore: {
		permission:     domainaccount.PermissionContentEnforce,
		targetType:     TargetVideo,
		route:          "/api/admin/videos/:videoId/restoration",
		method:         "POST",
		successKeys:    detailKeys("http_method", "new_status", "previous_status", "reason_code", "route"),
		successReasons: detailKeys("compliance_restored"),
	},
	ActionConfigPublish: {
		permission:     domainaccount.PermissionConfigPublish,
		targetType:     TargetConfig,
		route:          "/api/admin/configs/:configKey",
		method:         "PATCH",
		successKeys:    detailKeys("http_method", "new_revision", "previous_revision", "reason_code", "route"),
		successReasons: detailKeys("configuration_changed"),
	},
	ActionGovernanceExecute: {
		permission:     domainaccount.PermissionGovernanceExecute,
		targetType:     TargetGovernanceControl,
		successKeys:    detailKeys("http_method", "new_revision", "operation", "previous_revision", "reason_code", "route"),
		successReasons: detailKeys("governance_changed"),
	},
	ActionDeadLetterReplay: {
		permission: domainaccount.PermissionGovernanceExecute,
		targetType: TargetDeadLetterMessage,
		route:      "/api/admin/dead-letter-messages/:messageId/replay",
		method:     "POST",
		successKeys: detailKeys(
			"http_method", "original_event_id", "queue", "reason_code", "replay_id", "route",
		),
	},
	ActionKafkaDeadLetterReplay: {
		permission: domainaccount.PermissionGovernanceExecute,
		targetType: TargetKafkaDeadLetterRecord,
		route:      "/api/admin/kafka-dead-letters/:topic/records/:partition/:offset/replay",
		method:     "POST",
		successKeys: detailKeys(
			"consumer_group", "http_method", "offset", "original_event_id",
			"partition", "reason_code", "replay_id", "route", "source_offset",
			"source_partition", "source_topic", "topic",
		),
	},
	ActionAccountFreeze: {
		permission: domainaccount.PermissionAccountManage,
		targetType: TargetUserAccount,
		route:      "/api/admin/accounts/:userId/freeze",
		method:     "POST",
		successKeys: detailKeys(
			"http_method", "new_status", "new_version", "previous_status", "previous_version",
			"reason_code", "revoked_session_count", "route",
		),
		successReasons: detailKeys(
			domainaccount.AccountReasonPolicyViolation,
			domainaccount.AccountReasonAbuse,
			domainaccount.AccountReasonSecurityRisk,
		),
	},
	ActionAccountUnfreeze: {
		permission: domainaccount.PermissionAccountManage,
		targetType: TargetUserAccount,
		route:      "/api/admin/accounts/:userId/unfreeze",
		method:     "POST",
		successKeys: detailKeys(
			"http_method", "new_status", "new_version", "previous_status", "previous_version",
			"reason_code", "revoked_session_count", "route",
		),
		successReasons: detailKeys(
			domainaccount.AccountReasonAppealApproved,
			domainaccount.AccountReasonIssueResolved,
			domainaccount.AccountReasonManualCorrection,
		),
	},
	ActionAccountSessionsRevoke: {
		permission: domainaccount.PermissionAccountManage,
		targetType: TargetUserAccount,
		route:      "/api/admin/accounts/:userId/sessions/revoke",
		method:     "POST",
		successKeys: detailKeys(
			"http_method", "new_status", "new_version", "previous_status", "previous_version",
			"reason_code", "revoked_session_count", "route",
		),
		successReasons: detailKeys(
			domainaccount.AccountReasonSecurityResponse,
			domainaccount.AccountReasonUserRequest,
			domainaccount.AccountReasonOperatorRequest,
		),
	},
	ActionMediaProcessingRetry: {
		permission: domainaccount.PermissionContentEnforce,
		targetType: TargetMediaProcessingJob,
		successKeys: detailKeys(
			"http_method", "new_state", "previous_attempts", "previous_state",
			"reason_code", "route", "video_id",
		),
		successReasons: detailKeys(
			"configuration_changed", "temporary_failure", "operator_retry",
		),
	},
}

var validTargetTypes = map[TargetType]struct{}{
	TargetAuditTrail:            {},
	TargetReviewCase:            {},
	TargetVideo:                 {},
	TargetConfig:                {},
	TargetGovernanceControl:     {},
	TargetDeadLetterMessage:     {},
	TargetKafkaDeadLetterRecord: {},
	TargetUserAccount:           {},
	TargetMediaProcessingJob:    {},
}

var detailNumberPattern = regexp.MustCompile(`^[0-9]+$`)
var detailCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var detailIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
var requestIDPattern = regexp.MustCompile(`^audit-[a-f0-9]{32}$`)
var idempotencyKeyHashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var requestIDFallbackCounter atomic.Uint64

var deniedReasonCodes = detailKeys("account_missing", "inactive_account", "permission_denied")

var validDecisions = map[string]struct{}{
	"approved": {},
	"rejected": {},
}

var reviewApproveReasons = detailKeys("content_compliant", "false_positive")
var reviewRejectReasons = detailKeys(
	"sexual_content", "graphic_violence", "hate", "harassment", "self_harm",
	"illegal_activity", "spam", "other_policy_violation",
)

var governanceRoutes = map[string]string{
	"/api/admin/governance/controls":                "GET",
	"/api/admin/governance/controls/:key/revisions": "GET",
	"/api/admin/governance/controls/:key":           "PATCH",
	"/api/admin/governance/controls/:key/rollback":  "POST",
}

var kafkaDeadLetterRoutes = map[string]string{
	"/api/admin/kafka-dead-letters":                                          "GET",
	"/api/admin/kafka-dead-letters/:topic/records":                           "GET",
	"/api/admin/kafka-dead-letters/:topic/records/:partition/:offset/replay": "POST",
}

var mediaProcessingRetryRoutes = map[string]string{
	"/api/admin/media-processing/jobs/:jobId/retry": "POST",
	"/api/admin/media-processing/jobs/bulk-retry":   "POST",
}

var validStatuses = map[string]struct{}{
	"offline":        {},
	"pending_review": {},
	"published":      {},
	"rejected":       {},
	"normal":         {},
	"frozen":         {},
}

type FactInput struct {
	ActorID            int64
	Permission         domainaccount.AdminPermission
	Action             Action
	TargetType         TargetType
	TargetID           string
	Outcome            Outcome
	RequestID          string
	IdempotencyKeyHash string
	Detail             map[string]string
	CreatedAt          time.Time
}

type Fact struct {
	id                 int64
	actorID            int64
	permission         domainaccount.AdminPermission
	action             Action
	targetType         TargetType
	targetID           string
	outcome            Outcome
	requestID          string
	idempotencyKeyHash string
	detail             map[string]string
	createdAt          time.Time
}

func NewFact(input FactInput) (*Fact, error) {
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.IdempotencyKeyHash = strings.TrimSpace(input.IdempotencyKeyHash)
	input.CreatedAt = input.CreatedAt.UTC()

	if input.ActorID <= 0 {
		return nil, ErrInvalidActorID
	}
	if !domainaccount.ValidAdminPermission(input.Permission) {
		return nil, ErrInvalidPermission
	}
	if !ValidAction(input.Action) {
		return nil, ErrInvalidAction
	}
	schema := schemasByAction[input.Action]
	if input.Permission != schema.permission {
		return nil, ErrInvalidPermission
	}
	if !ValidTargetType(input.TargetType) {
		return nil, ErrInvalidTargetType
	}
	if input.TargetType != schema.targetType {
		return nil, ErrInvalidTargetType
	}
	if input.TargetID == "" {
		return nil, ErrInvalidTargetID
	}
	if len(input.TargetID) > MaxTargetIDLength {
		return nil, ErrTargetIDTooLong
	}
	if !ValidOutcome(input.Outcome) {
		return nil, ErrInvalidOutcome
	}
	if input.RequestID == "" {
		return nil, ErrInvalidRequestID
	}
	if len(input.RequestID) > MaxRequestIDLength {
		return nil, ErrRequestIDTooLong
	}
	if !requestIDPattern.MatchString(input.RequestID) {
		return nil, ErrInvalidRequestID
	}
	if input.IdempotencyKeyHash != "" && !idempotencyKeyHashPattern.MatchString(input.IdempotencyKeyHash) {
		return nil, ErrInvalidIdempotencyKeyHash
	}
	if input.CreatedAt.IsZero() {
		return nil, ErrInvalidCreatedAt
	}

	detail, err := validateDetail(input.Action, input.Outcome, input.Detail)
	if err != nil {
		return nil, err
	}
	return &Fact{
		actorID:            input.ActorID,
		permission:         input.Permission,
		action:             input.Action,
		targetType:         input.TargetType,
		targetID:           input.TargetID,
		outcome:            input.Outcome,
		requestID:          input.RequestID,
		idempotencyKeyHash: input.IdempotencyKeyHash,
		detail:             detail,
		createdAt:          input.CreatedAt,
	}, nil
}

func RestoreFact(id int64, input FactInput) (*Fact, error) {
	if id <= 0 {
		return nil, ErrInvalidEventID
	}
	fact, err := NewFact(input)
	if err != nil {
		return nil, err
	}
	fact.id = id
	return fact, nil
}

func ValidAction(action Action) bool {
	_, ok := allowedDetailsByAction[action]
	return ok
}

func ValidTargetType(targetType TargetType) bool {
	_, ok := validTargetTypes[targetType]
	return ok
}

func ValidOutcome(outcome Outcome) bool {
	return outcome == OutcomeSuccess || outcome == OutcomeDenied || outcome == OutcomeFailure
}

func (f *Fact) ID() int64                                 { return f.id }
func (f *Fact) ActorID() int64                            { return f.actorID }
func (f *Fact) Permission() domainaccount.AdminPermission { return f.permission }
func (f *Fact) Action() Action                            { return f.action }
func (f *Fact) TargetType() TargetType                    { return f.targetType }
func (f *Fact) TargetID() string                          { return f.targetID }
func (f *Fact) Outcome() Outcome                          { return f.outcome }
func (f *Fact) RequestID() string                         { return f.requestID }
func (f *Fact) IdempotencyKeyHash() string                { return f.idempotencyKeyHash }
func (f *Fact) CreatedAt() time.Time                      { return f.createdAt }

func (f *Fact) Detail() map[string]string {
	if f == nil {
		return nil
	}
	return cloneDetail(f.detail)
}

func validateDetail(action Action, outcome Outcome, detail map[string]string) (map[string]string, error) {
	if len(detail) > MaxDetailEntries {
		return nil, ErrDetailTooLarge
	}
	allowed := allowedDetailsByAction[action]
	cleaned := make(map[string]string, len(detail))
	for rawKey, rawValue := range detail {
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(rawValue)
		if key == "" || len(key) > MaxDetailKeyLength || len(value) > MaxDetailValueLength {
			return nil, ErrInvalidDetail
		}
		if _, ok := allowed[key]; !ok {
			return nil, ErrInvalidDetail
		}
		if !validDetailValue(action, key, value) {
			return nil, ErrInvalidDetail
		}
		cleaned[key] = value
	}
	if !validDetailSchema(action, outcome, cleaned) {
		return nil, ErrInvalidDetail
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, ErrInvalidDetail
	}
	if len(encoded) > MaxDetailBytes {
		return nil, ErrDetailTooLarge
	}
	return cleaned, nil
}

func validDetailValue(action Action, key, value string) bool {
	if value == "" {
		return false
	}
	switch key {
	case "filter_count", "new_revision", "previous_revision", "review_version",
		"offset", "partition", "source_offset", "source_partition",
		"new_version", "previous_version", "revoked_session_count",
		"previous_attempts", "video_id":
		return len(value) <= 20 && detailNumberPattern.MatchString(value)
	case "http_method":
		switch value {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
			return true
		default:
			return false
		}
	case "route":
		if action == ActionGovernanceExecute {
			_, ok := governanceRoutes[value]
			return ok
		}
		if action == ActionKafkaDeadLetterReplay {
			_, ok := kafkaDeadLetterRoutes[value]
			return ok
		}
		if action == ActionMediaProcessingRetry {
			_, ok := mediaProcessingRetryRoutes[value]
			return ok
		}
		return value == schemasByAction[action].route
	case "operation":
		return action == ActionGovernanceExecute && (value == "update" || value == "rollback")
	case "decision":
		_, ok := validDecisions[value]
		return ok
	case "new_status", "previous_status":
		_, ok := validStatuses[value]
		return ok
	case "new_state", "previous_state":
		return action == ActionMediaProcessingRetry &&
			(value == "failed" || value == "retryable")
	case "reason_code":
		return len(value) <= 64 && detailCodePattern.MatchString(value)
	case "failure_code":
		return (action == ActionDeadLetterReplay || action == ActionKafkaDeadLetterReplay) &&
			len(value) <= 64 && detailCodePattern.MatchString(value)
	case "queue":
		return action == ActionDeadLetterReplay &&
			len(value) <= MaxDetailValueLength && detailIdentifierPattern.MatchString(value)
	case "topic", "source_topic", "consumer_group":
		return action == ActionKafkaDeadLetterReplay &&
			len(value) <= MaxDetailValueLength && detailIdentifierPattern.MatchString(value)
	case "original_event_id", "replay_id":
		return (action == ActionDeadLetterReplay || action == ActionKafkaDeadLetterReplay) &&
			len(value) <= MaxDetailValueLength && detailIdentifierPattern.MatchString(value)
	default:
		return false
	}
}

func validDetailSchema(action Action, outcome Outcome, detail map[string]string) bool {
	schema := schemasByAction[action]
	if outcome == OutcomeDenied {
		if !sameDetailKeys(detail, detailKeys("http_method", "reason_code", "route")) {
			return false
		}
		if action == ActionGovernanceExecute {
			if governanceRoutes[detail["route"]] != detail["http_method"] {
				return false
			}
		} else if action == ActionKafkaDeadLetterReplay {
			if kafkaDeadLetterRoutes[detail["route"]] != detail["http_method"] {
				return false
			}
		} else if action == ActionMediaProcessingRetry {
			if mediaProcessingRetryRoutes[detail["route"]] != detail["http_method"] {
				return false
			}
		} else if detail["http_method"] != schema.method || detail["route"] != schema.route {
			return false
		}
		_, ok := deniedReasonCodes[detail["reason_code"]]
		return ok
	}
	if action == ActionDeadLetterReplay {
		expected := schema.successKeys
		if outcome == OutcomeFailure {
			expected = detailKeys(
				"failure_code", "http_method", "original_event_id", "queue",
				"reason_code", "replay_id", "route",
			)
		}
		return sameDetailKeys(detail, expected) &&
			detail["http_method"] == schema.method &&
			detail["route"] == schema.route
	}
	if action == ActionKafkaDeadLetterReplay {
		expected := schema.successKeys
		if outcome == OutcomeFailure {
			expected = detailKeys(
				"consumer_group", "failure_code", "http_method", "offset",
				"original_event_id", "partition", "reason_code", "replay_id",
				"route", "source_offset", "source_partition", "source_topic", "topic",
			)
		}
		return sameDetailKeys(detail, expected) &&
			detail["http_method"] == schema.method &&
			detail["route"] == schema.route
	}
	if outcome != OutcomeSuccess {
		return false
	}
	if !sameDetailKeys(detail, schema.successKeys) {
		return false
	}
	if action != ActionGovernanceExecute && action != ActionMediaProcessingRetry &&
		(detail["http_method"] != schema.method || detail["route"] != schema.route) {
		return false
	}
	if reason, ok := detail["reason_code"]; ok {
		if _, allowed := schema.successReasons[reason]; !allowed {
			return false
		}
	}
	switch action {
	case ActionAuditQuery:
		return detail["http_method"] == "GET" && detail["route"] == schema.route
	case ActionReviewDecide:
		version, err := strconv.ParseUint(detail["review_version"], 10, 64)
		if err != nil || version == 0 {
			return false
		}
		reasons := reviewApproveReasons
		if detail["decision"] == "rejected" {
			reasons = reviewRejectReasons
		}
		_, ok := reasons[detail["reason_code"]]
		return ok
	case ActionContentEnforce:
		return detail["previous_status"] == "published" && detail["new_status"] == "offline"
	case ActionContentRestore:
		return detail["previous_status"] == "offline" && detail["new_status"] == "published"
	case ActionConfigPublish:
		previous, previousErr := strconv.ParseUint(detail["previous_revision"], 10, 64)
		next, nextErr := strconv.ParseUint(detail["new_revision"], 10, 64)
		return previousErr == nil && nextErr == nil && next > previous
	case ActionGovernanceExecute:
		if governanceRoutes[detail["route"]] != detail["http_method"] {
			return false
		}
		if detail["operation"] == "update" && detail["route"] != "/api/admin/governance/controls/:key" {
			return false
		}
		if detail["operation"] == "rollback" && detail["route"] != "/api/admin/governance/controls/:key/rollback" {
			return false
		}
		previous, previousErr := strconv.ParseUint(detail["previous_revision"], 10, 64)
		next, nextErr := strconv.ParseUint(detail["new_revision"], 10, 64)
		return previousErr == nil && nextErr == nil && next > previous
	case ActionAccountFreeze, ActionAccountUnfreeze, ActionAccountSessionsRevoke:
		previousVersion, previousErr := strconv.ParseUint(detail["previous_version"], 10, 64)
		newVersion, newErr := strconv.ParseUint(detail["new_version"], 10, 64)
		if previousErr != nil || newErr != nil || previousVersion == 0 ||
			newVersion != previousVersion+1 {
			return false
		}
		switch action {
		case ActionAccountFreeze:
			return detail["previous_status"] == "normal" && detail["new_status"] == "frozen"
		case ActionAccountUnfreeze:
			return detail["previous_status"] == "frozen" && detail["new_status"] == "normal" &&
				detail["revoked_session_count"] == "0"
		case ActionAccountSessionsRevoke:
			return detail["previous_status"] == detail["new_status"] &&
				(detail["new_status"] == "normal" || detail["new_status"] == "frozen")
		}
		return false
	case ActionMediaProcessingRetry:
		if mediaProcessingRetryRoutes[detail["route"]] != detail["http_method"] ||
			detail["previous_state"] != "failed" || detail["new_state"] != "retryable" {
			return false
		}
		videoID, videoErr := strconv.ParseInt(detail["video_id"], 10, 64)
		attempts, attemptsErr := strconv.Atoi(detail["previous_attempts"])
		return videoErr == nil && videoID > 0 && attemptsErr == nil && attempts >= 0
	default:
		return false
	}
}

func sameDetailKeys(detail map[string]string, expected map[string]struct{}) bool {
	if len(detail) != len(expected) {
		return false
	}
	for key := range expected {
		if _, ok := detail[key]; !ok {
			return false
		}
	}
	return true
}

func cloneDetail(detail map[string]string) map[string]string {
	if len(detail) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(detail))
	for key, value := range detail {
		cloned[key] = value
	}
	return cloned
}

func detailKeys(keys ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		result[key] = struct{}{}
	}
	return result
}

func NewRequestID() string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf(
			"%d:%d",
			time.Now().UTC().UnixNano(),
			requestIDFallbackCounter.Add(1),
		)))
		random = sum[:16]
	}
	return "audit-" + hex.EncodeToString(random)
}

func DigestIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > MaxIdempotencyKeyLength {
		return "", ErrIdempotencyKeyTooLong
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
