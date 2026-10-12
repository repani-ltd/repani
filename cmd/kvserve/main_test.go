package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"repani.com/kv/server"
	"repani.com/kv/vault"
)

// TestParseVerb pins the dispatch. The case that matters is "serve":
// the sibling daemon on this host requires that verb in its unit file
// and kvserve used to reject it, so a unit copied between the two
// failed at ExecStart.
func TestParseVerb(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		verb string
		rest []string
	}{
		{"no arguments", nil, "serve", nil},
		{"flags alone", []string{"--addr", ":80"}, "serve", []string{"--addr", ":80"}},
		{"single-dash flags", []string{"-addr", ":80"}, "serve", []string{"-addr", ":80"}},
		{"explicit verb", []string{"serve", "--addr", ":80"}, "serve", []string{"--addr", ":80"}},
		{"explicit verb alone", []string{"serve"}, "serve", []string{}},
		{"gen-register-key", []string{"gen-register-key"}, "gen-register-key", []string{}},
		{"drop-vault", []string{"drop-vault", "--data", "d", "ab"}, "drop-vault", []string{"--data", "d", "ab"}},
		{"help", []string{"help"}, "help", []string{}},
		{"unknown", []string{"sevre"}, "sevre", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verb, rest := parseVerb(tc.args)
			if verb != tc.verb {
				t.Errorf("verb = %q, want %q", verb, tc.verb)
			}
			if !slices.Equal(rest, tc.rest) {
				t.Errorf("rest = %q, want %q", rest, tc.rest)
			}
		})
	}
}

// TestServeFlagsCarryQuotaDefaults keeps help honest: the defaults it
// prints are the ones the daemon runs with.
func TestServeFlagsCarryQuotaDefaults(t *testing.T) {
	fs, o := serveFlags()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if o.quotas != server.DefaultQuotas() {
		t.Errorf("quotas = %+v, want %+v", o.quotas, server.DefaultQuotas())
	}
	// TSSkewMs has no flag, so it must survive from the defaults.
	if o.quotas.TSSkewMs == 0 {
		t.Error("TSSkewMs was lost: no flag sets it and the default did not carry")
	}
}

// TestReadRegisterKeys: keys one per line, with labels and blank lines
// between; a file with none, or with a line that is not a key, is an
// error.
func TestReadRegisterKeys(t *testing.T) {
	a := vault.RegisterKey{1, 2, 3}
	b := vault.RegisterKey{16, 15, 14}
	write := func(s string) string {
		path := filepath.Join(t.TempDir(), "register.key")
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	keys, err := readRegisterKeys(write("# operator\n" + vault.EncodeRegisterKey(a) + "\n\n  # iou\n" + vault.EncodeRegisterKey(b) + "\n"))
	if err != nil || !slices.Equal(keys, []vault.RegisterKey{a, b}) {
		t.Fatalf("keys %v, %v; want a then b", keys, err)
	}
	for name, file := range map[string]string{
		"empty":        "",
		"only comment": "# iou\n\n",
		"bad line":     vault.EncodeRegisterKey(a) + "\nnot-a-key\n",
	} {
		if _, err := readRegisterKeys(write(file)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
