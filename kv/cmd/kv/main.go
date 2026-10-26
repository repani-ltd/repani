// kv: ASCON-KV client CLI. Run `kv help` for its commands and
// environment; that text is the one list of them.
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
	"unicode/utf8"

	"golang.org/x/term"
	"repani.com/durable"
	"repani.com/fact"
	"repani.com/kiosk"
	"repani.com/kv/client"
	"repani.com/kv/store"
	"repani.com/kv/vault"
)

const httpTimeout = 60 * time.Second

// lockWait bounds how long an invocation waits for another kv process
// holding the entry-log lock. Holders keep it only for milliseconds
// per operation, so waiting briefly lets concurrent invocations
// serialize instead of failing.
const lockWait = 5 * time.Second

// verbose enables a sync-traffic tracer on the client. It is package-
// level because several subcommands set it from their own flag sets
// before openClient runs.
var verbose bool

func addVerboseFlag(fs *flag.FlagSet) {
	fs.BoolVar(&verbose, "v", false, "print sync traffic (PROBE/PULL/PUSH/REGISTER) and stats to stderr")
}

// exitCode is an error that ends kv with that status and prints
// nothing more: what there was to say has been said.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func main() {
	err := run(os.Args[1:])
	var code exitCode
	switch {
	case err == nil:
	case errors.As(err, &code):
		os.Exit(int(code))
	default:
		fatal("%v", err)
	}
}

// home is the storage directory, resolved by run before any command.
var home string

// run resolves the storage directory and runs the command args names.
func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return exitCode(2)
	}
	cmd, args := args[0], args[1:]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		usage(os.Stdout)
		return nil
	}
	commands := map[string]func([]string) error{
		"init":             runInit,
		"pair":             runPair,
		"pair-blob":        runPairBlob,
		"set-register-key": runSetRegisterKey,
		"put":              runPut,
		"get":              runGet,
		"del":              runDel,
		"ls":               runLs,
		"sync":             runSync,
		"export":           runExport,
		"import":           runImport,
		"backup":           runBackup,
		"vault-id":         runVaultID,
		"search":           runSearch,
		"s":                runSearch,
		"version":          runVersion,
	}
	f, ok := commands[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "kv: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return exitCode(2)
	}
	home = os.Getenv("KV_HOME")
	if home == "" {
		ud, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %v", err)
		}
		home = filepath.Join(ud, ".kv")
	}
	return f(args)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `kv — ASCON-KV client CLI

Commands:
  init              generate a new vault's RootKey and print its pairing blob
  pair <blob|->     join an existing vault from a pairing or backup blob
                    (- reads it from stdin); a backup blob's register key
                    is installed too
  pair-blob         print this device's pairing blob (no register key)
  set-register-key <base32|->
                    install/replace the server register-gate secret used
                    to register with a gated server (26-char Crockford base32)
  put [-v] <key> [val]
                    set key to val (or read val from stdin if omitted or "-")
  get <key>         print the current value, or exit 1 if absent
  del [-v] [-f] <key>
                    tombstone a key; exit 1 if it is not present
                    (-f writes the tombstone regardless)
  ls                list live keys
  sync [-v]         run one sync cycle
  export [file|-]   dump live state as CSV (Key,Value); default stdout
  import [-v] <file|->
                    import rows from CSV (reads Key,Value columns by header)
  backup [file|-]   emit RootKey (+ register key if available locally)
                    as a single Crockford base32 blob for offline storage
  vault-id <blob|-> print the VaultID of a pairing or backup blob, in
                    hex: the vault's directory on the server, and what
                    kvserve drop-vault takes. Computed here alone: no
                    KV_HOME, no server
  version           print this binary's build, and the server's when
                    KV_SERVER is set; the useful question is usually
                    whether the two match
  search <query>    (alias: s) find keys/values containing the substring
                    (case-insensitive). Default output is a column-aligned
                    KEY/VALUE table. Flags:
                      --regex          interpret query as Go regexp
                      --key            match against keys only
                      --value          match against values only
                      --keys-only      print bare keys, one per line
                      --case-sensitive exact-case match
                      --truncate       one line per value (default: wrap)
                    Long values wrap to the terminal width, breaking on
                    their own newlines; piping prints full, untruncated
                    values.

put, del and import sync after writing. -v prints each sync frame and
a byte/entry stats summary to stderr.

Environment:
  KV_HOME           storage directory (default ~/.kv): device.fact, this
                    device's keys (FACT, mode 0600), and entries.log,
                    the local copy of the vault
  KV_SERVER         server base URL, e.g. https://vault.example

Specification (repani/kv, pica documents):
  PROTOCOL.t        the wire protocol kv speaks
  CLIENT.t          the client conventions: values, key blobs, the sync loop
`)
}

