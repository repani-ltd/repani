// kv: ASCON-KV client CLI.
//
// Commands:
//
//	kv init                       generate a new vault's RootKey
//	kv pair <blob|->              join an existing vault (use - to read stdin)
//	kv pair-blob                  print this device's pairing blob
//	kv set-register-key <b32|->   install/replace the server register-gate secret
//	kv put <key> [value|-]        set a key (value from argv or stdin)
//	kv get <key>                  print the current value
//	kv del <key>                  tombstone a key
//	kv ls                         list live keys
//	kv sync                       run one sync cycle
//	kv export [file|-]            dump live state as CSV (Key,Value)
//	kv import <file|->            import rows from a CSV file
//	kv backup [file|-]            emit RootKey (+ register key if local)
//	kv restore <blob|->           restore a vault from a backup blob
//	kv vault-id <blob|->          print a blob's VaultID (hex)
//	kv version [--url URL]        print this build, and the server's
//	kv search|s [flags] <pattern> case-insensitive substring match
//	                              across keys AND values; renders a
//	                              KEY/VALUE table by default.
//	                              --regex, --key, --value, --keys-only,
//	                              --case-sensitive
//
// Environment:
//
//	KV_HOME           storage directory (default ~/.kv)
//	KV_SERVER         server base URL, e.g. https://vault.example
//	KV_HTTP_TIMEOUT   per-request timeout (default 60s)
//	KV_LOCK_WAIT      wait for a concurrent kv holding the local
//	                  entry-log lock (default 5s)
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
	"repani.com/kiosk"
	"repani.com/kv/client"
	"repani.com/kv/internal/durable"
	"repani.com/kv/store"
	"repani.com/kv/vault"
)

const defaultHTTPTimeout = 60 * time.Second

// defaultLockWait bounds how long an invocation waits for another kv
// process holding the entry-log lock. Holders keep it only for
// milliseconds per operation, so waiting briefly lets concurrent
// invocations serialize instead of failing.
const defaultLockWait = 5 * time.Second

// verbose enables a sync-traffic tracer on the client. It is package-
// level because several subcommands set it from their own flag sets
// before openClient runs; KV_VERBOSE turns it on regardless of position.
var verbose bool

func addVerboseFlag(fs *flag.FlagSet) {
	// Default to the current value so a global `kv -v <cmd>` (which set
	// verbose before the subcommand parsed) is not reset to false.
	def := verbose
	fs.BoolVar(&verbose, "v", def, "verbose: print sync traffic (PROBE/PULL/PUSH/REGISTER) and stats to stderr")
	fs.BoolVar(&verbose, "verbose", def, "alias for -v")
}

