package store

import (
	"sync"

	"github.com/pavlos/kv/vault"
)

// MemStore is an in-memory Store backed by a map plus a lazily
// maintained sorted snapshot. It is safe for concurrent use.
type MemStore struct {
	mu       sync.Mutex
	byID     map[vault.EntryID]vault.Entry
	sorted   []vault.Entry // nil when stale
	ladder   map[uint32][16]byte
	terminal [16]byte
	termSet  bool
	dirty    bool
}

// NewMemStore returns a fresh empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		byID:   make(map[vault.EntryID]vault.Entry),
		ladder: make(map[uint32][16]byte),
	}
}

// Add implements Store.Add.
func (m *MemStore) Add(e vault.Entry) (bool, error) {
	id := e.ID()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[id]; ok {
		return false, nil
	}
	m.byID[id] = e
	m.dirty = true
	m.sorted = nil
	m.ladder = make(map[uint32][16]byte)
	m.termSet = false
	return true, nil
}

// Has implements Store.Has.
func (m *MemStore) Has(id vault.EntryID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.byID[id]
	return ok
}

// Len implements Store.Len.
func (m *MemStore) Len() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return uint32(len(m.byID))
}

// Sorted implements Store.Sorted. The returned slice is a copy; safe
// to use after the lock is released.
func (m *MemStore) Sorted() []vault.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sortedLocked()
}

// sortedLocked returns (and caches) the canonical-order snapshot.
func (m *MemStore) sortedLocked() []vault.Entry {
	if m.sorted != nil {
		out := make([]vault.Entry, len(m.sorted))
		copy(out, m.sorted)
		return out
	}
	out := make([]vault.Entry, 0, len(m.byID))
	for _, e := range m.byID {
		out = append(out, e)
	}
	vault.SortCanonical(out)
	m.sorted = out
	// Return a defensive copy so callers can't mutate our cache.
	cp := make([]vault.Entry, len(out))
	copy(cp, out)
	return cp
}

// LadderHashAt implements Store.LadderHashAt.
func (m *MemStore) LadderHashAt(k uint32) [16]byte {
	if k == 0 {
		panic("store: LadderHashAt requires k > 0")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.ladder[k]; ok {
		return h
	}
	sorted := m.sortedLocked()
	h := vault.LadderHashAt(sorted, k)
	m.ladder[k] = h
	return h
}

// TerminalHash implements Store.TerminalHash with caching.
func (m *MemStore) TerminalHash() [16]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.termSet {
		return m.terminal
	}
	sorted := m.sortedLocked()
	m.terminal = vault.TerminalHash(sorted)
	m.termSet = true
	return m.terminal
}

// NewMemStoreFromEntries is a test helper that builds a MemStore from
// a slice of entries.
func NewMemStoreFromEntries(entries []vault.Entry) *MemStore {
	m := NewMemStore()
	for _, e := range entries {
		_, _ = m.Add(e)
	}
	return m
}
