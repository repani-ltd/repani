package store

import (
	"bytes"
	"sort"
	"sync"

	"repani.com/kv/vault"
)

// MemStore is an in-memory Store backed by a map plus a lazily
// maintained canonical-order snapshot (entries and their ids in
// parallel). It is safe for concurrent use.
type MemStore struct {
	mu        sync.Mutex
	byID      map[vault.EntryID]vault.Entry
	sorted    []vault.Entry   // nil when stale
	sortedIDs []vault.EntryID // parallel to sorted
	setHash   [16]byte
	hashSet   bool
}

// NewMemStore returns a fresh empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{byID: make(map[vault.EntryID]vault.Entry)}
}

// Add implements Store.Add.
func (m *MemStore) Add(e vault.Entry) (bool, error) {
	return m.addID(e.ID(), e), nil
}

// AddBatch implements Store.AddBatch. Memory has no per-entry
// durability cost, so this is Add in a loop under one lock.
func (m *MemStore) AddBatch(entries []vault.Entry) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	added := 0
	for _, e := range entries {
		if m.addLocked(e.ID(), e) {
			added++
		}
	}
	return added, nil
}

// addID inserts e under an already-computed id, sparing callers that
// know the id a second XOF pass over the entry.
func (m *MemStore) addID(id vault.EntryID, e vault.Entry) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked(id, e)
}

// addLocked inserts e under id. m.mu held.
func (m *MemStore) addLocked(id vault.EntryID, e vault.Entry) bool {
	if _, ok := m.byID[id]; ok {
		return false
	}
	m.byID[id] = e
	m.sorted = nil
	m.sortedIDs = nil
	m.hashSet = false
	return true
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

// Sorted implements Store.Sorted, returning a defensive copy.
func (m *MemStore) Sorted() []vault.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureSortedLocked()
	out := make([]vault.Entry, len(m.sorted))
	copy(out, m.sorted)
	return out
}

// ensureSortedLocked (re)builds the canonical snapshot and id index.
func (m *MemStore) ensureSortedLocked() {
	if m.sorted != nil {
		return
	}
	out := make([]vault.Entry, 0, len(m.byID))
	for _, e := range m.byID {
		out = append(out, e)
	}
	vault.SortCanonical(out)
	ids := make([]vault.EntryID, len(out))
	for i := range out {
		ids[i] = out[i].ID()
	}
	m.sorted = out
	m.sortedIDs = ids
}

// SetHash implements Store.SetHash with caching.
func (m *MemStore) SetHash() [16]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hashSet {
		return m.setHash
	}
	m.ensureSortedLocked()
	// Reuse the canonical id index rather than re-hashing every entry.
	m.setHash = vault.SetHashIDs(m.sortedIDs)
	m.hashSet = true
	return m.setHash
}

// EntriesAfter implements Store.EntriesAfter: whole entries in
// canonical order strictly after the cursor (afterTs, afterID),
// accumulating until the next would exceed budget bytes — but always at
// least one, since every entry is smaller than the page budget.
func (m *MemStore) EntriesAfter(afterTs uint64, afterID vault.EntryID, budget int) []vault.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureSortedLocked()

	// First index strictly after the cursor (canonical order).
	start := sort.Search(len(m.sorted), func(i int) bool {
		if m.sorted[i].TsMs != afterTs {
			return m.sorted[i].TsMs > afterTs
		}
		return bytes.Compare(m.sortedIDs[i][:], afterID[:]) > 0
	})

	var out []vault.Entry
	used := 0
	for i := start; i < len(m.sorted); i++ {
		sz := m.sorted[i].Size()
		if len(out) > 0 && used+sz > budget {
			break
		}
		out = append(out, m.sorted[i])
		used += sz
	}
	return out
}
