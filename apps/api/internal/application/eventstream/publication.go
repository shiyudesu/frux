package applicationeventstream

import "errors"

type PossiblyAcknowledgedError interface {
	error
	MayHaveAcknowledged() bool
}

func MayHaveTransportAcknowledgement(err error) bool {
	var possible PossiblyAcknowledgedError
	return errors.As(err, &possible) && possible.MayHaveAcknowledged()
}
