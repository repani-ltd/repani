package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"repani.com/kv/vault"
)

// The commands here end in fatal, which exits, so they run as the test
// binary itself in a child process: TestMain hands a child to main.
func TestMain(m *testing.M) {
	if os.Getenv("KV_TEST_RUN_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// kvRun runs kv with args under home, returning its output and whether
// it succeeded.
func kvRun(t *testing.T, home string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "KV_TEST_RUN_MAIN=1", "KV_HOME="+home, "KV_SERVER=")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// TestFailedRunLeavesNoHalfVault: init, pair and restore check
// everything before writing, so one that fails leaves no vault.key
// behind, and the corrected command then succeeds.
func TestFailedRunLeavesNoHalfVault(t *testing.T) {
	rk, _ := vault.NewRandomRootKey()
	reg, _ := vault.NewRegisterKey()
	other, _ := vault.NewRegisterKey()
	for _, tc := range []struct {
		name      string
		installed *vault.RegisterKey // register.key already on the device
		bad, good []string
	}{
		{"init, bad register key", nil,
			[]string{"init", "--register-key", "not-a-key"}, []string{"init"}},
		{"pair, bad register key", nil,
			[]string{"pair", "--register-key", "not-a-key", vault.EncodeRootKey(rk)},
			[]string{"pair", vault.EncodeRootKey(rk)}},
		{"restore, another register key installed", &other,
			[]string{"restore", vault.EncodeBackup(rk, &reg)},
			[]string{"restore", vault.EncodeRootKey(rk)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.installed != nil {
				os.WriteFile(filepath.Join(home, "register.key"), []byte(vault.EncodeRegisterKey(*tc.installed)+"\n"), 0o600)
			}
			if out, ok := kvRun(t, home, tc.bad...); ok {
				t.Fatalf("%v succeeded: %s", tc.bad, out)
			}
			if _, err := os.Stat(filepath.Join(home, "vault.key")); !os.IsNotExist(err) {
				t.Fatalf("%v failed but left vault.key (%v)", tc.bad, err)
			}
			if out, ok := kvRun(t, home, tc.good...); !ok {
				t.Fatalf("%v after the failure: %s", tc.good, out)
			}
		})
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
			if out, ok := kvRun(t, t.TempDir(), cmd, path); ok {
				t.Fatalf("%s with no vault succeeded: %s", cmd, out)
			}
			if b, _ := os.ReadFile(path); !strings.Contains(string(b), "earlier copy") {
				t.Fatalf("%s failed and left %q", cmd, b)
			}
		})
	}
}
