package applicationeventstream

import "errors"

// PublicationAcknowledgementError exposes durable broker acknowledgements
// without coupling application services to transport implementations.
type PublicationAcknowledgementError interface {
	error
	TransportAcknowledged(transport string) bool
	AnyTransportAcknowledged() bool
}

func AnyTransportAcknowledged(err error) bool {
	var publicationErr PublicationAcknowledgementError
	return errors.As(err, &publicationErr) && publicationErr.AnyTransportAcknowledged()
}
