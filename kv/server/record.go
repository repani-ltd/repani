package server

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"repani.com/durable"
	"repani.com/fact"
	"repani.com/kv/vault"
)

// A vault's directory under DataDir holds two files: its record,
// vault.fact, and its entry log, entries.log. The record is all of the
// vault's state but its entries, replaced whole in one atomic write,
// so its parts never disagree: the vault exists once its record does,
// and has ended -- revoked for good, its entries deleted -- once the
// record says ended. No step depends on two files changing together
// (~/repos/CLAUDE.md); the entry log only follows the record, created
// after it and deleted after the record ends, and a start finishes a
// deletion a crash interrupted.
//
// An operator reads a record with any text tool and checks it with
// fact validate. Deleting a revoked vault's directory, server
// stopped, lets it be registered again.

type record struct {
	Vault vaultRecord `fact:"vault"`
}

type vaultRecord struct {
	// AuthKey is hex; none for a vault revoked before it ever reached
	// this server.
	AuthKey   *string    `fact:"auth_key"`
	Ephemeral bool       `fact:"ephemeral"`
	Created   time.Time  `fact:"created"`
	Ended     *time.Time `fact:"ended"`
	Note      *string    `fact:"note"` // why it ended
}

func recordPath(dir string) string { return filepath.Join(dir, "vault.fact") }
func logPath(dir string) string    { return filepath.Join(dir, "entries.log") }

// recordTime is t in ms as a record holds it: UTC, to the second.
func recordTime(ms int64) time.Time {
	return time.UnixMilli(ms).UTC().Truncate(time.Second)
}

func writeRecord(dir string, r vaultRecord) error {
	b, err := fact.Marshal(record{r})
	if err != nil {
		return err
	}
	return durable.WriteFile(recordPath(dir), b, 0o600)
}

// readRecord reads dir's record; an error wrapping os.ErrNotExist
// means there is none.
func readRecord(dir string) (vaultRecord, error) {
	b, err := os.ReadFile(recordPath(dir))
	if err != nil {
		return vaultRecord{}, err
	}
	var r record
	if err := fact.Unmarshal(b, &r); err != nil {
		return vaultRecord{}, fmt.Errorf("%s: %w", recordPath(dir), err)
	}
	return r.Vault, nil
}

// authKey decodes the record's AuthKey.
func (r vaultRecord) authKey() (vault.AuthKey, error) {
	var ak vault.AuthKey
	if r.AuthKey == nil {
		return ak, fmt.Errorf("server: record has no auth_key")
	}
	b, err := hex.DecodeString(*r.AuthKey)
	if err != nil || len(b) != vault.AuthKeySize {
		return ak, fmt.Errorf("server: record auth_key: want %d hex digits", 2*vault.AuthKeySize)
	}
	copy(ak[:], b)
	return ak, nil
}

// record is st's record while it is served.
func (st *vaultState) record() vaultRecord {
	ak := hex.EncodeToString(st.authKey[:])
	return vaultRecord{AuthKey: &ak, Ephemeral: st.ephemeral, Created: time.UnixMilli(st.created).UTC()}
}

// ended is r ended at ms, with note ("" for none).
func (r vaultRecord) ended(ms int64, note string) vaultRecord {
	t := recordTime(ms)
	r.Ended = &t
	if note != "" {
		r.Note = &note
	}
	return r
}
