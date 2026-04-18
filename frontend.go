package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web/*
var embeddedFrontendFiles embed.FS

var frontendFS = mustFrontendFS()

func mustFrontendFS() fs.FS {
	fsys, err := fs.Sub(embeddedFrontendFiles, "web")
	if err != nil {
		panic(err)
	}
	return fsys
}

func (s *Server) handleFrontendIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	clone := r.Clone(r.Context())
	clone.URL.Path = "/index.html"
	http.FileServer(http.FS(frontendFS)).ServeHTTP(w, clone)
}

func isPublicPath(path string) bool {
	switch {
	case path == "/", path == "/healthz":
		return true
	case strings.HasPrefix(path, "/ui/"):
		return true
	case strings.HasPrefix(path, "/api/login/"):
		return true
	default:
		return false
	}
}
