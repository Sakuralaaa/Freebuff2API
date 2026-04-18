package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type callStats struct {
	mu sync.RWMutex

	totalRequests int
	successCount  int
	failureCount  int
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
		model = "unknown"
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
