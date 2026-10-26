// kvserve: ASCON-KV server.
//
//	kvserve --addr 127.0.0.1:8082 --data /opt/kvserve/data \
//	        --register-key-file /opt/kvserve/register_keys.fact
//	kvserve gen-register-key LABEL
//	kvserve drop-vault --data /opt/kvserve/data VAULTID [NOTE]
//	kvserve help
//
// kvserve speaks plain HTTP on a loopback address; TLS belongs to a
// fronting reverse proxy (see deploy/). Every frame is sealed, so
// only metadata (vault id, sizes, timing) crosses the loopback.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"repani.com/fact"
	"repani.com/kiosk"
	"repani.com/kv/server"
	"repani.com/kv/vault"
)

const usage = `usage: kvserve [flags]              serve the sync endpoint
       kvserve gen-register-key LABEL
                                   print a fresh register-gate secret as a
                                   line of the register-key file
       kvserve drop-vault --data DIR VAULTID [NOTE]
                                   end a vault (32 hex) for good: its
                                   record says ended, with NOTE, and its
                                   entries are deleted; never served or
                                   registered again; stop the server first
       kvserve version             print this binary's build
       kvserve help                this text

The register-key file is FACT (fact spec), one line per key, the
label naming who holds it:

  register_key:iou.key: str = "<26-char Crockford base32>"

Revoking a key is deleting its line and restarting; the other keys
and every vault are untouched. A vault's state is the record in its
directory, DIR/VAULTID/vault.fact; deleting the directory of an ended
vault, server stopped, lets it be registered again.

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
		runGenRegisterKey(rest)
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

// parseVerb splits a leading verb from the flags. No arguments, or a
// leading flag, means serve: serving has no verb.
func parseVerb(args []string) (verb string, rest []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "serve", args
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
	fs.StringVar(&o.addr, "addr", "127.0.0.1:8080", "address to listen on; loopback, for TLS at a fronting proxy")
	fs.StringVar(&o.data, "data", "./data", "data directory (created if missing)")
	fs.DurationVar(&o.readTimeout, "read-timeout", 60*time.Second, "per-request read timeout")
	fs.DurationVar(&o.writeTimeout, "write-timeout", 60*time.Second, "per-request write timeout")
	fs.DurationVar(&o.idleTimeout, "idle-timeout", 120*time.Second, "keep-alive idle timeout")
	fs.DurationVar(&o.shutdownGrace, "shutdown-grace", 30*time.Second, "graceful shutdown deadline")
	fs.Uint64Var(&o.quotas.MaxVaults, "max-vaults", d.MaxVaults, "hard cap on number of registered vaults")
	fs.Uint64Var(&o.quotas.MaxVaultBytes, "max-vault-bytes", d.MaxVaultBytes, "hard cap on the bytes one vault may hold")
	fs.Uint64Var(&o.quotas.MaxEntries, "max-entries", d.MaxEntries, "hard cap on the entries one vault may hold")
	fs.Int64Var(&o.quotas.WaitMaxMs, "wait-max-ms", d.WaitMaxMs, "longest a WAIT is held before answering OK; keep it under write-timeout and any proxy's")
	fs.StringVar(&o.registerKeyFile, "register-key-file", "", "the register-gate secrets, a FACT file (see above); REQUIRED")
	return fs, o
}

// readRegisterKeys reads the register-key file: a FACT document whose
// every fact is register_key:LABEL.key, a str holding one RegisterKey
// blob. It returns the keys and their labels, sorted by label. A file
// with no key is an error: registration is disabled by not serving,
// never by an empty file.
func readRegisterKeys(path string) (keys []vault.RegisterKey, labels []string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("register-key-file %s: %w", path, err)
	}
	facts, errs := fact.Load(b)
	if len(errs) > 0 {
		var all []error
		for _, e := range errs {
			all = append(all, fmt.Errorf("register-key-file %s:%d: %s", path, e.Line, e.Msg))
		}
		return nil, nil, errors.Join(all...)
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].Key < facts[j].Key })
	for _, f := range facts {
		label, ok := strings.CutPrefix(f.Key, "register_key:")
		if ok {
			label, ok = strings.CutSuffix(label, ".key")
		}
		if !ok || strings.Contains(label, ".") || f.Type.Raw != "str" {
			return nil, nil, fmt.Errorf("register-key-file %s:%d: want register_key:LABEL.key: str = \"<base32>\", got %s", path, f.Line, f.Key)
		}
		var blob string
		if err := json.Unmarshal([]byte(f.Value.Elems[0]), &blob); err != nil {
			return nil, nil, fmt.Errorf("register-key-file %s:%d: %w", path, f.Line, err)
		}
		rk, err := vault.DecodeRegisterKey(blob)
		if err != nil {
			return nil, nil, fmt.Errorf("register-key-file %s:%d: %w", path, f.Line, err)
		}
		keys, labels = append(keys, rk), append(labels, label)
	}
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("register-key-file %s: no register key", path)
	}
	return keys, labels, nil
}

func runServe(args []string) {
	fs, o := serveFlags()
	_ = fs.Parse(args) // ExitOnError
	if o.registerKeyFile == "" {
		log.Fatal("kvserve: --register-key-file is required (make one with `kvserve gen-register-key LABEL`)")
	}
	keys, labels, err := readRegisterKeys(o.registerKeyFile)
	if err != nil {
		log.Fatalf("kvserve: %v", err)
	}
	srv, err := server.New(server.Options{DataDir: o.data, Quotas: o.quotas, RegisterKeys: keys})
	if err != nil {
		log.Fatalf("kvserve: %v", err)
	}
	log.Printf("kvserve: REGISTER gated by %d keys (%s) in %s, data=%s, quotas: %d vaults x %d bytes / %d entries",
		len(keys), strings.Join(labels, ", "), o.registerKeyFile, o.data, o.quotas.MaxVaults, o.quotas.MaxVaultBytes, o.quotas.MaxEntries)

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
		Drain:         srv.Drain, // answers held WAITs, so a stop does not wait them out
		Close:         srv.Close,
	}); err != nil {
		log.Fatalf("kvserve: %v", err)
	}
}

// runGenRegisterKey generates a fresh 16-byte register key and prints
// it as one line of the register-key file, labelled:
//
//	kvserve gen-register-key iou
//	register_key:iou.key: str = "…"
//
// The label names who holds the key; it is a FACT segment (letters,
// digits, _, starting with a letter).
func runGenRegisterKey(args []string) {
	if len(args) != 1 || !fact.IsSegment(args[0]) {
		log.Fatal("kvserve: gen-register-key: want one LABEL: letters, digits and _, starting with a letter")
	}
	rk, err := vault.NewRegisterKey()
	if err != nil {
		log.Fatalf("kvserve: gen-register-key: %v", err)
	}
	fmt.Println(registerKeyLine(args[0], rk))
}

// registerKeyLine is rk as the register-key file's line for label.
func registerKeyLine(label string, rk vault.RegisterKey) string {
	return "register_key:" + label + ".key: str = " + fact.Quote(vault.EncodeRegisterKey(rk))
}

// runDropVault ends a vault for good: its record is rewritten as
// ended, with the note, and its entry log deleted (server.DropVaultDir).
// The record is what makes the drop stick: without it any device still
// holding the RootKey and a RegisterKey creates the vault again on its
// next sync. A vault this server never held is revoked all the same.
//
// It runs only while kvserve is stopped: a running server holds every
// vault's log open, so a vault it serves is refused. The id is the
// vault's directory name; the RootKey never has to reach the server
// to find it.
//
//	systemctl stop kvserve
//	kvserve drop-vault --data /opt/kvserve/data VAULTID [NOTE]
//	systemctl start kvserve
func runDropVault(args []string) {
	fs := flag.NewFlagSet("drop-vault", flag.ExitOnError)
	data := fs.String("data", "", "data directory of the stopped server; REQUIRED")
	_ = fs.Parse(args) // ExitOnError
	if *data == "" {
		log.Fatal("kvserve: drop-vault: --data is required")
	}
	if fs.NArg() < 1 {
		log.Fatal("kvserve: drop-vault: want a VAULTID (32 hex digits) and an optional note")
	}
	vid, err := vault.ParseVaultID(fs.Arg(0))
	if err != nil {
		log.Fatalf("kvserve: drop-vault: %v", err)
	}
	msg, err := dropVault(*data, vid, strings.Join(fs.Args()[1:], " "))
	if err != nil {
		log.Fatalf("kvserve: drop-vault: %v", err)
	}
	fmt.Println(msg)
}

// dropVault ends vid and says what it did.
func dropVault(data string, vid vault.VaultID, note string) (string, error) {
	note = strings.Join(strings.Fields(note), " ")
	already, err := server.DropVaultDir(data, vid, note)
	switch {
	case errors.Is(err, server.ErrVaultInUse):
		return "", fmt.Errorf("%s is being served: stop kvserve and run this again", vid)
	case err != nil:
		return "", fmt.Errorf("%s: %w", vid, err)
	case already:
		return fmt.Sprintf("%s had already ended; its record is unchanged", vid), nil
	}
	return fmt.Sprintf("%s ended: revoked, and its entries deleted", vid), nil
}
