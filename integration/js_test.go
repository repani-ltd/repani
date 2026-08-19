// Drives the JavaScript client (js/kv.js) through js/kv_test.mjs:
// vectors generated here from the Go implementation, then live sync
// phases against an in-process server. Skips when node is absent.
package integration_test

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"repani.com/kv/vault"
	"repani.com/kv/wire"
	"repani.com/ascon"
)

// jsVectors builds the cross-implementation vector set consumed by
// js/kv_test.mjs. Every value is produced by the Go packages, so the
// JS side is always checked against the current Go behaviour.
func jsVectors(t *testing.T) map[string]any {
	t.Helper()
	h := hex.EncodeToString

	var rk vault.RootKey
	for i := range rk {
		rk[i] = byte(i)
	}
	var regk vault.RegisterKey
	for i := range regk {
		regk[i] = byte(0x80 + i)
	}
	aeadKey := vault.DeriveAEADKey(rk)
	vid := vault.DeriveVaultID(rk)
	ak := vault.DeriveAuthKey(rk)
	c2s := vault.DeriveC2S(ak)
	s2c := vault.DeriveS2C(ak)
	reg := vault.DeriveRegKey(regk)

	out := map[string]any{
		"rootKey":         h(rk[:]),
		"registerKey":     h(regk[:]),
		"aeadKey":         h(aeadKey[:]),
		"vaultID":         h(vid[:]),
		"authKey":         h(ak[:]),
		"c2s":             h(c2s[:]),
		"s2c":             h(s2c[:]),
		"reg":             h(reg[:]),
		"rootKeyBlob":     vault.EncodeRootKey(rk),
		"registerKeyBlob": vault.EncodeRegisterKey(regk),
		"backupBlob":      vault.EncodeBackup(rk, &regk),
	}

	xe := ascon.Sum16(nil)
	out["xofEmpty16"] = h(xe[:])
	xofMsg := "The quick brown fox jumps over the lazy dog"
	x40 := make([]byte, 40)
	ascon.XOF([]byte(xofMsg), x40)
	out["xofMsg"] = xofMsg
	out["xofMsg40"] = h(x40)

	// Deterministic entries: vault.Seal hedges its nonce, so seal via
	// the AEAD directly using Seal's exact plaintext and AD layout.
	aead, err := rk.AEAD()
	if err != nil {
		t.Fatal(err)
	}
	mkEntry := func(ts uint64, nonceFill byte, key, value []byte) vault.Entry {
		pt := make([]byte, 1+len(key)+2+len(value))
		pt[0] = byte(len(key))
		copy(pt[1:], key)
		pt[1+len(key)] = byte(len(value))
		pt[1+len(key)+1] = byte(len(value) >> 8)
		copy(pt[1+len(key)+2:], value)
		var nonce [16]byte
		for i := range nonce {
			nonce[i] = nonceFill
		}
		e := vault.Entry{TsMs: ts, Nonce: nonce}
		ad := e.Header()
		ctLen := len(pt) + ascon.AEADTagSize
		ad[24] = byte(ctLen)
		ad[25] = byte(ctLen >> 8)
		e.Ct = aead.Seal(nil, nonce[:], pt, ad)
		return e
	}
	// Values follow CLIENT.txt (fmt 0x00 || payload); e3 is a
	// tombstone for alpha with a ts tie against e2.
	e1 := mkEntry(1000, 0xA1, []byte("alpha"), append([]byte{0x00}, "first"...))
	e2 := mkEntry(2000, 0xA2, []byte("beta"), append([]byte{0x00}, "second"...))
	e3 := mkEntry(2000, 0xA3, []byte("alpha"), nil)
	entries := []vault.Entry{e1, e2, e3}
	var marshaled, ids []string
	for _, e := range entries {
		id := e.ID()
		marshaled = append(marshaled, h(e.Marshal()))
		ids = append(ids, h(id[:]))
	}
	out["entries"] = marshaled
	out["entryIDs"] = ids

	vault.SortCanonical(entries)
	sh := vault.SetHash(entries)
	out["setHash"] = h(sh[:])
	var sortedIDs []string
	for _, e := range entries {
		id := e.ID()
		sortedIDs = append(sortedIDs, h(id[:]))
	}
	out["sortedIDs"] = sortedIDs

	probe := wire.SyncReq{Op: wire.OpProbe, SetHash: sh}.Marshal()
	out["probePayload"] = h(probe)
	out["pullCursorTs"] = 12345
	pull := wire.SyncReq{Op: wire.OpPull, CurTs: 12345, CurID: e1.ID()}.Marshal()
	out["pullPayload"] = h(pull)
	// Push payload in original (unsorted) entry order, matching the
	// order of out["entries"].
	push := wire.SyncReq{Op: wire.OpPush, Entries: []vault.Entry{e1, e2, e3}}.Marshal()
	out["pushPayload"] = h(push)

	var fn [16]byte
	for i := range fn {
		fn[i] = 0xB0 + byte(i)
	}
	out["frameNonce"] = h(fn[:])
	frame, err := wire.SealFrame(wire.KindSync, vid, fn, c2s, probe)
	if err != nil {
		t.Fatal(err)
	}
	out["probeFrame"] = h(frame)
	resp := wire.Resp{Status: wire.StatusOK, ReqNonce: fn, Entries: entries}.Marshal()
	respFrame, err := wire.SealFrame(wire.KindResp, vid, fn, s2c, resp)
	if err != nil {
		t.Fatal(err)
	}
	out["respFrame"] = h(respFrame)
	return out
}

