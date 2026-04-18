package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

//go:embed web/*
var embeddedFrontendFiles embed.FS

var frontendFS = mustFrontendFS()
var frontendIndex = mustReadFrontendIndex()

func mustFrontendFS() fs.FS {
	fsys, err := fs.Sub(embeddedFrontendFiles, "web")
	if err != nil {
		panic(err)
	}
	return fsys
}

func mustReadFrontendIndex() []byte {
	data, err := fs.ReadFile(frontendFS, "index.html")
	if err != nil {
		panic(err)
	}
	return data
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(frontendIndex); err != nil {
		if s.logger != nil {
			s.logger.Printf("write frontend index failed: %v", err)
		} else {
			log.Printf("write frontend index failed: %v", err)
		}
	}
}

func (s *Server) handleFrontendUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}
	if r.URL.Path != "/ui" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(frontendIndex); err != nil {
		if s.logger != nil {
			s.logger.Printf("write frontend /ui failed: %v", err)
		} else {
			log.Printf("write frontend /ui failed: %v", err)
		}
	}
}

func requiresAdminSession(path string) bool {
	return strings.HasPrefix(path, "/api/login/") || path == "/api/admin/logout"
}

func requiresAPIKeyAuth(path string) bool {
	return strings.HasPrefix(path, "/v1/")
}
