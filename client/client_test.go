package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"repani.com/kv/server"
	"repani.com/kv/store"
	"repani.com/kv/vault"
	"repani.com/kv/wire"
)

var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	srv, err := server.New(server.Options{RegisterKeys: []vault.RegisterKey{testRegKey}})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// inProc sends frames straight to a Server, bypassing HTTP.
type inProc struct{ srv *server.Server }

func (t *inProc) Do(_ context.Context, req []byte) ([]byte, error) {
	return t.srv.HandleFrame(req)
}

// maul rewrites the response bytes before the client parses them.
type maul struct {
	srv    *server.Server
	mangle func([]byte) []byte
}

func (t *maul) Do(_ context.Context, req []byte) ([]byte, error) {
	resp, err := t.srv.HandleFrame(req)
	if err != nil {
		return nil, err
	}
	return t.mangle(resp), nil
}

func newClient(t *testing.T, srv *server.Server, rk vault.RootKey) *Client {
	t.Helper()
	c, err := New(rk, store.NewMemStore(), &inProc{srv}, Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestPutMany: every pair is written; one empty value refuses the
// whole call, writing nothing.
func TestPutMany(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, newTestServer(t), rk)
	if err := c.PutMany(map[string][]byte{"a": []byte("1"), "b": []byte("2")}); err != nil {
		t.Fatal(err)
	}
	if err := c.PutMany(map[string][]byte{"c": []byte("3"), "d": nil}); err == nil {
		t.Fatal("an empty value was accepted")
	}
	keys, _ := c.List()
	if strings.Join(keys, ",") != "a,b" {
		t.Fatalf("keys %v, want a,b", keys)
	}
	if c.Store().Len() != 2 {
		t.Fatalf("store holds %d entries, want 2", c.Store().Len())
	}
}

func TestPutGetDelete_FmtTagTransparent(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)

	if err := c.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := c.Get([]byte("k1"))
	if err != nil || !ok || !bytes.Equal(v, []byte("v1")) {
		t.Fatalf("Get: v=%q ok=%v err=%v", v, ok, err)
	}
	// Binary values with a leading 0x00 must round-trip (the fmt tag is
	// distinct from value bytes).
	bin := []byte{0x00, 0x01, 0xff, 0x00}
	if err := c.Put([]byte("bin"), bin); err != nil {
		t.Fatal(err)
	}
	v, ok, _ = c.Get([]byte("bin"))
	if !ok || !bytes.Equal(v, bin) {
		t.Fatalf("binary value round-trip: got %v", v)
	}

	if err := c.Delete([]byte("k1")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get([]byte("k1")); ok {
		t.Fatal("expected tombstoned key absent")
	}
}

func TestPut_RejectsEmptyValue(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	if err := c.Put([]byte("k"), nil); err == nil {
		t.Fatal("expected empty value to be rejected")
	}
}

func TestSync_LocalWriteReachesReader(t *testing.T) {
	ctx := context.Background()
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()

	a := newClient(t, srv, rk)
	if err := a.Put([]byte("apple"), []byte("green")); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	b := newClient(t, srv, rk) // fresh store, same RootKey
	if err := b.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := b.Get([]byte("apple")); !ok || !bytes.Equal(v, []byte("green")) {
		t.Fatalf("reader saw %q (ok=%v)", v, ok)
	}
	// Second sync on a converged client is a clean no-op (matching probe).
	if err := b.Sync(ctx); err != nil {
		t.Fatalf("steady-state sync: %v", err)
	}
}

// TestSync_RejectsForgedResponse verifies the client refuses a response
// whose seal does not open (any bit flipped in the sealed region).
func TestSync_RejectsForgedResponse(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	srv := newTestServer(t)
	tran := &maul{srv: srv, mangle: func(b []byte) []byte {
		if len(b) <= 1 {
			return b // don't disturb pre-channel bytes
		}
		out := append([]byte(nil), b...)
		out[len(out)-1] ^= 1 // flip a tag byte
		return out
	}}
	c, err := New(rk, store.NewMemStore(), tran, Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background()); err == nil {
		t.Fatal("expected sync to fail on a forged response")
	}
}

func TestSync_GatedWrongKeyRejected(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	var wrong vault.RegisterKey
	for i := range wrong {
		wrong[i] = 0xEE
	}
	c, err := New(rk, store.NewMemStore(), &inProc{srv}, Options{RegisterKey: &wrong})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background()); err == nil {
		t.Fatal("expected registration to fail with wrong register key")
	}
}

