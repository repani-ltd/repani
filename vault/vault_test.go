package vault

import (
	"bytes"
	"strings"
	"testing"
)

// --- key derivation ---

func TestDeriveAEADKey_Deterministic(t *testing.T) {
	var rk RootKey
	for i := range rk {
		rk[i] = byte(i)
	}
	a := DeriveAEADKey(rk)
	b := DeriveAEADKey(rk)
	if a != b {
		t.Fatal("AEAD derivation not deterministic")
	}
}

func TestDeriveVaultID_DiffersFromAEAD(t *testing.T) {
	var rk RootKey
	for i := range rk {
		rk[i] = byte(i)
	}
	aead := DeriveAEADKey(rk)
	vid := DeriveVaultID(rk)
	if bytes.Equal(aead[:], vid[:]) {
		t.Fatal("AEAD_Key and VaultID collided — domain separation broken")
	}
}

func TestRequestMAC_SensitiveToBody(t *testing.T) {
	rk, _ := NewRandomRootKey()
	ak := DeriveAuthKey(rk)
	m1 := RequestMAC(ak, []byte("hello"))
	m2 := RequestMAC(ak, []byte("hellp"))
	if m1 == m2 {
		t.Fatal("RequestMAC collision on single-byte diff")
	}
}

func TestDeriveAuthKey_IndependentOfAEAD(t *testing.T) {
	rk, _ := NewRandomRootKey()
	ak := DeriveAuthKey(rk)
	aead := DeriveAEADKey(rk)
	if bytes.Equal(ak[:], aead[:]) {
		t.Fatal("AuthKey and AEAD_Key collided — domain separation broken")
	}
}

func TestRegisterAdminMAC_BindsAllInputs(t *testing.T) {
	var rk RegisterKey
	for i := range rk {
		rk[i] = byte(0xA0 + i)
	}
	var ak AuthKey
	for i := range ak {
		ak[i] = byte(0xB0 + i)
	}
	var vid VaultID
	for i := range vid {
		vid[i] = byte(0xC0 + i)
	}
	base := RegisterAdminMAC(rk, ak, vid)

	// Same inputs → same output.
	if RegisterAdminMAC(rk, ak, vid) != base {
		t.Fatal("RegisterAdminMAC not deterministic")
	}

	// Flip one byte in the register key → different output.
	rk2 := rk
	rk2[0] ^= 1
	if RegisterAdminMAC(rk2, ak, vid) == base {
		t.Fatal("RegisterAdminMAC insensitive to RegisterKey bit flip")
	}

	// Flip one byte in the auth key → different output.
	ak2 := ak
	ak2[0] ^= 1
	if RegisterAdminMAC(rk, ak2, vid) == base {
		t.Fatal("RegisterAdminMAC insensitive to AuthKey bit flip")
	}

	// Flip one byte in the vault id → different output.
	vid2 := vid
	vid2[0] ^= 1
	if RegisterAdminMAC(rk, ak, vid2) == base {
		t.Fatal("RegisterAdminMAC insensitive to VaultID bit flip")
	}
}

func TestResponseMAC_BindsRequestMAC(t *testing.T) {
	rk, _ := NewRandomRootKey()
	ak := DeriveAuthKey(rk)
	var reqA, reqB [MACSize]byte
	for i := range reqA {
		reqA[i] = byte(i)
		reqB[i] = byte(i) ^ 1
	}
	body := []byte("response-body")
	ma := ResponseMAC(ak, reqA, body)
	mb := ResponseMAC(ak, reqB, body)
	if ma == mb {
		t.Fatal("ResponseMAC did not bind request MAC — cache-stitching possible")
	}
}

// --- entry seal / open / id ---

