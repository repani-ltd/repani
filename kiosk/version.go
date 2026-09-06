// Build identity: what this binary is, and a route that says so.
package kiosk

import (
	"fmt"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
)

// VersionPath is where Version is conventionally mounted, so every
// daemon here answers the same question at the same URL.
const VersionPath = "/version"

// Build describes the running binary: the program's import path, the
// commit it was built from, whether that tree was dirty, when it was
// built, and the toolchain. Every field can be empty -- a binary built
// outside a checkout, or with -buildvcs=false, carries no revision.
type Build struct {
	Program  string
	Revision string
	Modified bool
	Time     string
	Go       string
}

// ReadBuild reads the build stamp the Go toolchain embeds. It survives
// the flags a release build uses: -trimpath keeps it, and -ldflags
// "-s -w" strips the symbol table and DWARF, not the build info.
func ReadBuild() Build {
	b := Build{Program: "unknown"}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	b.Go = bi.GoVersion
	if bi.Path != "" {
		b.Program = path.Base(bi.Path)
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			b.Revision = s.Value
		case "vcs.time":
			b.Time = s.Value
		case "vcs.modified":
			b.Modified = s.Value == "true"
		}
	}
	return b
}

// String renders the build as one line:
//
//	kvserve 8fa83a6 2026-09-06T20:51:14Z go1.26.6
//
// A tree that had uncommitted changes when it was built is marked
// "+dirty", because then the revision alone does not describe what
// is running. A revision of unknown length is shortened to 7.
func (b Build) String() string {
	rev := b.Revision
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev == "" {
		rev = "norev"
	}
	if b.Modified {
		rev += "+dirty"
	}
	parts := []string{b.Program, rev}
	if b.Time != "" {
		parts = append(parts, b.Time)
	}
	if b.Go != "" {
		parts = append(parts, b.Go)
	}
	return strings.Join(parts, " ")
}

// Version returns a handler serving ReadBuild's line, to be mounted by
// the caller like Health:
//
//	mux.Handle("GET "+kiosk.VersionPath, kiosk.Version())
//
// Mounting it is a choice, not a default. The line names a commit, so
// on a public origin it tells anyone asking exactly which source is
// running; that is usually a fair trade for being able to ask a server
// what it is instead of trusting a deploy log, but it is the operator's
// trade to make.
func Version() http.Handler {
	line := ReadBuild().String()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, line)
	})
}
