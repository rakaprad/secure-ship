package store

import (
	"context"
	"sync"
)

// Memory is a concurrency-safe, in-process Store.
//
// Data lives only in the memory of a single process: it is lost on restart
// and is not shared between replicas. A capacity limit stops an attacker
// from exhausting memory by creating links in a loop.
type Memory struct {
	mu       sync.RWMutex
	links    map[string]Link
	maxLinks int
}

// NewMemory returns an empty Memory store holding at most maxLinks links.
// A maxLinks of zero or less means unlimited.
func NewMemory(maxLinks int) *Memory {
	return &Memory{links: make(map[string]Link), maxLinks: maxLinks}
}

// Create implements Store.
func (m *Memory) Create(_ context.Context, l Link) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.links[l.Code]; ok {
		return ErrConflict
	}
	if m.maxLinks > 0 && len(m.links) >= m.maxLinks {
		return ErrFull
	}
	m.links[l.Code] = l
	return nil
}

// Get implements Store.
func (m *Memory) Get(_ context.Context, code string) (Link, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}
	return l, nil
}

// Resolve implements Store.
func (m *Memory) Resolve(_ context.Context, code string) (Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}
	l.Hits++
	m.links[code] = l
	return l, nil
}
