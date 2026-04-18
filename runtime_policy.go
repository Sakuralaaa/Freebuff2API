package main

import (
	"errors"
	"sync"
	"time"
)

type runtimePolicySnapshot struct {
	MaxRetries             int           `json:"max_retries"`
	RetryBackoffBase       time.Duration `json:"retry_backoff_base"`
	RetryBackoffMax        time.Duration `json:"retry_backoff_max"`
	PerTokenConcurrency    int           `json:"per_token_concurrency"`
	HealthCheckEnabled     bool          `json:"health_check_enabled"`
	HealthCheckInterval    time.Duration `json:"health_check_interval"`
	HealthFailureThreshold int           `json:"health_failure_threshold"`
	NonStreamTimeout       time.Duration `json:"non_stream_timeout"`
	StreamTimeout          time.Duration `json:"stream_timeout"`
}

func defaultRuntimePolicy(cfg Config) runtimePolicySnapshot {
	policy := runtimePolicySnapshot{
		MaxRetries:             cfg.Policy.MaxRetries,
		RetryBackoffBase:       cfg.Policy.RetryBackoffBase,
		RetryBackoffMax:        cfg.Policy.RetryBackoffMax,
		PerTokenConcurrency:    cfg.Policy.PerTokenConcurrency,
		HealthCheckEnabled:     cfg.Policy.HealthCheckEnabled,
		HealthCheckInterval:    cfg.Policy.HealthCheckInterval,
		HealthFailureThreshold: cfg.Policy.HealthFailureThreshold,
		NonStreamTimeout:       cfg.RequestTimeout,
		StreamTimeout:          cfg.StreamTimeout,
	}
	if policy.RetryBackoffBase <= 0 {
		policy.RetryBackoffBase = 500 * time.Millisecond
	}
	if policy.RetryBackoffMax < policy.RetryBackoffBase {
		policy.RetryBackoffMax = 6 * time.Second
	}
	if policy.PerTokenConcurrency <= 0 {
		policy.PerTokenConcurrency = 8
	}
	if policy.HealthCheckInterval <= 0 {
		policy.HealthCheckInterval = 3 * time.Minute
	}
	if policy.HealthFailureThreshold <= 0 {
		policy.HealthFailureThreshold = 3
	}
	if policy.NonStreamTimeout <= 0 {
		policy.NonStreamTimeout = 15 * time.Minute
	}
	if policy.StreamTimeout <= 0 {
		policy.StreamTimeout = policy.NonStreamTimeout
	}
	return policy
}

func (p runtimePolicySnapshot) validate() error {
	switch {
	case p.MaxRetries < 0:
		return errors.New("max_retries cannot be negative")
	case p.RetryBackoffBase <= 0:
		return errors.New("retry_backoff_base must be greater than zero")
	case p.RetryBackoffMax < p.RetryBackoffBase:
		return errors.New("retry_backoff_max must be greater than or equal to retry_backoff_base")
	case p.PerTokenConcurrency <= 0:
		return errors.New("per_token_concurrency must be greater than zero")
	case p.HealthCheckInterval <= 0:
		return errors.New("health_check_interval must be greater than zero")
	case p.HealthFailureThreshold <= 0:
		return errors.New("health_failure_threshold must be greater than zero")
	case p.NonStreamTimeout <= 0:
		return errors.New("non_stream_timeout must be greater than zero")
	case p.StreamTimeout <= 0:
		return errors.New("stream_timeout must be greater than zero")
	}
	return nil
}

type runtimePolicyStore struct {
	mu     sync.RWMutex
	policy runtimePolicySnapshot
}

func newRuntimePolicyStore(initial runtimePolicySnapshot) *runtimePolicyStore {
	return &runtimePolicyStore{policy: initial}
}

func (s *runtimePolicyStore) Snapshot() runtimePolicySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy
}

func (s *runtimePolicyStore) Update(next runtimePolicySnapshot) error {
	if err := next.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.policy = next
	s.mu.Unlock()
	return nil
}

type policyPayload struct {
	MaxRetries             int    `json:"max_retries"`
	RetryBackoffBase       string `json:"retry_backoff_base"`
	RetryBackoffMax        string `json:"retry_backoff_max"`
	PerTokenConcurrency    int    `json:"per_token_concurrency"`
	HealthCheckEnabled     bool   `json:"health_check_enabled"`
	HealthCheckInterval    string `json:"health_check_interval"`
	HealthFailureThreshold int    `json:"health_failure_threshold"`
	NonStreamTimeout       string `json:"non_stream_timeout"`
	StreamTimeout          string `json:"stream_timeout"`
}

func policyPayloadFromSnapshot(policy runtimePolicySnapshot) policyPayload {
	return policyPayload{
		MaxRetries:             policy.MaxRetries,
		RetryBackoffBase:       policy.RetryBackoffBase.String(),
		RetryBackoffMax:        policy.RetryBackoffMax.String(),
		PerTokenConcurrency:    policy.PerTokenConcurrency,
		HealthCheckEnabled:     policy.HealthCheckEnabled,
		HealthCheckInterval:    policy.HealthCheckInterval.String(),
		HealthFailureThreshold: policy.HealthFailureThreshold,
		NonStreamTimeout:       policy.NonStreamTimeout.String(),
		StreamTimeout:          policy.StreamTimeout.String(),
	}
}

func (p policyPayload) toSnapshot() (runtimePolicySnapshot, error) {
	retryBase, err := time.ParseDuration(p.RetryBackoffBase)
	if err != nil {
		return runtimePolicySnapshot{}, errors.New("retry_backoff_base must be a valid duration")
	}
	retryMax, err := time.ParseDuration(p.RetryBackoffMax)
	if err != nil {
		return runtimePolicySnapshot{}, errors.New("retry_backoff_max must be a valid duration")
	}
	healthInterval, err := time.ParseDuration(p.HealthCheckInterval)
	if err != nil {
		return runtimePolicySnapshot{}, errors.New("health_check_interval must be a valid duration")
	}
	nonStreamTimeout, err := time.ParseDuration(p.NonStreamTimeout)
	if err != nil {
		return runtimePolicySnapshot{}, errors.New("non_stream_timeout must be a valid duration")
	}
	streamTimeout, err := time.ParseDuration(p.StreamTimeout)
	if err != nil {
		return runtimePolicySnapshot{}, errors.New("stream_timeout must be a valid duration")
	}
	snapshot := runtimePolicySnapshot{
		MaxRetries:             p.MaxRetries,
		RetryBackoffBase:       retryBase,
		RetryBackoffMax:        retryMax,
		PerTokenConcurrency:    p.PerTokenConcurrency,
		HealthCheckEnabled:     p.HealthCheckEnabled,
		HealthCheckInterval:    healthInterval,
		HealthFailureThreshold: p.HealthFailureThreshold,
		NonStreamTimeout:       nonStreamTimeout,
		StreamTimeout:          streamTimeout,
	}
	return snapshot, snapshot.validate()
}