func main() {
	// KV_VERBOSE seeds the single verbose flag; a global or per-subcommand
	// -v/--verbose can still turn it on. Reading it once here means the
	// rest of the program only ever consults `verbose`.
	verbose = os.Getenv("KV_VERBOSE") != ""
	rest := os.Args[1:]
	// Accept a global -v/--verbose before the subcommand, in addition to
	// the per-subcommand flag — so `kv -v put k v` and `kv put -v k v`
	// both work.
	for len(rest) > 0 && (rest[0] == "-v" || rest[0] == "--verbose") {
		verbose = true
		rest = rest[1:]
	}
	if len(rest) == 0 {
		usage(os.Stderr)
		os.Exit(2)
	}

	cmd := rest[0]
	args := rest[1:]

	switch cmd {
	case "init":
		runInit(args)
	case "pair":
		runPair(args)
	case "pair-blob":
		runPairBlob(args)
	case "set-register-key":
		runSetRegisterKey(args)
	case "put":
		runPut(args)
	case "get":
		runGet(args)
	case "del":
		runDel(args)
	case "ls":
		runLs(args)
	case "sync":
		runSync(args)
	case "export":
		runExport(args)
	case "import":
		runImport(args)
	case "backup":
		runBackup(args)
	case "restore":
		runRestore(args)
	case "vault-id":
		runVaultID(args)
	case "search", "s":
		runSearch(args)
	case "version":
		runVersion(args)
	case "-h", "--help", "help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "kv: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `kv — ASCON-KV client CLI

Commands:
  init              generate a new vault's RootKey
  pair <blob|->     join an existing vault from a pairing blob (use - to read stdin)
  pair-blob         print this device's pairing blob
  set-register-key <base32|->
                    install/replace the server register-gate secret used
                    to register with a gated server (26-char Crockford base32)
  put <key> [val]   set key to val (or read val from stdin if omitted or "-")
  get <key>         print the current value, or exit 1 if absent
  del [-f] <key>    tombstone a key; exit 1 if it is not present
                    (-f/--force writes the tombstone regardless)
  ls                list live keys
  sync [-v]         run one sync cycle (-v/--verbose prints per-frame
                    traffic and a byte/entry stats summary to stderr;
                    -v also works on put/del/import)
  export [file|-]   dump live state as CSV (Key,Value); default stdout
  import <file|->   import rows from CSV (reads Key,Value columns by header)
  backup [file|-]   emit RootKey (+ register key if available locally)
                    as a single Crockford base32 blob for offline storage
  restore <blob|->  recover a vault on a fresh device from a backup blob
  vault-id <blob|-> print the VaultID of a pairing or backup blob, in
                    hex: the vault's directory on the server, and what
                    kvserve drop-vault takes. Computed here alone: no
                    KV_HOME, no server
  version [--url U] print this binary's build, and the server's when
                    KV_SERVER (or --url) is set; the useful question is
                    usually whether the two match
  search <query>    (alias: s) find keys/values containing the substring
                    (case-insensitive). Default output is a column-aligned
                    KEY/VALUE table. Flags:
                      --regex          interpret query as Go regexp
                      --key            match against keys only
                      --value          match against values only
                      --keys-only      print bare keys, one per line
                      --case-sensitive exact-case match
                      --truncate       one line per value (default: wrap)
                      --width N        force table width (0 = auto/terminal)
                    Long values wrap to the terminal width, breaking on
                    their own newlines; piping prints full, untruncated
                    values.

Environment:
  KV_HOME           storage directory (default ~/.kv)
  KV_SERVER         server base URL, e.g. https://vault.example
  KV_HTTP_TIMEOUT   per-request timeout (default 60s)
  KV_VERBOSE        if set, print sync traffic + stats (same as -v)
  KV_SYNC_WINDOW    reconcile look-back window (Go duration, default 10m)
  KV_LOCK_WAIT      wait for a concurrent kv holding the local entry-log
                    lock (Go duration, default 5s; 0 fails immediately)

Specification (repani-private/kv, pica documents):
  PROTOCOL.t        the wire protocol kv speaks
  CLIENT.t          the client conventions: values, key blobs, the sync loop
`)
}

// --- bootstrap helpers ---

func homeDir() string {
	if h := os.Getenv("KV_HOME"); h != "" {
		return h
	}
	ud, err := os.UserHomeDir()
	if err != nil {
		fatal("could not determine home directory: %v", err)
	}
	return filepath.Join(ud, ".kv")
}

func vaultKeyPath() string    { return filepath.Join(homeDir(), "vault.key") }
func entriesPath() string     { return filepath.Join(homeDir(), "entries.log") }
func registerKeyPath() string { return filepath.Join(homeDir(), "register.key") }

func loadRootKey() vault.RootKey {
	b, err := os.ReadFile(vaultKeyPath())
	if err != nil {
		fatal("read %s: %v (have you run `kv init` or `kv pair`?)", vaultKeyPath(), err)
	}
	if len(b) != vault.RootKeySize {
		fatal("vault.key is %d bytes, expected %d", len(b), vault.RootKeySize)
	}
	return vault.RootKey(b)
}

func serverURL() string { return os.Getenv("KV_SERVER") }

