package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newFrontendTestServer() *Server {
	cfg := Config{
		ListenAddr:      ":8080",
		UpstreamBaseURL: "https://codebuff.com",
		RequestTimeout:  30 * time.Second,
	}
	return NewServer(cfg, log.New(io.Discard, "", 0), nil)
}

func TestFrontendRootServesIndex(t *testing.T) {
	srv := newFrontendTestServer()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestFrontendUIWithoutSlashDoesNotRedirect(t *testing.T) {
	srv := newFrontendTestServer()
	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Fatalf("expected no redirect location, got %q", location)
	}
}

func TestFrontendUIWithSlashServesWithoutRedirect(t *testing.T) {
	srv := newFrontendTestServer()
	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Fatalf("expected no redirect location, got %q", location)
	}
}
