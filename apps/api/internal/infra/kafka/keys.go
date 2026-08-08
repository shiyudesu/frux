package infrakafka

import (
	"errors"
	"fmt"
	"regexp"
)

var ErrInvalidEventKey = errors.New("invalid kafka event key")

type KeyCodec interface {
	Kind() KeyKind
	Encode(value any) ([]byte, error)
	Decode(data []byte) (any, error)
	Validate(data []byte) error
}

type ProbeKey struct {
	ProbeID string
}

type probeKeyCodec struct{}

var probeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func (probeKeyCodec) Kind() KeyKind {
	return KeyKindProbeID
}

func (probeKeyCodec) Encode(value any) ([]byte, error) {
	key, ok := value.(ProbeKey)
	if !ok || !probeIDPattern.MatchString(key.ProbeID) {
		return nil, ErrInvalidEventKey
	}
	return []byte("probe:" + key.ProbeID), nil
}

func (probeKeyCodec) Decode(data []byte) (any, error) {
	const prefix = "probe:"
	if len(data) <= len(prefix) || string(data[:len(prefix)]) != prefix {
		return nil, ErrInvalidEventKey
	}
	key := ProbeKey{ProbeID: string(data[len(prefix):])}
	if !probeIDPattern.MatchString(key.ProbeID) {
		return nil, ErrInvalidEventKey
	}
	return key, nil
}

func (codec probeKeyCodec) Validate(data []byte) error {
	_, err := codec.Decode(data)
	return err
}

var keyCodecs = [...]KeyCodec{probeKeyCodec{}}

func Codec(kind KeyKind) (KeyCodec, error) {
	for _, codec := range keyCodecs {
		if codec.Kind() == kind {
			return codec, nil
		}
	}
	return nil, fmt.Errorf("%w: key kind %q", ErrUnknownRegistryValue, kind)
}

func EncodeKey(kind KeyKind, value any) ([]byte, error) {
	codec, err := Codec(kind)
	if err != nil {
		return nil, err
	}
	return codec.Encode(value)
}

func DecodeKey(kind KeyKind, data []byte) (any, error) {
	codec, err := Codec(kind)
	if err != nil {
		return nil, err
	}
	return codec.Decode(data)
}

func ValidateKey(kind KeyKind, data []byte) error {
	codec, err := Codec(kind)
	if err != nil {
		return err
	}
	return codec.Validate(data)
}