// runVersion prints this binary's build, and the server's when one is
// configured. Two lines rather than one because the interesting
// question is usually whether they match.
//
// The server's line is fetched, so it can fail while the local one
// cannot: an unreachable or older server prints an error against the
// "server:" label and exits 1, leaving the local line already on
// stdout where it is still useful.
func runVersion(args []string) {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	url := fs.String("url", serverURL(), "server base URL (default $KV_SERVER)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	fmt.Printf("client: %s\n", kiosk.ReadBuild())
	if *url == "" {
		return
	}
	line, err := fetchVersion(*url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("server: %s\n", line)
}

func fetchVersion(base string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout())
	defer cancel()
	u := strings.TrimRight(base, "/") + kiosk.VersionPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// A server too old to carry the route answers 404 here, which
		// is worth saying plainly rather than printing an empty line.
		return "", fmt.Errorf("%s: %s (a server built before %s existed answers 404)", u, resp.Status, kiosk.VersionPath)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func httpTimeout() time.Duration {
	if v := os.Getenv("KV_HTTP_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return defaultHTTPTimeout
}

func lockWait() time.Duration {
	if v := os.Getenv("KV_LOCK_WAIT"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return defaultLockWait
}

// --- client open ---

func openClient() *client.Client {
	rk := loadRootKey()
	ls, err := store.OpenLogStore(entriesPath(), lockWait())
	if err != nil {
		if errors.Is(err, store.ErrBadMagic) {
			p := entriesPath()
			fatal("local entry cache %s is in an incompatible (pre-v1) format.\n"+
				"  It is only a cache — your RootKey (%s) is untouched. To upgrade:\n"+
				"    rm %s\n"+
				"  then run `kv sync` to rebuild from the server, or, if the server\n"+
				"  was also reset, `kv import <your-export.csv>` to restore your data.",
				p, vaultKeyPath(), p)
		}
		fatal("open log: %v", err)
	}
	url := serverURL()
	c, err := client.New(rk, ls, &client.HTTPTransport{Base: url, Timeout: httpTimeout()})
	if err != nil {
		fatal("client init: %v", err)
	}
	// Optimistically assume the vault is already registered: in steady
	// state the first PROBE/PUSH just works. If the server does not know
	// the vault it replies UNKNOWN_VAULT, and Sync self-heals by clearing
	// this belief and registering — costing one extra round trip once per
	// fresh device, and no persisted "registered" file to keep in step.
	c.SetRegistered(true)
	// If a register key was saved at init/pair time, pass it along
	// so the first REGISTER to a gated server succeeds.
	if rk := loadRegisterKey(); rk != nil {
		c.SetRegisterKey(rk)
	}
	if verbose {
		c.SetTracer(func(s string) { fmt.Fprintln(os.Stderr, s) })
	}
	if w := os.Getenv("KV_SYNC_WINDOW"); w != "" {
		d, err := time.ParseDuration(w)
		if err != nil {
			fatal("KV_SYNC_WINDOW: %v", err)
		}
		c.SetSyncWindow(d)
	}
	return c
}

// loadRegisterKey reads $KV_HOME/register.key if present and returns
// it, or nil if absent. The file holds the key in Crockford base32
// (matches what `kvserve gen-register-key` emits and what
// vault.EncodeRegisterKey produces). Any non-ENOENT error is fatal
// so the user knows their register-key file is corrupt rather than
// silently running with no register key.
func loadRegisterKey() *vault.RegisterKey {
	b, err := os.ReadFile(registerKeyPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		fatal("read %s: %v", registerKeyPath(), err)
	}
	rk, err := vault.DecodeRegisterKey(string(b))
	if err != nil {
		fatal("register.key: %v", err)
	}
	return &rk
}

// saveRegisterKey validates and writes the register key to disk in
// Crockford base32 form, mode 0600.
func saveRegisterKey(b32 string) error {
	rk, err := vault.DecodeRegisterKey(strings.TrimSpace(b32))
	if err != nil {
		return fmt.Errorf("register key: %w", err)
	}
	return writeKeyExclusive(registerKeyPath(), []byte(vault.EncodeRegisterKey(rk)+"\n"))
}

// --- commands ---

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	registerKey := fs.String("register-key", "",
		"server register-gate secret (26-char Crockford base32); needed to register with a gated server")
	_ = fs.Parse(args)
	if _, err := os.Stat(vaultKeyPath()); err == nil {
		fatal("refusing to overwrite existing vault at %s", vaultKeyPath())
	}
	rk, err := vault.NewRandomRootKey()
	if err != nil {
		fatal("rand: %v", err)
	}
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		fatal("mkdir: %v", err)
	}
	if err := writeKeyExclusive(vaultKeyPath(), rk[:]); err != nil {
		fatal("write vault.key: %v", err)
	}
	if *registerKey != "" {
		if err := saveRegisterKey(*registerKey); err != nil {
			fatal("%v", err)
		}
	}
	// Materialize an empty log with the proper magic header.
	if ls, err := store.OpenLogStore(entriesPath(), lockWait()); err == nil {
		_ = ls.Close()
	}
	fmt.Println(vault.EncodeRootKey(rk))
}

func runPair(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	registerKey := fs.String("register-key", "",
		"server register-gate secret (26-char Crockford base32); required by gated servers on a fresh device")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv pair <blob|->")
	}
	blob := fs.Arg(0)
	if blob == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal("read stdin: %v", err)
		}
		blob = strings.TrimSpace(string(b))
	}
	rk, err := vault.DecodeRootKey(blob)
	if err != nil {
		fatal("invalid pairing blob: %v", err)
	}
	if _, err := os.Stat(vaultKeyPath()); err == nil {
		fatal("refusing to overwrite existing vault at %s", vaultKeyPath())
	}
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		fatal("mkdir: %v", err)
	}
	if err := writeKeyExclusive(vaultKeyPath(), rk[:]); err != nil {
		fatal("write vault.key: %v", err)
	}
	if *registerKey != "" {
		if err := saveRegisterKey(*registerKey); err != nil {
			fatal("%v", err)
		}
	}
	if ls, err := store.OpenLogStore(entriesPath(), lockWait()); err == nil {
		_ = ls.Close()
	}
	fmt.Println("paired")
}

