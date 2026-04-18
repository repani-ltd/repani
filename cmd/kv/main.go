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
	"strings"
	"time"

	"github.com/pavlos/kv/client"
	"github.com/pavlos/kv/store"
	"github.com/pavlos/kv/vault"
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

func vaultKeyPath() string  { return filepath.Join(homeDir(), "vault.key") }
func entriesPath() string   { return filepath.Join(homeDir(), "entries.log") }
func registeredPath() string { return filepath.Join(homeDir(), "registered") }

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
// TOFU registration for the given server URL. The file stores one
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
	return c
}

// --- commands ---

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
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
	// Materialize an empty log with the proper magic header.
	if ls, err := store.OpenLogStore(entriesPath()); err == nil {
		_ = ls.Close()
	}
	fmt.Println(vault.EncodeRootKey(rk))
}

func runPair(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
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
	if ls, err := store.OpenLogStore(entriesPath()); err == nil {
		_ = ls.Close()
	}
	fmt.Println("paired")
}

func runPairBlob(_ []string) {
	rk := loadRootKey()
	fmt.Println(vault.EncodeRootKey(rk))
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
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
