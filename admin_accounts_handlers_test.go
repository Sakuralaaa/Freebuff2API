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

func newAccountsTestServer() *Server {
	cfg := Config{
		ListenAddr:      ":8080",
		UpstreamBaseURL: "https://codebuff.com",
		RequestTimeout:  30 * time.Second,
		AuthTokens:      []string{"token-a", "token-b"},
	}
	return NewServer(cfg, log.New(io.Discard, "", 0), nil)
}

func TestAccountsListEndpointReturnsConfiguredTokens(t *testing.T) {
	srv := newAccountsTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Total int               `json:"total"`
		Items []accountResponse `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.Total != 2 || len(payload.Items) != 2 {
		t.Fatalf("expected two accounts, total=%d len=%d", payload.Total, len(payload.Items))
	}
	if !payload.Items[0].Enabled || !payload.Items[1].Enabled {
		t.Fatalf("expected all accounts to be enabled by default")
	}
}

func TestAccountDisableEndpointDisablesToken(t *testing.T) {
	srv := newAccountsTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/token-1/disable", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)

	var payload struct {
		Items []accountResponse `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(payload.Items) == 0 || payload.Items[0].Enabled {
		t.Fatalf("expected first account to be disabled, got %+v", payload.Items)
	}
}

func TestAccountDeleteEndpointRemovesToken(t *testing.T) {
	srv := newAccountsTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/api/accounts/token-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)

	var payload struct {
		Total int               `json:"total"`
		Items []accountResponse `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 {
		t.Fatalf("expected one account after deletion, total=%d len=%d", payload.Total, len(payload.Items))
	}
	if payload.Items[0].Name != "token-2" {
		t.Fatalf("expected remaining account token-2, got %q", payload.Items[0].Name)
	}
}
