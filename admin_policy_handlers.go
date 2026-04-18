package main

import (
	"encoding/json"
	"net/http"
	"sort"
)

type modelAliasesPayload struct {
	Aliases map[string]string `json:"aliases"`
}

func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"policy": policyPayloadFromSnapshot(s.policy.Snapshot()),
		})
	case http.MethodPut:
		var req struct {
			Policy policyPayload `json:"policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "request body must be valid JSON", "invalid_request_error", "")
			return
		}
		next, err := req.Policy.toSnapshot()
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
			return
		}
		if err := s.policy.Update(next); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
			return
		}
		s.runs.UpdatePolicy(next)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"policy": policyPayloadFromSnapshot(next),
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
	}
}

func (s *Server) handleModelAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		aliases := s.aliases.Snapshot()
		writeJSON(w, http.StatusOK, map[string]any{
			"aliases": aliases,
			"items":   sortedAliasItems(aliases),
		})
	case http.MethodPut:
		var req modelAliasesPayload
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "request body must be valid JSON", "invalid_request_error", "")
			return
		}
		s.aliases.Replace(req.Aliases)
		aliases := s.aliases.Snapshot()
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"aliases": aliases,
			"items":   sortedAliasItems(aliases),
		})
	default:
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
	}
}

func sortedAliasItems(aliases map[string]string) []map[string]string {
	keys := make([]string, 0, len(aliases))
	for alias := range aliases {
		keys = append(keys, alias)
	}
	sort.Strings(keys)
	items := make([]map[string]string, 0, len(keys))
	for _, alias := range keys {
		items = append(items, map[string]string{
			"alias": alias,
			"model": aliases[alias],
		})
	}
	return items
}

