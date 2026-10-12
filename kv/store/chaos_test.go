package store

// Chaos test: real worker processes hammer one log through the CLI's
// open/append/close lifecycle while the parent SIGKILLs them
// mid-operation. It asserts the two crash-safety invariants: an entry
// acknowledged (Add returned, post-fsync) is durable across any later
// kill, and the log reopens clean no matter where a worker died.
//
// It forks the test binary itself as the worker, so it needs no built
// CLI. Deliberately not part of a normal `go test` run -- invoke it
// explicitly when needed:
//
//	KV_CHAOS=1 go test ./store -run Chaos -v

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"repani.com/kv/vault"
)

const (
	chaosWorkers = 2
	chaosOps     = 60 // appends per worker, one open/close cycle each
	chaosBudget  = 3 * time.Second
)

func TestChaosKilledWriters(t *testing.T) {
	if os.Getenv("KV_CHAOS") == "" {
		t.Skip("chaos test: run explicitly with KV_CHAOS=1")
	}
	path := filepath.Join(t.TempDir(), "entries.log")
	rk, err := vault.NewRandomRootKey()
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	acked := make(map[vault.EntryID]bool)
	kills := 0

	// runWorker forks one worker and streams its ACK lines. With kill
	// set it delivers SIGKILL after a few milliseconds, aiming inside
	// an append. Reports whether the worker ran to completion.
	runWorker := func(name string, kill bool) bool {
		cmd := exec.Command(os.Args[0], "-test.run=TestChaosWorkerProcess$")
		cmd.Env = append(os.Environ(),
			"KV_CHAOS_WORKER=1",
			"KV_CHAOS_PATH="+path,
			"KV_CHAOS_KEY="+vault.EncodeRootKey(rk),
			"KV_CHAOS_NAME="+name,
		)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				hexID, ok := strings.CutPrefix(sc.Text(), "ACK ")
				if !ok {
					continue // test-framework chatter
				}
				b, err := hex.DecodeString(hexID)
				if err != nil || len(b) != vault.EntryIDSize {
					continue
				}
				var id vault.EntryID
				copy(id[:], b)
				mu.Lock()
				acked[id] = true
				mu.Unlock()
			}
		}()
		if kill {
			time.Sleep(time.Duration(2+rand.Intn(20)) * time.Millisecond)
			_ = cmd.Process.Kill()
		}
		<-done
		return cmd.Wait() == nil
	}

	deadline := time.Now().Add(chaosBudget)
	var wg sync.WaitGroup
	for w := range chaosWorkers {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				if !runWorker(name, true) {
					mu.Lock()
					kills++
					mu.Unlock()
				}
			}
			// A final undisturbed run: every op must eventually land.
			if !runWorker(name, false) {
				t.Errorf("worker %s failed its clean run", name)
			}
		}(fmt.Sprintf("w%d", w))
	}
	wg.Wait()

	// The log must open clean and hold every acknowledged entry.
	ls, err := OpenLogStore(path, 0)
	if err != nil {
		t.Fatalf("log damaged after chaos: %v", err)
	}
	defer ls.Close()
	mu.Lock()
	defer mu.Unlock()
	for id := range acked {
		if !ls.Has(id) {
			t.Errorf("acked entry %x missing after replay", id)
		}
	}
	if kills == 0 {
		t.Error("no worker was ever killed mid-run; chaos exercised nothing")
	}
	t.Logf("chaos: %d kills, %d acked entries, %d entries in log", kills, len(acked), ls.Len())
}

// TestChaosWorkerProcess is not a test: it is the subprocess body
// forked by TestChaosKilledWriters, guarded by an env var so ordinary
// runs skip it silently. It mimics the CLI: each append is a full
// open, lock, replay, add+fsync, close cycle, acknowledged on stdout
// only once Add has returned.
func TestChaosWorkerProcess(t *testing.T) {
	if os.Getenv("KV_CHAOS_WORKER") == "" {
		t.Skip("chaos worker subprocess")
	}
	rk, err := vault.DecodeRootKey(os.Getenv("KV_CHAOS_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := rk.AEAD()
	if err != nil {
		t.Fatal(err)
	}
	name := os.Getenv("KV_CHAOS_NAME")
	path := os.Getenv("KV_CHAOS_PATH")
	for i := range chaosOps {
		ls, err := OpenLogStore(path, 10*time.Second)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		e, err := vault.Seal(aead, uint64(i+1), []byte(fmt.Sprintf("%s-%d", name, i)), []byte("v"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ls.Add(e); err != nil {
			t.Fatalf("add: %v", err)
		}
		fmt.Printf("ACK %x\n", e.ID())
		if err := ls.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}
