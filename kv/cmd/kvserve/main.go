// kvserve: ASCON-KV server.
//
//	kvserve [serve] --addr 127.0.0.1:8082 --data /opt/kvserve/data \
//	                --register-key-file /opt/kvserve/register.key
//	kvserve gen-register-key
//	kvserve drop-vault --data /opt/kvserve/data VAULTID
//	kvserve help
//
// The serving verb is optional: flags alone serve, and so does an
// explicit "serve", so a unit file names the verb or not.
//
// kvserve speaks plain HTTP on a loopback address; TLS belongs to a
// fronting reverse proxy (see deploy/). Every frame is sealed, so
// only metadata (vault id, sizes, timing) crosses the loopback.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"repani.com/kiosk"
	"repani.com/kv/server"
	"repani.com/kv/vault"
)

const usage = `usage: kvserve [serve] [flags]     serve the sync endpoint
       kvserve gen-register-key    print a fresh register-gate secret
       kvserve drop-vault [--data DIR] VAULTID
                                   delete a vault (32 hex) for good;
                                   stop the server first
       kvserve version             print this binary's build
       kvserve help                this text

The serving verb is optional: "kvserve --addr ..." and
"kvserve serve --addr ..." are the same command.

The protocol kvserve speaks is PROTOCOL.t (repani/kv, a pica
document).

flags:
`

func main() {
	verb, rest := parseVerb(os.Args[1:])
	switch verb {
	case "serve":
		runServe(rest)
	case "gen-register-key":
		runGenRegisterKey()
	case "drop-vault":
		runDropVault(rest)
	case "version":
		fmt.Println(kiosk.ReadBuild())
	case "help":
		printUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "kvserve: unknown subcommand %q\n\n", verb)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

// parseVerb splits the optional leading verb from the flags. No
// arguments, or a leading flag, means serve; so does an explicit
// "serve", which is what makes a unit file portable between this
// daemon and its siblings.
func parseVerb(args []string) (verb string, rest []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "serve", args
	}
	if args[0] == "serve" {
		return "serve", args[1:]
	}
	return args[0], args[1:]
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, usage)
	fs, _ := serveFlags()
	fs.SetOutput(w)
	fs.PrintDefaults()
}

// opts is everything the serve flags set.
type opts struct {
	addr, data, registerKeyFile                           string
	readTimeout, writeTimeout, idleTimeout, shutdownGrace time.Duration
	quotas                                                server.Quotas
}

// serveFlags builds the serve flag set, in one place so that help can
// print the same defaults the daemon runs with.
func serveFlags() (*flag.FlagSet, *opts) {
	o := &opts{quotas: server.DefaultQuotas()}
	d := server.DefaultQuotas()
	fs := flag.NewFlagSet("kvserve", flag.ExitOnError)
	fs.StringVar(&o.addr, "addr", ":8080", "address to listen on")
	fs.StringVar(&o.data, "data", "./data", "data directory (created if missing)")
	fs.DurationVar(&o.readTimeout, "read-timeout", 60*time.Second, "per-request read timeout")
	fs.DurationVar(&o.writeTimeout, "write-timeout", 60*time.Second, "per-request write timeout")
	fs.DurationVar(&o.idleTimeout, "idle-timeout", 120*time.Second, "keep-alive idle timeout")
	fs.DurationVar(&o.shutdownGrace, "shutdown-grace", 30*time.Second, "graceful shutdown deadline")
	fs.Uint64Var(&o.quotas.MaxVaults, "max-vaults", d.MaxVaults, "hard cap on number of registered vaults")
	fs.Uint64Var(&o.quotas.MaxVaultBytes, "max-vault-bytes", d.MaxVaultBytes, "hard cap on the bytes one vault may hold")
	fs.Uint64Var(&o.quotas.MaxEntries, "max-entries", d.MaxEntries, "hard cap on the entries one vault may hold")
	fs.StringVar(&o.registerKeyFile, "register-key-file", "", "path to the register-gate secrets (Crockford base32, one per line, # comments); REQUIRED")
	return fs, o
}

// readRegisterKeys reads the register-key file: one RegisterKey blob
// per line, blank lines and lines starting with "#" ignored, so each
// key can carry a label naming who holds it. Revoking one is deleting
// its line and restarting; the others and every vault are untouched.
// A file with no key is an error: registration is disabled by not
// serving, never by an empty file.
func readRegisterKeys(path string) ([]vault.RegisterKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("register-key-file %s: %w", path, err)
	}
	var keys []vault.RegisterKey
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rk, err := vault.DecodeRegisterKey(line)
		if err != nil {
			return nil, fmt.Errorf("register-key-file %s:%d: %w", path, i+1, err)
		}
		keys = append(keys, rk)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("register-key-file %s: no register key", path)
	}
	return keys, nil
}

