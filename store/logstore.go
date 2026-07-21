package store

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/pavlos/kv/vault"
)

// On-disk log format:
//
//	header:
//	  magic (8 bytes, "ASCONKVA")
//	repeat:
//	  | rec_len (4 bytes LE) | entry_bytes |
//
// rec_len is the length of entry_bytes, which itself is vault.Entry
// marshaled form. The length prefix lets us detect a mid-write crash
// by checking whether a trailing record is truncated. The magic
// header is written at file-creation time and rejects accidental
// cross-format loading.

// logMagic tags the v1 on-disk log. The v1 entry format (26-byte
// header, 2-byte ct_len) differs from earlier layouts, so the magic is
// bumped to reject a stale-format log rather than mis-parse it.
var logMagic = []byte("ASCONKVA")

// MaxRecordBytes caps the per-record length accepted during replay. A
// record is one marshaled entry, and vault.UnmarshalEntry rejects any
// entry larger than MaxEntryBytes, so this is exactly that bound.
const MaxRecordBytes = vault.MaxEntryBytes

// ErrBadMagic reports a file that is not an ASCON-KV log.
var ErrBadMagic = errors.New("logstore: bad or missing magic header")

// LogStore wraps a MemStore with an append-only log for persistence.
type LogStore struct {
	mem  *MemStore
	mu   sync.Mutex
	f    *os.File
	path string
}

// OpenLogStore opens (or creates) a log file at path and rebuilds the
// in-memory set from it. The parent directory is created if missing. An
// exclusive advisory lock is taken so a second process cannot append to
// the same log concurrently (which would interleave records).
func OpenLogStore(path string) (*LogStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	ls := &LogStore{
		mem:  NewMemStore(),
		f:    f,
		path: path,
	}
	if err := ls.init(dir); err != nil {
		_ = f.Close()
		return nil, err
	}
	return ls, nil
}

// init ensures the file has a valid magic header then replays records.
// For a zero-length file (fresh or kv-init-touched), write the magic.
func (ls *LogStore) init(dir string) error {
	fi, err := ls.f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		if _, err := ls.f.Write(logMagic); err != nil {
			return err
		}
		if err := ls.f.Sync(); err != nil {
			return err
		}
		fsyncDir(dir) // make the new file's directory entry durable
		return nil
	}
	if fi.Size() < int64(len(logMagic)) {
		return fmt.Errorf("%w: file too small", ErrBadMagic)
	}
	hdr := make([]byte, len(logMagic))
	if _, err := ls.f.ReadAt(hdr, 0); err != nil {
		return err
	}
	if !bytes.Equal(hdr, logMagic) {
		return ErrBadMagic
	}
	return ls.replay()
}

// Close releases the file handle (and, with it, the advisory lock).
func (ls *LogStore) Close() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.f == nil {
		return nil
	}
	err := ls.f.Close()
	ls.f = nil
	return err
}

// replay reads every record from the log into memory. Any unparsable
// record is treated as an uncommitted torn tail and truncated away:
// every committed record is fsynced before the next append, and Add
// rolls back a torn write, so no valid committed data can sit after a
// bad record. Truncating restores an openable log; the sync protocol
// re-fetches anything lost.
func (ls *LogStore) replay() error {
	if _, err := ls.f.Seek(int64(len(logMagic)), io.SeekStart); err != nil {
		return err
	}
	offset := int64(len(logMagic))
	var hdr [4]byte
	for {
		if _, err := io.ReadFull(ls.f, hdr[:]); err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return ls.truncateTo(offset) // torn length prefix
			}
			return err
		}
		recLen := binary.LittleEndian.Uint32(hdr[:])
		if recLen == 0 || recLen > MaxRecordBytes {
			return ls.truncateTo(offset) // zero-filled or wild length: torn tail
		}
		buf := make([]byte, recLen)
		if _, err := io.ReadFull(ls.f, buf); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				return ls.truncateTo(offset)
			}
			return err
		}
		e, consumed, err := vault.UnmarshalEntry(buf)
		if err != nil || consumed != int(recLen) {
			return ls.truncateTo(offset) // unparsable body: torn tail
		}
		if _, err := ls.mem.Add(e); err != nil {
			return err
		}
		offset += 4 + int64(recLen)
	}
	// Seek to end so Add's writes append.
	if _, err := ls.f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	return nil
}

func (ls *LogStore) truncateTo(offset int64) error {
	if err := ls.f.Truncate(offset); err != nil {
		return err
	}
	if _, err := ls.f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	return nil
}

// Add implements Store.Add: dedup in memory, then append one record and
// fsync. The length prefix and body are written in a single call, and on
// any write/Sync error the file is truncated back to the pre-record
// offset so replay never sees a torn record. The entry is added to the
// in-memory set only after the write is durable, so memory and disk
// never disagree.
func (ls *LogStore) Add(e vault.Entry) (bool, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.mem.Has(e.ID()) {
		return false, nil
	}
	body := e.Marshal()
	rec := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(rec[:4], uint32(len(body)))
	copy(rec[4:], body)

	off, err := ls.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, err
	}
	if _, err := ls.f.Write(rec); err != nil {
		ls.rollback(off)
		return false, err
	}
	if err := ls.f.Sync(); err != nil {
		ls.rollback(off)
		return false, err
	}
	return ls.mem.Add(e)
}

// rollback undoes a torn record, restoring the append offset. Best-effort.
func (ls *LogStore) rollback(off int64) {
	_ = ls.f.Truncate(off)
	_, _ = ls.f.Seek(off, io.SeekStart)
}

// fsyncDir flushes a directory entry to disk so a freshly created file
// survives a crash. Best-effort: some filesystems reject a directory
// fsync, and the durability it adds is a backstop, not a correctness
// requirement.
func fsyncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

func (ls *LogStore) Has(id vault.EntryID) bool { return ls.mem.Has(id) }
func (ls *LogStore) Len() uint32               { return ls.mem.Len() }
func (ls *LogStore) Sorted() []vault.Entry     { return ls.mem.Sorted() }
func (ls *LogStore) SetHash() [16]byte         { return ls.mem.SetHash() }

func (ls *LogStore) EntriesAfter(afterTs uint64, afterID vault.EntryID, budget int) []vault.Entry {
	return ls.mem.EntriesAfter(afterTs, afterID, budget)
}
