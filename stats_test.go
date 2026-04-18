package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newStatsTestServer() *Server {
	cfg := Config{
		ListenAddr:      ":8080",
		UpstreamBaseURL: "https://codebuff.com",
		RequestTimeout:  30 * time.Second,
	}
	return NewServer(cfg, log.New(io.Discard, "", 0), nil)
}

func TestStatsEndpointMethodNotAllowed(t *testing.T) {
	srv := newStatsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/stats", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestStatsEndpointReturnsSnapshot(t *testing.T) {
	srv := newStatsTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON payload: %v", err)
	}

	if _, ok := payload["calls"]; !ok {
		t.Fatalf("expected calls field in stats payload")
	}
	if _, ok := payload["tokens"]; !ok {
		t.Fatalf("expected tokens field in stats payload")
	}
}
