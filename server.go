package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	cfg      Config
	logger   *log.Logger
	client   *UpstreamClient
	runs     *RunManager
	registry *ModelRegistry
	admin    *adminAuth
	started  time.Time
	stats    *callStats
	policy   *runtimePolicyStore
	aliases  *modelAliasStore
}

func NewServer(cfg Config, logger *log.Logger, registry *ModelRegistry) *Server {
	client := NewUpstreamClient(cfg)
	runManager := NewRunManager(cfg, client, logger)
	policy := newRuntimePolicyStore(defaultRuntimePolicy(cfg))

	return &Server{
		cfg:      cfg,
		logger:   logger,
		client:   client,
		runs:     runManager,
		registry: registry,
		admin:    newAdminAuth(cfg.AdminPassword),
		started:  time.Now(),
		stats:    newCallStats(),
		policy:   policy,
		aliases:  newModelAliasStore(cfg.ModelAliases),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleFrontendIndex)
	mux.HandleFunc("/ui", s.handleFrontendUI)
	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(frontendFS))))
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/admin/status", s.handleAdminStatus)
	mux.HandleFunc("/api/admin/login", s.handleAdminLogin)
	mux.HandleFunc("/api/admin/logout", s.handleAdminLogout)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/export/json", s.handleExportJSON)
	mux.HandleFunc("/api/login/session", s.handleCreateLoginSession)
	mux.HandleFunc("/api/login/status", s.handleLoginStatus)
	mux.HandleFunc("/api/policy", s.handlePolicy)
	mux.HandleFunc("/api/model-aliases", s.handleModelAliases)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/accounts/", s.handleAccountAction)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/metrics", s.handleMetrics)
	return s.withMiddleware(mux)
}

func (s *Server) Start(ctx context.Context) {
	s.runs.Start(ctx)
}

