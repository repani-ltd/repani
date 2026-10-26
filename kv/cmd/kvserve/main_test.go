package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"repani.com/kv/server"
	"repani.com/kv/vault"
)

// TestParseVerb pins the dispatch: serving has no verb, so flags alone
// (or nothing) serve, and anything else names a subcommand.
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
		{"gen-register-key", []string{"gen-register-key", "iou"}, "gen-register-key", []string{"iou"}},
		{"drop-vault", []string{"drop-vault", "--data", "d", "ab"}, "drop-vault", []string{"--data", "d", "ab"}},
		{"help", []string{"help"}, "help", []string{}},
		{"unknown", []string{"serve"}, "serve", []string{}},
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

// TestReadRegisterKeys: the lines gen-register-key prints make a file
// that reads back as their keys and labels; a file with none, one that
// is not FACT, or one with another fact, is an error.
func TestReadRegisterKeys(t *testing.T) {
	a := vault.RegisterKey{1, 2, 3}
	b := vault.RegisterKey{16, 15, 14}
	write := func(s string) string {
		path := filepath.Join(t.TempDir(), "register_keys.fact")
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	file := registerKeyLine("operator", a) + "\n\n" + registerKeyLine("iou", b) + "\n"
	keys, labels, err := readRegisterKeys(write(file))
	if err != nil || !slices.Equal(keys, []vault.RegisterKey{b, a}) || !slices.Equal(labels, []string{"iou", "operator"}) {
		t.Fatalf("keys %v, labels %v, %v; want iou's then operator's", keys, labels, err)
	}
	for name, file := range map[string]string{
		"empty":       "",
		"not fact":    vault.EncodeRegisterKey(a) + "\n",
		"comment":     "# iou\n" + registerKeyLine("iou", a) + "\n",
		"other fact":  registerKeyLine("iou", a) + "\nserver.port: int = 80\n",
		"not a key":   `register_key:iou.key: str = "not-a-key"` + "\n",
		"wrong type":  `register_key:iou.key: str? = none` + "\n",
		"deeper path": `register_key:iou.key.old: str = "x"` + "\n",
	} {
		if _, _, err := readRegisterKeys(write(file)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestDropVault: a served vault is refused; once the server stops, a
// drop ends it, a second drop changes nothing, and a vault this server
// never held is revoked all the same; a server started afterwards
// never creates either again.
func TestDropVault(t *testing.T) {
	data := t.TempDir()
	reg := vault.RegisterKey{9}
	srv, err := server.New(server.Options{DataDir: data, RegisterKeys: []vault.RegisterKey{reg}})
	if err != nil {
		t.Fatal(err)
	}
	rk, _ := vault.NewRandomRootKey()
	vid, ak := vault.DeriveVaultID(rk), vault.DeriveAuthKey(rk)
	if err := srv.EnsureVault(vid, ak); err != nil {
		t.Fatal(err)
	}

	if _, err := dropVault(data, vid, "in use"); err == nil || !strings.Contains(err.Error(), "being served") {
		t.Fatalf("drop of a served vault: %v", err)
	}
	srv.Close()
	if msg, err := dropVault(data, vid, "tester  reset"); err != nil || !strings.Contains(msg, "ended: revoked") {
		t.Fatalf("drop after stop: %q, %v", msg, err)
	}
	if msg, err := dropVault(data, vid, ""); err != nil || !strings.Contains(msg, "already ended") {
		t.Fatalf("second drop: %q, %v", msg, err)
	}
	other, _ := vault.NewRandomRootKey()
	if msg, err := dropVault(data, vault.DeriveVaultID(other), "never here"); err != nil || !strings.Contains(msg, "ended: revoked") {
		t.Fatalf("drop of a vault never held: %q, %v", msg, err)
	}
	b, _ := os.ReadFile(filepath.Join(data, vid.String(), "vault.fact"))
	if !strings.Contains(string(b), `vault.note: str? = "tester reset"`) {
		t.Fatalf("record after drop:\n%s", b)
	}

	srv2, err := server.New(server.Options{DataDir: data, RegisterKeys: []vault.RegisterKey{reg}})
	if err != nil {
		t.Fatal(err)
	}
	defer srv2.Close()
	for _, r := range []vault.RootKey{rk, other} {
		if err := srv2.EnsureVault(vault.DeriveVaultID(r), vault.DeriveAuthKey(r)); err != server.ErrRevoked {
			t.Fatalf("recreating a dropped vault: %v, want ErrRevoked", err)
		}
	}
}
