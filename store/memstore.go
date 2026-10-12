package store

import (
	"bytes"
	"cmp"
	"slices"
	"sync"

	"repani.com/kv/vault"
)

// MemStore is an in-memory Store. It keeps the entries in the order
// they were added, each with its id, and a canonical-order index that
// every insert updates in place. An entry is hashed once, when it is
// added: no read re-sorts or re-hashes the set. It is safe for
// concurrent use.
type MemStore struct {
	mu      sync.Mutex
	entries []vault.Entry   // in the order added
	ids     []vault.EntryID // ids[i] is entries[i]'s id
	has     map[vault.EntryID]bool
	order   []int // indexes into entries, in canonical order
	setHash [16]byte
	hashSet bool
}

// NewMemStore returns a fresh empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{has: make(map[vault.EntryID]bool)}
}

// Add implements Store.Add, under one lock.
func (m *MemStore) Add(entries ...vault.Entry) (int, error) {
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

// addLocked inserts e under id, placing it in canonical order: at the
// end, as most entries arrive, or by binary search. m.mu held.
func (m *MemStore) addLocked(id vault.EntryID, e vault.Entry) bool {
	if m.has[id] {
		return false
	}
	i := len(m.entries)
	m.entries = append(m.entries, e)
	m.ids = append(m.ids, id)
	m.has[id] = true
	if n := len(m.order); n == 0 || m.compare(m.order[n-1], e.TsMs, id) < 0 {
		m.order = append(m.order, i)
	} else {
		at, _ := slices.BinarySearchFunc(m.order, i, func(j, _ int) int { return m.compare(j, e.TsMs, id) })
		m.order = slices.Insert(m.order, at, i)
	}
	m.hashSet = false
	return true
}

// compare orders entry j against the position (ts, id) canonically.
func (m *MemStore) compare(j int, ts uint64, id vault.EntryID) int {
	if c := cmp.Compare(m.entries[j].TsMs, ts); c != 0 {
		return c
	}
	return bytes.Compare(m.ids[j][:], id[:])
}

// Has implements Store.Has.
func (m *MemStore) Has(id vault.EntryID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.has[id]
}

// Len implements Store.Len.
func (m *MemStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// Sorted implements Store.Sorted.
func (m *MemStore) Sorted() []vault.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]vault.Entry, len(m.order))
	for k, i := range m.order {
		out[k] = m.entries[i]
	}
	return out
}

// SetHash implements Store.SetHash, cached until the next add.
func (m *MemStore) SetHash() [16]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hashSet {
		ids := make([]vault.EntryID, len(m.order))
		for k, i := range m.order {
			ids[k] = m.ids[i]
		}
		m.setHash = vault.SetHashIDs(ids)
		m.hashSet = true
	}
	return m.setHash
}

// EntriesAfter implements Store.EntriesAfter: whole entries in
// canonical order strictly after the cursor (afterTs, afterID),
// accumulating until the next would exceed budget bytes — but always at
// least one, since every entry is smaller than the page budget.
func (m *MemStore) EntriesAfter(afterTs uint64, afterID vault.EntryID, budget int) []vault.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	start, found := slices.BinarySearchFunc(m.order, 0, func(j, _ int) int { return m.compare(j, afterTs, afterID) })
	if found {
		start++
	}
	var out []vault.Entry
	used := 0
	for _, i := range m.order[start:] {
		sz := m.entries[i].Size()
		if len(out) > 0 && used+sz > budget {
			break
		}
		out = append(out, m.entries[i])
		used += sz
	}
	return out
}

// Appended implements Store.Appended. Entries are only ever appended,
// so the returned slice, capped at its length, stays valid.
func (m *MemStore) Appended(n int) []vault.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	n = min(n, len(m.entries))
	return m.entries[n:len(m.entries):len(m.entries)]
}

// Newest implements Store.Newest.
func (m *MemStore) Newest() (vault.Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) == 0 {
		return vault.Entry{}, false
	}
	return m.entries[m.order[len(m.order)-1]], true
}
