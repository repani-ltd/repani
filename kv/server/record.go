package server

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// A record is five facts under vault:
//
//	vault.auth_key: str? = "<hex>"
//	vault.created: datetime = 2026-10-26T09:30:00Z
//	vault.ended: datetime? = none
//	vault.ephemeral: bool = false
//	vault.note: str? = none
type vaultRecord struct {
	// AuthKey is hex; none for a vault revoked before it ever reached
	// this server.
	AuthKey   *string
	Ephemeral bool
	Created   time.Time
	Ended     *time.Time
	Note      *string // why it ended
}

// factTime is the layout of a record's datetimes.
const factTime = "2006-01-02T15:04:05Z"

func recordPath(dir string) string { return filepath.Join(dir, "vault.fact") }
func logPath(dir string) string    { return filepath.Join(dir, "entries.log") }

// recordTime is t in ms as a record holds it: UTC, to the second.
func recordTime(ms int64) time.Time {
	return time.UnixMilli(ms).UTC().Truncate(time.Second)
}

func writeRecord(dir string, r vaultRecord) error {
	b, err := r.encode()
	if err != nil {
		return err
	}
	return durable.WriteFile(recordPath(dir), b, 0o600)
}

// encode is r as a canonical FACT document. fact.Load checks the
// lines as any reader would, so a record never holds what fact
// validate rejects.
func (r vaultRecord) encode() ([]byte, error) {
	ended := "none"
	if r.Ended != nil {
		ended = r.Ended.UTC().Format(factTime)
	}
	src := "vault.auth_key: str? = " + optQuote(r.AuthKey) + "\n" +
		"vault.created: datetime = " + r.Created.UTC().Format(factTime) + "\n" +
		"vault.ended: datetime? = " + ended + "\n" +
		"vault.ephemeral: bool = " + strconv.FormatBool(r.Ephemeral) + "\n" +
		"vault.note: str? = " + optQuote(r.Note) + "\n"
	facts, errs := fact.Load([]byte(src))
	if len(errs) > 0 {
		return nil, fmt.Errorf("server: record: %s", errs[0].Msg)
	}
	return fact.Canonical(facts), nil
}

// optQuote is s as a str? value.
func optQuote(s *string) string {
	if s == nil {
		return "none"
	}
	return fact.Quote(*s)
}

// readRecord reads dir's record; an error wrapping os.ErrNotExist
// means there is none.
func readRecord(dir string) (vaultRecord, error) {
	b, err := os.ReadFile(recordPath(dir))
	if err != nil {
		return vaultRecord{}, err
	}
	r, err := decodeRecord(b)
	if err != nil {
		return vaultRecord{}, fmt.Errorf("%s: %w", recordPath(dir), err)
	}
	return r, nil
}

// decodeRecord is the record b holds. A key it does not name is
// ignored; one it names must have the type encode gives it, and a
// missing optional is none.
func decodeRecord(b []byte) (vaultRecord, error) {
	facts, errs := fact.Load(b)
	if len(errs) > 0 {
		return vaultRecord{}, fmt.Errorf("line %d: %s", errs[0].Line, errs[0].Msg)
	}
	m, err := fact.Bind(facts)
	if err != nil {
		return vaultRecord{}, err
	}
	v, _ := m["vault"].(map[string]any)
	var r vaultRecord
	var ok bool
	if r.Ephemeral, ok = v["ephemeral"].(bool); !ok {
		return vaultRecord{}, fmt.Errorf("vault.ephemeral: want bool")
	}
	if r.Created, err = bindTime(v["created"]); err != nil {
		return vaultRecord{}, fmt.Errorf("vault.created: %w", err)
	}
	if v["ended"] != nil {
		t, err := bindTime(v["ended"])
		if err != nil {
			return vaultRecord{}, fmt.Errorf("vault.ended: %w", err)
		}
		r.Ended = &t
	}
	if r.AuthKey, err = bindOptStr(v["auth_key"]); err != nil {
		return vaultRecord{}, fmt.Errorf("vault.auth_key: %w", err)
	}
	if r.Note, err = bindOptStr(v["note"]); err != nil {
		return vaultRecord{}, fmt.Errorf("vault.note: %w", err)
	}
	return r, nil
}

// bindTime is a bound datetime: a date-time, or a full-date read as
// its midnight UTC.
func bindTime(x any) (time.Time, error) {
	s, ok := x.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("want datetime")
	}
	layout := factTime
	if len(s) == len("2006-01-02") {
		layout = "2006-01-02"
	}
	return time.Parse(layout, s)
}

// bindOptStr is a bound str?: nil for none.
func bindOptStr(x any) (*string, error) {
	if x == nil {
		return nil, nil
	}
	s, ok := x.(string)
	if !ok {
		return nil, fmt.Errorf("want str")
	}
	return &s, nil
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
