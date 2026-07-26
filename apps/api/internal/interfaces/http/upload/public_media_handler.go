package interfaceshttpupload

import (
	domainmedia "GCFeed/internal/domain/media"
	infrahttphertz "GCFeed/internal/infra/httphertz"
	inframedia "GCFeed/internal/infra/media"
	"context"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

type PublicMediaHandler struct {
	store *inframedia.LocalStore
	get   app.HandlerFunc
	head  app.HandlerFunc
}

func NewPublicMediaHandler(store *inframedia.LocalStore, root, prefix string) (*PublicMediaHandler, error) {
	getHandler, headHandler, err := infrahttphertz.StaticHandlers(root, prefix)
	if err != nil {
		return nil, err
	}
	return &PublicMediaHandler{store: store, get: getHandler, head: headHandler}, nil
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
	if strings.HasSuffix(strings.ToLower(key), ".mpd") {
		return "public, max-age=60"
	}
	return "public, max-age=31536000, immutable"
}
