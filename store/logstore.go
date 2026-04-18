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
//	  magic (8 bytes, "ASCONKV1")
//	repeat:
//	  | rec_len (4 bytes LE) | entry_bytes |
//
// rec_len is the length of entry_bytes, which itself is vault.Entry
// marshaled form. The length prefix lets us detect a mid-write crash
// by checking whether a trailing record is truncated. The magic
// header is written at file-creation time and rejects accidental
// cross-format loading.

var logMagic = []byte("ASCONKV1")

// MaxRecordBytes caps the per-record length we are willing to allocate
// during replay, protecting against corrupted length prefixes blowing
// the heap. It is intentionally a bit larger than vault.MaxCtBytes
// plus the entry header, to leave headroom for any future framing.
const MaxRecordBytes = vault.MaxCtBytes + vault.EntryHeaderSize + 1024

var (
	// ErrBadMagic reports a file that is not an ASCON-KV log.
	ErrBadMagic = errors.New("logstore: bad or missing magic header")
	// ErrRecordTooLarge reports a length prefix that exceeds MaxRecordBytes.
	ErrRecordTooLarge = errors.New("logstore: record exceeds MaxRecordBytes")
)

// LogStore wraps a MemStore with an append-only log for persistence.
type LogStore struct {
	mem  *MemStore
	mu   sync.Mutex
	f    *os.File
	path string
}

// OpenLogStore opens (or creates) a log file at path and rebuilds the
// in-memory set from it. The parent directory is created if missing.
func OpenLogStore(path string) (*LogStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	ls := &LogStore{
		mem:  NewMemStore(),
		f:    f,
		path: path,
	}
	if err := ls.init(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return ls, nil
}

// init ensures the file has a valid magic header then replays records.
// For a zero-length file (fresh or kv-init-touched), write the magic.
func (ls *LogStore) init() error {
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

// Close releases the file handle.
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

// replay reads every record from the log into memory. A truncated
// trailing record is silently discarded (the common crash case); a
// mid-stream record that fails to parse is a hard error.
func (ls *LogStore) replay() error {
	if _, err := ls.f.Seek(int64(len(logMagic)), io.SeekStart); err != nil {
		return err
	}
	offset := int64(len(logMagic))
	var hdr [4]byte
	for {
		n, err := io.ReadFull(ls.f, hdr[:])
		if err == io.EOF {
			break
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return ls.truncateTo(offset)
		}
		if err != nil {
			return err
		}
		_ = n
		recLen := binary.LittleEndian.Uint32(hdr[:])
		if recLen == 0 || recLen > MaxRecordBytes {
			return fmt.Errorf("%w: %d at offset %d", ErrRecordTooLarge, recLen, offset)
		}
		buf := make([]byte, recLen)
		if _, err := io.ReadFull(ls.f, buf); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				return ls.truncateTo(offset)
			}
			return err
		}
		e, consumed, err := vault.UnmarshalEntry(buf)
		if err != nil {
			return fmt.Errorf("logstore: corrupt record at offset %d: %w", offset, err)
		}
		if consumed != int(recLen) {
			return fmt.Errorf("logstore: record length mismatch at offset %d: header %d, entry %d", offset, recLen, consumed)
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

// Add implements Store.Add: dedup in memory, then on first write
// append to the log and fsync.
func (ls *LogStore) Add(e vault.Entry) (bool, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	added, err := ls.mem.Add(e)
	if err != nil || !added {
		return added, err
	}
	body := e.Marshal()
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := ls.f.Write(hdr[:]); err != nil {
		return false, err
	}
	if _, err := ls.f.Write(body); err != nil {
		return false, err
	}
	if err := ls.f.Sync(); err != nil {
		return false, err
	}
	return true, nil
}

func (ls *LogStore) Has(id vault.EntryID) bool      { return ls.mem.Has(id) }
func (ls *LogStore) Len() uint32                    { return ls.mem.Len() }
func (ls *LogStore) Sorted() []vault.Entry          { return ls.mem.Sorted() }
func (ls *LogStore) LadderHashAt(k uint32) [16]byte { return ls.mem.LadderHashAt(k) }
func (ls *LogStore) TerminalHash() [16]byte         { return ls.mem.TerminalHash() }
