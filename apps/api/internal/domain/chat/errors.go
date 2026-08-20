package domainchat

import "errors"

var (
	ErrInvalidUserID          = errors.New("invalid chat user id")
	ErrInvalidTargetUserID    = errors.New("invalid chat target user id")
	ErrInvalidConversationID  = errors.New("invalid chat conversation id")
	ErrSelfConversation       = errors.New("chat self conversation is forbidden")
	ErrNotEligible            = errors.New("chat participants are not mutually eligible")
	ErrAccountUnavailable     = errors.New("chat account unavailable")
	ErrConversationNotFound   = errors.New("chat conversation not found")
	ErrNotMember              = errors.New("chat user is not a conversation member")
	ErrInvalidCursor          = errors.New("invalid chat cursor")
	ErrInvalidQuery           = errors.New("invalid chat query")
	ErrInvalidLimit           = errors.New("invalid chat limit")
	ErrInvalidMessageShape    = errors.New("invalid chat message shape")
	ErrEmptyText              = errors.New("chat text is empty")
	ErrTextTooLong            = errors.New("chat text is too long")
	ErrInvalidVideoID         = errors.New("invalid chat video id")
	ErrVideoUnavailable       = errors.New("chat video unavailable")
	ErrMessageNotFound        = errors.New("chat message not found")
	ErrIdempotencyKeyRequired = errors.New("chat idempotency key is required")
	ErrIdempotencyKeyTooLong  = errors.New("chat idempotency key is too long")
	ErrIdempotencyConflict    = errors.New("chat idempotency key conflict")
	ErrInvalidMessageID       = errors.New("invalid chat message id")
	ErrPersistence            = errors.New("chat persistence failure")
)
