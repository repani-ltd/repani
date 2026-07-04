// kv: ASCON-KV client CLI.
//
// Commands:
//
//	kv init                       generate a new vault's RootKey
//	kv pair <blob|->              join an existing vault (use - to read stdin)
//	kv pair-blob                  print this device's pairing blob
//	kv put <key> [value|-]        set a key (value from argv or stdin)
//	kv get <key>                  print the current value
//	kv del <key>                  tombstone a key
//	kv ls                         list live keys
//	kv sync                       run one sync cycle
//	kv export [file|-]            dump live state as CSV (Key,Value)
//	kv import <file|->            import rows from a CSV file
//	kv backup [file|-]            emit RootKey (+ register key if local)
//	kv restore <blob|->           restore a vault from a backup blob
//	kv search [flags] <pattern>   case-insensitive substring match
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
package main

import (
	"bufio"
	"bytes"
	"context"
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

	"github.com/pavlos/kv/client"
	"github.com/pavlos/kv/store"
	"github.com/pavlos/kv/vault"
	"github.com/pavlos/kv/wire"
)

const defaultHTTPTimeout = 60 * time.Second

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		runInit(args)
	case "pair":
		runPair(args)
	case "pair-blob":
		runPairBlob(args)
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
	case "search":
		runSearch(args)
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
  put <key> [val]   set key to val (or read val from stdin if omitted or "-")
  get <key>         print the current value, or exit 1 if absent
  del <key>         tombstone a key
  ls                list live keys
  sync              run one sync cycle
  export [file|-]   dump live state as CSV (Key,Value); default stdout
  import <file|->   import rows from CSV (reads Key,Value columns by header)
  backup [file|-]   emit RootKey (+ register key if available locally)
                    as a single Crockford base32 blob for offline storage
  restore <blob|->  recover a vault on a fresh device from a backup blob
  search <query>    find keys/values containing the substring (case-
                    insensitive). Default output is a column-aligned
                    KEY/VALUE table. Flags:
                      --regex          interpret query as Go regexp
                      --key            match against keys only
                      --value          match against values only
                      --keys-only      print bare keys, one per line
                      --case-sensitive exact-case match

Environment:
  KV_HOME           storage directory (default ~/.kv)
  KV_SERVER         server base URL, e.g. https://vault.example
  KV_HTTP_TIMEOUT   per-request timeout (default 60s)
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
func registeredPath() string  { return filepath.Join(homeDir(), "registered") }
func registerKeyPath() string { return filepath.Join(homeDir(), "register.key") }

func loadRootKey() vault.RootKey {
	b, err := os.ReadFile(vaultKeyPath())
	if err != nil {
		fatal("read %s: %v (have you run `kv init` or `kv pair`?)", vaultKeyPath(), err)
	}
	if len(b) != vault.RootKeySize {
		zero(b)
		fatal("vault.key is %d bytes, expected %d", len(b), vault.RootKeySize)
	}
	var rk vault.RootKey
	copy(rk[:], b)
	zero(b)
	return rk
}

func serverURL() string { return os.Getenv("KV_SERVER") }

