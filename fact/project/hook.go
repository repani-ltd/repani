// Agent-harness integration: regenerate a package's stored projection
// after a source edit and surface the projection diff — the impact report
// of SPEC §11.1 — back to the editing agent. The hook also runs goimports
// on the edited file and, when the package no longer compiles, surfaces
// the compiler diagnostics instead: the edit→build→read-errors loop
// collapses into the edit itself.
//
// A report is worth its context only when the agent will act on it now,
// so the hook says less during a burst of saves and more at the end of
// the turn. Per save it reports formatting rewrites, syntax errors in the
// edited file, compile errors that are new since the last save (the rest
// as counts), and declarations removed or changed; a diff that only adds
// declarations is one line, since additions break nobody. Per turn, the
// Stop hook rebuilds every package the turn touched, regenerates any
// stale projection silently, and refuses the stop only when a package
// does not compile, with the diagnostics as the reason. The state that
// makes the deltas possible lives per session under the user cache
// directory (FACT_HOOK_STATE overrides the base) and is cleared at the
// stop.

package project

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/imports"

	"repani.com/fact"
)

// File projects target (a package directory or import path, as Lines) and
// renders the stored file form: the generated header plus the canonical
// fact set (SPEC §11.1).
func File(target string) ([]byte, error) {
	lines, err := Lines(target)
	if err != nil {
		return nil, err
	}
	facts, errs := fact.Load([]byte(strings.Join(lines, "\n") + "\n"))
	if len(errs) > 0 { // a generator bug, not a user error
		return nil, fmt.Errorf("generator produced invalid FACT: %s", errs[0].Error())
	}
	return append([]byte(Header+"\n"), fact.Canonical(facts)...), nil
}

// WriteReadOnly stores a projection at target, read-only per SPEC §11.1,
// creating parent directories as needed. An existing file with identical
// bytes is left untouched (no mtime churn on an unchanged projection);
// the result reports whether the file was (re)written.
func WriteReadOnly(target string, out []byte) (changed bool, err error) {
	if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, out) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	os.Remove(target) // read-only: cannot be overwritten in place
	return true, os.WriteFile(target, out, 0o444)
}

// Refresh regenerates dir's stored pkg.fact (which must exist: projection
// is opt-in per package), rewriting it only when the projection changed,
// and returns the canonical line diff. Empty diff with nil error means
// the edit was declaration-neutral (the churn invariant, SPEC §11.2).
func Refresh(dir string) (removed, added []string, err error) {
	target := filepath.Join(dir, "pkg.fact")
	existing, err := os.ReadFile(target)
	if err != nil {
		return nil, nil, err
	}
	out, err := File(dir)
	if err != nil {
		return nil, nil, err
	}
	if changed, err := WriteReadOnly(target, out); err != nil || !changed {
		return nil, nil, err
	}
	removed, added = diffLines(existing, out)
	return removed, added, nil
}

// diffLines set-diffs two canonical projections line-wise. Canonical files
// are bytewise-sorted, so membership is the whole story: there are no
// move or reorder cases to report.
func diffLines(before, after []byte) (removed, added []string) {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	beforeSet := map[string]bool{}
	for _, l := range beforeLines {
		beforeSet[l] = true
	}
	afterSet := map[string]bool{}
	for _, l := range afterLines {
		afterSet[l] = true
	}
	for _, l := range beforeLines {
		if !afterSet[l] {
			removed = append(removed, l)
		}
	}
	for _, l := range afterLines {
		if !beforeSet[l] {
			added = append(added, l)
		}
	}
	return removed, added
}

func splitLines(b []byte) []string {
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// hookInput is the subset of Claude Code's hook payload the fact hook
// consumes: the edited file for PostToolUse, the session for the state
// the deltas need, and for Stop whether this stop already follows a
// refusal (a second refusal would loop the agent).
type hookInput struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
	ToolInput      struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// maxHookDiffLines caps the impact report surfaced to the agent; the full
// diff is always available as the pkg.fact working-tree diff.
const maxHookDiffLines = 80

// maxHookDiagnostics caps the compile errors surfaced to the agent.
const maxHookDiagnostics = 20

// maxNamedAdditions is the most added declarations the one-line
// additions report names; more are counted by kind.
const maxNamedAdditions = 5

// Event reports which hook event a payload carries ("PostToolUse",
// "Stop", ...), so the command can dispatch without parsing twice.
func Event(payload []byte) string {
	var in hookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return ""
	}
	return in.HookEventName
}

