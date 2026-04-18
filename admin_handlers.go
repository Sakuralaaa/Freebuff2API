package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type adminLoginRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       s.admin != nil && s.admin.Enabled(),
		"authenticated": s.adminAuthorized(r),
	})
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}
	if s.admin == nil || !s.admin.Enabled() {
		writeOpenAIError(w, http.StatusBadRequest, "admin password is not configured", "invalid_request_error", "")
		return
	}

	var req adminLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "request body must be valid JSON", "invalid_request_error", "")
		return
	}
	sessionToken, err := s.admin.Login(strings.TrimSpace(req.Password))
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid admin password", "authentication_error", "")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}
	if cookie, err := r.Cookie(adminSessionCookieName); err == nil {
		s.admin.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	tokenState := s.runs.Snapshots()
	totalTokenRequests := 0
	totalTokenSuccess := 0
	totalTokenFailed := 0
	activeRuns := 0
	drainingRuns := 0

	for _, token := range tokenState {
		totalTokenRequests += token.TotalRequests
		totalTokenSuccess += token.SuccessCount
		totalTokenFailed += token.FailureCount
		activeRuns += len(token.Runs)
		drainingRuns += token.DrainingRuns
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"started_at": s.started.UTC(),
		"uptime_sec": int(time.Since(s.started).Seconds()),
		"calls":      s.stats.Snapshot(),
		"tokens": map[string]any{
			"total":          len(tokenState),
			"active_runs":    activeRuns,
			"draining_runs":  drainingRuns,
			"total_requests": totalTokenRequests,
			"success_count":  totalTokenSuccess,
			"failure_count":  totalTokenFailed,
			"items":          tokenState,
		},
	})
}
