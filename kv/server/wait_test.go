package server

import (
	"context"
	"testing"
	"time"

	"repani.com/kv/vault"
	"repani.com/kv/wire"
)

// sendCtx is harness.send through HandleFrameContext.
func (h *harness) sendCtx(ctx context.Context, payload []byte) (wire.Resp, wire.PreChannel) {
	h.t.Helper()
	nonce, err := vault.FrameNonce(h.vid)
	if err != nil {
		h.t.Fatal(err)
	}
	frame, err := wire.SealFrame(wire.Header{Kind: wire.KindSync, VaultID: h.vid, Nonce: nonce}, h.c2s, payload)
	if err != nil {
		h.t.Fatal(err)
	}
	respBytes, err := h.srv.HandleFrameContext(ctx, frame)
	if err != nil {
		h.t.Fatal(err)
	}
	if len(respBytes) == 1 {
		return wire.Resp{}, wire.PreChannel(respBytes[0])
	}
	rh, sealed, err := wire.ParseHeader(respBytes)
	if err != nil {
		h.t.Fatal(err)
	}
	out, err := wire.OpenFrame(h.s2c, rh, sealed)
	if err != nil {
		h.t.Fatalf("open response: %v", err)
	}
	resp, err := wire.ParseResp(out)
	if err != nil {
		h.t.Fatal(err)
	}
	if resp.ReqNonce != nonce {
		h.t.Fatal("response nonce mismatch")
	}
	return resp, 0
}

func (h *harness) wait(ctx context.Context, setHash [16]byte) (wire.Resp, wire.PreChannel) {
	return h.sendCtx(ctx, wire.SyncReq{Op: wire.OpWait, SetHash: setHash}.Marshal())
}

// waitServer is a server whose WAIT gives up after waitMax, so the
// timeout path is testable in milliseconds.
func waitServer(t *testing.T, waitMax time.Duration) (*Server, *harness) {
	t.Helper()
	srv, err := New(Options{
		RegisterKeys: []vault.RegisterKey{testRegKey},
		Quotas:       Quotas{WaitMaxMs: waitMax.Milliseconds()},
	})
	if err != nil {
		t.Fatal(err)
	}
	rk := vault.RootKey{9, 9, 9}
	h := newHarness(t, srv, rk, testRegKey)
	if _, pre := h.register(); pre != 0 {
		t.Fatalf("register: %v", pre)
	}
	return srv, h
}

// emptyHash is the set hash of a vault with no entries: what a fresh
// client holds, and what the server holds right after REGISTER.
func emptyHash() [16]byte { return vault.SetHash(nil) }

func TestWait_DifferAtOnce(t *testing.T) {
	_, h := waitServer(t, time.Minute)
	start := time.Now()
	resp, pre := h.wait(context.Background(), [16]byte{0xFF})
	if pre != 0 || resp.Status != wire.StatusDiffer {
		t.Fatalf("WAIT on a stale hash: %v %s", pre, resp.Status)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a WAIT whose hash already differs must not be held")
	}
}

