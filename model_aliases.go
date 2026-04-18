package main

import (
	"sort"
	"sync"
)

type modelAliasStore struct {
	mu      sync.RWMutex
	aliases map[string]string
}

func newModelAliasStore(initial map[string]string) *modelAliasStore {
	return &modelAliasStore{aliases: normalizeModelAliases(initial)}
}

func (s *modelAliasStore) Resolve(model string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if resolved, ok := s.aliases[model]; ok {
		return resolved
	}
	return model
}

func (s *modelAliasStore) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.aliases))
	for alias, model := range s.aliases {
		out[alias] = model
	}
	return out
}

func (s *modelAliasStore) Replace(aliases map[string]string) {
	s.mu.Lock()
	s.aliases = normalizeModelAliases(aliases)
	s.mu.Unlock()
}

func (s *modelAliasStore) SortedPairs() []map[string]string {
	aliases := s.Snapshot()
	keys := make([]string, 0, len(aliases))
	for alias := range aliases {
		keys = append(keys, alias)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, alias := range keys {
		out = append(out, map[string]string{
			"alias": alias,
			"model": aliases[alias],
		})
	}
	return out
}