func TestSealOpen_RoundTrip(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, err := rk.AEAD()
	if err != nil {
		t.Fatal(err)
	}
	e, err := Seal(aead, 1_700_000_000_000, []byte("mykey"), []byte("s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	k, v, err := Open(aead, e)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(k, []byte("mykey")) || !bytes.Equal(v, []byte("s3cret")) {
		t.Fatalf("round trip mismatch: key=%q value=%q", k, v)
	}
}

func TestSealOpen_MaxKey(t *testing.T) {
	// Regression: a plaintext with klen near 2^16 used to wrap the
	// bounds check in Open (uint16 arithmetic) and panic on the
	// slice. Open must now reject or handle cleanly.
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	key := bytes.Repeat([]byte{'k'}, 65535)
	e, err := Seal(aead, 1, key, []byte("v"))
	if err != nil {
		t.Fatal(err)
	}
	gotK, gotV, err := Open(aead, e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(gotK, key) || !bytes.Equal(gotV, []byte("v")) {
		t.Fatal("max-key round-trip mismatch")
	}
}

func TestSealOpen_Tombstone(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	e, err := Seal(aead, 42, []byte("mykey"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, v, err := Open(aead, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 0 {
		t.Fatalf("expected empty value (tombstone), got %q", v)
	}
}

func TestEntry_MarshalRoundTrip(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	e, _ := Seal(aead, 1234, []byte("k"), []byte("v"))
	b := e.Marshal()
	got, n, err := UnmarshalEntry(b)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(b) {
		t.Fatalf("UnmarshalEntry consumed %d, marshaled %d", n, len(b))
	}
	if got.TsMs != e.TsMs || got.Nonce != e.Nonce || !bytes.Equal(got.Ct, e.Ct) {
		t.Fatalf("entry round-trip mismatch")
	}
	if got.ID() != e.ID() {
		t.Fatal("EntryID changed after round trip")
	}
}

func TestEntryID_ContentAddressed(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	e1, _ := Seal(aead, 1, []byte("a"), []byte("1"))
	e2 := Entry{TsMs: e1.TsMs, Nonce: e1.Nonce, Ct: append([]byte(nil), e1.Ct...)}
	if e1.ID() != e2.ID() {
		t.Fatal("same bytes, different EntryID")
	}
	e2.Ct[0] ^= 1
	if e1.ID() == e2.ID() {
		t.Fatal("byte flip did not change EntryID")
	}
}

// --- ladder ---

func TestLadderSizes(t *testing.T) {
	// The progressive ladder is powers of two only; full-set coverage
	// is delivered separately by TerminalHash.
	cases := []struct {
		n    uint32
		want []uint32
	}{
		{0, nil},
		{1, []uint32{1}},
		{2, []uint32{1, 2}},
		{3, []uint32{1, 2}},
		{4, []uint32{1, 2, 4}},
		{5, []uint32{1, 2, 4}},
		{8, []uint32{1, 2, 4, 8}},
		{1000, []uint32{1, 2, 4, 8, 16, 32, 64, 128, 256, 512}},
	}
	for _, tc := range cases {
		got := LadderSizes(tc.n)
		if len(got) != len(tc.want) {
			t.Fatalf("LadderSizes(%d)=%v want %v", tc.n, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("LadderSizes(%d)[%d]=%d want %d", tc.n, i, got[i], tc.want[i])
			}
		}
	}
}

func TestLadder_DeterministicOverShuffles(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	var entries []Entry
	for i := range 10 {
		e, _ := Seal(aead, uint64(i*1000), []byte{byte('a' + i)}, []byte{byte(i)})
		entries = append(entries, e)
	}
	a := append([]Entry(nil), entries...)
	b := append([]Entry(nil), entries...)
	// Reverse b so ordering-dependent bugs would show up.
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	SortCanonical(a)
	SortCanonical(b)

	la := ComputeLadder(a)
	lb := ComputeLadder(b)
	if len(la) != len(lb) {
		t.Fatalf("ladder length mismatch: %d vs %d", len(la), len(lb))
	}
	for i := range la {
		if la[i] != lb[i] {
			t.Fatalf("ladder[%d] differs across permutations", i)
		}
	}
}

func TestLadder_TerminalChangesOnInsert(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	var entries []Entry
	for i := range 4 {
		e, _ := Seal(aead, uint64(i*1000), []byte{byte('a' + i)}, []byte{byte(i)})
		entries = append(entries, e)
	}
	SortCanonical(entries)
	beforeTerm := TerminalHash(entries)

	extra, _ := Seal(aead, 500, []byte("x"), []byte("x"))
	entries = append(entries, extra)
	SortCanonical(entries)
	afterTerm := TerminalHash(entries)

	if beforeTerm == afterTerm {
		t.Fatal("TerminalHash unchanged after insert — count/id binding broken")
	}
}

func TestTerminalHash_CountSensitive(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	var a, b []Entry
	for i := range 5 {
		e, _ := Seal(aead, uint64(i+1), []byte{byte('a' + i)}, []byte{byte(i)})
		a = append(a, e)
	}
	// b has the same top 5 as a plus one backdated entry at ts=0.
	backdated, _ := Seal(aead, 0, []byte("x"), []byte("x"))
	b = append(b, backdated)
	b = append(b, a...)
	SortCanonical(a)
	SortCanonical(b)
	if TerminalHash(a) == TerminalHash(b) {
		t.Fatal("TerminalHash collided across sets of different size — count not bound into hash")
	}
}

// --- pairing ---

func TestPairing_RoundTrip(t *testing.T) {
	rk, _ := NewRandomRootKey()
	enc := EncodeRootKey(rk)
	if len(enc) != 52 {
		t.Fatalf("encoded length %d, want 52", len(enc))
	}
	dec, err := DecodeRootKey(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != rk {
		t.Fatal("pairing blob round-trip mismatch")
	}
}

func TestPairing_AliasesAccepted(t *testing.T) {
	rk, _ := NewRandomRootKey()
	enc := EncodeRootKey(rk)
	var b strings.Builder
	for _, c := range enc {
		switch c {
		case '0':
			b.WriteByte('O')
		case '1':
			b.WriteByte('I')
		default:
			b.WriteRune(c)
		}
	}
	dec, err := DecodeRootKey(b.String())
	if err != nil {
		t.Fatalf("DecodeRootKey with aliases failed: %v", err)
	}
	if dec != rk {
		t.Fatal("alias decode produced wrong key")
	}
}

func TestPairing_LowercaseAccepted(t *testing.T) {
	rk, _ := NewRandomRootKey()
	enc := EncodeRootKey(rk)
	dec, err := DecodeRootKey(strings.ToLower(enc))
	if err != nil {
		t.Fatalf("lowercase decode failed: %v", err)
	}
	if dec != rk {
		t.Fatal("lowercase decode produced wrong key")
	}
}

func TestPairing_RejectsTruncation(t *testing.T) {
	rk, _ := NewRandomRootKey()
	enc := EncodeRootKey(rk)
	if _, err := DecodeRootKey(enc[:len(enc)-1]); err == nil {
		t.Fatal("expected error on truncated blob")
	}
}
