package domaingovernance

import "errors"

var (
	ErrUnknownControl      = errors.New("unknown degradation control")
	ErrUnsupportedProcess  = errors.New("control is unsupported by process")
	ErrInvalidControlValue = errors.New("invalid degradation control value")
	ErrInvalidRevision     = errors.New("invalid degradation control revision")
	ErrRevisionConflict    = errors.New("degradation control revision conflict")
	ErrRevisionNotFound    = errors.New("degradation control revision not found")
	ErrInvalidActorID      = errors.New("invalid degradation control actor id")
	ErrInvalidReason       = errors.New("invalid degradation control reason")
	ErrReasonTooLong       = errors.New("degradation control reason is too long")
	ErrInvalidExpiry       = errors.New("invalid degradation control expiry")
	ErrInvalidCreatedAt    = errors.New("invalid degradation control creation time")
	ErrInvalidLimit        = errors.New("invalid degradation control list limit")
	ErrInvalidPollInterval = errors.New("invalid degradation control poll interval")
	ErrInvalidPollTimeout  = errors.New("invalid degradation control poll timeout")
)
