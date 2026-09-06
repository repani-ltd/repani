// kvserve: ASCON-KV server.
//
//	kvserve [serve] --addr 127.0.0.1:8082 --data /opt/kvserve/data \
//	                --register-key-file /opt/kvserve/register.key
//	kvserve gen-register-key
//	kvserve help
//
// The serving verb is optional: flags alone serve, and so does an
// explicit "serve". qcweb, the sibling daemon on the same host,
// requires the verb in its unit file, and kvserve used to reject it,
// so a unit copied between the two died at ExecStart with exit 2 --
// visible only in the journal, and now (with Restart=always and a
// start limit) ending in a service systemd gives up on.
//
// kvserve speaks plain HTTP on a loopback address; TLS belongs to a
// fronting reverse proxy (see deploy/). Every frame is sealed, so
// only metadata (vault id, sizes, timing) crosses the loopback.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"repani.com/kiosk"
	"repani.com/kv/server"
	"repani.com/kv/vault"
)

func main() {
	// Subcommand dispatch. Anything that doesn't start with '-' on the
	// command line is treated as a subcommand; otherwise fall through
	// to the default serve-with-flags path.
	if len(os.Args) >= 2 && len(os.Args[1]) > 0 && os.Args[1][0] != '-' {
		switch os.Args[1] {
		case "gen-register-key":
			runGenRegisterKey()
			return
		default:
			fmt.Fprintf(os.Stderr, "kvserve: unknown subcommand %q\n", os.Args[1])
			os.Exit(2)
		}
	}

	addr := flag.String("addr", ":8080", "address to listen on")
	data := flag.String("data", "./data", "data directory (created if missing)")
	readTimeout := flag.Duration("read-timeout", 60*time.Second, "per-request read timeout")
	writeTimeout := flag.Duration("write-timeout", 60*time.Second, "per-request write timeout")
	idleTimeout := flag.Duration("idle-timeout", 120*time.Second, "keep-alive idle timeout")
	shutdownGrace := flag.Duration("shutdown-grace", 30*time.Second, "graceful shutdown deadline")
	defaults := server.DefaultQuotas()
	maxVaults := flag.Uint64("max-vaults", defaults.MaxVaults, "hard cap on number of registered vaults (0 = unlimited)")
	maxVaultBytes := flag.Uint64("max-vault-bytes", defaults.MaxVaultBytes, "hard cap on the bytes one vault may hold")
	maxEntries := flag.Uint64("max-entries", defaults.MaxEntries, "hard cap on the entries one vault may hold")
	registerKeyFile := flag.String("register-key-file", "", "path to the register-gate secret (Crockford base32); REQUIRED")
	flag.Parse()

	if *registerKeyFile == "" {
		log.Fatal("kvserve: --register-key-file is required (generate one with `kvserve gen-register-key`)")
	}
	quotas := defaults
	quotas.MaxVaults = *maxVaults
	quotas.MaxVaultBytes = *maxVaultBytes
	quotas.MaxEntries = *maxEntries
	srv, err := server.NewWithOptions(server.Options{
		DataDir:         *data,
		Quotas:          quotas,
		RegisterKeyFile: *registerKeyFile,
	})
	if err != nil {
		log.Fatalf("kvserve: %v", err)
	}
	log.Printf("kvserve: REGISTER gated by %s, data=%s, quotas: %d vaults x %d bytes / %d entries",
		*registerKeyFile, *data, quotas.MaxVaults, quotas.MaxVaultBytes, quotas.MaxEntries)

	// The protocol routes under "/", liveness beside them. The kv
	// server declares its own method and path, so a wrong method gets
	// 405 with an Allow header from its mux; "GET /healthz" is the
	// more specific pattern and wins over "/".
	mux := http.NewServeMux()
	mux.Handle("/", srv)
	mux.Handle("GET "+kiosk.HealthPath, kiosk.Health())

	// kiosk owns the lifecycle: bind before serving, signals installed
	// before the listener exists, READY=1 to the service manager, then
	// drain and close the vault stores in that order. Close is the
	// call this daemon never used to make -- it releases every vault's
	// advisory lock, which is what lets the next process take them
	// without waiting.
	if err := kiosk.Serve(context.Background(), kiosk.Config{
		Addr:          *addr,
		Handler:       mux,
		AccessLog:     true,
		ReadTimeout:   *readTimeout,
		WriteTimeout:  *writeTimeout,
		IdleTimeout:   *idleTimeout,
		ShutdownGrace: *shutdownGrace,
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