// Hook implements a Claude Code PostToolUse hook: when the edited file is
// a .go file in a package carrying a stored pkg.fact, it runs goimports
// on the file, regenerates the projection, and returns what the agent
// should act on now (see the package comment for what that is) as
// context. Test files are formatted but sit outside the projection
// (generator scope). An empty return means nothing to report: not a
// projected package, or a declaration-neutral edit that goimports left
// untouched (the churn invariant, SPEC §11.2).
func Hook(payload []byte) (string, error) {
	var in hookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return "", err
	}
	fp := in.ToolInput.FilePath
	if !strings.HasSuffix(fp, ".go") {
		return "", nil
	}
	dir := filepath.Dir(fp)
	if _, err := os.Stat(filepath.Join(dir, "pkg.fact")); err != nil {
		return "", nil // projection is opt-in per package
	}
	var report []string
	// A goimports failure (typically a syntax error) leaves the file
	// untouched; Refresh below reports the diagnostics.
	if changed, err := goimports(fp); err == nil && changed {
		report = append(report, fmt.Sprintf("goimports rewrote %s (formatting/imports) — re-read it before editing it again", fp))
	}
	if strings.HasSuffix(fp, "_test.go") {
		return strings.Join(report, "\n"), nil
	}
	st := loadState(in.SessionID, dir)
	removed, added, err := Refresh(dir)
	var ce *CompileError
	if errors.As(err, &ce) {
		report = append(report, compileReport(dir, fp, ce, st.Diagnostics))
		st.Diagnostics = ce.Diagnostics
		st.save()
		return strings.Join(report, "\n"), nil
	}
	if err != nil {
		return strings.Join(report, "\n"), err
	}
	if len(st.Diagnostics) > 0 {
		report = append(report, fmt.Sprintf("%s compiles again.", dir))
	}
	st.Diagnostics = nil
	st.save()
	if len(removed)+len(added) > 0 {
		report = append(report, diffReport(dir, removed, added))
	}
	return strings.Join(report, "\n"), nil
}

// Stop implements a Claude Code Stop hook: it rebuilds every package the
// session's saves touched, regenerates any projection left stale, clears
// the session's state, and returns a reason to refuse the stop when a
// package does not compile — empty when everything builds, or when the
// stop already follows a refusal, so a broken package is reported once.
func Stop(payload []byte) (string, error) {
	var in hookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return "", err
	}
	touched, err := touchedPackages(in.SessionID)
	if err != nil {
		return "", err
	}
	var broken []string
	for _, dir := range touched {
		_, _, err := Refresh(dir)
		var ce *CompileError
		if errors.As(err, &ce) {
			broken = append(broken, compileReport(dir, "", ce, nil))
		}
	}
	os.RemoveAll(stateDir(in.SessionID))
	if len(broken) == 0 || in.StopHookActive {
		return "", nil
	}
	return "The turn leaves a package that does not compile:\n" + strings.Join(broken, "\n"), nil
}

// --- session state ---

// pkgState is what the hook remembers about one package between saves
// of one session: the diagnostics of the last save, so the next can
// report only what is new.
type pkgState struct {
	Dir         string   `json:"dir"`
	Diagnostics []string `json:"diagnostics,omitempty"`
	path        string
}

func stateDir(session string) string {
	base := os.Getenv("FACT_HOOK_STATE")
	if base == "" {
		if c, err := os.UserCacheDir(); err == nil {
			base = filepath.Join(c, "fact", "hook")
		} else {
			base = filepath.Join(os.TempDir(), "fact-hook")
		}
	}
	if session == "" {
		session = "default"
	}
	return filepath.Join(base, session)
}

func loadState(session, dir string) *pkgState {
	sum := sha1.Sum([]byte(dir))
	st := &pkgState{Dir: dir, path: filepath.Join(stateDir(session), hex.EncodeToString(sum[:8])+".json")}
	if b, err := os.ReadFile(st.path); err == nil {
		json.Unmarshal(b, st)
	}
	return st
}

func (st *pkgState) save() {
	os.MkdirAll(filepath.Dir(st.path), 0o755)
	b, _ := json.Marshal(st)
	os.WriteFile(st.path, b, 0o644)
}

