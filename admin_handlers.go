package main

import (
	"encoding/json"
	"net/http"
	"strings"
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
		Secure:   isRequestSecure(r),
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
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}