// runVaultID prints the VaultID a pairing or backup blob's RootKey
// derives. It reads nothing from KV_HOME and sends nothing anywhere,
// so it answers for any vault, not just this device's, and the blob
// never reaches the server whose directory it names. "-" reads the
// blob from stdin, keeping it out of shell history.
func runVaultID(args []string) {
	if len(args) != 1 {
		fatal("usage: kv vault-id <blob|->")
	}
	blob := args[0]
	if blob == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal("read stdin: %v", err)
		}
		blob = strings.TrimSpace(string(b))
	}
	id, err := vaultIDOf(blob)
	if err != nil {
		fatal("invalid blob: %v", err)
	}
	fmt.Println(id)
}

// vaultIDOf decodes a pairing (32-char) or backup (58-char) blob and
// returns its VaultID in hex.
func vaultIDOf(blob string) (string, error) {
	rk, _, err := vault.DecodeBackup(blob)
	if err != nil {
		return "", err
	}
	id := vault.DeriveVaultID(rk)
	return hex.EncodeToString(id[:]), nil
}

func runPairBlob(_ []string) {
	rk := loadRootKey()
	fmt.Println(vault.EncodeRootKey(rk))
}

// runSetRegisterKey installs (or replaces) the server register-gate
// secret on this device so a subsequent sync can register with a gated
// server. Unlike init/pair it does not touch the vault — it only writes
// $KV_HOME/register.key (mode 0600), overwriting any existing value.
func runSetRegisterKey(args []string) {
	fs := flag.NewFlagSet("set-register-key", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv set-register-key <base32|->")
	}
	arg := fs.Arg(0)
	if arg == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal("read stdin: %v", err)
		}
		arg = strings.TrimSpace(string(b))
	}
	rk, err := vault.DecodeRegisterKey(arg)
	if err != nil {
		fatal("invalid register key: %v", err)
	}
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		fatal("mkdir: %v", err)
	}
	// Replacing a key may overwrite one; first-time writes go through
	// writeKeyExclusive, which refuses to replace anything.
	if err := durable.WriteFile(registerKeyPath(), []byte(vault.EncodeRegisterKey(rk)+"\n"), 0o600); err != nil {
		fatal("write register.key: %v", err)
	}
	fmt.Fprintln(os.Stderr, "register key saved to "+registerKeyPath())
}

