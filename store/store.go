// Package store provides the client- and server-side entry set used
// by ASCON-KV. A Store is an unordered set keyed by EntryID that also
// exposes a canonical-order view and a set hash; see PROTOCOL.txt.
package store

import "github.com/pavlos/kv/vault"

// Store is the contract both the client and the server depend on.
// Implementations must be safe for concurrent use.
type Store interface {
	// Add inserts e. It returns true if e was new, false if an entry
	// with the same ID was already present. It is idempotent and
	// safe under concurrent callers.
	Add(e vault.Entry) (added bool, err error)

	// Has reports whether the store contains the given id.
	Has(id vault.EntryID) bool

	// Len returns the number of distinct entries.
	Len() uint32

	// Sorted returns a snapshot of the entries in canonical order
	// (TsMs asc, EntryID asc). The returned slice is safe for the
	// caller to read but must not be mutated.
	Sorted() []vault.Entry

	// SetHash returns the 16-byte fingerprint of the whole entry set
	// (vault.SetHash), used as the sync equality probe.
	SetHash() [16]byte

	// EntriesAfter returns whole entries in canonical order strictly
	// after the cursor (afterTs, afterID), up to budget bytes but always
	// at least one if any remain. Powers the paged PULL.
	EntriesAfter(afterTs uint64, afterID vault.EntryID, budget int) []vault.Entry
}
