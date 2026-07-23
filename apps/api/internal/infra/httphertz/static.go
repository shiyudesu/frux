package infrahttphertz

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
)

// StaticHandlers serves file bodies through net/http and preserves HEAD metadata natively.
func StaticHandlers(root string, prefix string) (app.HandlerFunc, app.HandlerFunc, error) {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}

	fileServer := http.StripPrefix(prefix, http.FileServer(onlyFilesFS{FileSystem: http.Dir(rootPath)}))
	getHandler := adaptor.HertzHandler(fileServer)
	headHandler := func(_ context.Context, c *app.RequestContext) {
		request := staticHeadRequest(c)
		response := newStaticHeadResponse()
		fileServer.ServeHTTP(response, request)
		response.copyTo(c)
	}

	return getHandler, headHandler, nil
}

type onlyFilesFS struct {
	FileSystem http.FileSystem
}

func (fs onlyFilesFS) Open(name string) (http.File, error) {
	file, err := fs.FileSystem.Open(name)
	if err != nil {
		return nil, err
	}
	return noDirectoryListingFile{File: file}, nil
}

type noDirectoryListingFile struct {
	http.File
}

func (file noDirectoryListingFile) Readdir(_ int) ([]os.FileInfo, error) {
	return nil, nil
}

type staticHeadResponse struct {
	header http.Header
	status int
}

func newStaticHeadResponse() *staticHeadResponse {
	return &staticHeadResponse{header: make(http.Header)}
}

func (w *staticHeadResponse) Header() http.Header {
	return w.header
}

func (w *staticHeadResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *staticHeadResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(body), nil
}

func (w *staticHeadResponse) copyTo(c *app.RequestContext) {
	for key, values := range w.header {
		for _, value := range values {
			c.Response.Header.Add(key, value)
		}
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	c.Status(status)
}

func staticHeadRequest(c *app.RequestContext) *http.Request {
	requestURL := &url.URL{
		Path:     string(c.Path()),
		RawQuery: string(c.Request.URI().QueryString()),
	}
	request := &http.Request{
		Method:     http.MethodHead,
		URL:        requestURL,
		Header:     make(http.Header),
		RequestURI: string(c.Request.RequestURI()),
	}
	c.Request.Header.VisitAll(func(key []byte, value []byte) {
		request.Header.Add(string(key), string(value))
	})
	return request
}
