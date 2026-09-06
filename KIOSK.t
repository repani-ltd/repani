KIOSK -- SERVING A PUBLICATION OVER HTTP
.date 2026-09-06
.by Pavlos Christoforou
.rights All rights reserved (c) repani.com
.rem Lab document. "The contract" is normative; "Findings" records
.rem what was measured on the live station on 2026-09-06 and why
.rem each default is what it is; "Decisions" is the ledger.

A kiosk is the unattended public stand where a reader collects a
copy and hands nothing back. It answers GET and HEAD, holds no
session, reads no request body, and writes nothing anywhere. TLS
belongs to the reverse proxy in front of it; a kiosk speaks plain
HTTP on a loopback address and never sees a certificate.

The package exists because three daemons in this house grew three
HTTP lifecycles, and each was correct exactly where the others
were wrong. This document states the contract they now share, the
measurements that set its defaults, and the deployment shape that
makes a restart invisible to readers.

# The contract

.item Routes declare their own methods. Serve has no method policy:
http.ServeMux has matched on method since Go 1.22 and answers a
mismatch with 405 and an Allow header built from the patterns
actually registered. A publication registers GET and refuses the
rest without this package holding an opinion; a station that takes
a sealed envelope registers POST and is carried unchanged.
.item The bounds are defaults, not options. A server with a zero
timeout has no bound at all, and every duration left unset takes
the value in the table below.
.item Serve binds before it serves. A bind failure is returned to
the caller; nothing calls os.Exit from inside a goroutine.
.item The signal handler is installed before the listener exists,
so a signal in the startup window cannot take the default action.
.item Readiness is announced, not assumed: READY=1 goes to the
service manager after the listener binds, so a restart completes
when the kiosk is serving rather than when it forked.
.item Shutdown has one order -- stop accepting, drain in-flight
requests within the grace, then close the content source -- and
Config.Close is the step a listener-goroutine exit never reaches.
.item The origin sets the headers only the origin can set:
nosniff, a referrer policy, and a content security policy.
Strict-Transport-Security is NOT among them: it belongs to
whoever terminates TLS.
.item Every body carries a validator and a caching rule. There is
no third state, and no body is served without both.
.item kiosk routes nothing and claims no path. Liveness is a
handler the caller mounts at /healthz in its own table, so it is an
ordinary GET route that the mux matches like any other.

# Defaults

.table 64 22L 12R 28P
  Bound | Default | Why
  ReadHeaderTimeout | 5s | A slowloris costs the attacker nothing
  ReadTimeout | 30s | No kiosk request has a body
  WriteTimeout | 30s | Nothing here streams
  IdleTimeout | 120s | Keep-alive is worth holding for a page's assets
  ShutdownGrace | 20s | Longer than any GET, shorter than a deploy
  MaxHeaderBytes | 64 KiB | Well over a real request, well under a page
.end

# Caching

Two policies carry a publication, and they fall out of its shape
rather than being chosen: a file named by its content can never
carry different bytes at that URL, and exactly one file changes in
place.

.table 64 10L 30L 22P
  Policy | Header | For
  Immutable | public, max-age=31536000, immutable | A content-named file
  Revalidate | no-cache | The manifest; any URL that outlives its bytes
  NoStore | no-store | Errors, health, anything a shared cache must not hold
.end

The validator is a strong ETag: the first eight bytes of the
body's SHA-256, hex, quoted. Conditional requests are answered by
net/http's ServeContent against that tag, which is the whole
reason to route through it -- If-None-Match is a LIST, the weak
comparison function is the correct one for GET, and "*" is legal.
Comparing that header to a single tag with == passes every hand
test in one browser and quietly returns a full body to any client
that keeps two candidate versions.

# Findings

Measured against the live station and its two siblings on
2026-09-06. Each default above answers one of these.

.item The live server had no timeouts at all -- not one of the
five -- so any connection could be held open indefinitely at no
cost. The only thing in front of it was the proxy's own defaults.
.item One daemon set WriteTimeout to zero deliberately, because a
single streaming endpoint needed it, and so lost the write bound
for every request in the process. The GET-only cut is what buys
that timeout back.
.item ReadHeaderTimeout appeared once across three daemons;
MaxHeaderBytes, ErrorLog, BaseContext and ConnState appeared
nowhere.
.item One daemon called os.Exit from inside its listener
goroutine on a bind failure, so its vault was never closed. One
registered its signal handler after it was already serving. One
never called its store's documented Close on the way out.
.item None signalled readiness. Every unit was Type=simple, so
the deploy script slept two seconds and hoped.
.item Conditional GET was wrong for conforming clients:
"If-None-Match: X" returned 304 and "If-None-Match: Y, X"
returned a full 200. Reproduced against the live site.
.item Fonts were served with no ETag, no Last-Modified and no
Cache-Control -- the embedded filesystem reports a zero modtime,
so the standard file server emitted no validator at all.
.item The shell carried an ETag and no Cache-Control, leaving
freshness to browser heuristics with nothing to compute from.
.item A directory listing was reachable, and inside it a second,
un-transformed copy of the front page at its own URL.
.item No security headers anywhere, and no compression.

