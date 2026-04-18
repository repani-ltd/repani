// kvserve: ASCON-KV server.
//
// Usage:
//
//	kvserve --addr :8080 --data ./data
//	kvserve --addr :8443 --data ./data --tls-cert cert.pem --tls-key key.pem
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pavlos/kv/server"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	data := flag.String("data", "./data", "data directory (created if missing)")
	tlsCert := flag.String("tls-cert", "", "path to TLS certificate (enables TLS)")
	tlsKey := flag.String("tls-key", "", "path to TLS private key (required with --tls-cert)")
	readTimeout := flag.Duration("read-timeout", 60*time.Second, "per-request read timeout")
	writeTimeout := flag.Duration("write-timeout", 60*time.Second, "per-request write timeout")
	idleTimeout := flag.Duration("idle-timeout", 120*time.Second, "keep-alive idle timeout")
	shutdownGrace := flag.Duration("shutdown-grace", 30*time.Second, "graceful shutdown deadline")
	maxVaults := flag.Uint64("max-vaults", 10_000, "hard cap on number of registered vaults (0 = unlimited)")
	flag.Parse()

	if (*tlsCert != "") != (*tlsKey != "") {
		log.Fatal("kvserve: --tls-cert and --tls-key must be provided together")
	}

	quotas := server.DefaultQuotas()
	quotas.MaxVaults = *maxVaults
	srv, err := server.NewWithOptions(server.Options{DataDir: *data, Quotas: quotas})
	if err != nil {
		log.Fatalf("kvserve: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadTimeout:       *readTimeout,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      *writeTimeout,
		IdleTimeout:       *idleTimeout,
	}

	// Catch SIGINT/SIGTERM for graceful shutdown so in-flight requests
	// complete their fsync rather than leaving a half-written log record.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		if *tlsCert != "" {
			serverErr <- httpSrv.ListenAndServeTLS(*tlsCert, *tlsKey)
		} else {
			serverErr <- httpSrv.ListenAndServe()
		}
	}()

	if *tlsCert != "" {
		log.Printf("kvserve: listening on %s (TLS), data=%s, max-vaults=%d", *addr, *data, *maxVaults)
	} else {
		log.Printf("kvserve: listening on %s (plaintext), data=%s, max-vaults=%d", *addr, *data, *maxVaults)
		log.Print("kvserve: plaintext deployment — v1 MACs protect steady-state traffic, " +
			"but the first sync for each vault is vulnerable to MITM hijack. TLS recommended.")
	}

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
