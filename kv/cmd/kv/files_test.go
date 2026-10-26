package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repani.com/kv/vault"
)

// TestDeviceFile: a device's keys are one file. pair writes both keys
// into it, and backup gives them back; set-register-key replaces the
// register key and keeps the RootKey; pair over an existing vault, and
// set-register-key with no vault, are refused and change nothing; and
// the home holds only the device file and the entry log.
func TestDeviceFile(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	reg, _ := vault.NewRegisterKey()
	other, _ := vault.NewRegisterKey()
	home := t.TempDir()
	t.Setenv("KV_HOME", home)
	device := filepath.Join(home, "device.fact")
	backupOf := func() string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "backup")
		if err := run([]string{"backup", path}); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		return strings.TrimSpace(string(b))
	}

	if err := run([]string{"set-register-key", vault.EncodeRegisterKey(reg)}); err == nil {
		t.Fatal("set-register-key with no vault succeeded")
	}
	if _, err := os.Stat(device); !os.IsNotExist(err) {
		t.Fatalf("a refused set-register-key left %s (%v)", device, err)
	}

	if err := run([]string{"pair", vault.EncodeBackup(rk, &reg)}); err != nil {
		t.Fatal(err)
	}
	if got := backupOf(); got != vault.EncodeBackup(rk, &reg) {
		t.Fatalf("backup after pair = %q, want the paired blob", got)
	}

	before, _ := os.ReadFile(device)
	if err := run([]string{"pair", vault.EncodeRootKey(rk)}); err == nil {
		t.Fatal("pair over an existing vault succeeded")
	}
	if after, _ := os.ReadFile(device); string(after) != string(before) {
		t.Fatalf("a refused pair changed %s:\n%s", device, after)
	}

	if err := run([]string{"set-register-key", vault.EncodeRegisterKey(other)}); err != nil {
		t.Fatal(err)
	}
	if got := backupOf(); got != vault.EncodeBackup(rk, &other) {
		t.Fatalf("backup after set-register-key = %q, want the RootKey with the new key", got)
	}
	if fi, err := os.Stat(device); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode %v, %v; want 0600", device, fi.Mode().Perm(), err)
	}
	ents, _ := os.ReadDir(home)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, " ") != "device.fact entries.log" {
		t.Fatalf("home holds %v, want device.fact and entries.log", names)
	}
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