func runServe(args []string) {
	fs, o := serveFlags()
	_ = fs.Parse(args) // ExitOnError
	if o.registerKeyFile == "" {
		log.Fatal("kvserve: --register-key-file is required (generate one with `kvserve gen-register-key`)")
	}
	keys, err := readRegisterKeys(o.registerKeyFile)
	if err != nil {
		log.Fatalf("kvserve: %v", err)
	}
	srv, err := server.New(server.Options{DataDir: o.data, Quotas: o.quotas, RegisterKeys: keys})
	if err != nil {
		log.Fatalf("kvserve: %v", err)
	}
	log.Printf("kvserve: REGISTER gated by %d keys in %s, data=%s, quotas: %d vaults x %d bytes / %d entries",
		len(keys), o.registerKeyFile, o.data, o.quotas.MaxVaults, o.quotas.MaxVaultBytes, o.quotas.MaxEntries)

	// The protocol routes under "/", liveness beside them. The kv
	// server declares its own method and path, so a wrong method gets
	// 405 with an Allow header from its mux; "GET /healthz" is the
	// more specific pattern and wins over "/".
	mux := http.NewServeMux()
	mux.Handle("/", srv)
	mux.Handle("GET "+kiosk.HealthPath, kiosk.Health())
	// /version names the commit this binary was built from, so an
	// operator can ask the server what it is rather than trust a
	// deploy log. `kv version` reads it.
	mux.Handle("GET "+kiosk.VersionPath, kiosk.Version())

	// kiosk owns the lifecycle: bind before serving, signals installed
	// before the listener exists, READY=1 to the service manager, then
	// drain and close the vault stores in that order. Close releases
	// every vault's advisory lock, so the next process takes them
	// without waiting.
	if err := kiosk.Serve(context.Background(), kiosk.Config{
		Addr:          o.addr,
		Handler:       mux,
		AccessLog:     true,
		ReadTimeout:   o.readTimeout,
		WriteTimeout:  o.writeTimeout,
		IdleTimeout:   o.idleTimeout,
		ShutdownGrace: o.shutdownGrace,
		Close:         srv.Close,
	}); err != nil {
		log.Fatalf("kvserve: %v", err)
	}
}

// runGenRegisterKey generates a fresh 16-byte register key, encodes
// it as Crockford base32, and prints it (with a trailing newline) to
// stdout. Intended for the deploy pipeline:
//
//	kvserve gen-register-key > /opt/kvserve/register.key
//
// The emitted file is consumed by `kvserve --register-key-file`.
func runGenRegisterKey() {
	rk, err := vault.NewRegisterKey()
	if err != nil {
		log.Fatalf("kvserve: gen-register-key: %v", err)
	}
	fmt.Println(vault.EncodeRegisterKey(rk))
}

// runDropVault deletes one vault's directory from the data directory,
// entry log included. It works only while kvserve is stopped: a running
// server holds every vault's log open, so files removed under it keep
// being served until it restarts -- which is why removing them by hand
// appears to do nothing. The id is the vault's directory name; the
// RootKey never has to reach the server to find it.
//
//	systemctl stop kvserve
//	kvserve drop-vault --data /opt/kvserve/data VAULTID
//	systemctl start kvserve
func runDropVault(args []string) {
	fs := flag.NewFlagSet("drop-vault", flag.ExitOnError)
	data := fs.String("data", "./data", "data directory of the stopped server")
	_ = fs.Parse(args) // ExitOnError
	if fs.NArg() != 1 {
		log.Fatal("kvserve: drop-vault: want one VAULTID (32 hex digits)")
	}
	vid, err := vault.ParseVaultID(fs.Arg(0))
	if err != nil {
		log.Fatalf("kvserve: drop-vault: %v", err)
	}
	switch err := server.DropVaultDir(*data, vid); {
	case errors.Is(err, server.ErrVaultInUse):
		log.Fatalf("kvserve: drop-vault: %s is being served; stop kvserve first", vid)
	case errors.Is(err, server.ErrUnknownVault):
		log.Fatalf("kvserve: drop-vault: no vault %s in %s", vid, *data)
	case err != nil:
		log.Fatalf("kvserve: drop-vault: %v", err)
	}
	fmt.Printf("dropped %s\n", vid)
}
