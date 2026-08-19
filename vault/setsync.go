package vault

import (
	"bytes"
	"encoding/binary"
	"sort"

	"repani.com/prim/ascon"
)

// SortCanonical sorts entries in place by (TsMs asc, EntryID asc),
// precomputing IDs once so comparisons are O(1). This order drives the
// last-writer-wins merge and the paged sync cursor.
func SortCanonical(entries []Entry) {
	if len(entries) < 2 {
		return
	}
	ids := make([]EntryID, len(entries))
	for i := range entries {
		ids[i] = entries[i].ID()
	}
	sort.Sort(&canonicalSort{entries: entries, ids: ids})
}

type canonicalSort struct {
	entries []Entry
	ids     []EntryID
}

func (s *canonicalSort) Len() int { return len(s.entries) }

func (s *canonicalSort) Less(i, j int) bool {
	if s.entries[i].TsMs != s.entries[j].TsMs {
		return s.entries[i].TsMs < s.entries[j].TsMs
	}
	return bytes.Compare(s.ids[i][:], s.ids[j][:]) < 0
}

func (s *canonicalSort) Swap(i, j int) {
	s.entries[i], s.entries[j] = s.entries[j], s.entries[i]
	s.ids[i], s.ids[j] = s.ids[j], s.ids[i]
}

// SetHash returns the 16-byte fingerprint of an entire entry set:
//
//	XOF("kv/sset/" || u32(N) || concat(entry_ids in canonical order), 16)
//
// Two parties agree on it iff they hold identical sets (up to a
// negligible collision). The sync protocol uses it only as a cheap
// equality probe. sortedEntries must be in canonical order.
func SetHash(sortedEntries []Entry) [16]byte {
	ids := make([]EntryID, len(sortedEntries))
	for i, e := range sortedEntries {
		ids[i] = e.ID()
	}
	return SetHashIDs(ids)
}

// SetHashIDs is SetHash computed from entry ids already in canonical
// order, avoiding a re-hash of every entry's bytes. Callers that keep a
// cached id index (e.g. store.MemStore) use this directly.
func SetHashIDs(ids []EntryID) [16]byte {
	buf := make([]byte, 0, len(domSet)+4+len(ids)*EntryIDSize)
	buf = append(buf, domSet...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(ids)))
	for i := range ids {
		buf = append(buf, ids[i][:]...)
	}
	var out [16]byte
	ascon.XOF(buf, out[:])
	return out
}
