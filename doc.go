/*
Package kiosk serves a publication over HTTP: the unattended public
stand where a reader collects a copy and hands nothing back.

What a kiosk serves is its routes' business, not this package's. A
publication registers GET and refuses everything else; a station that
takes a sealed envelope across the counter registers POST. Since Go
1.22 http.ServeMux matches on method and answers a mismatch with 405
and an Allow header built from the patterns actually registered, so
the refusal is already correct, already per-route, and better informed
than a server-wide list could be. kiosk adds no method policy of its
own and has nothing to keep in step.

What stays out is long-lived connections -- server-sent events, a
socket to talk back on. That is not a method rule but a lifecycle one:
one streaming endpoint is what cost the last server its write timeout
for every request in the process.

# What this package is for

Three daemons in this house grew their own HTTP lifecycle, and each
was correct where the others were wrong: one had no timeouts at all,
one disabled its write timeout for a streaming endpoint and so lost
the bound for every request, one exited from inside its listener
goroutine and skipped its own cleanup, one registered its signal
handler after it was already serving, and one never closed its store
on the way out. None signalled readiness, so no deploy could tell
whether a restart had worked. kiosk is those five lifecycles written
once, with the bounds on by default rather than available on request.

# Serving

Serve runs one server until its context ends or a signal arrives. It
binds the listener before it returns, so a bind failure is an error to
the caller and not a process that dies in a goroutine; it installs the
signal handler before the listener exists, so a signal in the startup
window cannot take the default action; and it drains in one order --
stop accepting, let in-flight requests finish inside the grace, then
close the content source.

Serve wraps the caller's handler in three things, outermost first:

  - /healthz, answered by the package, so a proxy can health-check an
    upstream and a unit can gate its restart on a real answer. It is
    the one route the caller's mux never sees, so it answers its own
    methods, and it is a GET whatever else the server serves: a
    station that takes only POST still has to say whether it is alive;
  - the access log, when Config.AccessLog is set: method, path,
    status, bytes and duration, and deliberately no client address --
    the TLS proxy in front already logs those, and a publication has
    no reason to keep them twice;
  - the response headers every origin must set for itself: nosniff, a
    referrer policy, and a content security policy.

# TLS

There is none, by design. A kiosk speaks plain HTTP on a loopback
address and a reverse proxy in front of it terminates TLS, owns the
certificate, and sets Strict-Transport-Security -- the one security
header that belongs to whoever holds the TLS connection, and the one
this package therefore does not set. See KIOSK.t for the unit and
site-block shapes that make a restart invisible to readers.

# Caching

Static serves one body with a Policy that says how a client may reuse
it. The two that matter come straight from the shape of a publication:
a file named by its content can never change at its URL and is served
Immutable, for a year, with no revalidation; the one file that changes
in place is served Revalidate, which stores it but asks every time and
usually gets a 304 back.

Conditional requests are handled by net/http's ServeContent against
the ETag set here, which is the whole reason to route through it: an
If-None-Match carrying a list of candidate tags, a weak tag, or "*" is
answered correctly, and hand-rolled equality on that header -- the bug
this package was written to stop repeating -- silently returns a full
body to every client that keeps two versions.
*/
package kiosk
