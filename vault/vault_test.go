package vault

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func errorsIs(err, target error) bool { return errors.Is(err, target) }

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

func TestDeriveAuthKey_IndependentOfAEAD(t *testing.T) {
	rk, _ := NewRandomRootKey()
	ak := DeriveAuthKey(rk)
	aead := DeriveAEADKey(rk)
	if bytes.Equal(ak[:], aead[:]) {
		t.Fatal("AuthKey and AEAD_Key collided — domain separation broken")
	}
}

// TestChannelKeys_Distinct verifies K_c2s, K_s2c, and the AuthKey they
// derive from are all different — directional key separation is what
// prevents frame reflection and cross-direction nonce reuse.
func TestChannelKeys_Distinct(t *testing.T) {
	rk, _ := NewRandomRootKey()
	ak := DeriveAuthKey(rk)
	c2s := DeriveC2S(ak)
	s2c := DeriveS2C(ak)
	if c2s == s2c {
		t.Fatal("K_c2s == K_s2c — directional separation broken")
	}
	if bytes.Equal(c2s[:], ak[:]) || bytes.Equal(s2c[:], ak[:]) {
		t.Fatal("channel key collided with AuthKey")
	}
	// Deterministic.
	if DeriveC2S(ak) != c2s || DeriveS2C(ak) != s2c {
		t.Fatal("channel key derivation not deterministic")
	}
}

func TestRegKey_DerivedAndDeterministic(t *testing.T) {
	var reg RegisterKey
	for i := range reg {
		reg[i] = byte(0xA0 + i)
	}
	k := DeriveRegKey(reg)
	if DeriveRegKey(reg) != k {
		t.Fatal("K_reg not deterministic")
	}
	reg2 := reg
	reg2[0] ^= 1
	if DeriveRegKey(reg2) == k {
		t.Fatal("K_reg insensitive to RegisterKey bit flip")
	}
}