func httpTimeout() time.Duration {
	if v := os.Getenv("KV_HTTP_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return defaultHTTPTimeout
}

// --- registration bit (per server URL) ---

// registeredFor reports whether we have recorded a successful
// registration for the given server URL. The file stores one
// line per registered URL.
func registeredFor(url string) bool {
	b, err := os.ReadFile(registeredPath())
	if err != nil {
		return false
	}
	target := strings.TrimSpace(url)
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

func markRegistered(url string) error {
	if url == "" {
		return nil
	}
	if registeredFor(url) {
		return nil
	}
	f, err := os.OpenFile(registeredPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, url)
	return err
}

// --- client open ---

func openClient() *client.Client {
	rk := loadRootKey()
	ls, err := store.OpenLogStore(entriesPath())
	if err != nil {
		fatal("open log: %v", err)
	}
	url := serverURL()
	c, err := client.New(rk, ls, &httpTransport{url: url, timeout: httpTimeout()})
	if err != nil {
		fatal("client init: %v", err)
	}
	if registeredFor(url) {
		c.SetRegistered(true)
	}
	// If a register key was saved at init/pair time, pass it along
	// so the first REGISTER to a gated server succeeds.
	if rk := loadRegisterKey(); rk != nil {
		c.SetRegisterKey(rk)
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
	defer zero(b)
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
	out := []byte(vault.EncodeRegisterKey(rk) + "\n")
	defer zero(out)
	return writeKeyExclusive(registerKeyPath(), out)
}

// --- commands ---

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	registerKey := fs.String("register-key", "",
		"server register-gate secret (26-char Crockford base32); required for `kv init`")
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
	if ls, err := store.OpenLogStore(entriesPath()); err == nil {
		_ = ls.Close()
	}
	fmt.Println(vault.EncodeRootKey(rk))
}

func runPair(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	registerKey := fs.String("register-key", "",
		"server register-gate secret (32 hex chars); required by gated servers on a fresh device")
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
	if ls, err := store.OpenLogStore(entriesPath()); err == nil {
		_ = ls.Close()
	}
	fmt.Println("paired")
}

func runPairBlob(_ []string) {
	rk := loadRootKey()
	fmt.Println(vault.EncodeRootKey(rk))
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
		defer f.Close()
		w = f
	}

	rk := loadRootKey()
	regKey := loadRegisterKey() // may be nil
	blob := vault.EncodeBackup(rk, regKey)
	// Best-effort scrub.
	for i := range rk {
		rk[i] = 0
	}

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
	// Best-effort scrub of the in-memory RootKey.
	for i := range rk {
		rk[i] = 0
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
	if ls, err := store.OpenLogStore(entriesPath()); err == nil {
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
	type hit struct{ k, v string }
	var hits []hit
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
			hits = append(hits, hit{k, v})
		}
	}
	if len(hits) == 0 {
		return
	}

	if *keysOnly {
		for _, h := range hits {
			fmt.Println(h.k)
		}
		return
	}

	// Tabular output. tabwriter pads columns so the VALUE column
	// aligns across rows. Multi-line values are emitted raw; the
	// operator who put them there can deal with wrapping.
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tVALUE")
	for _, h := range hits {
		// Collapse newlines in the value so a multi-line secret does
		// not break table alignment. Original value is still
		// retrievable via `kv get`.
		v := strings.ReplaceAll(h.v, "\n", " ⏎ ")
		fmt.Fprintf(tw, "%s\t%s\n", h.k, v)
	}
	_ = tw.Flush()
}

func runPut(args []string) {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	offline := fs.Bool("offline", false, "skip auto-sync")
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
	if !bytes.HasSuffix(v, []byte("\n")) {
		fmt.Println()
	}
}

func runDel(args []string) {
	fs := flag.NewFlagSet("del", flag.ExitOnError)
	offline := fs.Bool("offline", false, "skip auto-sync")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fatal("usage: kv del <key>")
	}
	c := openClient()
	if err := c.Delete([]byte(fs.Arg(0))); err != nil {
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

func runSync(_ []string) {
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
	if c.Registered() {
		if err := markRegistered(serverURL()); err != nil {
			fatal("mark registered: %v", err)
		}
	}
}

// --- http transport ---

type httpTransport struct {
	url     string
	timeout time.Duration
}

func (h *httpTransport) Do(ctx context.Context, req []byte) ([]byte, error) {
	if h.url == "" {
		return nil, errors.New("no server URL configured (set KV_SERVER)")
	}
	reqCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, h.url+"/v1/sync", bytes.NewReader(req))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: h.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, wire.MaxFrameBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server %d", resp.StatusCode)
	}
	return body, nil
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

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func fatal(f string, args ...any) {
	fmt.Fprintf(os.Stderr, "kv: "+f+"\n", args...)
	os.Exit(1)
}
