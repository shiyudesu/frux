package domainadminaudit

import "errors"

var (
	ErrInvalidEventID            = errors.New("invalid audit event id")
	ErrInvalidActorID            = errors.New("invalid audit actor id")
	ErrInvalidPermission         = errors.New("invalid audit permission")
	ErrInvalidAction             = errors.New("invalid audit action")
	ErrInvalidTargetType         = errors.New("invalid audit target type")
	ErrInvalidTargetID           = errors.New("invalid audit target id")
	ErrTargetIDTooLong           = errors.New("audit target id too long")
	ErrInvalidOutcome            = errors.New("invalid audit outcome")
	ErrInvalidRequestID          = errors.New("invalid audit request id")
	ErrRequestIDTooLong          = errors.New("audit request id too long")
	ErrIdempotencyKeyTooLong     = errors.New("audit idempotency key too long")
	ErrInvalidIdempotencyKeyHash = errors.New("invalid audit idempotency key hash")
	ErrInvalidDetail             = errors.New("invalid audit detail")
	ErrDetailTooLarge            = errors.New("audit detail too large")
	ErrInvalidCreatedAt          = errors.New("invalid audit created time")
	ErrInvalidCursor             = errors.New("invalid audit cursor")
	ErrInvalidTimeRange          = errors.New("invalid audit time range")
	ErrTimeRangeTooLarge         = errors.New("audit time range too large")
	ErrInvalidLimit              = errors.New("invalid audit limit")
	ErrAuditQueryFailed          = errors.New("audit query failed")
)
