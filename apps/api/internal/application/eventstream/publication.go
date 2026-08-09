package applicationeventstream

import "errors"

// PublicationAcknowledgementError exposes durable broker acknowledgements
// without coupling application services to transport implementations.
type PublicationAcknowledgementError interface {
	error
	TransportAcknowledged(transport string) bool
	PrimaryTransportAcknowledged() bool
	AnyTransportAcknowledged() bool
	PrimaryTransportMayBeAcknowledged() bool
	AnyTransportMayBeAcknowledged() bool
}

type PossiblyAcknowledgedError interface {
	error
	MayHaveAcknowledged() bool
}

func AnyTransportAcknowledged(err error) bool {
	var publicationErr PublicationAcknowledgementError
	return errors.As(err, &publicationErr) && publicationErr.AnyTransportAcknowledged()
}

func PrimaryTransportAcknowledged(err error) bool {
	var publicationErr PublicationAcknowledgementError
	return errors.As(err, &publicationErr) && publicationErr.PrimaryTransportAcknowledged()
}

func IsMultiTransportPublicationError(err error) bool {
	var publicationErr PublicationAcknowledgementError
	return errors.As(err, &publicationErr)
}

func MayHaveTransportAcknowledgement(err error) bool {
	var publicationErr PublicationAcknowledgementError
	if errors.As(err, &publicationErr) {
		return publicationErr.AnyTransportMayBeAcknowledged()
	}
	var possible PossiblyAcknowledgedError
	return errors.As(err, &possible) && possible.MayHaveAcknowledged()
}