// --- bootstrap helpers ---

func devicePath() string  { return filepath.Join(home, "device.fact") }
func entriesPath() string { return filepath.Join(home, "entries.log") }

// This device's keys are one FACT file, device.fact, mode 0600: its
// vault's RootKey, as its pairing blob, and the register key it
// registers with, if it has one. Its presence is what makes a vault
// here. init and pair create it whole and never over another
// (durable.CreateFile); set-register-key replaces it whole
// (durable.WriteFile). The entry log beside it is only a cache. Its
// facts:
//
//	device.register_key: str? = "<vault.EncodeRegisterKey>"  (none without one)
//	device.root_key: str = "<vault.EncodeRootKey>"

// device is this device's keys, decoded.
type device struct {
	rk  vault.RootKey
	reg *vault.RegisterKey // nil: none
}

func loadDevice() (device, error) {
	b, err := os.ReadFile(devicePath())
	if errors.Is(err, os.ErrNotExist) {
		return device{}, fmt.Errorf("no vault on this device: %s is missing (run `kv init` or `kv pair`)", devicePath())
	}
	if err != nil {
		return device{}, err
	}
	facts, errs := fact.Load(b)
	if len(errs) > 0 {
		return device{}, fmt.Errorf("%s:%d: %s", devicePath(), errs[0].Line, errs[0].Msg)
	}
	m, err := fact.Bind(facts)
	if err != nil {
		return device{}, fmt.Errorf("%s: %v", devicePath(), err)
	}
	keys, _ := m["device"].(map[string]any)
	blob, ok := keys["root_key"].(string)
	if !ok {
		return device{}, fmt.Errorf("%s: root_key: want str", devicePath())
	}
	rk, err := vault.DecodeRootKey(blob)
	if err != nil {
		return device{}, fmt.Errorf("%s: root_key: %v", devicePath(), err)
	}
	d := device{rk: rk}
	if x := keys["register_key"]; x != nil {
		blob, ok := x.(string)
		if !ok {
			return device{}, fmt.Errorf("%s: register_key: want str", devicePath())
		}
		reg, err := vault.DecodeRegisterKey(blob)
		if err != nil {
			return device{}, fmt.Errorf("%s: register_key: %v", devicePath(), err)
		}
		d.reg = &reg
	}
	return d, nil
}

// marshal is d as device.fact: canonical, and checked by fact.Load as
// any reader would check it.
func (d device) marshal() ([]byte, error) {
	reg := "none"
	if d.reg != nil {
		reg = fact.Quote(vault.EncodeRegisterKey(*d.reg))
	}
	src := "device.register_key: str? = " + reg + "\n" +
		"device.root_key: str = " + fact.Quote(vault.EncodeRootKey(d.rk)) + "\n"
	facts, errs := fact.Load([]byte(src))
	if len(errs) > 0 {
		return nil, fmt.Errorf("device file: %s", errs[0].Msg)
	}
	return fact.Canonical(facts), nil
}

func serverURL() string { return os.Getenv("KV_SERVER") }

