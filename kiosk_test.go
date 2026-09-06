package kiosk

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// okHandler is a caller's routes: one resource, nothing else.
func okHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", Static(body, "text/html; charset=utf-8", Revalidate))
	return mux
}

func wrapped(t *testing.T, cfg Config) http.Handler {
	t.Helper()
	cfg.Addr = "127.0.0.1:0"
	if cfg.Handler == nil {
		cfg.Handler = okHandler()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if err := cfg.setDefaults(); err != nil {
		t.Fatal(err)
	}
	return wrap(cfg)
}

func TestMethodGate(t *testing.T) {
	h := wrapped(t, Config{})
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
		resp := get(t, h, m, "/", nil)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s -> %d, want 405", m, resp.StatusCode)
		}
		if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s: Allow = %q", m, got)
		}
	}
	for _, m := range []string{"GET", "HEAD"} {
		if resp := get(t, h, m, "/", nil); resp.StatusCode != 200 {
			t.Errorf("%s -> %d, want 200", m, resp.StatusCode)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := wrapped(t, Config{})
	resp := get(t, h, "GET", "/", nil)
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": DefaultCSP,
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// Strict-Transport-Security belongs to whoever terminates TLS.
	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("kiosk set HSTS itself: %q", got)
	}
}

func TestCustomCSP(t *testing.T) {
	h := wrapped(t, Config{CSP: "default-src 'none'"})
	if got := get(t, h, "GET", "/", nil).Header.Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("CSP = %q", got)
	}
}

func TestHealth(t *testing.T) {
	h := wrapped(t, Config{})
	resp := get(t, h, "GET", HealthPath, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	b, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(b)) != "ok" {
		t.Errorf("body = %q", b)
	}
	if resp := get(t, h, "POST", HealthPath, nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST %s -> %d, want 405", HealthPath, resp.StatusCode)
	}
}

// TestHealthDoesNotReachTheHandler keeps a proxy's polling out of the
// caller's routes.
func TestHealthDoesNotReachTheHandler(t *testing.T) {
	reached := false
	h := wrapped(t, Config{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})})
	get(t, h, "GET", HealthPath, nil)
	if reached {
		t.Fatal("the health check reached the caller's handler")
	}
}

func TestConfigRequiresAddrAndHandler(t *testing.T) {
	if err := (&Config{Handler: okHandler()}).setDefaults(); err == nil {
		t.Error("empty Addr accepted")
	}
	if err := (&Config{Addr: "127.0.0.1:0"}).setDefaults(); err == nil {
		t.Error("nil Handler accepted")
	}
}

func TestDefaultsAreBounds(t *testing.T) {
	cfg := Config{Addr: "127.0.0.1:0", Handler: okHandler()}
	if err := cfg.setDefaults(); err != nil {
		t.Fatal(err)
	}
	// The failure this package exists to stop: a zero timeout is not
	// a default, it is no bound at all.
	for name, d := range map[string]time.Duration{
		"ReadHeaderTimeout": cfg.ReadHeaderTimeout,
		"ReadTimeout":       cfg.ReadTimeout,
		"WriteTimeout":      cfg.WriteTimeout,
		"IdleTimeout":       cfg.IdleTimeout,
		"ShutdownGrace":     cfg.ShutdownGrace,
	} {
		if d <= 0 {
			t.Errorf("%s = %v", name, d)
		}
	}
	if cfg.MaxHeaderBytes <= 0 {
		t.Errorf("MaxHeaderBytes = %d", cfg.MaxHeaderBytes)
	}
	if cfg.CSP == "" {
		t.Error("CSP is empty")
	}
}

// freeAddr returns a loopback address nothing is listening on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// waitUp polls the health endpoint until the kiosk answers.
func waitUp(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + HealthPath)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("kiosk never came up on %s", addr)
}

func TestServeShutsDownCleanly(t *testing.T) {
	addr := freeAddr(t)
	closed := false
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Config{
			Addr:    addr,
			Handler: okHandler(),
			Logger:  slog.New(slog.DiscardHandler),
			Close:   func() error { closed = true; return nil },
		})
	}()

	waitUp(t, addr)
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the context was cancelled")
	}
	// The step a server that exits from its listener goroutine never
	// reaches.
	if !closed {
		t.Fatal("Config.Close was not called")
	}
}

// TestServeReturnsBindError is the regression for a daemon that
// called os.Exit from inside its listener goroutine, skipping its own
// cleanup and giving its caller nothing to handle.
func TestServeReturnsBindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	closed := false
	err = Serve(context.Background(), Config{
		Addr:    ln.Addr().String(),
		Handler: okHandler(),
		Logger:  slog.New(slog.DiscardHandler),
		Close:   func() error { closed = true; return nil },
	})
	if err == nil {
		t.Fatal("Serve accepted an address already in use")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("error = %v, want a listen failure", err)
	}
	// Nothing was opened, so nothing is closed: the caller still owns
	// its content source and can decide what a dead port means.
	if closed {
		t.Error("Close was called for a server that never served")
	}
}

func TestServeReportsCloseError(t *testing.T) {
	addr := freeAddr(t)
	want := errors.New("store did not flush")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Config{
			Addr:    addr,
			Handler: okHandler(),
			Logger:  slog.New(slog.DiscardHandler),
			Close:   func() error { return want },
		})
	}()
	waitUp(t, addr)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("Serve returned %v, want %v", err, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return")
	}
}
