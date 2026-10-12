package vault

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"slices"

	"repani.com/ascon"
)

// SortCanonical sorts entries in place by (TsMs asc, EntryID asc),
// hashing each entry's id once. This order drives the last-writer-wins
// merge and the paged sync cursor.
func SortCanonical(entries []Entry) {
	type pair struct {
		e  Entry
		id EntryID
	}
	ps := make([]pair, len(entries))
	for i, e := range entries {
		ps[i] = pair{e, e.ID()}
	}
	slices.SortFunc(ps, func(a, b pair) int {
		if c := cmp.Compare(a.e.TsMs, b.e.TsMs); c != 0 {
			return c
		}
		return bytes.Compare(a.id[:], b.id[:])
	})
	for i, p := range ps {
		entries[i] = p.e
	}
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
