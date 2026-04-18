package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newLoginTestServer() *Server {
	cfg := Config{
		ListenAddr:      ":8080",
		UpstreamBaseURL: "https://codebuff.com",
		RequestTimeout:  30 * time.Second,
	}
	return NewServer(cfg, log.New(io.Discard, "", 0), nil)
}

func TestCreateLoginSessionParsesCamelCasePayload(t *testing.T) {
	origClient := freebuffLoginHTTPClient
	t.Cleanup(func() {
		freebuffLoginHTTPClient = origClient
	})
	freebuffLoginHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.String() != freebuffCLICodeURL {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"fingerprintHash":"hash-camel",
					"expiresAt":"exp-camel",
					"loginUrl":"https://freebuff.com/login/camel"
				}`)),
				Header: make(http.Header),
			}, nil
		}),
	}

	srv := newLoginTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/login/session", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"fingerprint_hash":"hash-camel"`) {
		t.Fatalf("expected fingerprint_hash in response, got %s", body)
	}
	if !strings.Contains(body, `"expires_at":"exp-camel"`) {
		t.Fatalf("expected expires_at in response, got %s", body)
	}
	if !strings.Contains(body, `"login_url":"https://freebuff.com/login/camel"`) {
		t.Fatalf("expected login_url in response, got %s", body)
	}
}

func TestCreateLoginSessionParsesNestedSnakeCasePayload(t *testing.T) {
	origClient := freebuffLoginHTTPClient
	t.Cleanup(func() {
		freebuffLoginHTTPClient = origClient
	})
	freebuffLoginHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.String() != freebuffCLICodeURL {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"data":{
						"fingerprint_hash":"hash-snake",
						"expires_at":"exp-snake",
						"login_url":"https://freebuff.com/login/snake"
					}
				}`)),
				Header: make(http.Header),
			}, nil
		}),
	}

	srv := newLoginTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/login/session", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"fingerprint_hash":"hash-snake"`) {
		t.Fatalf("expected fingerprint_hash in response, got %s", body)
	}
	if !strings.Contains(body, `"expires_at":"exp-snake"`) {
		t.Fatalf("expected expires_at in response, got %s", body)
	}
	if !strings.Contains(body, `"login_url":"https://freebuff.com/login/snake"`) {
		t.Fatalf("expected login_url in response, got %s", body)
	}
}

func TestCreateLoginSessionParsesNumericExpiresAtAndDeepPayload(t *testing.T) {
	origClient := freebuffLoginHTTPClient
	t.Cleanup(func() {
		freebuffLoginHTTPClient = origClient
	})
	freebuffLoginHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.String() != freebuffCLICodeURL {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(`{
					"payload":{
						"data":{
							"fingerprintHash":"hash-numeric",
							"expiresAt":1735689600,
							"loginUrl":"https://freebuff.com/login/numeric"
						}
					}
				}`)),
				Header: make(http.Header),
			}, nil
		}),
	}

	srv := newLoginTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/login/session", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"fingerprint_hash":"hash-numeric"`) {
		t.Fatalf("expected fingerprint_hash in response, got %s", body)
	}
	if !strings.Contains(body, `"expires_at":"1735689600"`) {
		t.Fatalf("expected expires_at in response, got %s", body)
	}
	if !strings.Contains(body, `"login_url":"https://freebuff.com/login/numeric"`) {
		t.Fatalf("expected login_url in response, got %s", body)
	}
}
