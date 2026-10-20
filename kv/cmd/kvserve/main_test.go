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

// TestReadRevoked: ids one per line, notes after #; an empty file
// revokes nothing, and a missing file or a bad line is an error.
func TestReadRevoked(t *testing.T) {
	a, b := vault.VaultID{1}, vault.VaultID{2}
	path := filepath.Join(t.TempDir(), "revoked")
	os.WriteFile(path, []byte("# dropped vaults\n"+a.String()+"  # old trip\n\n"+b.String()+"\n"), 0o600)
	if ids, err := readRevoked(path); err != nil || !slices.Equal(ids, []vault.VaultID{a, b}) {
		t.Fatalf("ids %v, %v; want a then b", ids, err)
	}
	os.WriteFile(path, nil, 0o600)
	if ids, err := readRevoked(path); err != nil || len(ids) != 0 {
		t.Fatalf("empty file: %v, %v", ids, err)
	}
	os.WriteFile(path, []byte(a.String()+"\nnot-an-id\n"), 0o600)
	if _, err := readRevoked(path); err == nil {
		t.Error("bad line accepted")
	}
	if _, err := readRevoked(path + ".missing"); err == nil {
		t.Error("missing file accepted")
	}
}

// TestDropVault: a drop revokes before it deletes, once; a vault with
// no data is revoked all the same; a served vault is revoked and its
// data kept until the server stops; and a server started on the file
// never registers the vault again.
func TestDropVault(t *testing.T) {
	data := t.TempDir()
	revoked := filepath.Join(t.TempDir(), "revoked")
	os.WriteFile(revoked, nil, 0o600)
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

	if _, err := dropVault(data, revoked, vid, "in use"); err == nil || !strings.Contains(err.Error(), "being served") {
		t.Fatalf("drop of a served vault: %v", err)
	}
	srv.Close()
	if msg, err := dropVault(data, revoked, vid, ""); err != nil || msg != vid.String()+" already revoked and dropped" {
		t.Fatalf("drop after stop: %q, %v", msg, err)
	}
	other := vault.VaultID{7}
	if msg, err := dropVault(data, revoked, other, "removed by hand,  long ago"); err != nil || !strings.Contains(msg, "revoked; no data") {
		t.Fatalf("drop of a vault with no data: %q, %v", msg, err)
	}
	b, _ := os.ReadFile(revoked)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], vid.String()+"  # dropped ") || !strings.HasSuffix(lines[0], ": in use") ||
		!strings.HasSuffix(lines[1], ": removed by hand, long ago") {
		t.Fatalf("revoked file:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(data, vid.String())); !os.IsNotExist(err) {
		t.Fatalf("data still there: %v", err)
	}

	ids, err := readRevoked(revoked)
	if err != nil {
		t.Fatal(err)
	}
	srv2, err := server.New(server.Options{DataDir: data, RegisterKeys: []vault.RegisterKey{reg}, Revoked: ids})
	if err != nil {
		t.Fatal(err)
	}
	defer srv2.Close()
	if err := srv2.EnsureVault(vid, ak); err != server.ErrRevoked {
		t.Fatalf("recreating a dropped vault: %v, want ErrRevoked", err)
	}
}
