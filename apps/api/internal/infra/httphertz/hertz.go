package infrahttphertz

import (
	"context"
	infraconfig "github.com/shiyudesu/frux/internal/infra/config"
	inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	cloudnetpoll "github.com/cloudwego/netpoll"
)

const maxBufferedRequestBodySize = 4 << 20
const requestReadTimeout = 5 * time.Minute
const contextRequestBodyConsumedKey = "hertz_request_body_consumed"

// Init creates the Hertz server with the API's request limits and middleware.
func Init(cfg *infraconfig.Config) (*server.Hertz, error) {
	addr := ":" + strconv.Itoa(cfg.Port)
	listener, err := cloudnetpoll.CreateListener("tcp", addr)
	if err != nil {
		return nil, err
	}
	h := server.Default(
		server.WithListener(listener),
		server.WithMaxRequestBodySize(maxBufferedRequestBodySize),
		server.WithMaxKeepBodySize(0),
		server.WithStreamBody(true),
		server.WithDisablePreParseMultipartForm(true),
		server.WithSenseClientDisconnection(true),
		server.WithReadTimeout(requestReadTimeout),
		server.WithRedirectTrailingSlash(false),
		server.WithRedirectFixedPath(false),
		server.WithHandleMethodNotAllowed(false),
	)
	h.Use(inframetrics.HTTPMiddleware(), RequestStreamCleanupMiddleware())
	h.NoRoute(NotFound)
	return h, nil
}

// Run blocks until the Hertz server fails or receives a shutdown signal.
func Run(h *server.Hertz) error {
	runErr := make(chan error, 1)
	go func() {
		runErr <- h.Engine.Run()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-runErr:
		return err
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.Engine.Shutdown(shutdownCtx); err != nil {
			return err
		}
		select {
		case err := <-runErr:
			return err
		case <-shutdownCtx.Done():
			return shutdownCtx.Err()
		}
	}
}

// NotFound preserves the previous default route-miss response contract.
func NotFound(_ context.Context, c *app.RequestContext) {
	c.Data(http.StatusNotFound, "text/plain; charset=utf-8", []byte("404 page not found"))
}

// RequestStreamCleanupMiddleware closes only streamed request bodies that handlers did not consume.
func RequestStreamCleanupMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if !c.Request.IsBodyStream() || RequestBodyConsumed(c) {
				return
			}
			c.SetConnectionClose()
			_ = c.Request.CloseBodyStream()
		}()
		c.Next(ctx)
	}
}

// MarkRequestBodyConsumed records that a handler fully read a streamed request body.
func MarkRequestBodyConsumed(c *app.RequestContext) {
	c.Set(contextRequestBodyConsumedKey, true)
}

// RequestBodyConsumed reports whether a handler fully read a streamed request body.
func RequestBodyConsumed(c *app.RequestContext) bool {
	value, exists := c.Get(contextRequestBodyConsumedKey)
	consumed, ok := value.(bool)
	return exists && ok && consumed
}

// RegisterTrailingSlashRedirects adds Gin-compatible redirects inside the middleware chain.
func RegisterTrailingSlashRedirects(h *server.Hertz) {
	routes := h.Routes()
	registered := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range routes {
		if route.Path == "/" || strings.Contains(route.Path, "*") {
			continue
		}
		alternate := route.Path + "/"
		if strings.HasSuffix(route.Path, "/") {
			alternate = strings.TrimSuffix(route.Path, "/")
		}
		key := route.Method + " " + alternate
		if _, exists := registered[key]; exists {
			continue
		}
		h.Handle(route.Method, alternate, redirectTrailingSlash)
		registered[key] = struct{}{}
	}
}

func RedirectTo(target string) app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		redirect(c, target)
	}
}

func redirectTrailingSlash(_ context.Context, c *app.RequestContext) {
	path := string(c.Request.URI().PathOriginal())
	if strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	} else {
		path += "/"
	}
	redirect(c, path)
}

func redirect(c *app.RequestContext, target string) {
	if query := c.Request.QueryString(); len(query) > 0 {
		target += "?" + string(query)
	}
	status := http.StatusTemporaryRedirect
	if string(c.Method()) == http.MethodGet {
		status = http.StatusMovedPermanently
	}
	c.Redirect(status, []byte(target))
	c.Abort()
}