// argOrStdin is arg, or when arg is "-" what stdin holds, trimmed:
// "-" keeps a blob or key out of shell history.
func argOrStdin(arg string) (string, error) {
	if arg != "-" {
		return arg, nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %v", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// runVersion prints this binary's build, and the server's when one is
// configured. Two lines rather than one because the interesting
// question is usually whether they match.
//
// The server's line is fetched, so it can fail while the local one
// cannot: an unreachable or older server prints an error against the
// "server:" label and exits 1, leaving the local line already on
// stdout where it is still useful.
func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	_ = fs.Parse(args)
	fmt.Printf("client: %s\n", kiosk.ReadBuild())
	if serverURL() == "" {
		return nil
	}
	line, err := fetchVersion(serverURL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		return exitCode(1)
	}
	fmt.Printf("server: %s\n", line)
	return nil
}

func fetchVersion(base string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
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

// --- client open ---

// openClient opens this device's vault; the returned func closes its
// entry log, releasing the lock.
func openClient() (*client.Client, func(), error) {
	d, err := loadDevice()
	if err != nil {
		return nil, nil, err
	}
	ls, err := store.OpenLogStore(entriesPath(), lockWait)
	if err != nil {
		if errors.Is(err, store.ErrBadMagic) {
			p := entriesPath()
			return nil, nil, fmt.Errorf("local entry cache %s is in an incompatible (pre-v1) format.\n"+
				"  It is only a cache — your RootKey (%s) is untouched. To upgrade:\n"+
				"    rm %s\n"+
				"  then run `kv sync` to rebuild from the server, or, if the server\n"+
				"  was also reset, `kv import <your-export.csv>` to restore your data.",
				p, devicePath(), p)
		}
		return nil, nil, fmt.Errorf("open log: %v", err)
	}
	opts := client.Options{
		// Optimistically assume the vault is already registered: in
		// steady state the first PROBE/PUSH just works. If the server
		// does not know the vault it replies UNKNOWN_VAULT, and Sync
		// registers -- one extra round trip once per fresh device, and
		// no persisted "registered" file to keep in step.
		Registered: true,
		// A register key installed by pair or set-register-key lets
		// that REGISTER through a gated server.
		RegisterKey: d.reg,
	}
	if verbose {
		opts.Trace = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}
	transport := &client.HTTPTransport{Base: serverURL(), Client: &http.Client{Timeout: httpTimeout}}
	c, err := client.New(d.rk, ls, transport, opts)
	if err != nil {
		ls.Close()
		return nil, nil, fmt.Errorf("client init: %v", err)
	}
	return c, func() { ls.Close() }, nil
}

// createVault makes this device's vault from rk, with regKey when it
// is not nil, for init and pair: device.fact, created whole in one
// write that fails if the file exists, so no run leaves a half-made
// vault and none replaces another's keys.
func createVault(rk vault.RootKey, regKey *vault.RegisterKey) error {
	b, err := device{rk: rk, reg: regKey}.marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("mkdir: %v", err)
	}
	if err := durable.CreateFile(devicePath(), b, 0o600); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("refusing to overwrite existing vault at %s", devicePath())
		}
		return fmt.Errorf("write %s: %v", devicePath(), err)
	}
	// Materialize an empty log with the proper magic header.
	if ls, err := store.OpenLogStore(entriesPath(), lockWait); err == nil {
		_ = ls.Close()
	}
	return nil
}

// --- commands ---

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	_ = fs.Parse(args)
	rk, err := vault.NewRandomRootKey()
	if err != nil {
		return fmt.Errorf("rand: %v", err)
	}
	if err := createVault(rk, nil); err != nil {
		return err
	}
	fmt.Println(vault.EncodeRootKey(rk))
	return nil
}

// runPair joins an existing vault from a pairing blob (`kv init`,
// `kv pair-blob`) or a backup blob (`kv backup`), which also carries
// the register key. Refuses to run if a vault already exists locally.
func runPair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: kv pair <blob|->")
	}
	blob, err := argOrStdin(fs.Arg(0))
	if err != nil {
		return err
	}
	rk, regKey, err := vault.DecodeBackup(blob)
	if err != nil {
		return fmt.Errorf("invalid blob: %v", err)
	}
	if err := createVault(rk, regKey); err != nil {
		return err
	}
	if regKey != nil {
		fmt.Println("paired (RootKey + register key)")
	} else {
		fmt.Println("paired (RootKey only)")
	}
	return nil
}

// runVaultID prints the VaultID a pairing or backup blob's RootKey
// derives. It reads nothing from KV_HOME and sends nothing anywhere,
// so it answers for any vault, not just this device's, and the blob
// never reaches the server whose directory it names.
func runVaultID(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: kv vault-id <blob|->")
	}
	blob, err := argOrStdin(args[0])
	if err != nil {
		return err
	}
	id, err := vaultIDOf(blob)
	if err != nil {
		return fmt.Errorf("invalid blob: %v", err)
	}
	fmt.Println(id)
	return nil
}