# Deployment

The unit is Type=notify, which is what the readiness notification
is for: systemctl restart returns when the kiosk is serving.

.pre
  [Service]
  Type=notify
  ExecStart=/opt/NAME/NAME --addr 127.0.0.1:PORT
  Restart=always
  RestartSec=1s
  StartLimitIntervalSec=60
  StartLimitBurst=5
  NoNewPrivileges=true
  ProtectSystem=strict
  ProtectHome=true
  PrivateTmp=true
  PrivateDevices=true
  RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
  ReadWritePaths=/opt/NAME/data
.end

AF_UNIX stays in the address families because the readiness
notification is a unixgram socket. Restart=always with a start
limit replaces on-failure: a public reader that exits cleanly for
a reason nobody predicted should come back, and a genuine crash
loop should still give up.

The site block is where TLS, HSTS, compression and the retry
live. lb_try_duration is what makes a restart invisible: the
proxy retries the upstream for the few hundred milliseconds the
new process needs to bind, instead of returning 502 to whoever
asked during the swap.

.pre
  site.example.com {
      encode zstd gzip
      header Strict-Transport-Security "max-age=31536000"
      reverse_proxy 127.0.0.1:PORT {
          lb_try_duration 5s
          health_uri /healthz
      }
  }
.end

Strict-Transport-Security carries no includeSubDomains here on
purpose: the sibling names under this domain are separate
services with their own certificates, and a subdomain directive
speaks for all of them.

# Non-goals

.item TLS. The proxy owns it. The one previous attempt at ACME
in-process shipped, was never enabled, and had no timeouts of its
own.
.item SSE, WebSockets, sessions, cookies, authentication. A
long-lived connection is the lifecycle's business, not a route's:
one streaming endpoint is what cost the last server its write
timeout for every request in the process. Methods are NOT on this
list -- a route that wants POST says so and Serve carries it.
.item Serving a directory tree from disk. PUBLISH.t describes one
and it will want this package, but nothing writes such a tree
yet, and a tree server built before its publisher would be a
guess about path handling, listings and content types. Static
covers every resource the servers being replaced actually had.
.item Compression in-process. The proxy does it for every
upstream at once, and a kiosk that compressed would have to
manage a second validator per body.
.item Client addresses in the access log. The proxy in front logs
them; a publication has no use for a second copy.

# Decisions

.item The method gate was built and then removed, on the day it
was written, by asking what kv would need. kv is one route,
POST /v1/sync, and it wanted the whole lifecycle and none of the
policy -- so the seam was never between a publication and other
apps, it was between mechanism and policy. Widening a gate to
admit POST leaves machinery that admits every method anyone would
register and a guarantee that no longer holds; removing it leaves
the routing to the mux, which already does it per route and with a
better Allow header. The GET-only contract now lives where it is
enforced, in the publication's route table.
.item Conditional requests go through http.ServeContent rather
than a hand-rolled comparison. The hand-rolled one is what broke,
in two independent codebases, in the same way.
.item Content types are required, never sniffed. A resource whose
type nobody can name is a resource nobody should serve, and
sniffing is what nosniff exists to switch off.
.item The ETag is content-derived and the modtime is zero. A
build-time modtime makes byte-identical bodies look different
across a rebuild, which is exactly backwards.
.item Liveness stopped being an installed route on the day the
method gate went, for the same reason. Intercepting /healthz ahead
of the mux claimed a path out of the caller's namespace and
re-derived, by hand, the method matching the mux does for free. The
argument that had justified it -- keeping a once-a-second poll out
of the access log -- is a logging concern with an existing answer:
Config.Logger is injectable and slog handlers compose. And the
default-safe reasoning that wins for timeouts does not reach here:
forgetting a timeout is silent, while forgetting to mount liveness
turns the proxy's health check red on the first deploy.
.item Bodies are held in memory as given. Where they came from --
an embedded file, a decoded page, generated bytes -- is the
caller's business, and the package assumes nothing about them.

.width 74
.cols 1
.font sans
