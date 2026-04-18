package main

import (
	"net/http"
	"net/url"
	"strings"
)

type accountResponse struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Enabled       bool          `json:"enabled"`
	Healthy       bool          `json:"healthy"`
	LastError     string        `json:"last_error,omitempty"`
	Inflight      int           `json:"inflight"`
	MaxConcurrent int           `json:"max_concurrent"`
	TotalRequests int           `json:"total_requests"`
	SuccessCount  int           `json:"success_count"`
	FailureCount  int           `json:"failure_count"`
	LastUsedAt    string        `json:"last_used_at,omitempty"`
	Runs          []runSnapshot `json:"runs,omitempty"`
}

func snapshotToAccount(snapshot tokenSnapshot) accountResponse {
	account := accountResponse{
		ID:            snapshot.Name,
		Name:          snapshot.Name,
		Enabled:       snapshot.Enabled,
		Healthy:       snapshot.Healthy,
		LastError:     snapshot.LastError,
		Inflight:      snapshot.Inflight,
		MaxConcurrent: snapshot.MaxConcurrent,
		TotalRequests: snapshot.TotalRequests,
		SuccessCount:  snapshot.SuccessCount,
		FailureCount:  snapshot.FailureCount,
		Runs:          snapshot.Runs,
	}
	if !snapshot.LastUsedAt.IsZero() {
		account.LastUsedAt = snapshot.LastUsedAt.UTC().Format(timeLayoutRFC3339)
	}
	return account
}

const timeLayoutRFC3339 = "2006-01-02T15:04:05Z07:00"

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	snapshots := s.runs.Snapshots()
	items := make([]accountResponse, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, snapshotToAccount(snapshot))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

func (s *Server) handleAccountAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	name, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(name) == "" {
		writeOpenAIError(w, http.StatusBadRequest, "invalid account id", "invalid_request_error", "")
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodDelete {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
			return
		}
		if err := s.runs.RemoveToken(name); err != nil {
			writeOpenAIError(w, http.StatusNotFound, err.Error(), "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"deleted": name,
		})
		return
	}

	if len(parts) == 2 && r.Method == http.MethodPost {
		action := strings.ToLower(strings.TrimSpace(parts[1]))
		switch action {
		case "disable":
			account, err := s.runs.SetTokenEnabled(name, false)
			if err != nil {
				writeOpenAIError(w, http.StatusNotFound, err.Error(), "invalid_request_error", "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      true,
				"account": snapshotToAccount(account),
			})
			return
		case "enable":
			account, err := s.runs.SetTokenEnabled(name, true)
			if err != nil {
				writeOpenAIError(w, http.StatusNotFound, err.Error(), "invalid_request_error", "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      true,
				"account": snapshotToAccount(account),
			})
			return
		default:
			http.NotFound(w, r)
			return
		}
	}

	writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
}
