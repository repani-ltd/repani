// kvserve: ASCON-KV server.
//
//	kvserve --addr 127.0.0.1:8082 --data /opt/kvserve/data \
//	        --register-key-file /opt/kvserve/register.key
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
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	log.Printf("kvserve: REGISTER gated by %s", *registerKeyFile)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadTimeout:       *readTimeout,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      *writeTimeout,
		IdleTimeout:       *idleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() { serverErr <- httpSrv.ListenAndServe() }()
	log.Printf("kvserve: listening on %s (plaintext behind your TLS proxy), data=%s, max-vaults=%d",
		*addr, *data, *maxVaults)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("kvserve: %v", err)
		}
	case sig := <-stop:
		log.Printf("kvserve: %s received, draining…", sig)
		ctx, cancel := context.WithTimeout(context.Background(), *shutdownGrace)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("kvserve: shutdown: %v", err)
		}
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