// vaultIDOf decodes a pairing (32-char) or backup (58-char) blob and
// returns its VaultID in hex.
func vaultIDOf(blob string) (string, error) {
	rk, _, err := vault.DecodeBackup(blob)
	if err != nil {
		return "", err
	}
	return vault.DeriveVaultID(rk).String(), nil
}

func runPairBlob(_ []string) error {
	d, err := loadDevice()
	if err != nil {
		return err
	}
	fmt.Println(vault.EncodeRootKey(d.rk))
	return nil
}

// runSetRegisterKey installs (or replaces) the server register-gate
// secret on this device's vault, so a later sync can register with a
// gated server: device.fact is replaced whole, its RootKey unchanged.
// The device must have a vault (init or pair first).
func runSetRegisterKey(args []string) error {
	fs := flag.NewFlagSet("set-register-key", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: kv set-register-key <base32|->")
	}
	arg, err := argOrStdin(fs.Arg(0))
	if err != nil {
		return err
	}
	reg, err := vault.DecodeRegisterKey(arg)
	if err != nil {
		return fmt.Errorf("invalid register key: %v", err)
	}
	d, err := loadDevice()
	if err != nil {
		return err
	}
	d.reg = &reg
	b, err := d.marshal()
	if err != nil {
		return err
	}
	if err := durable.WriteFile(devicePath(), b, 0o600); err != nil {
		return fmt.Errorf("write %s: %v", devicePath(), err)
	}
	fmt.Fprintln(os.Stderr, "register key saved to "+devicePath())
	return nil
}

// runBackup prints a single Crockford base32 blob containing this
// device's RootKey and, if it has one, its register key. The output is
// the minimum a user needs to restore full read/write access on a
// fresh machine (`kv pair`); treat it as sensitive.
func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	_ = fs.Parse(args)

	d, err := loadDevice()
	if err != nil {
		return err
	}
	blob := vault.EncodeBackup(d.rk, d.reg)

	if d.reg == nil {
		fmt.Fprintln(os.Stderr,
			"warning: no register key on this device; backup contains RootKey only.")
		fmt.Fprintln(os.Stderr,
			"         To include the register key, install it first with")
		fmt.Fprintln(os.Stderr,
			"         `kv set-register-key <base32>`.")
	}
	return writeOutput(fs.Arg(0), []byte(blob+"\n"))
}

// writeOutput writes out to stdout when path is "" or "-", and
// otherwise to path, mode 0600 (it may hold a RootKey or plaintext
// values), replacing the file only once all of out is written. A
// command builds out whole before calling it, so a run that fails
// leaves an earlier backup or export as it was.
func writeOutput(path string, out []byte) error {
	if path == "" || path == "-" {
		if _, err := os.Stdout.Write(out); err != nil {
			return fmt.Errorf("write: %v", err)
		}
		return nil
	}
	if err := durable.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %v", path, err)
	}
	return nil
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
func runSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	useRegex := fs.Bool("regex", false, "interpret query as a Go regexp")
	scopeKey := fs.Bool("key", false, "match against keys only")
	scopeValue := fs.Bool("value", false, "match against values only")
	keysOnly := fs.Bool("keys-only", false, "print bare keys, one per line (no table)")
	caseSensitive := fs.Bool("case-sensitive", false, "exact-case match (default is case-insensitive)")
	truncate := fs.Bool("truncate", false, "truncate long values to one line (default: wrap)")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: kv search [--regex] [--key|--value] [--keys-only] [--case-sensitive] [--truncate] <query>")
	}
	if *scopeKey && *scopeValue {
		return errors.New("--key and --value are mutually exclusive (omit both to match either)")
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
			return fmt.Errorf("regex: %v", err)
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

	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	all, err := c.All()
	if err != nil {
		return fmt.Errorf("search: %v", err)
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
		return nil
	}

	if *keysOnly {
		for _, h := range hits {
			fmt.Println(h.key)
		}
		return nil
	}
	renderTable(hits, *truncate)
	return nil
}

// --- KEY/VALUE table rendering ---

type kvRow struct{ key, val string }

