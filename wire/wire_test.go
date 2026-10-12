package wire

import (
	"bytes"
	"errors"
	"testing"

	"repani.com/kv/vault"
)

func mkEntry(t *testing.T, rk vault.RootKey, ts uint64, key, value string) vault.Entry {
	t.Helper()
	aead, err := rk.AEAD()
	if err != nil {
		t.Fatal(err)
	}
	e, err := vault.Seal(aead, ts, []byte(key), []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func testKeys(t *testing.T) (vault.VaultID, vault.ChannelKey, [16]byte) {
	t.Helper()
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	nonce, err := vault.FrameNonce(vault.DeriveVaultID(rk))
	if err != nil {
		t.Fatal(err)
	}
	return vault.DeriveVaultID(rk), vault.DeriveC2S(ak), nonce
}

func TestFrame_SealOpenRoundTrip(t *testing.T) {
	vid, k, nonce := testKeys(t)
	payload := []byte("hello payload")
	want := Header{Kind: KindSync, VaultID: vid, Nonce: nonce}
	frame, err := SealFrame(want, k, payload)
	if err != nil {
		t.Fatal(err)
	}
	h, sealed, err := ParseHeader(frame)
	if err != nil {
		t.Fatal(err)
	}
	if h != want {
		t.Fatal("header round-trip mismatch")
	}
	got, err := OpenFrame(k, h, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: %q", got)
	}
}

func TestFrame_WrongKeyFails(t *testing.T) {
	vid, k, nonce := testKeys(t)
	frame, _ := SealFrame(Header{KindSync, vid, nonce}, k, []byte("x"))
	h, sealed, _ := ParseHeader(frame)
	var wrong vault.ChannelKey
	wrong[0] = k[0] ^ 1
	if _, err := OpenFrame(wrong, h, sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen under wrong key, got %v", err)
	}
}

// TestFrame_KindBoundInAD verifies that flipping the kind byte in the
// header breaks the open — direction/type cannot be forged.
func TestFrame_KindBoundInAD(t *testing.T) {
	vid, k, nonce := testKeys(t)
	frame, _ := SealFrame(Header{KindSync, vid, nonce}, k, []byte("x"))
	frame[1] = KindResp // flip kind
	h, sealed, err := ParseHeader(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFrame(k, h, sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen after kind tamper, got %v", err)
	}
}

func TestFrame_VaultIDBoundInAD(t *testing.T) {
	vid, k, nonce := testKeys(t)
	frame, _ := SealFrame(Header{KindSync, vid, nonce}, k, []byte("x"))
	frame[2] ^= 1 // flip a vault_id byte
	h, sealed, _ := ParseHeader(frame)
	if _, err := OpenFrame(k, h, sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("expected ErrOpen after vault_id tamper, got %v", err)
	}
}

func TestFrame_BadVersion(t *testing.T) {
	vid, k, nonce := testKeys(t)
	frame, _ := SealFrame(Header{KindSync, vid, nonce}, k, []byte("x"))
	frame[0] = 0xFF
	if _, _, err := ParseHeader(frame); !errors.Is(err, ErrVersion) {
		t.Fatalf("expected ErrVersion, got %v", err)
	}
}

func TestFrame_Short(t *testing.T) {
	if _, _, err := ParseHeader([]byte{0x01, 0x02}); !errors.Is(err, ErrShort) {
		t.Fatalf("expected ErrShort, got %v", err)
	}
}

func TestSyncReq_RoundTrips(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	e1 := mkEntry(t, rk, 100, "k1", "v1")
	e2 := mkEntry(t, rk, 200, "k2", "v2")

	cases := []SyncReq{
		{Op: OpProbe, SetHash: [16]byte{1, 2, 3}},
		{Op: OpPull, CurTs: 42, CurID: e1.ID()},
		{Op: OpPush, Entries: []vault.Entry{e1, e2}},
	}
	for _, want := range cases {
		got, err := ParseSync(want.Marshal())
		if err != nil {
			t.Fatalf("op 0x%02x parse: %v", want.Op, err)
		}
		if got.Op != want.Op {
			t.Fatalf("op mismatch")
		}
		switch want.Op {
		case OpProbe:
			if got.SetHash != want.SetHash {
				t.Fatal("set_hash mismatch")
			}
		case OpPull:
			if got.CurTs != want.CurTs || got.CurID != want.CurID {
				t.Fatal("cursor mismatch")
			}
		case OpPush:
			if len(got.Entries) != 2 || got.Entries[0].ID() != e1.ID() || got.Entries[1].ID() != e2.ID() {
				t.Fatal("push entries mismatch")
			}
		}
	}
}

func TestResp_RoundTrip(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	e := mkEntry(t, rk, 1, "k", "v")
	var reqNonce [vault.NonceSize]byte
	for i := range reqNonce {
		reqNonce[i] = byte(i)
	}
	want := Resp{Status: StatusOK, ReqNonce: reqNonce, Entries: []vault.Entry{e}}
	got, err := ParseResp(want.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusOK || got.ReqNonce != reqNonce || len(got.Entries) != 1 {
		t.Fatalf("resp round-trip mismatch: %+v", got)
	}
	if got.Entries[0].ID() != e.ID() {
		t.Fatal("resp entry mismatch")
	}
}

func TestParseSync_BadOp(t *testing.T) {
	if _, err := ParseSync([]byte{0x7f}); !errors.Is(err, ErrBadOp) {
		t.Fatalf("expected ErrBadOp, got %v", err)
	}
}
