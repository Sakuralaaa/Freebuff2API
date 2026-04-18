package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	freebuffCLICodeURL   = "https://freebuff.com/api/auth/cli/code"
	freebuffCLIStatusURL = "https://freebuff.com/api/auth/cli/status"
	fingerprintPrefix    = "codebuff-cli-"
)

var freebuffLoginHTTPClient = &http.Client{Timeout: 30 * time.Second}

type loginSessionRequest struct {
	FingerprintID string `json:"fingerprint_id"`
}

type loginSessionResponse struct {
	FingerprintID   string `json:"fingerprint_id"`
	FingerprintHash string `json:"fingerprint_hash"`
	ExpiresAt       string `json:"expires_at"`
	LoginURL        string `json:"login_url"`
}

func (s *Server) handleCreateLoginSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	fingerprintID := buildFingerprintID()
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "failed to read request body", "invalid_request_error", "")
		return
	}
	if len(bytes.TrimSpace(requestBody)) > 0 {
		var req loginSessionRequest
		if err := json.Unmarshal(requestBody, &req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "request body must be valid JSON", "invalid_request_error", "")
			return
		}
		if trimmed := strings.TrimSpace(req.FingerprintID); trimmed != "" {
			fingerprintID = trimmed
		}
	}

	payload, err := requestFreebuffJSON(r.Context(), http.MethodPost, freebuffCLICodeURL, map[string]string{
		"fingerprintId": fingerprintID,
	})
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "server_error", "")
		return
	}

	resp := loginSessionResponse{
		FingerprintID:   fingerprintID,
		FingerprintHash: strings.TrimSpace(stringValue(payload, "fingerprintHash")),
		ExpiresAt:       strings.TrimSpace(stringValue(payload, "expiresAt")),
		LoginURL:        strings.TrimSpace(stringValue(payload, "loginUrl")),
	}
	if resp.FingerprintHash == "" || resp.ExpiresAt == "" || resp.LoginURL == "" {
		writeOpenAIError(w, http.StatusBadGateway, "freebuff login session response is incomplete", "server_error", "")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLoginStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	query := r.URL.Query()
	fingerprintID := strings.TrimSpace(query.Get("fingerprint_id"))
	fingerprintHash := strings.TrimSpace(query.Get("fingerprint_hash"))
	expiresAt := strings.TrimSpace(query.Get("expires_at"))

	if fingerprintID == "" || fingerprintHash == "" || expiresAt == "" {
		writeOpenAIError(w, http.StatusBadRequest, "fingerprint_id, fingerprint_hash and expires_at are required", "invalid_request_error", "")
		return
	}

	statusURL := fmt.Sprintf("%s?%s", freebuffCLIStatusURL, url.Values{
		"fingerprintId":   []string{fingerprintID},
		"fingerprintHash": []string{fingerprintHash},
		"expiresAt":       []string{expiresAt},
	}.Encode())

	payload, err := requestFreebuffJSON(r.Context(), http.MethodGet, statusURL, nil)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "server_error", "")
		return
	}
	payload["fingerprintId"] = fingerprintID
	payload["fingerprintHash"] = fingerprintHash
	payload["expiresAt"] = expiresAt
	user, loggedIn := payload["user"].(map[string]any)
	if loggedIn {
		authToken := strings.TrimSpace(stringValue(user, "authToken"))
		authTokenAlt := strings.TrimSpace(stringValue(user, "auth_token"))
		if authToken == "" {
			authToken = authTokenAlt
		}
		if authToken != "" {
			user["authToken"] = authToken
			user["auth_token"] = authToken
		}
		payload["user"] = user
	}
	payload["login_success"] = loggedIn
	writeJSON(w, http.StatusOK, payload)
}

func buildFingerprintID() string {
	seed := make([]byte, 20)
	if _, err := rand.Read(seed); err != nil {
		return fmt.Sprintf("%s%d", fingerprintPrefix, time.Now().UnixNano())
	}
	token := base64.RawURLEncoding.EncodeToString(seed)
	if len(token) > 26 {
		token = token[:26]
	}
	return fingerprintPrefix + token
}

func requestFreebuffJSON(ctx context.Context, method, targetURL string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "freebuff-login-helper/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := freebuffLoginHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request freebuff failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read freebuff response: %w", err)
	}

	var payload map[string]any
	if len(bytes.TrimSpace(rawBody)) > 0 {
		if err := json.Unmarshal(rawBody, &payload); err != nil {
			return nil, fmt.Errorf("parse freebuff response: %w", err)
		}
	} else {
		payload = map[string]any{}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("freebuff returned %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}
	return payload, nil
}

func stringValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
