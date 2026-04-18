package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RunManager struct {
	cfg    Config
	logger *log.Logger
	client *UpstreamClient
	mu     sync.RWMutex
	pools  []*tokenPool
	next   atomic.Uint64
	policy runtimePolicySnapshot

	stopCh chan struct{}
	wg     sync.WaitGroup
	live   atomic.Bool
}

type tokenPool struct {
	name   string
	token  string
	cfg    Config
	client *UpstreamClient
	logger *log.Logger

	mu            sync.Mutex
	runs          map[string]*managedRun // agentID → current run
	draining      []*managedRun
	enabled       bool
	lastError     string
	cooldownUntil time.Time
	totalRequests int
	successCount  int
	failureCount  int
	lastUsedAt    time.Time
	maxConcurrent int
	healthy       bool
	lastHealthAt  time.Time
	lastHealthyAt time.Time
	healthStreak  int
	inflightTotal int
}

type managedRun struct {
	id           string
	agentID      string
	startedAt    time.Time
	inflight     int
	requestCount int
	finishing    bool
}

type runLease struct {
	pool *tokenPool
	run  *managedRun
}

type tokenSnapshot struct {
	Name          string        `json:"name"`
	Enabled       bool          `json:"enabled"`
	Runs          []runSnapshot `json:"runs"`
	DrainingRuns  int           `json:"draining_runs"`
	CooldownUntil time.Time     `json:"cooldown_until,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	TotalRequests int           `json:"total_requests"`
	SuccessCount  int           `json:"success_count"`
	FailureCount  int           `json:"failure_count"`
	LastUsedAt    time.Time     `json:"last_used_at,omitempty"`
	Inflight      int           `json:"inflight"`
	MaxConcurrent int           `json:"max_concurrent"`
	Healthy       bool          `json:"healthy"`
	LastHealthAt  time.Time     `json:"last_health_at,omitempty"`
	LastHealthyAt time.Time     `json:"last_healthy_at,omitempty"`
	HealthStreak  int           `json:"health_failure_streak"`
}

type runSnapshot struct {
	AgentID      string    `json:"agent_id"`
	RunID        string    `json:"run_id"`
	StartedAt    time.Time `json:"started_at"`
	Inflight     int       `json:"inflight"`
	RequestCount int       `json:"request_count"`
}

func NewRunManager(cfg Config, client *UpstreamClient, logger *log.Logger) *RunManager {
	initialPolicy := defaultRuntimePolicy(cfg)
	pools := make([]*tokenPool, 0, len(cfg.AuthTokens))
	for index, token := range cfg.AuthTokens {
		pools = append(pools, &tokenPool{
			name:          fmt.Sprintf("token-%d", index+1),
			token:         token,
			cfg:           cfg,
			client:        client,
			runs:          make(map[string]*managedRun),
			logger:        logger,
			maxConcurrent: initialPolicy.PerTokenConcurrency,
			enabled:       true,
			healthy:       true,
		})
	}

	return &RunManager{
		cfg:    cfg,
		logger: logger,
		client: client,
		pools:  pools,
		policy: initialPolicy,
		stopCh: make(chan struct{}),
	}
}

func (m *RunManager) Start(ctx context.Context) {
	m.live.Store(true)
	// Pre-warm runs for all tracked agents in background.
	// The server is already listening; if a request arrives before
	// pre-warming finishes, acquire() will lazily create the run.
	go m.prewarm()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				policy := m.PolicySnapshot()
				maintainCtx, cancel := context.WithTimeout(context.Background(), m.cfg.RequestTimeout)
				for _, pool := range m.currentPools() {
					if err := pool.maintain(maintainCtx); err != nil {
						m.logger.Printf("%s: maintenance failed: %v", pool.name, err)
					}
					if err := pool.healthCheck(maintainCtx, policy); err != nil {
						m.logger.Printf("%s: health check failed: %v", pool.name, err)
					}
				}
				cancel()
			case <-m.stopCh:
				return
			}
		}
	}()
}

func (m *RunManager) prewarm() {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.RequestTimeout)
	defer cancel()

	for _, pool := range m.currentPools() {
		for _, agentID := range trackedAgents {
			if err := pool.rotateAgent(ctx, agentID); err != nil {
				m.logger.Printf("%s: prewarm %s failed: %v", pool.name, agentID, err)
			} else {
				m.logger.Printf("%s: prewarmed %s", pool.name, agentID)
			}
		}
	}
}

func (m *RunManager) Close(ctx context.Context) {
	m.live.Store(false)
	close(m.stopCh)
	m.wg.Wait()
	for _, pool := range m.currentPools() {
		if err := pool.shutdown(ctx); err != nil {
			m.logger.Printf("%s: shutdown failed: %v", pool.name, err)
		}
	}
}

func (m *RunManager) Acquire(ctx context.Context, agentID string) (*runLease, error) {
	pools := m.currentPools()
	if len(pools) == 0 {
		return nil, errors.New("no auth tokens configured")
	}

	startIndex := int(m.next.Add(1)-1) % len(pools)
	var errs []string
	for offset := 0; offset < len(pools); offset++ {
		pool := pools[(startIndex+offset)%len(pools)]
		lease, err := pool.acquire(ctx, agentID)
		if err == nil {
			return lease, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", pool.name, err))
	}

	return nil, fmt.Errorf("unable to acquire run from any token (%s)", strings.Join(errs, "; "))
}

func (m *RunManager) Release(lease *runLease) {
	if lease == nil || lease.pool == nil || lease.run == nil {
		return
	}
	lease.pool.release(lease.run)
}

func (m *RunManager) Invalidate(lease *runLease, reason string) {
	if lease == nil || lease.pool == nil || lease.run == nil {
		return
	}
	lease.pool.invalidate(lease.run, reason)
}

func (m *RunManager) Cooldown(lease *runLease, duration time.Duration, reason string) {
	if lease == nil || lease.pool == nil {
		return
	}
	lease.pool.markCooldown(duration, reason)
}

func (m *RunManager) RecordResult(lease *runLease, success bool) {
	if lease == nil || lease.pool == nil {
		return
	}
	lease.pool.recordResult(success)
}

func (m *RunManager) Snapshots() []tokenSnapshot {
	pools := m.currentPools()
	snapshots := make([]tokenSnapshot, 0, len(pools))
	for _, pool := range pools {
		snapshots = append(snapshots, pool.snapshot())
	}
	return snapshots
}

func (m *RunManager) Tokens() []string {
	pools := m.currentPools()
	tokens := make([]string, 0, len(pools))
	for _, pool := range pools {
		token := strings.TrimSpace(pool.token)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// AddToken registers an auth token into the runtime pool and returns the pool
// name, whether it was newly added, and any error. This method is safe for
// concurrent use.
func (m *RunManager) AddToken(token string) (name string, added bool, err error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false, errors.New("auth token cannot be empty")
	}

	m.mu.Lock()
	for _, pool := range m.pools {
		if pool.token == token {
			name = pool.name
			m.mu.Unlock()
			return name, false, nil
		}
	}

	pool := &tokenPool{
		name:          fmt.Sprintf("token-%d", len(m.pools)+1),
		token:         token,
		cfg:           m.cfg,
		client:        m.client,
		runs:          make(map[string]*managedRun),
		logger:        m.logger,
		maxConcurrent: m.policy.PerTokenConcurrency,
		enabled:       true,
		healthy:       true,
	}
	m.pools = append(m.pools, pool)
	m.mu.Unlock()

	if m.live.Load() {
		pool.setMaxConcurrent(m.PolicySnapshot().PerTokenConcurrency)
		go m.prewarmPool(pool)
	}
	return pool.name, true, nil
}

func (m *RunManager) prewarmPool(pool *tokenPool) {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.RequestTimeout)
	defer cancel()
	for _, agentID := range trackedAgents {
		if err := pool.rotateAgent(ctx, agentID); err != nil {
			m.logger.Printf("%s: prewarm %s failed: %v", pool.name, agentID, err)
		} else {
			m.logger.Printf("%s: prewarmed %s", pool.name, agentID)
		}
	}
}

func (m *RunManager) currentPools() []*tokenPool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]*tokenPool(nil), m.pools...)
}

func (p *tokenPool) acquire(ctx context.Context, agentID string) (*runLease, error) {
	p.mu.Lock()
	if !p.enabled {
		p.mu.Unlock()
		return nil, errors.New("token is disabled")
	}
	if now := time.Now(); now.Before(p.cooldownUntil) {
		cooldownUntil := p.cooldownUntil
		p.mu.Unlock()
		return nil, fmt.Errorf("token cooling down until %s", cooldownUntil.Format(time.RFC3339))
	}
	run := p.runs[agentID]
	if p.maxConcurrent > 0 && p.inflightTotal >= p.maxConcurrent {
		maxConcurrent := p.maxConcurrent
		p.mu.Unlock()
		return nil, fmt.Errorf("token inflight limit reached (%d)", maxConcurrent)
	}
	needsRotate := run == nil || time.Since(run.startedAt) >= p.cfg.RotationInterval
	p.mu.Unlock()

	if needsRotate {
		if err := p.rotateAgent(ctx, agentID); err != nil {
			return nil, err
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	run = p.runs[agentID]
	if run == nil {
		return nil, errors.New("run missing after rotation")
	}
	run.inflight++
	p.inflightTotal++
	run.requestCount++
	p.totalRequests++
	p.lastUsedAt = time.Now()
	return &runLease{pool: p, run: run}, nil
}

func (p *tokenPool) maintain(ctx context.Context) error {
	p.mu.Lock()
	enabled := p.enabled
	var toRotate []string
	if enabled {
		for agentID, run := range p.runs {
			if time.Since(run.startedAt) >= p.cfg.RotationInterval {
				toRotate = append(toRotate, agentID)
			}
		}
	}
	draining := append([]*managedRun(nil), p.draining...)
	p.mu.Unlock()

	for _, agentID := range toRotate {
		if err := p.rotateAgent(ctx, agentID); err != nil {
			p.logger.Printf("%s: rotate agent %s failed: %v", p.name, agentID, err)
		}
	}

	for _, run := range draining {
		if err := p.finishIfReady(run); err != nil {
			p.logger.Printf("%s: finish draining run %s failed: %v", p.name, run.id, err)
		}
	}
	return nil
}

func (p *tokenPool) shutdown(ctx context.Context) error {
	p.mu.Lock()
	var allRuns []*managedRun
	for _, run := range p.runs {
		allRuns = append(allRuns, run)
	}
	allRuns = append(allRuns, p.draining...)
	p.runs = make(map[string]*managedRun)
	p.draining = nil
	p.mu.Unlock()

	var errs []string
	for _, run := range allRuns {
		if err := p.client.FinishRun(ctx, p.token, run.id, run.requestCount); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (p *tokenPool) rotateAgent(ctx context.Context, agentID string) error {
	p.mu.Lock()
	if !p.enabled {
		p.mu.Unlock()
		return errors.New("token is disabled")
	}
	if now := time.Now(); now.Before(p.cooldownUntil) {
		cooldownUntil := p.cooldownUntil
		p.mu.Unlock()
		return fmt.Errorf("token cooling down until %s", cooldownUntil.Format(time.RFC3339))
	}
	p.mu.Unlock()

	runID, err := p.client.StartRun(ctx, p.token, agentID)
	if err != nil {
		p.mu.Lock()
		p.lastError = err.Error()
		p.mu.Unlock()
		return err
	}

	p.mu.Lock()
	oldRun := p.runs[agentID]
	p.runs[agentID] = &managedRun{
		id:        runID,
		agentID:   agentID,
		startedAt: time.Now(),
	}
	p.lastError = ""
	if oldRun != nil {
		p.draining = append(p.draining, oldRun)
	}
	p.mu.Unlock()

	if oldRun != nil {
		go func(run *managedRun) {
			if err := p.finishIfReady(run); err != nil {
				p.logger.Printf("%s: finish rotated run %s (agent %s) failed: %v", p.name, run.id, run.agentID, err)
			}
		}(oldRun)
	}
	return nil
}

func (p *tokenPool) release(run *managedRun) {
	if run == nil {
		return
	}

	p.mu.Lock()
	if run.inflight > 0 {
		run.inflight--
	}
	if p.inflightTotal > 0 {
		p.inflightTotal--
	}
	p.mu.Unlock()

	if err := p.finishIfReady(run); err != nil {
		p.logger.Printf("%s: finish released run %s failed: %v", p.name, run.id, err)
	}
}

func (p *tokenPool) finishIfReady(run *managedRun) error {
	p.mu.Lock()
	if run == nil || run.inflight > 0 || run.finishing {
		p.mu.Unlock()
		return nil
	}
	// Only finish if this run is no longer the current run for its agent
	if current, ok := p.runs[run.agentID]; ok && current == run {
		p.mu.Unlock()
		return nil
	}
	run.finishing = true
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.RequestTimeout)
	defer cancel()

	if err := p.client.FinishRun(ctx, p.token, run.id, run.requestCount); err != nil {
		p.mu.Lock()
		run.finishing = false
		p.lastError = err.Error()
		p.mu.Unlock()
		return err
	}

	p.mu.Lock()
	filtered := p.draining[:0]
	for _, drainingRun := range p.draining {
		if drainingRun != run {
			filtered = append(filtered, drainingRun)
		}
	}
	p.draining = filtered
	p.mu.Unlock()
	return nil
}

func (p *tokenPool) invalidate(run *managedRun, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from current runs if it matches
	if current, ok := p.runs[run.agentID]; ok && current == run {
		delete(p.runs, run.agentID)
	}

	filtered := p.draining[:0]
	for _, drainingRun := range p.draining {
		if drainingRun != run {
			filtered = append(filtered, drainingRun)
		}
	}
	p.draining = filtered
	if reason != "" {
		p.lastError = reason
	}
}

func (p *tokenPool) markCooldown(duration time.Duration, reason string) {
	if duration <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cooldownUntil = time.Now().Add(duration)
	if reason != "" {
		p.lastError = reason
	}
}

func (p *tokenPool) setMaxConcurrent(limit int) {
	p.mu.Lock()
	p.maxConcurrent = limit
	p.mu.Unlock()
}

func (p *tokenPool) setEnabled(enabled bool) {
	p.mu.Lock()
	p.enabled = enabled
	if !enabled {
		p.healthy = false
		p.lastError = "disabled by admin"
	} else if p.lastError == "disabled by admin" {
		p.lastError = ""
	}
	p.mu.Unlock()
}

func (p *tokenPool) recordResult(success bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if success {
		p.successCount++
		return
	}
	p.failureCount++
}

func (p *tokenPool) snapshot() tokenSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	snapshot := tokenSnapshot{
		Name:          p.name,
		Enabled:       p.enabled,
		DrainingRuns:  len(p.draining),
		CooldownUntil: p.cooldownUntil,
		LastError:     p.lastError,
		TotalRequests: p.totalRequests,
		SuccessCount:  p.successCount,
		FailureCount:  p.failureCount,
		LastUsedAt:    p.lastUsedAt,
		Inflight:      p.inflightTotal,
		MaxConcurrent: p.maxConcurrent,
		Healthy:       p.healthy,
		LastHealthAt:  p.lastHealthAt,
		LastHealthyAt: p.lastHealthyAt,
		HealthStreak:  p.healthStreak,
	}
	for agentID, run := range p.runs {
		snapshot.Runs = append(snapshot.Runs, runSnapshot{
			AgentID:      agentID,
			RunID:        run.id,
			StartedAt:    run.startedAt,
			Inflight:     run.inflight,
			RequestCount: run.requestCount,
		})
	}
	return snapshot
}

func (m *RunManager) PolicySnapshot() runtimePolicySnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.policy
}

func (m *RunManager) UpdatePolicy(policy runtimePolicySnapshot) {
	m.mu.Lock()
	m.policy = policy
	pools := append([]*tokenPool(nil), m.pools...)
	m.mu.Unlock()
	for _, pool := range pools {
		pool.setMaxConcurrent(policy.PerTokenConcurrency)
	}
}

func (p *tokenPool) healthCheck(ctx context.Context, policy runtimePolicySnapshot) error {
	if !policy.HealthCheckEnabled {
		return nil
	}
	p.mu.Lock()
	if !p.enabled {
		p.mu.Unlock()
		return nil
	}
	if time.Since(p.lastHealthAt) < policy.HealthCheckInterval {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	runID, err := p.client.StartRun(ctx, p.token, trackedAgents[0])
	now := time.Now()
	p.mu.Lock()
	p.lastHealthAt = now
	if err != nil {
		p.healthStreak++
		p.healthy = p.healthStreak < policy.HealthFailureThreshold
		p.lastError = err.Error()
		p.mu.Unlock()
		return err
	}
	p.healthy = true
	p.healthStreak = 0
	p.lastHealthyAt = now
	p.mu.Unlock()
	if finishErr := p.client.FinishRun(ctx, p.token, runID, 0); finishErr != nil {
		p.mu.Lock()
		p.lastError = finishErr.Error()
		p.mu.Unlock()
		return finishErr
	}
	return nil
}

func (m *RunManager) SetTokenEnabled(name string, enabled bool) (tokenSnapshot, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return tokenSnapshot{}, errors.New("token name is required")
	}
	m.mu.RLock()
	var pool *tokenPool
	for _, candidate := range m.pools {
		if candidate.name == name {
			pool = candidate
			break
		}
	}
	m.mu.RUnlock()
	if pool == nil {
		return tokenSnapshot{}, fmt.Errorf("token %q not found", name)
	}
	pool.setEnabled(enabled)
	return pool.snapshot(), nil
}

func (m *RunManager) RemoveToken(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("token name is required")
	}
	m.mu.Lock()
	index := -1
	var pool *tokenPool
	for i, candidate := range m.pools {
		if candidate.name == name {
			index = i
			pool = candidate
			break
		}
	}
	if index == -1 {
		m.mu.Unlock()
		return fmt.Errorf("token %q not found", name)
	}
	m.pools = append(m.pools[:index], m.pools[index+1:]...)
	m.mu.Unlock()
	if pool != nil {
		pool.setEnabled(false)
	}
	return nil
}