// stuckServer is a minimal fake that seals valid responses but always
// returns the SAME pull page, so its cursor never advances past the
// first entry — exercising the client's pull loop protection.
type stuckServer struct {
	vid   vault.VaultID
	c2s   vault.ChannelKey
	s2c   vault.ChannelKey
	reg   vault.ChannelKey
	fixed vault.Entry
}

func (s *stuckServer) Do(_ context.Context, req []byte) ([]byte, error) {
	kind, vid, nonce, sealed, err := wire.ParseHeader(req)
	if err != nil {
		return wire.ErrBadFrame.Reply(), nil
	}
	openK := s.c2s
	if kind == wire.KindRegister {
		openK = s.reg
	}
	payload, err := wire.OpenFrame(openK, kind, vid, nonce, sealed)
	if err != nil {
		return wire.ErrBadAuth.Reply(), nil
	}
	resp := wire.Resp{ReqNonce: nonce, Status: wire.StatusOK}
	if kind == wire.KindSync {
		sr, _ := wire.ParseSync(payload)
		switch sr.Op {
		case wire.OpProbe:
			resp.Status = wire.StatusDiffer
		case wire.OpPull:
			resp.Entries = []vault.Entry{s.fixed} // never advances
		}
	}
	rn, err := vault.FrameNonce(vid)
	if err != nil {
		return nil, err
	}
	return wire.SealFrame(wire.KindResp, vid, rn, s.s2c, resp.Marshal())
}

func TestSync_PullLoopProtection(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	aead, _ := rk.AEAD()
	fixed, _ := vault.Seal(aead, 1000, []byte("k"), []byte("\x00v"))
	ak := vault.DeriveAuthKey(rk)
	srv := &stuckServer{
		vid:   vault.DeriveVaultID(rk),
		c2s:   vault.DeriveC2S(ak),
		s2c:   vault.DeriveS2C(ak),
		reg:   vault.DeriveRegKey(testRegKey),
		fixed: fixed,
	}
	c, err := New(rk, store.NewMemStore(), srv, Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}

	err = c.Sync(context.Background())
	if err == nil {
		t.Fatal("expected sync to error against a non-advancing server, not hang")
	}
	if !strings.Contains(err.Error(), "advance") {
		t.Fatalf("expected a cursor-advance error, got: %v", err)
	}
}

func TestReconcileAnchor(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)

	// Empty store → full walk (anchor zero).
	if ts, _ := c.reconcileAnchor(0); ts != 0 {
		t.Fatalf("empty store round 0 anchor ts=%d, want 0", ts)
	}
	// A store whose frontier is a real (large) timestamp → round 0 looks
	// back one window; round 1+ is a full walk.
	now := uint64(time.Now().UnixMilli())
	aead, _ := rk.AEAD()
	e, _ := vault.Seal(aead, now, []byte("k"), []byte("\x00v"))
	_, _ = c.Store().Add(e)
	if ts, _ := c.reconcileAnchor(0); ts != now-defaultSyncWindowMs {
		t.Fatalf("round 0 anchor ts=%d, want %d", ts, now-defaultSyncWindowMs)
	}
	if ts, _ := c.reconcileAnchor(1); ts != 0 {
		t.Fatalf("round 1 anchor ts=%d, want 0 (full walk)", ts)
	}
}