func TestWait_WokenByPush(t *testing.T) {
	_, h := waitServer(t, time.Minute)
	hash := emptyHash()
	done := make(chan wire.Resp, 1)
	go func() {
		resp, pre := h.wait(context.Background(), hash)
		if pre != 0 {
			t.Error(pre)
		}
		done <- resp
	}()
	time.Sleep(30 * time.Millisecond) // let the WAIT take hold
	select {
	case <-done:
		t.Fatal("WAIT answered before anything changed")
	default:
	}
	if resp, pre := h.push([]vault.Entry{mkEntry(t, h.rk, nowMs(), "k", "v")}); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("push: %v %s", pre, resp.Status)
	}
	select {
	case resp := <-done:
		if resp.Status != wire.StatusDiffer {
			t.Fatalf("WAIT after a push answered %s, want DIFFER", resp.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a push did not wake the WAIT")
	}
}

func TestWait_WokenByOwnerAdd(t *testing.T) {
	srv, h := waitServer(t, time.Minute)
	hash := emptyHash()
	done := make(chan wire.Resp, 1)
	go func() {
		resp, _ := h.wait(context.Background(), hash)
		done <- resp
	}()
	time.Sleep(30 * time.Millisecond)
	if err := srv.AddEntries(h.vid, []vault.Entry{mkEntry(t, h.rk, nowMs(), "k", "v")}); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-done:
		if resp.Status != wire.StatusDiffer {
			t.Fatalf("got %s, want DIFFER", resp.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an owner write did not wake the WAIT")
	}
}

func TestWait_TimesOutOK(t *testing.T) {
	_, h := waitServer(t, 60*time.Millisecond)
	hash := emptyHash()
	start := time.Now()
	resp, pre := h.wait(context.Background(), hash)
	if pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("timed-out WAIT: %v %s, want OK", pre, resp.Status)
	}
	if d := time.Since(start); d < 50*time.Millisecond || d > 2*time.Second {
		t.Fatalf("WAIT held for %v, want about WaitMaxMs", d)
	}
}

func TestWait_ContextEndsIt(t *testing.T) {
	_, h := waitServer(t, time.Minute)
	hash := emptyHash()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan wire.Resp, 1)
	go func() {
		resp, _ := h.wait(ctx, hash)
		done <- resp
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case resp := <-done:
		if resp.Status != wire.StatusOK {
			t.Fatalf("got %s, want OK", resp.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the context did not end the WAIT")
	}
}

func TestWait_CloseAnswersOK(t *testing.T) {
	srv, h := waitServer(t, time.Minute)
	hash := emptyHash()
	done := make(chan wire.Resp, 1)
	go func() {
		resp, _ := h.wait(context.Background(), hash)
		done <- resp
	}()
	time.Sleep(30 * time.Millisecond)
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-done:
		if resp.Status != wire.StatusOK {
			t.Fatalf("got %s, want OK", resp.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not release the WAIT")
	}
}

// TestWait_DrainAnswersOK: Drain, called as an HTTP shutdown starts,
// answers a held WAIT with OK and a later one at once, while every
// other operation is still served, so a drain is never held up for
// WAIT_MAX_MS.
func TestWait_DrainAnswersOK(t *testing.T) {
	srv, h := waitServer(t, time.Minute)
	hash := emptyHash()
	done := make(chan wire.Resp, 1)
	go func() {
		resp, _ := h.wait(context.Background(), hash)
		done <- resp
	}()
	time.Sleep(30 * time.Millisecond)
	srv.Drain()
	select {
	case resp := <-done:
		if resp.Status != wire.StatusOK {
			t.Fatalf("held WAIT got %s, want OK", resp.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Drain did not release the WAIT")
	}
	start := time.Now()
	if resp, _ := h.wait(context.Background(), hash); resp.Status != wire.StatusOK || time.Since(start) > time.Second {
		t.Fatalf("WAIT after Drain: %s after %v, want OK at once", resp.Status, time.Since(start))
	}
	if resp, pre := h.probe(hash); pre != 0 || resp.Status != wire.StatusOK {
		t.Fatalf("PROBE after Drain: pre=%v status=%s", pre, resp.Status)
	}
	srv.Drain() // twice is harmless, as is Close after it
}

func TestWait_ManyWaitersAllWake(t *testing.T) {
	_, h := waitServer(t, time.Minute)
	hash := emptyHash()
	const n = 8
	done := make(chan wire.Status, n)
	for range n {
		go func() {
			resp, _ := h.wait(context.Background(), hash)
			done <- resp.Status
		}()
	}
	time.Sleep(50 * time.Millisecond)
	h.push([]vault.Entry{mkEntry(t, h.rk, nowMs(), "k", "v")})
	for i := 0; i < n; i++ {
		select {
		case s := <-done:
			if s != wire.StatusDiffer {
				t.Fatalf("waiter %d got %s", i, s)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d waiters woke", i, n)
		}
	}
}

// A server that does not know an op answers BAD_REQUEST inside the
// channel: that is the defined answer a v1 server without WAIT gives,
// and what a client takes as "WAIT is not offered here".
func TestUnknownOp_IsBadRequest(t *testing.T) {
	_, h := waitServer(t, time.Minute)
	resp, pre := h.send(wire.KindSync, h.c2s, []byte{0x7F})
	if pre != 0 || resp.Status != wire.StatusBadRequest {
		t.Fatalf("unknown op: %v %s, want BAD_REQUEST", pre, resp.Status)
	}
}
