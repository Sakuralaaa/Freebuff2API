package main

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error", "")
		return
	}
	stats := s.stats.Snapshot()
	tokens := s.runs.Snapshots()
	var b strings.Builder
	b.WriteString("# HELP freebuff_requests_total Total requests handled by proxy\n")
	b.WriteString("# TYPE freebuff_requests_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_requests_total %d\n", stats.TotalRequests))
	b.WriteString("# HELP freebuff_requests_success_total Successful requests handled by proxy\n")
	b.WriteString("# TYPE freebuff_requests_success_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_requests_success_total %d\n", stats.SuccessCount))
	b.WriteString("# HELP freebuff_requests_failure_total Failed requests handled by proxy\n")
	b.WriteString("# TYPE freebuff_requests_failure_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_requests_failure_total %d\n", stats.FailureCount))
	b.WriteString("# TYPE freebuff_retries_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_retries_total %d\n", stats.RetryCount))
	b.WriteString("# TYPE freebuff_retries_429_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_retries_429_total %d\n", stats.Retry429Count))
	b.WriteString("# TYPE freebuff_status_429_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_status_429_total %d\n", stats.Status429Count))
	b.WriteString("# TYPE freebuff_timeout_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_timeout_total %d\n", stats.TimeoutCount))
	b.WriteString("# TYPE freebuff_run_invalid_total counter\n")
	b.WriteString(fmt.Sprintf("freebuff_run_invalid_total %d\n", stats.RunInvalidCount))
	b.WriteString("# TYPE freebuff_token_inflight gauge\n")
	for _, token := range tokens {
		b.WriteString(fmt.Sprintf("freebuff_token_inflight{token=%q} %d\n", token.Name, token.Inflight))
		b.WriteString(fmt.Sprintf("freebuff_token_healthy{token=%q} %d\n", token.Name, boolToFloat(token.Healthy)))
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func boolToFloat(value bool) int {
	if value {
		return 1
	}
	return 0
}

