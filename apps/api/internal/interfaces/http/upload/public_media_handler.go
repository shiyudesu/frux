package interfaceshttpupload

import (
	"context"
	"errors"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrahttphertz "github.com/shiyudesu/frux/internal/infra/httphertz"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type PublicMediaHandler struct {
	store      *inframedia.LocalStore
	authorizer PublicMediaAuthorizer
	get        app.HandlerFunc
	head       app.HandlerFunc
}

type PublicMediaAuthorizer interface {
	AuthorizePublicMediaObject(ctx context.Context, objectKey string) (bool, error)
}

const maxPublicManifestBytes int64 = 4 << 20

type PublicMediaRedirectStore interface {
	PresignGet(ctx context.Context, key string, expiry time.Duration) (*domainmedia.PresignedRequest, error)
	Open(ctx context.Context, key string) (io.ReadCloser, *domainmedia.ObjectMetadata, error)
	Head(ctx context.Context, key string) (*domainmedia.ObjectMetadata, error)
}

var errInvalidPublicMediaRedirectHandler = errors.New("invalid public media redirect handler")

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
	if !authorizePublicMedia(ctx, c, h.authorizer, key) {
		return
	}
	metadata, err := h.store.Head(ctx, key)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	etag := `"` + metadata.ETag + `"`
	c.Response.Header.Set("ETag", etag)
	c.Response.Header.Set("Accept-Ranges", "bytes")
	c.Response.Header.Set("Cache-Control", publicMediaCacheControl(key))
	if strings.TrimSpace(string(c.GetHeader("If-None-Match"))) == etag {
		c.Status(http.StatusNotModified)
		return
	}
	staticHandler(ctx, c)
}

func publicMediaCacheControl(key string) string {
	return "public, max-age=60, must-revalidate"
}

type PublicMediaRedirectHandler struct {
	store      PublicMediaRedirectStore
	authorizer PublicMediaAuthorizer
	ttl        time.Duration
}

func NewPublicMediaRedirectHandler(
	store PublicMediaRedirectStore,
	authorizer PublicMediaAuthorizer,
	ttl time.Duration,
) (*PublicMediaRedirectHandler, error) {
	if store == nil || authorizer == nil || ttl <= 0 || ttl > time.Minute {
		return nil, errInvalidPublicMediaRedirectHandler
	}
	return &PublicMediaRedirectHandler{
		store: store, authorizer: authorizer, ttl: ttl,
	}, nil
}

func (h *PublicMediaRedirectHandler) Get(ctx context.Context, c *app.RequestContext) {
	key, ok := h.authorizedKey(ctx, c)
	if !ok {
		return
	}
	if strings.HasSuffix(strings.ToLower(key), ".mpd") {
		h.serveManifest(ctx, c, key)
		return
	}
	h.redirect(ctx, c, key)
}

func (h *PublicMediaRedirectHandler) Head(ctx context.Context, c *app.RequestContext) {
	key, ok := h.authorizedKey(ctx, c)
	if !ok {
		return
	}
	metadata, err := h.store.Head(ctx, key)
	if err != nil {
		writePublicMediaStoreError(c, err)
		return
	}
	setPublicMediaMetadataHeaders(c, metadata)
	c.Status(http.StatusOK)
}

func (h *PublicMediaRedirectHandler) redirect(
	ctx context.Context,
	c *app.RequestContext,
	key string,
) {
	request, err := h.store.PresignGet(ctx, key, h.ttl)
	if err != nil || request == nil || strings.TrimSpace(request.URL) == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Redirect(http.StatusTemporaryRedirect, []byte(request.URL))
	c.Abort()
}

func (h *PublicMediaRedirectHandler) authorizedKey(
	ctx context.Context,
	c *app.RequestContext,
) (string, bool) {
	c.Response.Header.Set("Cache-Control", "private, no-store")
	c.Response.Header.Set("Pragma", "no-cache")
	key, ok := publicMediaObjectKey(c)
	if !ok {
		c.Status(http.StatusNotFound)
		return "", false
	}
	if !authorizePublicMedia(ctx, c, h.authorizer, key) {
		return "", false
	}
	return key, true
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
	c.Response.Header.Set("Cache-Control", publicMediaCacheControl(metadata.Key))
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

func authorizePublicMedia(
	ctx context.Context,
	c *app.RequestContext,
	authorizer PublicMediaAuthorizer,
	key string,
) bool {
	if authorizer == nil {
		c.Status(http.StatusNotFound)
		return false
	}
	allowed, err := authorizer.AuthorizePublicMediaObject(ctx, key)
	if err != nil || !allowed {
		c.Status(http.StatusNotFound)
		return false
	}
	return true
}
