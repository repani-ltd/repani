package row

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func seed(f *testing.F, bin bool) {
	v := vectors{}
	func() {
		defer func() { recover() }()
		t := &testing.T{}
		v = load(t)
	}()
	for _, c := range v.Valid {
		if bin {
			b, _ := hex.DecodeString(c.Hex)
			f.Add(b)
		} else {
			f.Add([]byte(c.Text))
		}
	}
	for _, c := range v.Lenient {
		if !bin {
			f.Add([]byte(c.Text))
		}
	}
}

// FuzzDecode: bytes that decode re-encode to themselves (ONE ENCODING),
// and their text parses back to the same bytes.
func FuzzDecode(f *testing.F) {
	seed(f, true)
	f.Fuzz(func(t *testing.T, b []byte) {
		r, err := Decode(b)
		if err != nil {
			if _, ok := code(err); !ok {
				t.Fatalf("decode %x: an error without a code: %v", b, err)
			}
			return
		}
		again, err := Encode(r)
		if err != nil || !bytes.Equal(again, b) {
			t.Fatalf("encode(decode(%x)) = %x, %v", b, again, err)
		}
		text, err := Format(r)
		if err != nil {
			t.Fatalf("format(decode(%x)): %v", b, err)
		}
		p, err := Parse(text)
		if err != nil {
			t.Fatalf("parse(format(decode(%x))) %q: %v", b, text, err)
		}
		if got, _ := Encode(p); !bytes.Equal(got, b) {
			t.Fatalf("text %q of %x encodes to %x", text, b, got)
		}
	})
}

// FuzzParse: text that parses formats to a canonical text that parses
// to the same row and formats to itself.
func FuzzParse(f *testing.F) {
	seed(f, false)
	f.Fuzz(func(t *testing.T, in []byte) {
		r, err := Parse(string(in))
		if err != nil {
			if _, ok := code(err); !ok {
				t.Fatalf("parse %q: an error without a code: %v", in, err)
			}
			return
		}
		b, err := Encode(r)
		if err != nil {
			t.Fatalf("encode(parse(%q)): %v", in, err)
		}
		text, err := Format(r)
		if err != nil {
			t.Fatalf("format(parse(%q)): %v", in, err)
		}
		p, err := Parse(text)
		if err != nil {
			t.Fatalf("canonical %q does not parse: %v", text, err)
		}
		if again, _ := Format(p); again != text {
			t.Fatalf("canonical %q formats to %q", text, again)
		}
		if got, _ := Encode(p); !bytes.Equal(got, b) {
			t.Fatalf("canonical %q of %q encodes to %x, want %x", text, in, got, b)
		}
	})
}
