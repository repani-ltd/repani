package store

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"repani.com/kv/vault"
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

// ErrLocked reports a log whose advisory lock another process holds.
var ErrLocked = errors.New("logstore: locked by another process")

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
// the same log concurrently (which would interleave records). A busy
// lock is retried until lockWait elapses: holders keep the lock only
// for milliseconds per operation, so a short wait lets concurrent
// invocations serialize instead of failing. Zero tries once and fails
// fast with ErrLocked.
func OpenLogStore(path string, lockWait time.Duration) (*LogStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFileWait(f, lockWait); err != nil {
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

// lockFileWait retries a busy lock with backoff until wait elapses.
// Errors other than ErrLocked (an unlockable filesystem, say) are
// permanent and returned immediately.
func lockFileWait(f *os.File, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	delay := 5 * time.Millisecond
	for {
		err := lockFile(f)
		if err == nil || !errors.Is(err, ErrLocked) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(delay)
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
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
	// Buffered reads and one reused record buffer: replay is the fixed
	// cost of every open, and per-record syscalls and allocations
	// dominate it. UnmarshalEntry copies what it keeps, so reusing buf
	// across records is safe.
	r := bufio.NewReaderSize(ls.f, 1<<16)
	offset := int64(len(logMagic))
	var hdr [4]byte
	var buf []byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
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
		if cap(buf) < int(recLen) {
			buf = make([]byte, recLen)
		}
		buf = buf[:recLen]
		if _, err := io.ReadFull(r, buf); err != nil {
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

// Add implements Store.Add: append every new record, then fsync once,
// so a pulled page or an import pays one disk flush instead of one per
// entry. The call is a single durability unit: on any write/Sync error
// the file is truncated back to its offset before the call and none of
// it reaches the in-memory set, so replay never sees a torn record and
// memory and disk never disagree.
func (ls *LogStore) Add(entries ...vault.Entry) (int, error) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ids := make([]vault.EntryID, 0, len(entries))
	news := make([]vault.Entry, 0, len(entries))
	seen := make(map[vault.EntryID]bool, len(entries))
	total := 0
	for _, e := range entries {
		id := e.ID()
		if seen[id] || ls.mem.Has(id) {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		news = append(news, e)
		total += 4 + e.Size()
	}
	if len(news) == 0 {
		return 0, nil
	}
	buf := make([]byte, total)
	pos := 0
	for _, e := range news {
		binary.LittleEndian.PutUint32(buf[pos:pos+4], uint32(e.Size()))
		pos += 4 + e.MarshalTo(buf[pos+4:])
	}
	off, err := ls.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if _, err := ls.f.Write(buf); err != nil {
		ls.rollback(off)
		return 0, err
	}
	if err := ls.f.Sync(); err != nil {
		ls.rollback(off)
		return 0, err
	}
	for i := range news {
		ls.mem.addID(ids[i], news[i])
	}
	return len(news), nil
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