// runBackup prints a single Crockford base32 blob containing this
// device's RootKey and, if present, its register.key. The output is
// the minimum a user needs to restore full read/write access on a
// fresh machine; treat it as sensitive.
func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	_ = fs.Parse(args)

	var w io.Writer = os.Stdout
	if fs.NArg() >= 1 && fs.Arg(0) != "-" {
		f, err := os.OpenFile(fs.Arg(0), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			fatal("open %s: %v", fs.Arg(0), err)
		}
		// OpenFile applies 0600 only when creating; force it so a
		// pre-existing (possibly world-readable) file cannot leak the RootKey.
		if err := f.Chmod(0o600); err != nil {
			fatal("chmod %s: %v", fs.Arg(0), err)
		}
		defer f.Close()
		w = f
	}

	rk := loadRootKey()
	regKey := loadRegisterKey() // may be nil
	blob := vault.EncodeBackup(rk, regKey)

	if regKey == nil {
		fmt.Fprintln(os.Stderr,
			"warning: no register.key on this device; backup contains RootKey only.")
		fmt.Fprintln(os.Stderr,
			"         To include the register key, run this command on a device that was paired")
		fmt.Fprintln(os.Stderr,
			"         with `kv pair --register-key <base32>`.")
	}
	fmt.Fprintln(w, blob)
}

// runRestore consumes a blob produced by `kv backup` (or the
// RootKey-only form produced by `kv pair-blob`) and recreates the
// local vault. Refuses to run if a vault already exists locally.
func runRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv restore <blob|->")
	}
	blob := fs.Arg(0)
	if blob == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal("read stdin: %v", err)
		}
		blob = strings.TrimSpace(string(b))
	}
	rk, regKey, err := vault.DecodeBackup(blob)
	if err != nil {
		fatal("invalid backup blob: %v", err)
	}
	if _, err := os.Stat(vaultKeyPath()); err == nil {
		fatal("refusing to overwrite existing vault at %s", vaultKeyPath())
	}
	if err := os.MkdirAll(homeDir(), 0o700); err != nil {
		fatal("mkdir: %v", err)
	}
	if err := writeKeyExclusive(vaultKeyPath(), rk[:]); err != nil {
		fatal("write vault.key: %v", err)
	}
	if regKey != nil {
		if err := writeKeyExclusive(
			registerKeyPath(),
			[]byte(vault.EncodeRegisterKey(*regKey)+"\n"),
		); err != nil {
			fatal("write register.key: %v", err)
		}
	}
	// Materialize an empty entries.log with the proper magic header.
	if ls, err := store.OpenLogStore(entriesPath(), lockWait()); err == nil {
		_ = ls.Close()
	}
	if regKey != nil {
		fmt.Println("restored (RootKey + register key)")
	} else {
		fmt.Println("restored (RootKey only)")
	}
}

// runSearch is the client-side full-text search. The server never
// sees plaintext, so the CLI replays the full local store, decrypts
// every entry, and filters in memory.
//
// Default: case-insensitive substring match against both keys and
// values, rendered as a column-aligned table:
//
//	KEY             VALUE
//	aws_prod_key    AKIA…
//	github_token    ghp_…
//
// --keys-only prints bare keys one per line (pipe-friendly).
func runSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	useRegex := fs.Bool("regex", false, "interpret query as a Go regexp")
	scopeKey := fs.Bool("key", false, "match against keys only")
	scopeValue := fs.Bool("value", false, "match against values only")
	keysOnly := fs.Bool("keys-only", false, "print bare keys, one per line (no table)")
	caseSensitive := fs.Bool("case-sensitive", false, "exact-case match (default is case-insensitive)")
	truncate := fs.Bool("truncate", false, "truncate long values to one line (default: wrap)")
	width := fs.Int("width", 0, "table width (0 = auto/terminal)")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv search [--regex] [--key|--value] [--keys-only] [--case-sensitive] <query>")
	}
	if *scopeKey && *scopeValue {
		fatal("--key and --value are mutually exclusive (omit both to match either)")
	}
	query := fs.Arg(0)

	var match func(string) bool
	switch {
	case *useRegex:
		pattern := query
		if !*caseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			fatal("regex: %v", err)
		}
		match = re.MatchString
	default:
		if *caseSensitive {
			needle := query
			match = func(s string) bool { return strings.Contains(s, needle) }
		} else {
			needleLower := strings.ToLower(query)
			match = func(s string) bool {
				return strings.Contains(strings.ToLower(s), needleLower)
			}
		}
	}

	c := openClient()
	all, err := c.All()
	if err != nil {
		fatal("search: %v", err)
	}

	// Collect hits first so we know whether to even print a table
	// header (avoid emitting a blank header when there are no hits).
	var hits []kvRow
	for _, e := range all {
		k := string(e.Key)
		v := string(e.Value)
		var ok bool
		switch {
		case *scopeKey:
			ok = match(k)
		case *scopeValue:
			ok = match(v)
		default:
			ok = match(k) || match(v)
		}
		if ok {
			hits = append(hits, kvRow{k, v})
		}
	}
	if len(hits) == 0 {
		return
	}

	if *keysOnly {
		for _, h := range hits {
			fmt.Println(h.key)
		}
		return
	}
	renderTable(hits, *truncate, *width)
}