// touchedPackages lists the package directories the session's saves
// recorded, in a fixed order.
func touchedPackages(session string) ([]string, error) {
	entries, err := os.ReadDir(stateDir(session))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(stateDir(session), e.Name()))
		if err != nil {
			continue
		}
		var st pkgState
		if json.Unmarshal(b, &st) == nil && st.Dir != "" {
			dirs = append(dirs, st.Dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// --- reports ---

// diffReport formats the projection diff as the impact report. A diff
// that only adds is one line naming or counting the declarations, since
// an addition breaks no caller; removals and changes are listed.
func diffReport(dir string, removed, added []string) string {
	var b strings.Builder
	if !anyDeclaration(removed) {
		if names := entities(added); names != "" {
			fmt.Fprintf(&b, "pkg.fact regenerated for %s: added %s\n", dir, names)
		} else {
			fmt.Fprintf(&b, "pkg.fact regenerated for %s: imports changed, declarations as before\n", dir)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "pkg.fact regenerated for %s — projection diff (impact report, SPEC §11.1):\n", dir)
	n := 0
	for _, l := range removed {
		if n++; n > maxHookDiffLines {
			break
		}
		fmt.Fprintf(&b, "- %s\n", l)
	}
	for _, l := range added {
		if n++; n > maxHookDiffLines {
			break
		}
		fmt.Fprintf(&b, "+ %s\n", l)
	}
	if total := len(removed) + len(added); total > maxHookDiffLines {
		fmt.Fprintf(&b, "… (%d more lines; see the pkg.fact diff)\n", total-maxHookDiffLines)
	}
	return b.String()
}

// declaration splits a projection line into the kind and name of the
// declaration it describes: a key is "kind:Name.attribute". The
// package's own lines (pkg.path, imports) have no kind and report ok
// false: they are not declarations and break no caller.
func declaration(line string) (kind, name string, ok bool) {
	i := strings.Index(line, ": ")
	if i < 0 {
		return "", "", false
	}
	key := line[:i]
	kind, name, ok = strings.Cut(key, ":")
	if !ok {
		return "", "", false
	}
	if j := strings.Index(name, "."); j >= 0 {
		name = name[:j]
	}
	return kind, name, true
}

func anyDeclaration(lines []string) bool {
	for _, l := range lines {
		if _, _, ok := declaration(l); ok {
			return true
		}
	}
	return false
}

// entities summarises projection lines as the declarations they
// describe: named when few, counted by kind when many; empty when the
// lines carry no declaration.
func entities(lines []string) string {
	seen := map[string]bool{}
	var names []string
	kinds := map[string]int{}
	for _, l := range lines {
		kind, name, ok := declaration(l)
		if !ok {
			continue
		}
		id := kind + ":" + name
		if seen[id] {
			continue
		}
		seen[id] = true
		names = append(names, id)
		kinds[kind]++
	}
	if len(names) <= maxNamedAdditions {
		return strings.Join(names, ", ")
	}
	var parts []string
	for _, k := range sortedKeys(kinds) {
		n := kinds[k]
		s := k
		if n > 1 {
			s += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, s))
	}
	return strings.Join(parts, ", ") + " (no removals)"
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// compileReport formats a CompileError as agent context: the edit left
// the package broken, so the projection is stale by necessity, and the
// compiler diagnostics are the actionable payload. Syntax errors in the
// edited file are always listed; other errors are listed when new since
// prev (the last save's diagnostics) and counted when they persist.
// With no prev and no edited file (the Stop report) every error lists.
func compileReport(dir, edited string, ce *CompileError, prev []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s does not compile — pkg.fact not regenerated (stale until the package builds):\n", dir)
	old := map[string]bool{}
	for _, d := range prev {
		old[d] = true
	}
	n, persisting := 0, 0
	for i, d := range ce.Diagnostics {
		syntax := i < len(ce.Syntax) && ce.Syntax[i]
		inEdited := edited != "" && (strings.HasPrefix(d, edited+":") || strings.HasPrefix(d, filepath.Base(edited)+":"))
		if !(syntax && inEdited) && old[d] {
			persisting++
			continue
		}
		if n++; n > maxHookDiagnostics {
			continue
		}
		fmt.Fprintf(&b, "%s\n", relPos(d))
	}
	if n > maxHookDiagnostics {
		fmt.Fprintf(&b, "… (%d more errors)\n", n-maxHookDiagnostics)
	}
	fixed := 0
	now := map[string]bool{}
	for _, d := range ce.Diagnostics {
		now[d] = true
	}
	for _, d := range prev {
		if !now[d] {
			fixed++
		}
	}
	switch {
	case persisting > 0 && n == 0:
		fmt.Fprintf(&b, "%d errors persist from the last save, none new", persisting)
		if fixed > 0 {
			fmt.Fprintf(&b, "; %d fixed", fixed)
		}
		b.WriteString("\n")
	case persisting > 0 || fixed > 0:
		fmt.Fprintf(&b, "(%d persisting from the last save not repeated", persisting)
		if fixed > 0 {
			fmt.Fprintf(&b, "; %d fixed", fixed)
		}
		b.WriteString(")\n")
	}
	return b.String()
}

// relPos shortens a diagnostic's absolute file position
// ("/abs/pkg/file.go:3:1: msg") relative to the working directory,
// purely for report brevity.
func relPos(d string) string {
	i := strings.Index(d, ".go:")
	if i < 0 || !filepath.IsAbs(d) {
		return d
	}
	path := d[:i+3]
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, path); err == nil && len(rel) < len(path) {
			return rel + d[i+3:]
		}
	}
	return d
}

// goimports formats fp in place with import fixing (the goimports
// algorithm) and reports whether the file changed. On error (typically a
// syntax error) the file is left untouched.
func goimports(fp string) (bool, error) {
	src, err := os.ReadFile(fp)
	if err != nil {
		return false, err
	}
	out, err := imports.Process(fp, src, nil)
	if err != nil || bytes.Equal(src, out) {
		return false, err
	}
	info, err := os.Stat(fp)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(fp, out, info.Mode())
}