func (s *Server) Shutdown(ctx context.Context) {
	s.runs.Close(ctx)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requiresAdminSession(r.URL.Path) && !s.adminAuthorized(r) {
			writeOpenAIError(w, http.StatusUnauthorized, "admin login required", "authentication_error", "")
			return
		}
		if requiresAPIKeyAuth(r.URL.Path) && len(s.cfg.APIKeys) > 0 && !s.authorized(r) {
			writeOpenAIError(w, http.StatusUnauthorized, "invalid proxy api key", "authentication_error", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminAuthorized(r *http.Request) bool {
	if s.admin == nil || !s.admin.Enabled() {
		return true
	}
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil {
		return false
	}
	return s.admin.IsAuthorized(cookie.Value)
}

func (s *Server) authorized(r *http.Request) bool {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return false
	}
	apiKey := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	return containsString(s.cfg.APIKeys, apiKey)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	response := map[string]any{
		"ok":          true,
		"started_at":  s.started.UTC(),
		"uptime_sec":  int(time.Since(s.started).Seconds()),
		"token_state": s.runs.Snapshots(),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	created := s.started.Unix()
	modelsList := make([]string, 0)
	if s.registry != nil {
		modelsList = s.registry.Models()
	}
	for alias := range s.aliases.Snapshot() {
		modelsList = append(modelsList, alias)
	}
	models := make([]map[string]any, 0, len(modelsList))
	for _, model := range modelsList {
		models = append(models, map[string]any{
			"id":         model,
			"object":     "model",
			"created":    created,
			"owned_by":   "Freebuff-Go",
			"root":       model,
			"permission": []any{},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   models,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}

	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "failed to read request body", "invalid_request_error", "")
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "request body must be valid JSON", "invalid_request_error", "")
		return
	}

	requestedModel, _ := payload["model"].(string)
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "")
		return
	}
	resolvedModel := s.aliases.Resolve(requestedModel)
	if s.registry == nil {
		writeOpenAIError(w, http.StatusBadGateway, "model registry unavailable", "server_error", "")
		return
	}
	agentID, ok := s.registry.AgentForModel(resolvedModel)
	if !ok {
		writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("unsupported model %q", requestedModel), "invalid_request_error", "model_not_found")
		return
	}
	policy := s.policy.Snapshot()
	s.runs.UpdatePolicy(policy)
	isStream, _ := payload["stream"].(bool)
	timeout := policy.NonStreamTimeout
	if isStream {
		timeout = policy.StreamTimeout
	}
	requestCtx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	startTime := time.Now()
	var recordResultOnce sync.Once
	recordResult := func(success bool) {
		recordResultOnce.Do(func() {
			s.stats.Record(requestedModel, success)
		})
	}

	totalAttempts := policy.MaxRetries + 1
	for attempt := 0; attempt < totalAttempts; attempt++ {
		lease, err := s.runs.Acquire(requestCtx, agentID)
		if err != nil {
			recordResult(false)
			writeOpenAIError(w, http.StatusBadGateway, "no healthy upstream auth token available", "server_error", "")
			return
		}

		s.logger.Printf("[%s] Routing request (model: %s -> %s) via run: %s", lease.pool.name, requestedModel, resolvedModel, lease.run.id)

		upstreamBody, err := s.injectUpstreamMetadata(payload, resolvedModel, lease.run.id)
		if err != nil {
			s.runs.Release(lease)
			writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
			return
		}

		resp, errorBody, err := s.client.ChatCompletions(requestCtx, lease.pool.token, upstreamBody)
		if err != nil {
			s.runs.RecordResult(lease, false)
			s.runs.Release(lease)
			if errors.Is(err, context.DeadlineExceeded) {
				s.stats.RecordTimeout()
			}
			recordResult(false)
			writeOpenAIError(w, http.StatusBadGateway, err.Error(), "server_error", "")
			return
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer resp.Body.Close()
			copyHeaders(w.Header(), resp.Header)
			w.WriteHeader(resp.StatusCode)
			if err := copyResponseBody(w, resp.Body); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Printf("[%s] proxy response copy failed: %v", lease.pool.name, err)
			}
			s.logger.Printf("[%s] Request completed successfully in %v (status: %d)", lease.pool.name, time.Since(startTime).Round(time.Millisecond), resp.StatusCode)
			s.runs.RecordResult(lease, true)
			s.runs.Release(lease)
			recordResult(true)
			return
		}

		if isRunInvalid(resp.StatusCode, errorBody) {
			s.logger.Printf("%s: run %s invalid, rotating and retrying", lease.pool.name, lease.run.id)
			s.runs.RecordResult(lease, false)
			s.runs.Invalidate(lease, strings.TrimSpace(string(errorBody)))
			s.runs.Release(lease)
			s.stats.RecordRunInvalid()
			if attempt+1 < totalAttempts {
				s.stats.RecordRetry(resp.StatusCode)
				if err := waitRetryBackoff(requestCtx, policy, attempt, resp.Header.Get("Retry-After"), resp.StatusCode); err != nil {
					recordResult(false)
					writeOpenAIError(w, http.StatusBadGateway, "request canceled during retry backoff", "server_error", "")
					return
				}
			}
			continue
		}

		if resp.StatusCode == http.StatusUnauthorized {
			s.runs.Cooldown(lease, 30*time.Minute, "upstream auth rejected token")
		}
		s.stats.RecordStatusCode(resp.StatusCode)

		s.runs.RecordResult(lease, false)
		s.runs.Release(lease)
		s.logger.Printf("[%s] upstream error response: %s", lease.pool.name, string(errorBody))
		if resp.StatusCode == http.StatusTooManyRequests && attempt+1 < totalAttempts {
			s.stats.RecordRetry(resp.StatusCode)
			if err := waitRetryBackoff(requestCtx, policy, attempt, resp.Header.Get("Retry-After"), resp.StatusCode); err != nil {
				recordResult(false)
				writeOpenAIError(w, http.StatusBadGateway, "request canceled during retry backoff", "server_error", "")
				return
			}
			continue
		}
		recordResult(false)
		writePassthroughError(w, resp.StatusCode, errorBody)
		return
	}

	recordResult(false)
	writeOpenAIError(w, http.StatusBadGateway, "upstream request exhausted retries", "server_error", "")
}

