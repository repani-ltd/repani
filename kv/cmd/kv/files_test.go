package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repani.com/kv/vault"
)

// TestFailedPairLeavesNoHalfVault: pair checks everything before
// writing, so one that fails leaves no vault.key behind, and the
// corrected command then succeeds. A register.key equal to the blob's
// is kept; another one is refused.
func TestFailedPairLeavesNoHalfVault(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	reg, _ := vault.NewRegisterKey()
	other, _ := vault.NewRegisterKey()
	backup := vault.EncodeBackup(rk, &reg)

	t.Run("another register key installed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KV_HOME", home)
		os.WriteFile(filepath.Join(home, "register.key"), []byte(vault.EncodeRegisterKey(other)+"\n"), 0o600)
		if err := run([]string{"pair", backup}); err == nil {
			t.Fatal("pair over a different register key succeeded")
		}
		if _, err := os.Stat(filepath.Join(home, "vault.key")); !os.IsNotExist(err) {
			t.Fatalf("pair failed but left vault.key (%v)", err)
		}
		if err := run([]string{"pair", vault.EncodeRootKey(rk)}); err != nil {
			t.Fatalf("pair after the failure: %v", err)
		}
	})
	t.Run("same register key installed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("KV_HOME", home)
		os.WriteFile(filepath.Join(home, "register.key"), []byte(vault.EncodeRegisterKey(reg)+"\n"), 0o600)
		if err := run([]string{"pair", backup}); err != nil {
			t.Fatalf("pair over the same register key: %v", err)
		}
	})
	t.Run("existing vault", func(t *testing.T) {
		t.Setenv("KV_HOME", t.TempDir())
		if err := run([]string{"init"}); err != nil {
			t.Fatal(err)
		}
		if err := run([]string{"pair", backup}); err == nil {
			t.Fatal("pair over an existing vault succeeded")
		}
	})
}

// TestFailedRunKeepsOutputFile: backup and export replace their output
// file only once the output is whole, so a run that fails (here, no
// vault on the device) leaves the earlier file as it was.
func TestFailedRunKeepsOutputFile(t *testing.T) {
	for _, cmd := range []string{"backup", "export"} {
		t.Run(cmd, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "earlier")
			os.WriteFile(path, []byte("earlier copy\n"), 0o600)
			t.Setenv("KV_HOME", t.TempDir())
			if err := run([]string{cmd, path}); err == nil {
				t.Fatalf("%s with no vault succeeded", cmd)
			}
			if b, _ := os.ReadFile(path); !strings.Contains(string(b), "earlier copy") {
				t.Fatalf("%s failed and left %q", cmd, b)
			}
		})
	}
}