// renderTable prints rows as an aligned KEY/VALUE table. On a terminal
// the value column is sized to the width and a long value is wrapped
// across continuation lines aligned under the value column — breaking
// on the value's own newlines first, then wrapping each logical line.
// With truncate set, each value is instead squeezed onto a single line
// with an ellipsis. When stdout is not a terminal, full values are
// emitted tab-separated so pipes and scripts get everything. Widths
// are measured in runes; wide (CJK) glyphs are not double-counted.
func renderTable(rows []kvRow, truncate bool) {
	width := tableWidth()
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

// tableWidth is the terminal's width (80 if a terminal of unknown
// size), or 0 when stdout is not a terminal: emit full, untruncated
// output.
func tableWidth() int {
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

func runPut(args []string) error {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: kv put [-v] <key> [value|-]")
	}
	key := []byte(fs.Arg(0))
	var value []byte
	switch {
	case fs.NArg() >= 2 && fs.Arg(1) != "-":
		value = []byte(fs.Arg(1))
	default:
		b, err := io.ReadAll(bufio.NewReader(os.Stdin))
		if err != nil {
			return fmt.Errorf("read stdin: %v", err)
		}
		value = b
	}
	if len(value) == 0 {
		return fmt.Errorf("empty value is a tombstone; use `kv del %s` instead", string(key))
	}
	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	if err := c.Put(key, value); err != nil {
		return fmt.Errorf("put: %v", err)
	}
	return syncOnce(c)
}

func runGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: kv get <key>")
	}
	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	v, ok, err := c.Get([]byte(fs.Arg(0)))
	if err != nil {
		return fmt.Errorf("get: %v", err)
	}
	if !ok {
		return exitCode(1)
	}
	_, _ = os.Stdout.Write(v)
	// Add a trailing newline only for interactive display; when piped or
	// redirected, emit the value verbatim so it round-trips byte-exactly.
	if term.IsTerminal(int(os.Stdout.Fd())) && !bytes.HasSuffix(v, []byte("\n")) {
		fmt.Println()
	}
	return nil
}

func runDel(args []string) error {
	fs := flag.NewFlagSet("del", flag.ExitOnError)
	force := fs.Bool("f", false, "tombstone even if the key is not present locally")
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: kv del [-v] [-f] <key>")
	}
	key := []byte(fs.Arg(0))

	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	// Refuse to write a tombstone for a key that is not currently live —
	// that would only add dead weight. -f overrides (e.g. to delete a
	// value that exists on the server but has not been synced locally).
	if !*force {
		if _, ok, err := c.Get(key); err != nil {
			return fmt.Errorf("del: %v", err)
		} else if !ok {
			fmt.Fprintf(os.Stderr, "kv: no such key: %s\n", fs.Arg(0))
			return exitCode(1)
		}
	}
	if err := c.Delete(key); err != nil {
		return fmt.Errorf("del: %v", err)
	}
	return syncOnce(c)
}

func runLs(_ []string) error {
	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	keys, err := c.List()
	if err != nil {
		return fmt.Errorf("list: %v", err)
	}
	for _, k := range keys {
		fmt.Println(k)
	}
	return nil
}

func runSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	return syncOnce(c)
}

func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	_ = fs.Parse(args)

	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	var buf bytes.Buffer
	if err := exportCSV(c, &buf); err != nil {
		return fmt.Errorf("export: %v", err)
	}
	return writeOutput(fs.Arg(0), buf.Bytes())
}

func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	addVerboseFlag(fs)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: kv import [-v] <file|->")
	}
	var r io.Reader
	if fs.Arg(0) == "-" {
		r = bufio.NewReader(os.Stdin)
	} else {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			return fmt.Errorf("open %s: %v", fs.Arg(0), err)
		}
		defer f.Close()
		r = f
	}
	c, closeLog, err := openClient()
	if err != nil {
		return err
	}
	defer closeLog()
	n, err := importCSV(c, r)
	if err != nil {
		return fmt.Errorf("import: %v", err)
	}
	fmt.Fprintf(os.Stderr, "imported %d rows\n", n)
	return syncOnce(c)
}

func syncOnce(c *client.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*httpTimeout)
	defer cancel()
	if err := c.Sync(ctx); err != nil {
		return fmt.Errorf("sync: %v", err)
	}
	return nil
}

// --- helpers ---

func fatal(f string, args ...any) {
	fmt.Fprintf(os.Stderr, "kv: "+f+"\n", args...)
	os.Exit(1)
}
