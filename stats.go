package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const fallbackModelName = "unspecified-model"

type callStats struct {
	mu sync.RWMutex

	totalRequests int
	successCount  int
	failureCount  int
	retryCount    int
	retry429Count int
	status429Count int
	status401Count int
	timeoutCount  int
	runInvalidCount int
	lastRequestAt time.Time
	lastSuccessAt time.Time
	lastFailureAt time.Time
	perModel      map[string]*modelCallStats
}

type modelCallStats struct {
	Model    string `json:"model"`
	Requests int    `json:"requests"`
	Success  int    `json:"success"`
	Failed   int    `json:"failed"`
}

type callStatsSnapshot struct {
	TotalRequests int              `json:"total_requests"`
	SuccessCount  int              `json:"success_count"`
	FailureCount  int              `json:"failure_count"`
	RetryCount    int              `json:"retry_count"`
	Retry429Count int              `json:"retry_429_count"`
	Status429Count int             `json:"status_429_count"`
	Status401Count int             `json:"status_401_count"`
	TimeoutCount  int              `json:"timeout_count"`
	RunInvalidCount int            `json:"run_invalid_count"`
	LastRequestAt time.Time        `json:"last_request_at,omitempty"`
	LastSuccessAt time.Time        `json:"last_success_at,omitempty"`
	LastFailureAt time.Time        `json:"last_failure_at,omitempty"`
	ByModel       []modelCallStats `json:"by_model"`
}

func newCallStats() *callStats {
	return &callStats{
		perModel: make(map[string]*modelCallStats),
	}
}

func (s *callStats) Record(model string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	model = strings.TrimSpace(model)
	if model == "" {
		model = fallbackModelName
	}

	s.totalRequests++
	s.lastRequestAt = now

	entry, ok := s.perModel[model]
	if !ok {
		entry = &modelCallStats{Model: model}
		s.perModel[model] = entry
	}
	entry.Requests++

	if success {
		s.successCount++
		s.lastSuccessAt = now
		entry.Success++
		return
	}
	s.failureCount++
	s.lastFailureAt = now
	entry.Failed++
}

func (s *callStats) Snapshot() callStatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := callStatsSnapshot{
		TotalRequests: s.totalRequests,
		SuccessCount:  s.successCount,
		FailureCount:  s.failureCount,
		RetryCount:    s.retryCount,
		Retry429Count: s.retry429Count,
		Status429Count: s.status429Count,
		Status401Count: s.status401Count,
		TimeoutCount:  s.timeoutCount,
		RunInvalidCount: s.runInvalidCount,
		LastRequestAt: s.lastRequestAt,
		LastSuccessAt: s.lastSuccessAt,
		LastFailureAt: s.lastFailureAt,
		ByModel:       make([]modelCallStats, 0, len(s.perModel)),
	}
	for _, modelStats := range s.perModel {
		snapshot.ByModel = append(snapshot.ByModel, *modelStats)
	}
	// Keep most frequently used models first for dashboard readability.
	sort.Slice(snapshot.ByModel, func(i, j int) bool {
		return snapshot.ByModel[i].Requests > snapshot.ByModel[j].Requests
	})
	return snapshot
}

func (s *callStats) RecordRetry(statusCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retryCount++
	if statusCode == 429 {
		s.retry429Count++
	}
}

func (s *callStats) RecordStatusCode(statusCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch statusCode {
	case 429:
		s.status429Count++
	case 401:
		s.status401Count++
	}
}

func (s *callStats) RecordTimeout() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timeoutCount++
}

func (s *callStats) RecordRunInvalid() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runInvalidCount++
}
