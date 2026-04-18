// kvserve: ASCON-KV server.
//
// Three TLS modes:
//
//	# 1. plain HTTP (dev/local)
//	kvserve --addr :8080 --data ./data
//
//	# 2. file-based TLS (behind a cert you already have)
//	kvserve --addr :8443 --data ./data \
//	        --tls-cert cert.pem --tls-key key.pem
//
//	# 3. autocert / Let's Encrypt (production, single public domain)
//	kvserve --domain vault.example.com --data /opt/kvserve/data \
//	        --cert-dir /opt/kvserve/data/acme-cache
//
// In mode 3 the server binds :80 (for HTTP-01 ACME challenges and a
// redirect to HTTPS) and :443 (the actual API). Both ports require
// CAP_NET_BIND_SERVICE when running under a non-root user.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/pavlos/kv/server"
	"github.com/pavlos/kv/vault"
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

	addr := flag.String("addr", ":8080", "address to listen on (modes 1 and 2)")
	data := flag.String("data", "./data", "data directory (created if missing)")
	tlsCert := flag.String("tls-cert", "", "path to TLS certificate (mode 2)")
	tlsKey := flag.String("tls-key", "", "path to TLS private key (mode 2)")
	domain := flag.String("domain", "", "public DNS name for autocert (mode 3); enables :80 and :443")
	certDir := flag.String("cert-dir", "", "ACME cache directory (mode 3, required with --domain)")
	readTimeout := flag.Duration("read-timeout", 60*time.Second, "per-request read timeout")
	writeTimeout := flag.Duration("write-timeout", 60*time.Second, "per-request write timeout")
	idleTimeout := flag.Duration("idle-timeout", 120*time.Second, "keep-alive idle timeout")
	shutdownGrace := flag.Duration("shutdown-grace", 30*time.Second, "graceful shutdown deadline")
	maxVaults := flag.Uint64("max-vaults", 10_000, "hard cap on number of registered vaults (0 = unlimited)")
	registerKeyFile := flag.String("register-key-file", "", "path to a 16-byte file that gates REGISTER; omit for permissive TOFU")
	flag.Parse()

	if (*tlsCert != "") != (*tlsKey != "") {
		log.Fatal("kvserve: --tls-cert and --tls-key must be provided together")
	}
	if *domain != "" && *certDir == "" {
		log.Fatal("kvserve: --domain requires --cert-dir")
	}
	if *domain != "" && *tlsCert != "" {
		log.Fatal("kvserve: --domain and --tls-cert are mutually exclusive")
	}

	quotas := server.DefaultQuotas()
	quotas.MaxVaults = *maxVaults
	srv, err := server.NewWithOptions(server.Options{
		DataDir:         *data,
		Quotas:          quotas,
		RegisterKeyFile: *registerKeyFile,
	})
	if err != nil {
		log.Fatalf("kvserve: %v", err)
	}
	if *registerKeyFile != "" {
		log.Printf("kvserve: REGISTER gated by %s", *registerKeyFile)
	} else {
		log.Print("kvserve: REGISTER is permissive (any client can create a new vault). Set --register-key-file for production.")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	switch {
	case *domain != "":
		runAutocert(srv, *domain, *certDir, *readTimeout, *writeTimeout, *idleTimeout, *shutdownGrace, stop, *data, *maxVaults)
	default:
		runSingle(srv, *addr, *tlsCert, *tlsKey, *readTimeout, *writeTimeout, *idleTimeout, *shutdownGrace, stop, *data, *maxVaults)
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

// runSingle handles modes 1 (plain HTTP) and 2 (file-based TLS).
func runSingle(h http.Handler, addr, certFile, keyFile string, rto, wto, ito, grace time.Duration, stop chan os.Signal, data string, maxVaults uint64) {
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadTimeout:       rto,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      wto,
		IdleTimeout:       ito,
	}

	serverErr := make(chan error, 1)
	go func() {
		if certFile != "" {
			serverErr <- httpSrv.ListenAndServeTLS(certFile, keyFile)
		} else {
			serverErr <- httpSrv.ListenAndServe()
		}
	}()

	if certFile != "" {
		log.Printf("kvserve: listening on %s (TLS), data=%s, max-vaults=%d", addr, data, maxVaults)
	} else {
		log.Printf("kvserve: listening on %s (plaintext), data=%s, max-vaults=%d", addr, data, maxVaults)
		log.Print("kvserve: plaintext deployment — v1 MACs protect steady-state traffic, " +
			"but the first sync for each vault is vulnerable to MITM hijack. TLS recommended.")
	}

	awaitShutdown(httpSrv, nil, serverErr, stop, grace)
}

// runAutocert handles mode 3 (Let's Encrypt via autocert). A second
// server on :80 serves ACME challenges and HTTPS-redirects everything
// else.
func runAutocert(h http.Handler, domain, certDir string, rto, wto, ito, grace time.Duration, stop chan os.Signal, data string, maxVaults uint64) {
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		log.Fatalf("kvserve: cert-dir: %v", err)
	}
	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
		Cache:      autocert.DirCache(certDir),
	}

	// :443 — the API itself.
	tlsCfg := m.TLSConfig()
	tlsCfg.MinVersion = tls.VersionTLS12
	tlsSrv := &http.Server{
		Addr:              ":443",
		Handler:           h,
		TLSConfig:         tlsCfg,
		ReadTimeout:       rto,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      wto,
		IdleTimeout:       ito,
	}

	// :80 — ACME HTTP-01 challenges and HTTPS redirects.
	httpSrv := &http.Server{
		Addr:              ":80",
		Handler:           m.HTTPHandler(nil),
		ReadTimeout:       rto,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      wto,
		IdleTimeout:       ito,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpSrv.ListenAndServe()
	}()
	go func() {
		serverErr <- tlsSrv.ListenAndServeTLS("", "")
	}()

	log.Printf("kvserve: listening on :80 and :443 (autocert %s), data=%s, cert-dir=%s, max-vaults=%d",
		domain, data, certDir, maxVaults)

	awaitShutdown(tlsSrv, httpSrv, serverErr, stop, grace)
}

// awaitShutdown blocks until either a server errors or a stop signal
// arrives, then gracefully shuts down both servers (httpSrv may be nil).
func awaitShutdown(primary, secondary *http.Server, serverErr chan error, stop chan os.Signal, grace time.Duration) {
	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("kvserve: %v", err)
		}
	case sig := <-stop:
		log.Printf("kvserve: %s received, draining…", sig)
		ctx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		if err := primary.Shutdown(ctx); err != nil {
			log.Printf("kvserve: shutdown: %v", err)
		}
		if secondary != nil {
			if err := secondary.Shutdown(ctx); err != nil {
				log.Printf("kvserve: shutdown http: %v", err)
			}
		}
	}
}
