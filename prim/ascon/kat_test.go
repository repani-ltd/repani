package ascon

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// Official reference KAT files, copied verbatim from ascon/ascon-c
// (NIST SP 800-232). See testdata/SOURCE.md for provenance.
const (
	xofKATFile  = "testdata/LWC_XOF_KAT_128_512.txt"
	aeadKATFile = "testdata/LWC_AEAD_KAT_128_128.txt"
)

// katRecord is one blank-line-delimited block of "Field = HEX" lines.
type katRecord map[string]string

// parseKAT reads the NIST LWC KAT format: records separated by blank
// lines, each record a set of "Name = value" lines where value is hex
// (possibly empty).
func parseKAT(t *testing.T, path string) []katRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open KAT file %s: %v", path, err)
	}
	defer f.Close()

	var out []katRecord
	cur := katRecord{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // vectors run to ~1 KiB hex lines
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = katRecord{}
			}
			continue
		}
		name, val, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("%s: malformed line %q", path, line)
		}
		cur[strings.TrimSpace(name)] = strings.TrimSpace(val)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex decode %q: %v", s, err)
	}
	return b
}

// TestXOF_ReferenceKAT runs every official Ascon-XOF128 vector.
func TestXOF_ReferenceKAT(t *testing.T) {
	vecs := parseKAT(t, xofKATFile)
	if len(vecs) == 0 {
		t.Fatalf("no vectors parsed from %s", xofKATFile)
	}
	for _, v := range vecs {
		msg := mustHex(t, v["Msg"])
		want := mustHex(t, v["MD"])
		got := make([]byte, len(want))
		XOF(msg, got)
		if !bytes.Equal(got, want) {
			t.Fatalf("XOF count=%s (msglen=%d):\n got  %x\n want %x",
				v["Count"], len(msg), got, want)
		}
	}
	t.Logf("verified %d Ascon-XOF128 vectors", len(vecs))
}

// TestAEAD_ReferenceKAT runs every official Ascon-AEAD128 vector,
// checking both Seal output and the Open round trip.
func TestAEAD_ReferenceKAT(t *testing.T) {
	vecs := parseKAT(t, aeadKATFile)
	if len(vecs) == 0 {
		t.Fatalf("no vectors parsed from %s", aeadKATFile)
	}
	for _, v := range vecs {
		key := mustHex(t, v["Key"])
		nonce := mustHex(t, v["Nonce"])
		pt := mustHex(t, v["PT"])
		ad := mustHex(t, v["AD"])
		want := mustHex(t, v["CT"])

		a, err := NewAEAD(key)
		if err != nil {
			t.Fatalf("count=%s NewAEAD: %v", v["Count"], err)
		}
		got := a.Seal(nil, nonce, pt, ad)
		if !bytes.Equal(got, want) {
			t.Fatalf("AEAD Seal count=%s (ptlen=%d adlen=%d):\n got  %x\n want %x",
				v["Count"], len(pt), len(ad), got, want)
		}
		opened, err := a.Open(nil, nonce, got, ad)
		if err != nil {
			t.Fatalf("AEAD Open count=%s: %v", v["Count"], err)
		}
		if !bytes.Equal(opened, pt) {
			t.Fatalf("AEAD round-trip count=%s: plaintext mismatch", v["Count"])
		}
	}
	t.Logf("verified %d Ascon-AEAD128 vectors", len(vecs))
}
