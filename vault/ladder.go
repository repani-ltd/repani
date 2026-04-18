package vault

import (
	"bytes"
	"encoding/binary"
	"sort"

	"github.com/pavlos/kv/ascon"
)

// Domain separator for the terminal "hash-of-all" ladder entry.
var domLadT = []byte("kv/ladT/")

// CanonicalLess returns true if a should sort before b under the
// protocol's canonical order: TsMs ascending, then EntryID ascending.
//
// This is the slow form — it computes both IDs on each call. For
// sorting large entry sets, use SortCanonical which precomputes IDs
// once outside the comparator.
func CanonicalLess(a, b Entry) bool {
	if a.TsMs != b.TsMs {
		return a.TsMs < b.TsMs
	}
	ai := a.ID()
	bi := b.ID()
	return bytes.Compare(ai[:], bi[:]) < 0
}

// SortCanonical sorts entries in place by (TsMs asc, EntryID asc).
// Entry IDs are precomputed once before the sort so that comparisons
// are O(1) rather than re-XOF-ing both operands.
func SortCanonical(entries []Entry) {
	n := len(entries)
	if n < 2 {
		return
	}
	ids := make([]EntryID, n)
	for i := range entries {
		ids[i] = entries[i].ID()
	}
	sort.Sort(&canonicalSort{entries: entries, ids: ids})
}

// canonicalSort sorts entries and their precomputed ids in parallel.
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

// LadderSizes returns the progressive window sizes used by the
// suffix ladder for a vault of n entries:
//
//	{ 1, 2, 4, 8, ..., 2^p }
//
// where 2^p is the largest power of two not greater than n. For n = 0
// the sequence is empty. The ladder is PROGRESSIVE only; full-set
// coverage is provided by the separate TerminalHash.
func LadderSizes(n uint32) []uint32 {
	if n == 0 {
		return nil
	}
	out := make([]uint32, 0, 32)
	var k uint32 = 1
	for k <= n {
		out = append(out, k)
		if k > (1 << 30) {
			break
		}
		k <<= 1
	}
	return out
}

// LadderHashAt computes L_k = XOF("kv/lad1/" || u32(k) || last-k entry
// ids, 16). sortedEntries must be in canonical order. When k exceeds
// len(sortedEntries), all available entries are used — this matches
// the protocol's "or all of them if the server holds fewer than D".
func LadderHashAt(sortedEntries []Entry, k uint32) [16]byte {
	n := uint32(len(sortedEntries))
	use := min(k, n)
	tail := sortedEntries[uint32(len(sortedEntries))-use:]

	buf := make([]byte, 0, len(domLAD)+4+int(use)*EntryIDSize)
	buf = append(buf, domLAD...)
	buf = binary.LittleEndian.AppendUint32(buf, k)
	for _, e := range tail {
		id := e.ID()
		buf = append(buf, id[:]...)
	}
	var out [16]byte
	ascon.XOF(buf, out[:])
	return out
}

// TerminalHash returns the 16-byte "hash of all my entries" binding
// in the party's actual count:
//
//	XOF("kv/ladT/" || u32(N_actual) || concat(sorted_all_entry_ids), 16)
//
// Two parties that hold the same set produce the same TerminalHash.
// Parties that differ in either the set or the count produce
// different hashes, so terminal-based comparison cannot be fooled by
// a server that holds more entries than the client's last-N window
// covers (e.g., an entry backdated below the client's ladder floor).
// The empty set (n=0) has its own well-defined terminal.
func TerminalHash(sortedEntries []Entry) [16]byte {
	n := uint32(len(sortedEntries))
	buf := make([]byte, 0, len(domLadT)+4+int(n)*EntryIDSize)
	buf = append(buf, domLadT...)
	buf = binary.LittleEndian.AppendUint32(buf, n)
	for _, e := range sortedEntries {
		id := e.ID()
		buf = append(buf, id[:]...)
	}
	var out [16]byte
	ascon.XOF(buf, out[:])
	return out
}

// ComputeLadder returns the progressive ladder hashes aligned with
// LadderSizes(len(sortedEntries)).
func ComputeLadder(sortedEntries []Entry) [][16]byte {
	sizes := LadderSizes(uint32(len(sortedEntries)))
	if len(sizes) == 0 {
		return nil
	}
	out := make([][16]byte, len(sizes))
	for i, k := range sizes {
		out[i] = LadderHashAt(sortedEntries, k)
	}
	return out
}
