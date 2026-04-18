package wire

import (
	"bytes"
	"errors"
	"testing"

	"github.com/pavlos/kv/vault"
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

func TestRequest_RoundTrip_Empty(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	r := Request{
		VaultID: vault.DeriveVaultID(rk),
		N:       0,
	}
	signed, err := SignRequest(ak, &r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRequest(signed)
	if err != nil {
		t.Fatal(err)
	}
	if got.VaultID != r.VaultID {
		t.Fatal("VaultID mismatch")
	}
	if got.N != 0 {
		t.Fatal("N mismatch")
	}
	if len(got.Ladder) != 0 || len(got.Pushes) != 0 {
		t.Fatal("expected empty ladder and pushes")
	}
	if err := VerifyRequestMAC(ak, got); err != nil {
		t.Fatalf("MAC verify: %v", err)
	}
}

func TestRequest_RoundTrip_WithLadderAndPushes(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	// N=3 ⇒ progressive sizes {1, 2}. Terminal is a separate field.
	ladder := [][16]byte{{1}, {2}}
	terminal := [16]byte{9, 9, 9}
	e1 := mkEntry(t, rk, 100, "k1", "v1")
	e2 := mkEntry(t, rk, 200, "k2", "v2")
	r := Request{
		VaultID:  vault.DeriveVaultID(rk),
		N:        3,
		Ladder:   ladder,
		Terminal: terminal,
		Pushes:   []vault.Entry{e1, e2},
	}
	signed, err := SignRequest(ak, &r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRequest(signed)
	if err != nil {
		t.Fatal(err)
	}
	if got.N != 3 || len(got.Ladder) != 2 {
		t.Fatalf("ladder len %d N=%d", len(got.Ladder), got.N)
	}
	if got.Terminal != terminal {
		t.Fatal("terminal round-trip mismatch")
	}
	if len(got.Pushes) != 2 {
		t.Fatalf("got %d pushes, want 2", len(got.Pushes))
	}
	if got.Pushes[0].ID() != e1.ID() || got.Pushes[1].ID() != e2.ID() {
		t.Fatal("pushes out of order or corrupt")
	}
	if err := VerifyRequestMAC(ak, got); err != nil {
		t.Fatalf("MAC verify: %v", err)
	}
}

func TestRequest_Register_RoundTrip(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	r := Request{
		VaultID: vault.DeriveVaultID(rk),
		Flags:   FlagRegister,
		RegAuth: ak,
	}
	signed, err := SignRequest(ak, &r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRequest(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsRegister() {
		t.Fatal("register flag lost in round-trip")
	}
	if got.RegAuth != ak {
		t.Fatal("RegAuth round-trip mismatch")
	}
	if err := VerifyRequestMAC(ak, got); err != nil {
		t.Fatalf("MAC verify: %v", err)
	}
}

func TestRequest_MACRejection(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	r := Request{
		VaultID: vault.DeriveVaultID(rk),
		N:       0,
	}
	signed, err := SignRequest(ak, &r)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a bit somewhere in the body (after the version byte).
	signed[5] ^= 1
	got, err := UnmarshalRequest(signed)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRequestMAC(ak, got); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("expected ErrBadMAC on tampered body, got %v", err)
	}
}

func TestRequest_BadVersion(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	r := Request{VaultID: vault.DeriveVaultID(rk)}
	signed, err := SignRequest(ak, &r)
	if err != nil {
		t.Fatal(err)
	}
	signed[0] = 0xFF
	if _, err := UnmarshalRequest(signed); !errors.Is(err, ErrVersion) {
		t.Fatalf("expected ErrVersion, got %v", err)
	}
}

func TestRequest_ReservedFlagsRejected(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	r := Request{
		VaultID: vault.DeriveVaultID(rk),
		Flags:   0x80, // reserved
	}
	if _, err := SignRequest(ak, &r); !errors.Is(err, ErrFlags) {
		t.Fatalf("expected ErrFlags from SignRequest, got %v", err)
	}
}

func TestRequest_Truncated(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	r := Request{
		VaultID: vault.DeriveVaultID(rk),
		N:       0,
	}
	signed, err := SignRequest(ak, &r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalRequest(signed[:len(signed)-1]); !errors.Is(err, ErrShort) {
		t.Fatalf("expected ErrShort, got %v", err)
	}
}

func TestResponse_RoundTrip(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	var reqMAC [vault.MACSize]byte
	for i := range reqMAC {
		reqMAC[i] = byte(i)
	}
	e1 := mkEntry(t, rk, 100, "k", "v1")
	e2 := mkEntry(t, rk, 200, "k", "v2")
	resp := Response{
		Status:  StatusOK,
		Window:  4,
		Entries: []vault.Entry{e1, e2},
	}
	b, err := SignResponse(ak, reqMAC, resp)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Window != 4 || len(got.Entries) != 2 {
		t.Fatalf("got %+v", got)
	}
	if !bytes.Equal(got.Entries[0].Ct, e1.Ct) {
		t.Fatal("entry ciphertext mismatch")
	}
	if err := VerifyResponseMAC(ak, reqMAC, got); err != nil {
		t.Fatalf("resp MAC verify: %v", err)
	}
}

func TestResponse_MACBindsRequest(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	var reqA, reqB [vault.MACSize]byte
	for i := range reqA {
		reqA[i] = byte(i)
		reqB[i] = byte(i) ^ 1
	}
	resp := Response{Status: StatusOK}
	b, err := SignResponse(ak, reqA, resp)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := UnmarshalResponse(b)
	if err := VerifyResponseMAC(ak, reqB, got); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("expected ErrBadMAC when reqMAC mismatches, got %v", err)
	}
}

func TestResponse_InSyncSize(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	ak := vault.DeriveAuthKey(rk)
	b, err := SignResponse(ak, [vault.MACSize]byte{}, Response{Status: StatusOK})
	if err != nil {
		t.Fatal(err)
	}
	// ver(1) + status(1) + window(4) + push_count(2) + mac(16) = 24
	if len(b) != 24 {
		t.Fatalf("in-sync response is %d bytes, want 24", len(b))
	}
}
