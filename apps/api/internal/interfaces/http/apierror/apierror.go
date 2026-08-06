package interfaceshttpapierror

import (
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
)

type Envelope struct {
	Code              string `json:"code"`
	Error             string `json:"error"`
	Message           string `json:"message,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

const (
	CodeInvalidRequest         = "INVALID_REQUEST"
	CodeInvalidAccessToken     = "AUTH_INVALID_ACCESS_TOKEN"
	CodeAuthenticationRequired = "AUTHENTICATION_REQUIRED"
	CodeInternalTokenRequired  = "AUTH_INTERNAL_TOKEN_REQUIRED"
	CodeInvalidInternalToken   = "AUTH_INVALID_INTERNAL_TOKEN"
	CodeForbidden              = "FORBIDDEN"
	CodeNotFound               = "NOT_FOUND"
	CodeConflict               = "CONFLICT"
	CodeRateLimited            = "RATE_LIMITED"
	CodeRateLimitUnavailable   = "RATE_LIMIT_UNAVAILABLE"
	CodeServiceUnavailable     = "SERVICE_UNAVAILABLE"
	CodeInternal               = "INTERNAL_ERROR"

	CodeAccountValidationFailed = "ACCOUNT_VALIDATION_FAILED"
	CodeAccountAlreadyExists    = "ACCOUNT_ALREADY_EXISTS"
	CodeAuthInvalidCredentials  = "AUTH_INVALID_CREDENTIALS"
	CodeAccountNotFound         = "ACCOUNT_NOT_FOUND"

	CodeAdminPermissionDenied         = "ADMIN_PERMISSION_DENIED"
	CodeAdminAuthorizationUnavailable = "ADMIN_AUTHORIZATION_UNAVAILABLE"
	CodeAdminAuditQueryInvalid        = "ADMIN_AUDIT_QUERY_INVALID"
	CodeAdminAuditCursorInvalid       = "ADMIN_AUDIT_CURSOR_INVALID"
	CodeAdminAuditUnavailable         = "ADMIN_AUDIT_UNAVAILABLE"
	CodeAdminVideoValidationFailed    = "ADMIN_VIDEO_VALIDATION_FAILED"
	CodeAdminVideoCursorInvalid       = "ADMIN_VIDEO_CURSOR_INVALID"
	CodeAdminVideoVersionConflict     = "ADMIN_VIDEO_VERSION_CONFLICT"
	CodeAdminVideoStateConflict       = "ADMIN_VIDEO_STATE_CONFLICT"
	CodeAdminVideoUnavailable         = "ADMIN_VIDEO_UNAVAILABLE"

	CodeGovernanceValidationFailed = "GOVERNANCE_VALIDATION_FAILED"
	CodeGovernanceControlUnknown   = "GOVERNANCE_CONTROL_UNKNOWN"
	CodeGovernanceRevisionConflict = "GOVERNANCE_REVISION_CONFLICT"
	CodeGovernanceRevisionNotFound = "GOVERNANCE_REVISION_NOT_FOUND"
	CodeGovernanceUnavailable      = "GOVERNANCE_UNAVAILABLE"

	CodeFeedValidationFailed             = "FEED_VALIDATION_FAILED"
	CodeFeedRecommendationContextInvalid = "FEED_RECOMMENDATION_CONTEXT_INVALID"
	CodeFeedCursorInvalid                = "FEED_CURSOR_INVALID"

	CodeSearchQueryRequired      = "SEARCH_QUERY_REQUIRED"
	CodeSearchQueryInvalid       = "SEARCH_QUERY_INVALID"
	CodeSearchQueryTooLong       = "SEARCH_QUERY_TOO_LONG"
	CodeSearchParametersInvalid  = "SEARCH_PARAMETERS_INVALID"
	CodeSearchServiceUnavailable = "SEARCH_SERVICE_UNAVAILABLE"

	CodeRecommendationValidationFailed = "RECOMMENDATION_VALIDATION_FAILED"
	CodeRecommendationFeedbackConflict = "RECOMMENDATION_FEEDBACK_CONFLICT"
	CodeRecommendationVideoNotFound    = "RECOMMENDATION_VIDEO_NOT_FOUND"

	CodeExposureValidationFailed = "EXPOSURE_VALIDATION_FAILED"
	CodeExposureVideoNotFound    = "EXPOSURE_VIDEO_NOT_FOUND"
	CodeExposureEventConflict    = "EXPOSURE_EVENT_CONFLICT"

	CodePlaybackValidationFailed     = "PLAYBACK_VALIDATION_FAILED"
	CodePlaybackTelemetryRateLimited = "PLAYBACK_TELEMETRY_RATE_LIMITED"
	CodePlaybackTelemetryConflict    = "PLAYBACK_TELEMETRY_CONFLICT"

	CodeInteractionValidationFailed        = "INTERACTION_VALIDATION_FAILED"
	CodeInteractionResourceNotFound        = "INTERACTION_RESOURCE_NOT_FOUND"
	CodeInteractionCommentPermissionDenied = "INTERACTION_COMMENT_PERMISSION_DENIED"
	CodeInteractionIdempotencyConflict     = "INTERACTION_IDEMPOTENCY_CONFLICT"

	CodeRelationValidationFailed    = "RELATION_VALIDATION_FAILED"
	CodeRelationTargetUserNotFound  = "RELATION_TARGET_USER_NOT_FOUND"
	CodeRelationIdempotencyConflict = "RELATION_IDEMPOTENCY_CONFLICT"

	CodeMessageValidationFailed = "MESSAGE_VALIDATION_FAILED"

	CodeReviewValidationFailed            = "REVIEW_VALIDATION_FAILED"
	CodeReviewCaseNotFound                = "REVIEW_CASE_NOT_FOUND"
	CodeReviewConflict                    = "REVIEW_CONFLICT"
	CodeReviewUnavailable                 = "REVIEW_UNAVAILABLE"
	CodeReviewCaseClaimed                 = "REVIEW_CASE_CLAIMED"
	CodeReviewLeaseExpired                = "REVIEW_LEASE_EXPIRED"
	CodeReviewLeaseNotOwned               = "REVIEW_LEASE_NOT_OWNED"
	CodeReviewCaseVersionConflict         = "REVIEW_CASE_VERSION_CONFLICT"
	CodeReviewSubjectVersionConflict      = "REVIEW_SUBJECT_VERSION_CONFLICT"
	CodeReviewDecisionIdempotencyConflict = "REVIEW_DECISION_IDEMPOTENCY_CONFLICT"
	CodeReviewCursorInvalid               = "REVIEW_CURSOR_INVALID"

	CodeLibraryValidationFailed   = "LIBRARY_VALIDATION_FAILED"
	CodeLibraryVideoNotFound      = "LIBRARY_VIDEO_NOT_FOUND"
	CodeLibraryLikedVideosPrivate = "LIBRARY_LIKED_VIDEOS_PRIVATE"

	CodeVideoValidationFailed           = "VIDEO_VALIDATION_FAILED"
	CodeVideoNotFound                   = "VIDEO_NOT_FOUND"
	CodeMediaAssetNotFound              = "MEDIA_ASSET_NOT_FOUND"
	CodeVideoPermissionDenied           = "VIDEO_PERMISSION_DENIED"
	CodeLocalAssetPermissionDenied      = "LOCAL_ASSET_PERMISSION_DENIED"
	CodeVideoCollectionNotFound         = "VIDEO_COLLECTION_NOT_FOUND"
	CodeVideoCollectionPermissionDenied = "VIDEO_COLLECTION_PERMISSION_DENIED"
	CodeVideoIdempotencyConflict        = "VIDEO_IDEMPOTENCY_CONFLICT"

	CodeUploadFileRequired             = "UPLOAD_FILE_REQUIRED"
	CodeUploadKindInvalid              = "UPLOAD_KIND_INVALID"
	CodeUploadValidationFailed         = "UPLOAD_VALIDATION_FAILED"
	CodeUploadProcessingUnavailable    = "UPLOAD_PROCESSING_UNAVAILABLE"
	CodeUploadProcessingFailed         = "UPLOAD_PROCESSING_FAILED"
	CodeUploadStoragePreparationFailed = "UPLOAD_STORAGE_PREPARATION_FAILED"
	CodeUploadStorageWriteFailed       = "UPLOAD_STORAGE_WRITE_FAILED"
	CodeUploadRecordFailed             = "UPLOAD_RECORD_FAILED"
	CodeUploadSessionValidationFailed  = "UPLOAD_SESSION_VALIDATION_FAILED"
	CodeUploadSessionNotFound          = "UPLOAD_SESSION_NOT_FOUND"
	CodeUploadAssetNotFound            = "UPLOAD_ASSET_NOT_FOUND"
	CodeUploadAssetPermissionDenied    = "UPLOAD_ASSET_PERMISSION_DENIED"
	CodeUploadSessionConflict          = "UPLOAD_SESSION_CONFLICT"
	CodeUploadStorageUnavailable       = "UPLOAD_STORAGE_UNAVAILABLE"
)

func Write(c *app.RequestContext, status int, code, legacy string) {
	c.JSON(status, Envelope{Code: code, Error: legacy})
}

func Abort(c *app.RequestContext, status int, code, legacy string) {
	AbortWithMessage(c, status, code, legacy, legacy)
}

func AbortWithMessage(c *app.RequestContext, status int, code, legacy, message string) {
	c.AbortWithStatusJSON(status, Envelope{Code: code, Error: legacy, Message: message})
}

func AbortRateLimited(c *app.RequestContext, retryAfterSeconds int) {
	c.AbortWithStatusJSON(http.StatusTooManyRequests, Envelope{
		Code: CodeRateLimited, Error: "rate limit exceeded",
		RetryAfterSeconds: retryAfterSeconds,
	})
}

func AbortRateLimitUnavailable(c *app.RequestContext, retryAfterSeconds int) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, Envelope{
		Code: CodeRateLimitUnavailable, Error: "rate limit service unavailable",
		RetryAfterSeconds: retryAfterSeconds,
	})
}

func WriteInvalidRequest(c *app.RequestContext) {
	Write(c, http.StatusBadRequest, CodeInvalidRequest, "invalid request")
}

func WriteInvalidAccessToken(c *app.RequestContext) {
	Write(c, http.StatusUnauthorized, CodeInvalidAccessToken, "invalid access token")
}

func AbortInvalidAccessToken(c *app.RequestContext) {
	Abort(c, http.StatusUnauthorized, CodeInvalidAccessToken, "invalid access token")
}

func AbortInvalidAccessTokenWithMessage(c *app.RequestContext, message string) {
	AbortWithMessage(c, http.StatusUnauthorized, CodeInvalidAccessToken, "invalid access token", message)
}

func WriteInternal(c *app.RequestContext, legacy string, err error) {
	WriteInternalCode(c, CodeInternal, legacy, err)
}

func WriteInternalCode(c *app.RequestContext, code, legacy string, _ error) {
	Write(c, http.StatusInternalServerError, code, legacy)
}

func WriteServiceUnavailable(c *app.RequestContext, legacy string, err error) {
	WriteServiceUnavailableCode(c, CodeServiceUnavailable, legacy, err)
}

func WriteServiceUnavailableCode(c *app.RequestContext, code, legacy string, _ error) {
	Write(c, http.StatusServiceUnavailable, code, legacy)
}
