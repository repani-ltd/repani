// The server lifecycle: bounds, signals, readiness, shutdown.
package kiosk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Bounds every kiosk starts with. They are defaults rather than
// options because a server without them is the failure this package
// exists to stop: an unbounded connection costs an attacker nothing
// and costs the host a file descriptor for as long as it likes.
const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 30 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownGrace     = 20 * time.Second
	DefaultMaxHeaderBytes    = 64 << 10
)

// DefaultCSP suits a page that carries its own assets: everything
// loads from this origin, nothing frames it, no form goes anywhere. A
// page with inline styles or scripts needs its own policy naming
// them; kiosk will not guess one.
const DefaultCSP = "default-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// HealthPath is where Health is conventionally mounted, so that every
// daemon here spells it the same and one proxy configuration fits all
// of them. Mounting it is what lets a restart wait for a real answer
// instead of a sleep.
const HealthPath = "/healthz"

// Config is one kiosk. Addr and Handler are required; every duration
// left at zero takes its default above.
type Config struct {
	// Addr is the address to listen on -- a loopback address in
	// production, because TLS belongs to the proxy in front.
	Addr string

	// Handler serves every request, and its routes declare their own
	// methods: a route registered "GET /page" is answered 405 with an
	// Allow header for anything else, by http.ServeMux, before any
	// handler runs. Serve has no routes and no method policy of its
	// own -- mount Health here for liveness.
	Handler http.Handler

	// Logger receives the lifecycle lines, the access log, and the
	// http.Server's own connection errors. Nil means slog.Default.
	Logger *slog.Logger

	// AccessLog logs one line per request at info level.
	AccessLog bool

	// CSP is the Content-Security-Policy header value. Empty means
	// DefaultCSP.
	CSP string

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration

	// ShutdownGrace bounds how long in-flight requests have to finish
	// once shutdown starts.
	ShutdownGrace time.Duration

	MaxHeaderBytes int

	// Close releases whatever the handler reads from, after the last
	// request has finished and before Serve returns. This is the step
	// a server that exits from its listener goroutine never reaches.
	Close func() error
}

func (c *Config) setDefaults() error {
	if c.Addr == "" {
		return errors.New("kiosk: Config.Addr is empty")
	}
	if c.Handler == nil {
		return errors.New("kiosk: Config.Handler is nil")
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.CSP == "" {
		c.CSP = DefaultCSP
	}
	if c.ReadHeaderTimeout == 0 {
		c.ReadHeaderTimeout = DefaultReadHeaderTimeout
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = DefaultReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = DefaultWriteTimeout
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = DefaultIdleTimeout
	}
	if c.ShutdownGrace == 0 {
		c.ShutdownGrace = DefaultShutdownGrace
	}
	if c.MaxHeaderBytes == 0 {
		c.MaxHeaderBytes = DefaultMaxHeaderBytes
	}
	return nil
}

// Serve runs the kiosk until ctx is cancelled, SIGINT or SIGTERM
// arrives, or the listener fails, and returns only once everything is
// shut down. A failure to bind is returned, not fatal: the caller
// decides what a dead port means.
//
// The order matters and is the point of the function. The signal
// handler is installed before the listener exists, so a signal in the
// startup window cannot kill the process outright. The listener is
// bound before serving starts, so "listening" in the log is true when
// it is written and the readiness notification that follows it is
// not a guess. On the way out, the server stops accepting and drains
// within ShutdownGrace, and only then is Config.Close called.
func Serve(ctx context.Context, cfg Config) error {
	if err := cfg.setDefaults(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Handler:           wrap(cfg),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(cfg.Logger.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("kiosk: listen: %w", err)
	}
	cfg.Logger.Info("kiosk listening", "addr", ln.Addr().String())
	notify("READY=1")

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	var first error
	select {
	case <-ctx.Done():
		cfg.Logger.Info("kiosk shutting down", "grace", cfg.ShutdownGrace)
	case first = <-serveErr:
		if first != nil {
			first = fmt.Errorf("kiosk: serve: %w", first)
		}
	}
	notify("STOPPING=1")

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && first == nil {
		first = fmt.Errorf("kiosk: shutdown: %w", err)
	}
	if cfg.Close != nil {
		if err := cfg.Close(); err != nil && first == nil {
			first = fmt.Errorf("kiosk: close: %w", err)
		}
	}
	cfg.Logger.Info("kiosk stopped")
	return first
}

// wrap builds the handler chain, outermost first: access log,
// response headers, then the caller's routes.
//
// Nothing here routes. kiosk owns no mux and claims no path, so the
// caller's route table is the whole route table: it is what the
// package serves, and reading it tells you everything the server
// answers. Method matching comes with it -- http.ServeMux has matched
// on method since Go 1.22 and answers a mismatch with 405 and an
// Allow header built from the patterns actually registered, which is
// better informed than any server-wide list -- so a publication that
// registers "GET /" refuses POST without this package having an
// opinion, and a station that wants POST says so in its routes.
func wrap(cfg Config) http.Handler {
	h := secure(cfg.Handler, cfg.CSP)
	if cfg.AccessLog {
		h = accessLog(h, cfg.Logger)
	}
	return h
}

// Health returns the liveness handler, to be mounted by the caller:
//
//	mux.Handle("GET "+kiosk.HealthPath, kiosk.Health())
//
// It is a handler rather than a route the package installs for you.
// An installed route would have to be intercepted ahead of the mux,
// which claims a path out of the caller's own namespace and re-derives
// the method matching the mux does for free. Forgetting to mount it
// fails loudly -- the proxy's health check goes red on the first
// deploy -- so it needs no protection from forgetting.
//
// Register it for GET: it is the proxy asking whether the process is
// alive, and that question is a GET even on a server that otherwise
// answers only POST.
func Health() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, "ok")
	})
}

// secure sets the headers only the origin can set. Strict-Transport-
// Security is absent on purpose: it belongs to whoever terminates TLS,
// and a kiosk never does.
func secure(next http.Handler, csp string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

func accessLog(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("req",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"ms", time.Since(start).Milliseconds())
	})
}

// recorder remembers the status and size of a response. It carries no
// client address: the proxy in front logs those, and a publication
// has no use for a second copy.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
