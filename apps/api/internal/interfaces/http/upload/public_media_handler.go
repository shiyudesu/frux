package interfaceshttpupload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrahttphertz "github.com/shiyudesu/frux/internal/infra/httphertz"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"

	"github.com/cloudwego/hertz/pkg/app"
)

type PublicMediaHandler struct {
	store      *inframedia.LocalStore
	authorizer PublicMediaAuthorizer
	get        app.HandlerFunc
	head       app.HandlerFunc
}

type PublicMediaAuthorizer interface {
	ResolvePublicMediaObject(ctx context.Context, objectKey string) (*domainmedia.PublicMediaObject, error)
}

const maxPublicManifestBytes int64 = 4 << 20
const publicMediaResponseMaxAge = 30 * time.Minute
const publicMediaRedirectMaxAge = 25 * time.Minute
const maxCachedPublicRedirects = 2048

type PublicMediaRedirectStore interface {
	PresignPublicGet(ctx context.Context, key string, expiry time.Duration) (*domainmedia.PresignedRequest, error)
	Open(ctx context.Context, key string) (io.ReadCloser, *domainmedia.ObjectMetadata, error)
	Head(ctx context.Context, key string) (*domainmedia.ObjectMetadata, error)
}

var errInvalidPublicMediaRedirectHandler = errors.New("invalid public media redirect handler")
var errInvalidPublicMediaRedirect = errors.New("invalid public media redirect")

type PublicMediaOption func(*PublicMediaHandler)

func WithPublicMediaAuthorizer(authorizer PublicMediaAuthorizer) PublicMediaOption {
	return func(handler *PublicMediaHandler) {
		handler.authorizer = authorizer
	}
}

func NewPublicMediaHandler(store *inframedia.LocalStore, root, prefix string, options ...PublicMediaOption) (*PublicMediaHandler, error) {
	getHandler, headHandler, err := infrahttphertz.StaticHandlers(root, prefix)
	if err != nil {
		return nil, err
	}
	handler := &PublicMediaHandler{store: store, get: getHandler, head: headHandler}
	for _, option := range options {
		if option != nil {
			option(handler)
		}
	}
	return handler, nil
}

func (h *PublicMediaHandler) Get(ctx context.Context, c *app.RequestContext) {
	h.serve(ctx, c, h.get)
}

func (h *PublicMediaHandler) Head(ctx context.Context, c *app.RequestContext) {
	h.serve(ctx, c, h.head)
}

func (h *PublicMediaHandler) serve(ctx context.Context, c *app.RequestContext, staticHandler app.HandlerFunc) {
	key, ok := publicMediaObjectKey(c)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	object, ok := resolvePublicMedia(ctx, c, h.authorizer, key)
	if !ok {
		return
	}
	metadata, err := h.store.Head(ctx, object.StorageKey)
	if err != nil || metadata == nil {
		c.Status(http.StatusNotFound)
		return
	}
	etag := `"` + metadata.ETag + `"`
	c.Response.Header.Set("ETag", etag)
	c.Response.Header.Set("Accept-Ranges", "bytes")
	c.Response.Header.Set("Cache-Control", publicMediaCacheControl())
	if strings.TrimSpace(string(c.GetHeader("If-None-Match"))) == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Request.URI().SetPath("/media/" + object.StorageKey)
	staticHandler(ctx, c)
}

func publicMediaCacheControl() string {
	return "public, max-age=1800, must-revalidate"
}

type PublicMediaRedirectHandler struct {
	store      PublicMediaRedirectStore
	authorizer PublicMediaAuthorizer
	ttl        time.Duration
	cacheMu    sync.Mutex
	cache      map[string]cachedPublicRedirect
}

type cachedPublicRedirect struct {
	request   *domainmedia.PresignedRequest
	expiresAt time.Time
}

func NewPublicMediaRedirectHandler(
	store PublicMediaRedirectStore,
	authorizer PublicMediaAuthorizer,
	ttl time.Duration,
) (*PublicMediaRedirectHandler, error) {
	if store == nil || authorizer == nil || ttl < publicMediaResponseMaxAge {
		return nil, errInvalidPublicMediaRedirectHandler
	}
	return &PublicMediaRedirectHandler{
		store: store, authorizer: authorizer, ttl: ttl,
		cache: make(map[string]cachedPublicRedirect),
	}, nil
}