// --- KEY/VALUE table rendering ---

type kvRow struct{ key, val string }

// renderTable prints rows as an aligned KEY/VALUE table. On a terminal
// (or when forcedWidth > 0) the value column is sized to the width and a
// long value is wrapped across continuation lines aligned under the value
// column — breaking on the value's own newlines first, then wrapping each
// logical line. With truncate set, each value is instead squeezed onto a
// single line with an ellipsis. When stdout is not a terminal and no
// width is forced, full values are emitted tab-separated so pipes and
// scripts get everything. Widths are measured in runes; wide (CJK) glyphs
// are not double-counted.
func renderTable(rows []kvRow, truncate bool, forcedWidth int) {
	width := tableWidth(forcedWidth)
	if width == 0 {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "KEY\tVALUE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\n", r.key, collapseCell(r.val))
		}
		_ = tw.Flush()
		return
	}
	const gutter = "  "
	keyW := len("KEY")
	for _, r := range rows {
		if n := utf8.RuneCountInString(r.key); n > keyW {
			keyW = n
		}
	}
	if keyW > 32 { // keep a pathologically long key from eating the row
		keyW = 32
	}
	valW := width - keyW - len(gutter)
	if valW < 8 {
		valW = 8
	}

	fmt.Println(padRunes("KEY", keyW) + gutter + "VALUE")
	for _, r := range rows {
		key := truncRunes(r.key, keyW)
		if truncate {
			fmt.Println(padRunes(key, keyW) + gutter + truncRunes(collapseCell(r.val), valW))
			continue
		}
		for i, ln := range wrapValue(r.val, valW) {
			first := ""
			if i == 0 {
				first = key
			}
			fmt.Println(padRunes(first, keyW) + gutter + ln)
		}
	}
}

// wrapValue lays a value out for the wrapped table: it breaks on the
// value's own line breaks (CR/LF normalised), turns tabs into spaces, and
// wraps each resulting line to at most w runes. A trailing blank line
// (e.g. a value that ends in a newline) is dropped so it does not print a
// dangling empty row. An empty value yields a single empty line.
func wrapValue(s string, w int) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var out []string
	for _, ln := range lines {
		ln = strings.ReplaceAll(ln, "\t", "    ")
		out = append(out, chunkRunes(ln, w)...)
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// tableWidth resolves the render width: an explicit forcedWidth wins;
// otherwise the terminal size (default 80 if a terminal but size is
// unknown); 0 means "not a terminal — emit full untruncated output".
func tableWidth(forced int) int {
	if forced > 0 {
		return forced
	}
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return 0
	}
	if w, _, err := term.GetSize(fd); err == nil && w > 8 {
		return w
	}
	return 80
}

