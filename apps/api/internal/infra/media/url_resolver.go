package inframedia

import (
	"context"
	"net/url"
	"strings"
	"time"

	domainmedia "GCFeed/internal/domain/media"
)

type URLResolver struct {
	publicBaseURL string
	store         domainmedia.MediaObjectStore
}

func NewURLResolver(publicBaseURL string, store domainmedia.MediaObjectStore) (*URLResolver, error) {
	publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if publicBaseURL == "" || store == nil {
		return nil, domainmedia.ErrInvalidObjectKey
	}
	if _, err := url.Parse(publicBaseURL); err != nil {
		return nil, err
	}
	return &URLResolver{publicBaseURL: publicBaseURL, store: store}, nil
}

func (r *URLResolver) PublicURL(objectKey string) (string, error) {
	if !domainmedia.ValidObjectKey(objectKey) {
		return "", domainmedia.ErrInvalidObjectKey
	}
	segments := strings.Split(objectKey, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return r.publicBaseURL + "/" + strings.Join(segments, "/"), nil
}

func (r *URLResolver) ProtectedURL(ctx context.Context, objectKey string, expiry time.Duration) (string, time.Time, error) {
	if expiry <= 0 {
		return "", time.Time{}, domainmedia.ErrInvalidPresignExpiry
	}
	request, err := r.store.PresignGet(ctx, objectKey, expiry)
	if err != nil {
		return "", time.Time{}, err
	}
	return request.URL, request.ExpiresAt, nil
}
