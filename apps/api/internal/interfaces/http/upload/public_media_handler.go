package interfaceshttpupload

import (
	"context"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrahttphertz "github.com/shiyudesu/frux/internal/infra/httphertz"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"
	"net/http"
	"strings"

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
	key := strings.TrimPrefix(string(c.Path()), "/media/")
	if !domainmedia.ValidObjectKey(key) || !strings.HasPrefix(key, "media/") {
		c.Status(http.StatusNotFound)
		return
	}
	if h.authorizer != nil {
		allowed, err := h.authorizer.AuthorizePublicMediaObject(ctx, key)
		if err != nil || !allowed {
			c.Status(http.StatusNotFound)
			return
		}
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