// TestNonces_HedgedUnique checks nonce generators return fresh values
// (the random component dominates) and are 16 bytes.
func TestNonces_HedgedUnique(t *testing.T) {
	var vid VaultID
	seen := map[[NonceSize]byte]bool{}
	for range 1000 {
		fn, err := FrameNonce(vid)
		if err != nil {
			t.Fatal(err)
		}
		en, err := EntryNonce()
		if err != nil {
			t.Fatal(err)
		}
		if seen[fn] || seen[en] {
			t.Fatal("nonce collision across fresh generations")
		}
		seen[fn] = true
		seen[en] = true
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

func TestSealOpen_MaxKeyAndValue(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()

	// key_len is 1 byte, so 255 is the max; over-long keys are rejected.
	key := bytes.Repeat([]byte{'k'}, 255)
	value := bytes.Repeat([]byte{'v'}, 4096)
	e, err := Seal(aead, 1, key, value)
	if err != nil {
		t.Fatal(err)
	}
	gotK, gotV, err := Open(aead, e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(gotK, key) || !bytes.Equal(gotV, value) {
		t.Fatal("max-key round-trip mismatch")
	}
	if _, err := Seal(aead, 1, bytes.Repeat([]byte{'k'}, 256), []byte("v")); !errorsIs(err, ErrKeyTooLong) {
		t.Fatalf("expected ErrKeyTooLong for 256-byte key, got %v", err)
	}
}

func TestSeal_RejectsOversizeEntry(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	// A value near the 2-byte value_len limit that pushes the entry over
	// the 64 KiB cap must be rejected.
	big := bytes.Repeat([]byte{'x'}, MaxEntryBytes)
	if _, err := Seal(aead, 1, []byte("k"), big); !errorsIs(err, ErrEntryTooLarge) && !errorsIs(err, ErrValueTooLong) {
		t.Fatalf("expected oversize rejection, got %v", err)
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

// --- set hash ---

func TestSetHash_DeterministicOverShuffles(t *testing.T) {
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

	if SetHash(a) != SetHash(b) {
		t.Fatal("SetHash differs across permutations of the same set")
	}
}

func TestSetHash_ChangesOnInsert(t *testing.T) {
	rk, _ := NewRandomRootKey()
	aead, _ := rk.AEAD()
	var entries []Entry
	for i := range 4 {
		e, _ := Seal(aead, uint64(i*1000), []byte{byte('a' + i)}, []byte{byte(i)})
		entries = append(entries, e)
	}
	SortCanonical(entries)
	before := SetHash(entries)

	extra, _ := Seal(aead, 500, []byte("x"), []byte("x"))
	entries = append(entries, extra)
	SortCanonical(entries)
	after := SetHash(entries)

	if before == after {
		t.Fatal("SetHash unchanged after insert — count/id binding broken")
	}
}

func TestSetHash_CountSensitive(t *testing.T) {
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
	if SetHash(a) == SetHash(b) {
		t.Fatal("SetHash collided across sets of different size — count not bound into hash")
	}
}

func TestSetHash_EmptyStable(t *testing.T) {
	if SetHash(nil) != SetHash([]Entry{}) {
		t.Fatal("SetHash of the empty set is not stable")
	}
}

// --- pairing ---

func TestPairing_RoundTrip(t *testing.T) {
	rk, _ := NewRandomRootKey()
	enc := EncodeRootKey(rk)
	if len(enc) != 32 {
		t.Fatalf("encoded length %d, want 32", len(enc))
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

func TestRegisterKey_RoundTrip(t *testing.T) {
	var rk RegisterKey
	for i := range rk {
		rk[i] = byte(i * 7)
	}
	enc := EncodeRegisterKey(rk)
	if len(enc) != 26 {
		t.Fatalf("EncodeRegisterKey length %d, want 26", len(enc))
	}
	got, err := DecodeRegisterKey(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != rk {
		t.Fatal("RegisterKey round-trip mismatch")
	}
}

func TestRegisterKey_AliasesAndWhitespace(t *testing.T) {
	var rk RegisterKey
	for i := range rk {
		rk[i] = byte(0xA0 ^ i)
	}
	enc := EncodeRegisterKey(rk)
	// Inject some whitespace and dashes that decoder must ignore.
	messy := enc[:5] + "  " + enc[5:13] + "\n" + "-" + enc[13:]
	got, err := DecodeRegisterKey(messy)
	if err != nil {
		t.Fatal(err)
	}
	if got != rk {
		t.Fatal("whitespace-tolerant decode produced wrong key")
	}
}

func TestBackup_RoundTrip_WithBoth(t *testing.T) {
	var rk RootKey
	for i := range rk {
		rk[i] = byte(0x11 * (i + 1))
	}
	var reg RegisterKey
	for i := range reg {
		reg[i] = byte(0x22 * (i + 1))
	}
	blob := EncodeBackup(rk, &reg)
	if len(blob) != 58 {
		t.Fatalf("backup length %d, want 58", len(blob))
	}
	gotRK, gotReg, err := DecodeBackup(blob)
	if err != nil {
		t.Fatal(err)
	}
	if gotRK != rk {
		t.Fatal("backup RootKey round-trip mismatch")
	}
	if gotReg == nil || *gotReg != reg {
		t.Fatal("backup RegisterKey round-trip mismatch")
	}
}

func TestBackup_RoundTrip_RootOnly(t *testing.T) {
	var rk RootKey
	for i := range rk {
		rk[i] = byte(0xAA + i)
	}
	blob := EncodeBackup(rk, nil)
	if len(blob) != 32 {
		t.Fatalf("root-only backup length %d, want 32", len(blob))
	}
	gotRK, gotReg, err := DecodeBackup(blob)
	if err != nil {
		t.Fatal(err)
	}
	if gotRK != rk {
		t.Fatal("root-only backup RootKey mismatch")
	}
	if gotReg != nil {
		t.Fatal("root-only backup returned a non-nil RegisterKey")
	}
}

func TestBackup_RejectsWrongLength(t *testing.T) {
	if _, _, err := DecodeBackup("tooshort"); err == nil {
		t.Fatal("DecodeBackup should reject short input")
	}
	// 65 chars: valid Crockford alphabet but not 32 nor 58.
	bad := strings.Repeat("A", 65)
	if _, _, err := DecodeBackup(bad); err == nil {
		t.Fatal("DecodeBackup should reject a 65-char blob")
	}
}

func TestRegisterKey_RejectsPaddingBits(t *testing.T) {
	var rk RegisterKey
	enc := EncodeRegisterKey(rk) // all zeros
	// Flip a low bit of the last char — violates the 2-bit padding
	// invariant (low 2 bits of the last 10-bit accumulator must be 0).
	mangled := enc[:25] + string(crockford[1])
	if _, err := DecodeRegisterKey(mangled); err == nil {
		t.Fatal("decode should reject non-zero padding bits")
	}
}

func TestPairing_RejectsTruncation(t *testing.T) {
	rk, _ := NewRandomRootKey()
	enc := EncodeRootKey(rk)
	if _, err := DecodeRootKey(enc[:len(enc)-1]); err == nil {
		t.Fatal("expected error on truncated blob")
	}
}
