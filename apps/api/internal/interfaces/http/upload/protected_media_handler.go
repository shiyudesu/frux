package interfaceshttpupload

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	infrahttphertz "github.com/shiyudesu/frux/internal/infra/httphertz"
	inframedia "github.com/shiyudesu/frux/internal/infra/media"

	"github.com/cloudwego/hertz/pkg/app"
)

type ProtectedMediaVerifier interface {
	Verify(objectKey, rawExpiry, signature string) bool
}

type ProtectedMediaHandler struct {
	prefix   string
	store    *inframedia.LocalStore
	verifier ProtectedMediaVerifier
	get      app.HandlerFunc
	head     app.HandlerFunc
}

func NewProtectedMediaHandler(
	store *inframedia.LocalStore,
	root, prefix string,
	verifier ProtectedMediaVerifier,
) (*ProtectedMediaHandler, error) {
	getHandler, headHandler, err := infrahttphertz.StaticHandlers(root, prefix)
	if err != nil {
		return nil, err
	}
	return &ProtectedMediaHandler{
		prefix: strings.TrimRight(prefix, "/"), store: store, verifier: verifier,
		get: getHandler, head: headHandler,
	}, nil
}

func (h *ProtectedMediaHandler) Get(ctx context.Context, c *app.RequestContext) {
	h.serve(ctx, c, h.get)
}

func (h *ProtectedMediaHandler) Head(ctx context.Context, c *app.RequestContext) {
	h.serve(ctx, c, h.head)
}

func (h *ProtectedMediaHandler) serve(ctx context.Context, c *app.RequestContext, staticHandler app.HandlerFunc) {
	if h == nil || h.store == nil || h.verifier == nil {
		c.Status(http.StatusNotFound)
		return
	}
	rawKey := strings.TrimPrefix(string(c.Path()), h.prefix+"/")
	key, err := url.PathUnescape(rawKey)
	if err != nil || !domainmedia.ValidObjectKey(key) ||
		!h.verifier.Verify(key, c.Query("expires"), c.Query("signature")) {
		c.Status(http.StatusNotFound)
		return
	}
	c.Response.Header.Set("Accept-Ranges", "bytes")
	c.Response.Header.Set("Cache-Control", "private, no-store")
	staticHandler(ctx, c)
}