func (h *PublicMediaRedirectHandler) Get(ctx context.Context, c *app.RequestContext) {
	key, exposureKey, ok := h.authorizedKey(ctx, c)
	if !ok {
		return
	}
	if strings.HasSuffix(strings.ToLower(key), ".mpd") {
		h.serveManifest(ctx, c, key)
		return
	}
	h.redirect(ctx, c, exposureKey, key)
}

func (h *PublicMediaRedirectHandler) Head(ctx context.Context, c *app.RequestContext) {
	key, _, ok := h.authorizedKey(ctx, c)
	if !ok {
		return
	}
	metadata, err := h.store.Head(ctx, key)
	if err != nil || metadata == nil {
		if err == nil {
			err = domainmedia.ErrObjectNotFound
		}
		writePublicMediaStoreError(c, err)
		return
	}
	setPublicMediaMetadataHeaders(c, metadata)
	c.Status(http.StatusOK)
}

func (h *PublicMediaRedirectHandler) redirect(
	ctx context.Context,
	c *app.RequestContext,
	exposureKey string,
	key string,
) {
	metadata, err := h.store.Head(ctx, key)
	if err != nil || metadata == nil {
		if err == nil {
			err = domainmedia.ErrObjectNotFound
		}
		writePublicMediaStoreError(c, err)
		return
	}
	observePublicMediaRequest(c, metadata.SizeBytes)
	request, err := h.publicRedirect(ctx, exposureKey, key)
	if err != nil || request == nil || strings.TrimSpace(request.URL) == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Response.Header.Set("Cache-Control", "public, max-age=1500, must-revalidate")
	c.Redirect(http.StatusTemporaryRedirect, []byte(request.URL))
	c.Abort()
}

func (h *PublicMediaRedirectHandler) publicRedirect(
	ctx context.Context,
	exposureKey string,
	key string,
) (*domainmedia.PresignedRequest, error) {
	now := time.Now().UTC()
	h.cacheMu.Lock()
	if cached, ok := h.cache[exposureKey]; ok && now.Before(cached.expiresAt) {
		h.cacheMu.Unlock()
		return cached.request, nil
	}
	h.cacheMu.Unlock()

	request, err := h.store.PresignPublicGet(ctx, key, h.ttl)
	if err != nil {
		return nil, err
	}
	if request == nil || strings.TrimSpace(request.URL) == "" {
		return nil, errInvalidPublicMediaRedirect
	}
	cacheExpiresAt := now.Add(publicMediaRedirectMaxAge)
	if !request.ExpiresAt.IsZero() && request.ExpiresAt.Before(cacheExpiresAt) {
		cacheExpiresAt = request.ExpiresAt
	}
	if !cacheExpiresAt.After(now) {
		return nil, errInvalidPublicMediaRedirect
	}
	h.cacheMu.Lock()
	if len(h.cache) >= maxCachedPublicRedirects {
		for cachedKey, cached := range h.cache {
			if !now.Before(cached.expiresAt) {
				delete(h.cache, cachedKey)
			}
		}
	}
	if len(h.cache) >= maxCachedPublicRedirects {
		for cachedKey := range h.cache {
			delete(h.cache, cachedKey)
			break
		}
	}
	h.cache[exposureKey] = cachedPublicRedirect{
		request: request, expiresAt: cacheExpiresAt,
	}
	h.cacheMu.Unlock()
	return request, nil
}

func (h *PublicMediaRedirectHandler) authorizedKey(
	ctx context.Context,
	c *app.RequestContext,
) (string, string, bool) {
	c.Response.Header.Set("Cache-Control", "private, no-store")
	c.Response.Header.Set("Pragma", "no-cache")
	key, ok := publicMediaObjectKey(c)
	if !ok {
		c.Status(http.StatusNotFound)
		return "", "", false
	}
	object, ok := resolvePublicMedia(ctx, c, h.authorizer, key)
	if !ok {
		return "", "", false
	}
	c.Response.Header.Del("Pragma")
	return object.StorageKey, key, true
}

