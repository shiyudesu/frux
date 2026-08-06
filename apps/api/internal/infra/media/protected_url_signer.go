package inframedia

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

type LocalProtectedURLSigner struct {
	prefix string
	secret []byte
	maxTTL time.Duration
	now    func() time.Time
}

func NewLocalProtectedURLSigner(prefix, secret string, maxTTL time.Duration) (*LocalProtectedURLSigner, error) {
	prefix = "/" + strings.Trim(strings.TrimSpace(prefix), "/")
	secret = strings.TrimSpace(secret)
	if prefix == "/" || secret == "" || maxTTL <= 0 {
		return nil, domainmedia.ErrInvalidPresignExpiry
	}
	return &LocalProtectedURLSigner{
		prefix: prefix,
		secret: []byte(secret),
		maxTTL: maxTTL,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *LocalProtectedURLSigner) Sign(objectKey string, expiry time.Duration) (string, time.Time, error) {
	if s == nil || !domainmedia.ValidObjectKey(objectKey) {
		return "", time.Time{}, domainmedia.ErrInvalidObjectKey
	}
	if expiry <= 0 || expiry > s.maxTTL {
		return "", time.Time{}, domainmedia.ErrInvalidPresignExpiry
	}
	expiresAt := s.now().UTC().Add(expiry).Truncate(time.Second)
	rawExpiry := strconv.FormatInt(expiresAt.Unix(), 10)
	signature := s.signature(objectKey, rawExpiry)
	segments := strings.Split(objectKey, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return s.prefix + "/" + strings.Join(segments, "/") +
		"?expires=" + rawExpiry + "&signature=" + url.QueryEscape(signature), expiresAt, nil
}

func (s *LocalProtectedURLSigner) Verify(objectKey, rawExpiry, signature string) bool {
	if s == nil || !domainmedia.ValidObjectKey(objectKey) {
		return false
	}
	expiresUnix, err := strconv.ParseInt(strings.TrimSpace(rawExpiry), 10, 64)
	if err != nil {
		return false
	}
	now := s.now().UTC()
	expiresAt := time.Unix(expiresUnix, 0).UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(s.maxTTL+time.Second)) {
		return false
	}
	expected := s.signature(objectKey, strings.TrimSpace(rawExpiry))
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}

func (s *LocalProtectedURLSigner) signature(objectKey, rawExpiry string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(objectKey))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(rawExpiry))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
