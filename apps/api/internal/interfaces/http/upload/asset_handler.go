package interfaceshttpupload

import (
	infrahttphertz "github.com/shiyudesu/frux/internal/infra/httphertz"
	interfaceshttpmiddleware "github.com/shiyudesu/frux/internal/interfaces/http/middleware"
	"context"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

type AssetAuthorizer interface {
	AuthorizeLocalAsset(ctx context.Context, assetURL string, viewerID int64) (referenced, publiclyReadable, allowed bool, err error)
}

type AssetHandler struct {
	authorizer AssetAuthorizer
	get        app.HandlerFunc
	head       app.HandlerFunc
}

func NewAssetHandler(root, prefix string, authorizer AssetAuthorizer) (*AssetHandler, error) {
	getHandler, headHandler, err := infrahttphertz.StaticHandlers(root, prefix)
	if err != nil {
		return nil, err
	}
	return &AssetHandler{authorizer: authorizer, get: getHandler, head: headHandler}, nil
}

func (h *AssetHandler) Get(ctx context.Context, c *app.RequestContext) {
	h.serve(ctx, c, h.get)
}

func (h *AssetHandler) Head(ctx context.Context, c *app.RequestContext) {
	h.serve(ctx, c, h.head)
}

func (h *AssetHandler) serve(ctx context.Context, c *app.RequestContext, staticHandler app.HandlerFunc) {
	assetURL := string(c.Path())
	viewerID, _ := assetViewerID(c)
	referenced, _, allowed, err := h.authorizer.AuthorizeLocalAsset(ctx, assetURL, viewerID)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if referenced {
		if !allowed {
			c.Status(http.StatusNotFound)
			return
		}
		c.Response.Header.Set("Cache-Control", "no-store")
		staticHandler(ctx, c)
		return
	}
	if protectedUploadKind(assetURL) {
		c.Status(http.StatusNotFound)
		return
	}
	staticHandler(ctx, c)
}

func assetViewerID(c *app.RequestContext) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func protectedUploadKind(assetURL string) bool {
	kind := uploadKind(assetURL)
	return kind == "video" || kind == "cover"
}

func uploadKind(assetURL string) string {
	relative := strings.TrimPrefix(assetURL, "/uploads/")
	if relative == assetURL {
		return ""
	}
	kind, _, _ := strings.Cut(relative, "/")
	return strings.ToLower(strings.TrimSpace(kind))
}
