package main

import (
	"context"
	"io"
	"log"
	"testing"
	"time"
)

func newPolicyTestPool(name string, cfg Config) *tokenPool {
	return &tokenPool{
		name:  name,
		token: name + "-token",
		cfg:   cfg,
		runs: map[string]*managedRun{
			"agent": &managedRun{id: name + "-run", agentID: "agent", startedAt: time.Now()},
		},
		logger:        log.New(io.Discard, "", 0),
		enabled:       true,
		healthy:       true,
		maxConcurrent: 100,
	}
}

func TestRunManagerAcquireRoundRobin(t *testing.T) {
	cfg := Config{RotationInterval: 24 * time.Hour, RequestTimeout: time.Minute}
	pool1 := newPolicyTestPool("token-1", cfg)
	pool2 := newPolicyTestPool("token-2", cfg)
	manager := &RunManager{
		cfg:    cfg,
		logger: log.New(io.Discard, "", 0),
		pools:  []*tokenPool{pool1, pool2},
		policy: runtimePolicySnapshot{RoutingMode: routingModeRoundRobin, PriorityFailoverStep: 3},
	}

	lease1, err := manager.Acquire(context.Background(), "agent")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	manager.Release(lease1)
	if lease1.pool != pool1 {
		t.Fatalf("expected first lease from pool1, got %s", lease1.pool.name)
	}

	lease2, err := manager.Acquire(context.Background(), "agent")
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	manager.Release(lease2)
	if lease2.pool != pool2 {
		t.Fatalf("expected second lease from pool2, got %s", lease2.pool.name)
	}
}

func TestRunManagerPriorityFillSwitchAfterThreeFailures(t *testing.T) {
	cfg := Config{RotationInterval: 24 * time.Hour, RequestTimeout: time.Minute}
	pool1 := newPolicyTestPool("token-1", cfg)
	pool2 := newPolicyTestPool("token-2", cfg)
	manager := &RunManager{
		cfg:    cfg,
		logger: log.New(io.Discard, "", 0),
		pools:  []*tokenPool{pool1, pool2},
		policy: runtimePolicySnapshot{RoutingMode: routingModePriorityFill, PriorityFailoverStep: 3},
	}

	for i := 0; i < 3; i++ {
		lease, err := manager.Acquire(context.Background(), "agent")
		if err != nil {
			t.Fatalf("acquire %d failed: %v", i+1, err)
		}
		if lease.pool != pool1 {
			t.Fatalf("expected lease %d from pool1 before failover, got %s", i+1, lease.pool.name)
		}
		manager.RecordResult(lease, false)
		manager.Release(lease)
	}

	lease, err := manager.Acquire(context.Background(), "agent")
	if err != nil {
		t.Fatalf("acquire after failover failed: %v", err)
	}
	manager.Release(lease)
	if lease.pool != pool2 {
		t.Fatalf("expected failover to pool2 after three failures, got %s", lease.pool.name)
	}
}
