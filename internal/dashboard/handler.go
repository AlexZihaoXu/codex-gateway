package dashboard

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

type handler struct {
	assets     fs.FS
	fileServer http.Handler
}

func NewHandler() (http.Handler, error) {
	assets, err := fs.Sub(embeddedAssets, embeddedRoot)
	if err != nil {
		return nil, fmt.Errorf("open embedded dashboard: %w", err)
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, fmt.Errorf("find embedded dashboard index: %w", err)
	}

	return &handler{
		assets:     assets,
		fileServer: http.FileServerFS(assets),
	}, nil
}

func (h *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	assetPath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if assetPath == "." || assetPath == "" {
		h.serveIndex(response, request)
		return
	}

	info, err := fs.Stat(h.assets, assetPath)
	if err == nil && !info.IsDir() {
		h.fileServer.ServeHTTP(response, request)
		return
	}
	if path.Ext(assetPath) != "" {
		http.NotFound(response, request)
		return
	}

	h.serveIndex(response, request)
}

func (h *handler) serveIndex(response http.ResponseWriter, request *http.Request) {
	requestCopy := request.Clone(request.Context())
	requestCopy.URL.Path = "/"
	response.Header().Set("Cache-Control", "no-cache")
	h.fileServer.ServeHTTP(response, requestCopy)
}
