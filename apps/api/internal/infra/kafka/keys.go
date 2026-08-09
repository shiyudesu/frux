package infrakafka

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

type ActionStateKey struct {
	UserID     int64
	VideoID    int64
	ActionType string
}

type UserKey struct {
	UserID int64
}

type probeKeyCodec struct{}
type actionStateKeyCodec struct{}
type userKeyCodec struct{}

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

func (actionStateKeyCodec) Kind() KeyKind { return KeyKindActionState }

func (actionStateKeyCodec) Encode(value any) ([]byte, error) {
	key, ok := value.(ActionStateKey)
	actionType := strings.ToUpper(strings.TrimSpace(key.ActionType))
	if !ok || key.UserID <= 0 || key.VideoID <= 0 ||
		(actionType != "LIKE" && actionType != "FAVORITE") {
		return nil, ErrInvalidEventKey
	}
	return []byte(fmt.Sprintf("action:%d:%d:%s", key.UserID, key.VideoID, actionType)), nil
}

func (codec actionStateKeyCodec) Decode(data []byte) (any, error) {
	parts := strings.Split(string(data), ":")
	if len(parts) != 4 || parts[0] != "action" {
		return nil, ErrInvalidEventKey
	}
	userID, userErr := strconv.ParseInt(parts[1], 10, 64)
	videoID, videoErr := strconv.ParseInt(parts[2], 10, 64)
	actionType := strings.ToUpper(parts[3])
	if userErr != nil || videoErr != nil || userID <= 0 || videoID <= 0 ||
		(actionType != "LIKE" && actionType != "FAVORITE") {
		return nil, ErrInvalidEventKey
	}
	key := ActionStateKey{UserID: userID, VideoID: videoID, ActionType: actionType}
	canonical, err := codec.Encode(key)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, ErrInvalidEventKey
	}
	return key, nil
}

func (codec actionStateKeyCodec) Validate(data []byte) error {
	_, err := codec.Decode(data)
	return err
}

func (userKeyCodec) Kind() KeyKind { return KeyKindUserID }

func (userKeyCodec) Encode(value any) ([]byte, error) {
	key, ok := value.(UserKey)
	if !ok || key.UserID <= 0 {
		return nil, ErrInvalidEventKey
	}
	return []byte(fmt.Sprintf("user:%d", key.UserID)), nil
}

func (codec userKeyCodec) Decode(data []byte) (any, error) {
	const prefix = "user:"
	if !strings.HasPrefix(string(data), prefix) {
		return nil, ErrInvalidEventKey
	}
	userID, err := strconv.ParseInt(string(data[len(prefix):]), 10, 64)
	if err != nil || userID <= 0 {
		return nil, ErrInvalidEventKey
	}
	key := UserKey{UserID: userID}
	canonical, encodeErr := codec.Encode(key)
	if encodeErr != nil || !bytes.Equal(data, canonical) {
		return nil, ErrInvalidEventKey
	}
	return key, nil
}

func (codec userKeyCodec) Validate(data []byte) error {
	_, err := codec.Decode(data)
	return err
}

var keyCodecs = [...]KeyCodec{probeKeyCodec{}, actionStateKeyCodec{}, userKeyCodec{}}

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
