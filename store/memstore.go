package store

import (
	"bytes"
	"sort"
	"sync"

	"github.com/pavlos/kv/vault"
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
	id := e.ID()
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[id]; ok {
		return false, nil
	}
	m.byID[id] = e
	m.sorted = nil
	m.sortedIDs = nil
	m.hashSet = false
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