// collapseCell makes a value safe for a single table cell: newlines
// become a visible marker, tabs and CRs become spaces.
func collapseCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// padRunes left-aligns s to w columns (rune count).
func padRunes(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// truncRunes shortens s to at most w runes, ending with an ellipsis when
// it overflows.
func truncRunes(s string, w int) string {
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	r := []rune(s)
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}

// chunkRunes splits s into runs of at most w runes.
func chunkRunes(s string, w int) []string {
	r := []rune(s)
	if len(r) == 0 {
		return []string{""}
	}
	var out []string
	for len(r) > 0 {
		n := min(w, len(r))
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return out
}

func runPut(args []string) {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	offline := fs.Bool("offline", false, "skip auto-sync")
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv put <key> [value|-]")
	}
	key := []byte(fs.Arg(0))
	var value []byte
	switch {
	case fs.NArg() >= 2 && fs.Arg(1) != "-":
		value = []byte(fs.Arg(1))
	default:
		b, err := io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			fatal("read stdin: %v", err)
		}
		value = b
	}
	if len(value) == 0 {
		fatal("empty value is a tombstone; use `kv del %s` instead", string(key))
	}
	c := openClient()
	if err := c.Put(key, value); err != nil {
		fatal("put: %v", err)
	}
	if !*offline {
		mustSync(c)
	}
}

func runGet(args []string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv get <key>")
	}
	c := openClient()
	v, ok, err := c.Get([]byte(fs.Arg(0)))
	if err != nil {
		fatal("get: %v", err)
	}
	if !ok {
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(v)
	// Add a trailing newline only for interactive display; when piped or
	// redirected, emit the value verbatim so it round-trips byte-exactly.
	if term.IsTerminal(int(os.Stdout.Fd())) && !bytes.HasSuffix(v, []byte("\n")) {
		fmt.Println()
	}
}

func runDel(args []string) {
	fs := flag.NewFlagSet("del", flag.ExitOnError)
	offline := fs.Bool("offline", false, "skip auto-sync")
	force := fs.Bool("force", false, "tombstone even if the key is not present locally")
	fs.BoolVar(force, "f", false, "alias for --force")
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv del [-f] <key>")
	}
	key := []byte(fs.Arg(0))

	c := openClient()
	// Refuse to write a tombstone for a key that is not currently live —
	// that would only add dead weight. --force overrides (e.g. to delete
	// a value that exists on the server but has not been synced locally).
	if !*force {
		if _, ok, err := c.Get(key); err != nil {
			fatal("del: %v", err)
		} else if !ok {
			fmt.Fprintf(os.Stderr, "kv: no such key: %s\n", fs.Arg(0))
			os.Exit(1)
		}
	}
	if err := c.Delete(key); err != nil {
		fatal("del: %v", err)
	}
	if !*offline {
		mustSync(c)
	}
}

func runLs(_ []string) {
	c := openClient()
	keys, err := c.List()
	if err != nil {
		fatal("list: %v", err)
	}
	for _, k := range keys {
		fmt.Println(k)
	}
}

func runSync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	c := openClient()
	mustSync(c)
}

func runExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	_ = fs.Parse(args)

	var w io.Writer = os.Stdout
	if fs.NArg() >= 1 && fs.Arg(0) != "-" {
		f, err := os.OpenFile(fs.Arg(0), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			fatal("open %s: %v", fs.Arg(0), err)
		}
		// export writes plaintext keys and values; force 0600 so a
		// pre-existing file's looser permissions cannot leak them.
		if err := f.Chmod(0o600); err != nil {
			fatal("chmod %s: %v", fs.Arg(0), err)
		}
		defer f.Close()
		w = f
	}
	c := openClient()
	if err := c.ExportCSV(w); err != nil {
		fatal("export: %v", err)
	}
}

func runImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	offline := fs.Bool("offline", false, "skip auto-sync after import")
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv import <file|->")
	}
	var r io.Reader
	if fs.Arg(0) == "-" {
		r = bufio.NewReader(os.Stdin)
	} else {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			fatal("open %s: %v", fs.Arg(0), err)
		}
		defer f.Close()
		r = f
	}
	c := openClient()
	n, err := c.ImportCSV(r)
	if err != nil {
		fatal("import: %v", err)
	}
	fmt.Fprintf(os.Stderr, "imported %d rows\n", n)
	if !*offline {
		mustSync(c)
	}
}

func mustSync(c *client.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*httpTimeout())
	defer cancel()
	if err := c.Sync(ctx); err != nil {
		fatal("sync: %v", err)
	}
}

// --- helpers ---

func writeKeyExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func fatal(f string, args ...any) {
	fmt.Fprintf(os.Stderr, "kv: "+f+"\n", args...)
	os.Exit(1)
}