func (h *PublicMediaRedirectHandler) serveManifest(
	ctx context.Context,
	c *app.RequestContext,
	key string,
) {
	body, metadata, err := h.store.Open(ctx, key)
	if err != nil {
		writePublicMediaStoreError(c, err)
		return
	}
	defer body.Close()
	if metadata == nil || metadata.SizeBytes <= 0 || metadata.SizeBytes > maxPublicManifestBytes {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	etag := strings.TrimSpace(metadata.ETag)
	if etag != "" &&
		strings.TrimSpace(string(c.GetHeader("If-None-Match"))) == `"`+etag+`"` {
		setPublicMediaMetadataHeaders(c, metadata)
		c.Status(http.StatusNotModified)
		return
	}
	content, err := io.ReadAll(io.LimitReader(body, maxPublicManifestBytes+1))
	if err != nil || int64(len(content)) != metadata.SizeBytes {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	inframetrics.ObserveMediaObjectOutboundBytes("public_manifest", int64(len(content)))
	setPublicMediaMetadataHeaders(c, metadata)
	contentType := strings.TrimSpace(metadata.ContentType)
	if contentType == "" {
		contentType = "application/dash+xml"
	}
	c.Data(http.StatusOK, contentType, content)
}

func setPublicMediaMetadataHeaders(
	c *app.RequestContext,
	metadata *domainmedia.ObjectMetadata,
) {
	if contentType := strings.TrimSpace(metadata.ContentType); contentType != "" {
		c.Response.Header.Set("Content-Type", contentType)
	}
	if etag := strings.TrimSpace(metadata.ETag); etag != "" {
		c.Response.Header.Set("ETag", `"`+etag+`"`)
	}
	if metadata.SizeBytes >= 0 {
		c.Response.Header.Set("Content-Length", strconv.FormatInt(metadata.SizeBytes, 10))
	}
	c.Response.Header.Set("Accept-Ranges", "bytes")
	c.Response.Header.Set("Cache-Control", publicMediaCacheControl())
}

func writePublicMediaStoreError(c *app.RequestContext, err error) {
	if errors.Is(err, domainmedia.ErrObjectNotFound) {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusServiceUnavailable)
}

func publicMediaObjectKey(c *app.RequestContext) (string, bool) {
	key := strings.TrimPrefix(string(c.Path()), "/media/")
	return key, domainmedia.ValidObjectKey(key) && strings.HasPrefix(key, "media/")
}

func resolvePublicMedia(
	ctx context.Context,
	c *app.RequestContext,
	authorizer PublicMediaAuthorizer,
	key string,
) (*domainmedia.PublicMediaObject, bool) {
	if authorizer == nil {
		c.Status(http.StatusNotFound)
		return nil, false
	}
	object, err := authorizer.ResolvePublicMediaObject(ctx, key)
	if err != nil || object == nil || !domainmedia.ValidObjectKey(object.StorageKey) {
		c.Status(http.StatusNotFound)
		return nil, false
	}
	return object, true
}

func observePublicMediaRequest(c *app.RequestContext, sizeBytes int64) {
	if sizeBytes <= 0 {
		return
	}
	requested := estimatedRangeBytes(
		strings.TrimSpace(string(c.GetHeader("Range"))),
		sizeBytes,
	)
	source := "public_full_estimate"
	if requested < sizeBytes {
		source = "public_range_estimate"
	}
	inframetrics.ObserveMediaObjectOutboundBytes(source, requested)
}

func estimatedRangeBytes(header string, sizeBytes int64) int64 {
	if header == "" || !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return sizeBytes
	}
	value := strings.TrimPrefix(header, "bytes=")
	bounds := strings.SplitN(value, "-", 2)
	if len(bounds) != 2 {
		return sizeBytes
	}
	if bounds[0] == "" {
		suffix, err := strconv.ParseInt(bounds[1], 10, 64)
		if err != nil || suffix <= 0 {
			return sizeBytes
		}
		if suffix > sizeBytes {
			return sizeBytes
		}
		return suffix
	}
	start, err := strconv.ParseInt(bounds[0], 10, 64)
	if err != nil || start < 0 || start >= sizeBytes {
		return sizeBytes
	}
	if bounds[1] == "" {
		return sizeBytes - start
	}
	end, err := strconv.ParseInt(bounds[1], 10, 64)
	if err != nil || end < start {
		return sizeBytes
	}
	if end >= sizeBytes {
		end = sizeBytes - 1
	}
	return end - start + 1
}
