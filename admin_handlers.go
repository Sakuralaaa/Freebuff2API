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

type internalTokenStatsAggregate struct {
	totalRequests int
	totalSuccess  int
	totalFailed   int
	activeRuns    int
	drainingRuns  int
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
	aggregatedTokenStats := aggregateTokenStats(tokenState)

	writeJSON(w, http.StatusOK, map[string]any{
		"started_at": s.started.UTC(),
		"uptime_sec": int(time.Since(s.started).Seconds()),
		"calls":      s.stats.Snapshot(),
		"tokens": map[string]any{
			"total":          len(tokenState),
			"active_runs":    aggregatedTokenStats.activeRuns,
			"draining_runs":  aggregatedTokenStats.drainingRuns,
			"total_requests": aggregatedTokenStats.totalRequests,
			"success_count":  aggregatedTokenStats.totalSuccess,
			"failure_count":  aggregatedTokenStats.totalFailed,
			"items":          tokenState,
		},
	})
}

func aggregateTokenStats(tokens []tokenSnapshot) internalTokenStatsAggregate {
	result := internalTokenStatsAggregate{}
	for _, token := range tokens {
		result.totalRequests += token.TotalRequests
		result.totalSuccess += token.SuccessCount
		result.totalFailed += token.FailureCount
		result.activeRuns += len(token.Runs)
		result.drainingRuns += token.DrainingRuns
	}
	return result
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	authTokens := s.runs.Tokens()
	statsSnapshot := s.stats.Snapshot()
	tokenState := s.runs.Snapshots()
	aggregatedTokenStats := aggregateTokenStats(tokenState)
	tokenSummary := map[string]any{
		"total":          len(tokenState),
		"active_runs":    aggregatedTokenStats.activeRuns,
		"draining_runs":  aggregatedTokenStats.drainingRuns,
		"total_requests": aggregatedTokenStats.totalRequests,
		"success_count":  aggregatedTokenStats.totalSuccess,
		"failure_count":  aggregatedTokenStats.totalFailed,
		"items":          tokenState,
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":        "Freebuff",
		"exported_at": time.Now().UTC(),
		"auth_tokens": authTokens,
		"stats": map[string]any{
			"calls":  statsSnapshot,
			"tokens": tokenSummary,
		},
		"integration": map[string]any{
			"auth_tokens_env": "AUTH_TOKENS",
			"config_template": map[string]any{
				"LISTEN_ADDR":       s.cfg.ListenAddr,
				"UPSTREAM_BASE_URL": s.cfg.UpstreamBaseURL,
				"AUTH_TOKENS":       authTokens,
				"ROTATION_INTERVAL": s.cfg.RotationInterval.String(),
				"REQUEST_TIMEOUT":   s.cfg.RequestTimeout.String(),
			},
			"notes": []string{
				"AUTH_TOKENS 可直接用于其他兼容 OpenAI Proxy 项目。",
				"AUTH_TOKENS can be reused in other OpenAI-compatible proxy projects.",
				"建议在目标项目中按需配置 API_KEYS 和 ADMIN_PASSWORD。",
			},
		},
	})
}