// syncCounted syncs c and returns what the sync moved, read from the
// tracer's closing STATS line (what kv -v prints).
func syncCounted(t *testing.T, c *Client) (roundTrips, pushed, pulled int) {
	t.Helper()
	var stats string
	c.trace = func(s string) {
		if strings.HasPrefix(s, "STATS:") {
			stats = s
		}
	}
	defer func() { c.trace = nil }()
	err := c.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var b int
	if _, err := fmt.Sscanf(stats, "STATS: pushed %d entries (%d B), pulled %d entries (%d B); %d round-trips",
		&pushed, &b, &pulled, &b, &roundTrips); err != nil {
		t.Fatalf("STATS line %q: %v", stats, err)
	}
	return roundTrips, pushed, pulled
}

// TestSync_DeltaWindow_MovesOnlyRecent seeds a vault with many entries
// older than the reconcile window, converges, then makes ONE recent
// write and syncs — asserting that only the recent entry moves, not
// the whole vault.
func TestSync_DeltaWindow_MovesOnlyRecent(t *testing.T) {
	q := server.DefaultQuotas()
	q.TSSkewMs = 40 * 24 * 60 * 60 * 1000 // 40 days, so old entries are accepted
	srv, err := server.New(server.Options{Quotas: q, RegisterKeys: []vault.RegisterKey{testRegKey}})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	aead, _ := rk.AEAD()

	now := uint64(time.Now().UnixMilli())
	day := uint64(24 * 60 * 60 * 1000)
	const nOld = 20
	for i := range nOld {
		ts := now - (2+uint64(i))*day // 2..21 days ago (all older than the 24h window)
		e, err := vault.Seal(aead, ts, []byte(fmt.Sprintf("old%02d", i)), []byte("\x00v"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Store().Add(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Sync(context.Background()); err != nil { // seeds the server (full walk)
		t.Fatal(err)
	}

	// One recent write, then sync — should move only the new entry.
	if err := c.Put([]byte("newkey"), []byte("newval")); err != nil {
		t.Fatal(err)
	}
	if _, pushed, pulled := syncCounted(t, c); pushed > 2 || pulled > 2 {
		t.Fatalf("windowed sync moved too much: pushed=%d pulled=%d (want a handful, not ~%d)",
			pushed, pulled, nOld+1)
	}

	// Convergence: a fresh reader still sees all 21 keys.
	reader := newClient(t, srv, rk)
	if err := reader.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	all, _ := reader.All()
	if len(all) != nOld+1 {
		t.Fatalf("reader sees %d keys, want %d", len(all), nOld+1)
	}
}

// TestSync_FastPathLocalWrite verifies that after convergence, a single
// local write syncs by pushing just that entry and a matching probe —
// no pull, no window walk.
func TestSync_FastPathLocalWrite(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	for i := range 5 {
		if err := c.Put([]byte(fmt.Sprintf("k%d", i)), []byte("v")); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Sync(context.Background()); err != nil { // converge
		t.Fatal(err)
	}

	if err := c.Put([]byte("extra"), []byte("val")); err != nil {
		t.Fatal(err)
	}
	if _, pushed, pulled := syncCounted(t, c); pushed != 1 || pulled != 0 {
		t.Fatalf("fast path: pushed=%d pulled=%d, want 1 pushed / 0 pulled", pushed, pulled)
	}
}

// flaky is inProc that fails while down is set.
type flaky struct {
	inProc
	down bool
}

func (f *flaky) Do(ctx context.Context, req []byte) ([]byte, error) {
	if f.down {
		return nil, errors.New("connection refused")
	}
	return f.inProc.Do(ctx, req)
}

// TestSync_FailedSyncKeepsFastPath: a write whose sync fails goes out
// on the next sync's fast path, not only by a window walk.
func TestSync_FailedSyncKeepsFastPath(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	tr := &flaky{inProc: inProc{srv}}
	c, err := New(rk, store.NewMemStore(), tr, Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Sync(context.Background()); err != nil { // register
		t.Fatal(err)
	}
	if err := c.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	tr.down = true
	if err := c.Sync(context.Background()); err == nil {
		t.Fatal("sync with the transport down succeeded")
	}
	tr.down = false
	// The fast path is PUSH then a matching PROBE; a walk would take
	// PROBE, PULL, PUSH, PROBE.
	if trips, pushed, _ := syncCounted(t, c); trips != 2 || pushed != 1 {
		t.Fatalf("retry: %d round trips, pushed=%d; want the fast path's 2 and 1", trips, pushed)
	}
}

func TestReplay_SkipsUnopenableEntry(t *testing.T) {
	srv := newTestServer(t)
	rk, _ := vault.NewRandomRootKey()
	c := newClient(t, srv, rk)
	if err := c.Put([]byte("good"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	// Inject a garbage entry that cannot open.
	garbage := vault.Entry{
		TsMs:  1,
		Nonce: [vault.NonceSize]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
		Ct:    bytes.Repeat([]byte{0xCC}, 32),
	}
	if _, err := c.Store().Add(garbage); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := c.Get([]byte("good")); err != nil || !ok || !bytes.Equal(v, []byte("v")) {
		t.Fatalf("good key lost: ok=%v v=%q err=%v", ok, v, err)
	}
}

// tryLater answers every frame with the TRY_LATER byte.
type tryLater struct{}

func (tryLater) Do(context.Context, []byte) ([]byte, error) { return wire.ErrTryLater.Reply(), nil }

// TestSync_PreChannelIsAnError: a server's pre-channel byte reaches the
// caller as that wire.PreChannel, named by the step that drew it.
func TestSync_PreChannelIsAnError(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	c, err := New(rk, store.NewMemStore(), tryLater{}, Options{Registered: true})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Sync(context.Background())
	if !errors.Is(err, wire.ErrTryLater) {
		t.Fatalf("sync error %v, want TRY_LATER", err)
	}
	if want := "client: probe: server returned try-later"; err.Error() != want {
		t.Errorf("error text %q, want %q", err, want)
	}
}

// replayAll is the Merge of PROTOCOL.t done the long way: open every
// entry in canonical order, later overriding earlier.
func replayAll(t *testing.T, rk vault.RootKey, s store.Store) map[string]string {
	t.Helper()
	aead, err := rk.AEAD()
	if err != nil {
		t.Fatal(err)
	}
	view := map[string]string{}
	for _, e := range s.Sorted() {
		k, sv, err := vault.Open(aead, e)
		if err != nil {
			continue
		}
		if _, v, tomb := ParseValue(sv); tomb {
			delete(view, string(k))
		} else {
			view[string(k)] = string(v)
		}
	}
	return view
}

// TestView_MatchesFullReplay: the kept view equals a full replay after
// every arrival, whatever order entries arrive in -- shuffled
// timestamps, ties, tombstones, and entries that do not open.
func TestView_MatchesFullReplay(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	other, _ := vault.NewRandomRootKey()
	aead, _ := rk.AEAD()
	otherAEAD, _ := other.AEAD()
	var entries []vault.Entry
	for i := range 80 {
		a := aead
		if i%11 == 0 {
			a = otherAEAD // does not open under rk
		}
		var value []byte
		if i%7 != 0 { // every seventh a tombstone
			value = WrapValue(fmtRaw, fmt.Appendf(nil, "v%d", i))
		}
		e, err := vault.Seal(a, uint64(1000+(i*37)%23), []byte{'a' + byte(i%3)}, value)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	for i := len(entries) - 1; i > 0; i-- {
		j := (i * 7919) % (i + 1)
		entries[i], entries[j] = entries[j], entries[i]
	}
	s := store.NewMemStore()
	c, err := New(rk, s, tryLater{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		s.Add(e)
		all, _ := c.All()
		got := map[string]string{}
		for _, p := range all {
			got[string(p.Key)] = string(p.Value)
		}
		if want := replayAll(t, rk, s); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("after %d arrivals: view %v, full replay %v", i+1, got, want)
		}
	}
}