// TestJSClient checks js/kv.js against the Go implementation: byte-
// exact vectors for every protocol layer, then live PROBE/PULL sync
// phases against a real server populated by the Go client.
func TestJSClient(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found in PATH; skipping JS client test")
	}

	vecPath := filepath.Join(t.TempDir(), "vectors.json")
	vecJSON, err := json.Marshal(jsVectors(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vecPath, vecJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join("..", "js", "kv_test.mjs")

	// Live server, vault written by the Go client.
	_, ts := newServer(t)
	defer ts.Close()
	rk := vault.RootKey{42}
	dev := device(t, rk, ts.URL)
	put := func(key, value string) {
		t.Helper()
		if err := dev.Put([]byte(key), []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	put("p/001", "hello from go")
	put("p/002", "second page")
	put("doomed", "to be deleted")
	if err := dev.Delete([]byte("doomed")); err != nil {
		t.Fatal(err)
	}
	mustSync(t, dev)

	cmd := exec.Command(node, script, vecPath, ts.URL, vault.EncodeRootKey(rk))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr []byte
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { stderr, _ = io.ReadAll(stderrPipe); close(done) }()
	timer := time.AfterFunc(60*time.Second, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()

	replies := bufio.NewScanner(stdout)
	send := func(cmd map[string]any) {
		t.Helper()
		req, err := json.Marshal(cmd)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stdin.Write(append(req, '\n')); err != nil {
			t.Fatalf("write to js harness: %v", err)
		}
		if !replies.Scan() {
			t.Fatalf("js harness closed stdout; stderr:\n%s", stderr)
		}
		if reply := replies.Text(); reply != "OK" {
			t.Fatalf("js harness: %s", reply)
		}
	}
	step := func(expect map[string]string, probeOnly bool) {
		t.Helper()
		send(map[string]any{"cmd": "sync", "expect": expect, "probeOnly": probeOnly})
	}

	// Fresh full sync, then a steady-state probe-only sync.
	step(map[string]string{"p/001": "hello from go", "p/002": "second page"}, false)
	step(map[string]string{"p/001": "hello from go", "p/002": "second page"}, true)

	// Go client writes more; the JS client must converge incrementally.
	put("p/003", "third page")
	if err := dev.Delete([]byte("p/002")); err != nil {
		t.Fatal(err)
	}
	mustSync(t, dev)
	step(map[string]string{"p/001": "hello from go", "p/003": "third page"}, false)

	// --- write path: a JS-owned vault ---

	// The JS client registers a fresh vault and pushes its own writes
	// (fast path: put, put, tombstone, then one sync).
	rk2 := vault.RootKey{7}
	send(map[string]any{
		"cmd":         "vault2",
		"rootKey":     vault.EncodeRootKey(rk2),
		"registerKey": vault.EncodeRegisterKey(testRegKey),
	})
	send(map[string]any{"cmd": "put", "key": "js/a", "value": "from js"})
	send(map[string]any{"cmd": "put", "key": "js/tmp", "value": "temporary"})
	send(map[string]any{"cmd": "del", "key": "js/tmp"})
	step(map[string]string{"js/a": "from js"}, false)

	// A Go device on the same vault must see exactly the JS writes.
	dev2 := device(t, rk2, ts.URL)
	mustSync(t, dev2)
	assertState(t, dev2, map[string]string{"js/a": "from js"})

	// Bidirectional: Go writes, JS deletes, both converge.
	if err := dev2.Put([]byte("go/x"), []byte("from go")); err != nil {
		t.Fatal(err)
	}
	mustSync(t, dev2)
	send(map[string]any{"cmd": "del", "key": "js/a"})
	step(map[string]string{"go/x": "from go"}, false)
	mustSync(t, dev2)
	assertState(t, dev2, map[string]string{"go/x": "from go"})

	// Steady state on the JS-owned vault.
	step(map[string]string{"go/x": "from go"}, true)

	// --- failure paths ---

	// A wrong register key is rejected at the registration gate.
	var wrongReg vault.RegisterKey
	wrongReg[0] = 0xFF
	send(map[string]any{
		"cmd":         "vault2",
		"rootKey":     vault.EncodeRootKey(vault.RootKey{9}),
		"registerKey": vault.EncodeRegisterKey(wrongReg),
	})
	send(map[string]any{"cmd": "sync", "expectError": "wrong register key"})

	// A keyless client on an unregistered vault fails cleanly.
	send(map[string]any{"cmd": "vault2", "rootKey": vault.EncodeRootKey(vault.RootKey{11})})
	send(map[string]any{"cmd": "sync", "expectError": "no register key installed"})

	if _, err := io.WriteString(stdin, `{"cmd":"end"}`+"\n"); err != nil {
		t.Fatalf("write to js harness: %v", err)
	}
	<-done
	if err := cmd.Wait(); err != nil {
		t.Fatalf("js harness failed: %v\nstderr:\n%s", err, stderr)
	}
}