func (s *Server) injectUpstreamMetadata(payload map[string]any, requestedModel, runID string) ([]byte, error) {
	cloned := cloneMap(payload)
	cloned["model"] = requestedModel

	metadata, ok := cloned["codebuff_metadata"].(map[string]any)
	if !ok || metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["run_id"] = runID
	metadata["cost_mode"] = "free"
	metadata["client_id"] = generateClientSessionId()
	cloned["codebuff_metadata"] = metadata

	body, err := json.Marshal(cloned)
	if err != nil {
		return nil, fmt.Errorf("marshal upstream request: %w", err)
	}
	return body, nil
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case map[string]any:
			output[key] = cloneMap(typed)
		case []any:
			output[key] = cloneSlice(typed)
		default:
			output[key] = value
		}
	}
	return output
}

func cloneSlice(input []any) []any {
	output := make([]any, len(input))
	for index, value := range input {
		switch typed := value.(type) {
		case map[string]any:
			output[index] = cloneMap(typed)
		case []any:
			output[index] = cloneSlice(typed)
		default:
			output[index] = value
		}
	}
	return output
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponseBody(w http.ResponseWriter, body io.Reader) error {
	flusher, _ := w.(http.Flusher)
	buffer := responseBufferPool.Get().([]byte)
	defer responseBufferPool.Put(buffer)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

var responseBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 32*1024)
	},
}

func waitRetryBackoff(ctx context.Context, policy runtimePolicySnapshot, attempt int, retryAfter string, statusCode int) error {
	delay := retryAfterDuration(retryAfter)
	if statusCode == http.StatusTooManyRequests && delay <= 0 {
		exponent := math.Pow(2, float64(attempt))
		delay = time.Duration(float64(policy.RetryBackoffBase) * exponent)
	}
	if delay <= 0 {
		delay = policy.RetryBackoffBase
	}
	if delay > policy.RetryBackoffMax {
		delay = policy.RetryBackoffMax
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRunInvalid(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	message := strings.ToLower(string(body))
	return strings.Contains(message, "runid not found") || strings.Contains(message, "runid not running")
}

func writePassthroughError(w http.ResponseWriter, statusCode int, body []byte) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && json.Valid(trimmed) {
		message, errorType, code := extractUpstreamError(trimmed)
		writeOpenAIError(w, statusCode, message, errorType, code)
		return
	}
	writeOpenAIError(w, statusCode, strings.TrimSpace(string(trimmed)), "upstream_error", "")
}

func extractUpstreamError(body []byte) (message, errorType, code string) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body)), "upstream_error", ""
	}

	errorType = "upstream_error"

	if rawError, ok := payload["error"]; ok {
		switch typed := rawError.(type) {
		case string:
			code = typed
		case map[string]any:
			if value, ok := typed["message"].(string); ok && strings.TrimSpace(value) != "" {
				message = value
			}
			if value, ok := typed["type"].(string); ok && strings.TrimSpace(value) != "" {
				errorType = value
			}
			if value, ok := typed["code"].(string); ok && strings.TrimSpace(value) != "" {
				code = value
			}
		}
	}

	if value, ok := payload["message"].(string); ok && strings.TrimSpace(value) != "" {
		message = value
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	return message, errorType, code
}

func writeOpenAIError(w http.ResponseWriter, statusCode int, message, errorType, code string) {
	if message == "" {
		message = http.StatusText(statusCode)
	}
	payload := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorType,
		},
	}
	if code != "" {
		payload["error"].(map[string]any)["code"] = code
	}
	writeJSON(w, statusCode, payload)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"error":{"message":"failed to encode response","type":"server_error"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
