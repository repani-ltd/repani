# Design notes: pica as a troff successor

Status: design direction agreed 2026-07-22, nothing below is implemented
unless marked "exists". This document is the handoff for whoever (human or
agent) picks up the work. Read CLAUDE.md first for the pkg.fact navigation
workflow. Line numbers cited below are anchors from when this was written;
prefer the function names and re-locate via pkg.fact (`grep '\.file' pkg.fact`).

## 1. The thesis

pica grows into a troff-like typesetting system that keeps troff's
temperament — imperative streaming galley model, line-oriented plain-text
source, terse dot-commands, pipeline-friendliness — but has **no macro
language at all**. Extension happens in Go, against a typed API. The
`.table` command is the precedent and the pattern.

### Why no macro language

Troff's brittleness has four root causes, and every macro language shares
some of them:

1. **Rescanning.** Macro bodies are captured text re-read through the input
   machinery, so escape evaluation time is encoded in backslash count
   (`\n` vs `\\n` vs `\\\\n`). This is ~70% of troff's fragility. TeX has
   the same disease via token rewriting.
2. **Stringly-typed global state.** Registers/strings/macros share one flat
   namespace; packages collide by construction; dimensions and booleans are
   all "a number register".
3. **Diversions are macros.** Captured output is stored as replayable
   *input*, so measuring a box means diverting and reading side-channel
   registers, and replay may behave differently in a new context.
4. **Silence on error.** Unknown request = line silently dropped.

Historically macro languages existed because users couldn't recompile the
formatter. With agents writing extension code, that rationale is gone. Go
gives the extension layer a type system, tests, debugger, and package
management for free; compile errors + tests give an agent a feedback loop
no macro language ever had.

### The architecture

- **Document layer: fully declarative, zero programmability.** Content
  lines + `.command` lines. A document is inert data — this is also the
  security boundary (rendering an untrusted document never executes code).
- **Every dot-command is a registered Go handler** receiving a parsed
  region and a *capability-scoped* context. A "macro package" is a Go
  package that registers commands (an `-ms` equivalent is
  `import "pica/ms"`).
- **The one in-band definition facility is `.alias`** (not yet designed in
  detail): single-pass substitution of parsed content, positional
  interpolation only, no recursion, no conditionals. Deliberately crippled
  so it cannot grow into a macro language. Anything needing logic graduates
  to Go.
- **Distribution:** the shipped `pica` binary is batteries-included
  (rendering plain documents never needs a Go toolchain). Extenders treat
  their document repo as a Go module: a ten-line `main.go` imports
  typeset + their handlers; `go run .` renders; go.mod pins the
  document↔package version coupling (a problem troff never solved).
  Go `plugin` is explicitly rejected (platform-limited, version-locked).
  An xcaddy-style builder is a later option if third-party users appear.
- **API discipline (critical):** handlers must NOT receive a `*Formatter`
  or any grab-bag of mutable engine state — that recreates troff's global
  register soup in Go. Each hook point gets a parameter object exposing
  only what that hook may do (e.g. a page handle with `Remaining()` /
  `Reserve()` but no galley writer). The contract is enforced by what is
  unreachable, not by documentation.

### Known costs, accepted

- Documents are no longer self-contained single files; the repo/module is
  the distribution unit.
- Macro changes add a `go build` to the edit-render loop (~1–2 s warm).
- The two-line one-off (`.de`-style shortcut) is covered only by `.alias`.

### No intermediate output language (decided 2026-07-22)

There is deliberately NO ditroff-style device-independent output format.
Each output device is a Go backend consuming post-layout values (composed
lines / flowed columns) in-process, with full Go available. Rationale: the
interchange format is Go types — type-checked, compiler-versioned, no
parser, no `x X`-style escape hatch (capabilities are typed fields, e.g.
`sline.href`). Same reasoning that killed the macro language.

Discipline that replaces the format:

- The shared core owns measurement, wrapping, and flow; backends own only
  drawing. Layout decisions must never be re-implemented per-backend
  (guard against text/PDF drift; today both go through `typeset` wrapping
  and the source-declared `.width` — keep it that way).
- `fblock`/`sline` stay unexported inside `cmd/pica` until a second real
  backend forces promotion to a shared package; that promotion is a design
  event (it makes them the load-bearing contract ditroff's format was).
- A debug backend that dumps composed values as text is fine for
  inspection but must never become a contract.

Refined 2026-07-31: the display list (§5) reifies the *draw* boundary as
Go values — still no serialized format, no parser. The discipline above
stands unchanged; what §5 settles is the shape of the values backends
consume once a second backend exists.

## 2. Architecture validation (paper check, done against real code)

Three classic troff stress cases were checked against
`cmd/pica/broadsheet.go`. Two already exist and are clean; the third
defines the next core work item.

### Existing machinery (the vocabulary of the design)

- `compose` (broadsheet.go, ~line 108): `typeset.Block` → `fblock`. This
  IS the "diversion as value" primitive: an `fblock` is a pre-formatted,
  measurable, splittable box — `segs []seg`, `height()`, `rest(k)`,
  `repeat` (lead-in re-emitted after splits, e.g. table headers),
  `atomic`, `keepNext`, `tight`.
- `flow(blocks, capacity func(col int) int) [][]sline` (~line 191): a
  **pure function** distributing blocks into columns. Orphan/widow rules
  via `minKeep`/`splitSegs`, forced progress via `forceSplit`.
- The balance pass (~line 380) re-runs `flow` inside a binary search to
  balance an underfull page. This establishes the house idiom:
  **speculative pure re-layout**, cheap because flow is pure.

### Case results

1. **Keeps** — exists, clean. `.KS/.KE` = `atomic`; heading-keep =
   `keepNext` + lookahead; table-header replay = `repeat`. New keep-like
   constructs are composer-side only; no core changes.
2. **Multi-column** — exists, clean. Variant geometry is just a different
   `capacity` closure; balancing falls out of flow's purity (troff's -mc,
   being one-pass, structurally cannot do this).
3. **Footnotes** — NOT expressible today, and the failure is precise:
   `capacity(colIdx)` is pre-committed geometry; there is no channel from
   "content placed on this page" back to "this page's capacity". A note
   referenced in column 3 must retroactively shorten columns 1–2. This is
   essential difficulty (cf. LaTeX multicol+footnotes), not an API wart.

### The footnote design (decided, not implemented)

**Decision: pure speculative reflow, NOT mutation hooks.** An earlier
sketch (`page.reserve()` + `on page.end` hooks — the TeX insert model) was
considered and rejected; the balance pass shows reflow is the idiom, and it
eliminates any trap/re-entrancy contract entirely.

Core additions needed (both in core, once — not per-extension):

- **Attachments:** a composed line (`sline` or `seg`) may carry attached,
  pre-composed `fblock`s (the note body, formatted at compose time). Floats
  and margin notes later ride the same mechanism with different placement
  rules.
- **Placement, two modes:**
  - *Per-column notes* (note at bottom of referencing column, as -ms does):
    strictly forward-only. Placement becomes transactional — a line commits
    only if its note's reservation also fits in the column; else line and
    note move to the next column together. No backtracking.
  - *Page-spanning notes* (broadsheet style): inherent fixpoint. Flow the
    page; sum note heights for refs that landed; re-flow with shrunken
    capacity; repeat. Naive iteration oscillates (evicting a ref frees
    space that re-attracts it), so the contract needs one monotone rule:
    **once a ref is pushed off a page it stays off for that page**.
    Terminates in 1–2 iterations in practice.

Resulting extension API has exactly two tiers:

1. **Block composers** (`Block → fblock`) — exists as `compose`'s switch;
   should become a registry so extensions can add cases.
2. **Geometry claimants** (footnotes/floats/margin notes) — extensions
   still only write composers (format the note body to an fblock); the
   reflow fixpoint lives once in core `flow`.

## 3. Variable-width fonts (stages 1–3 and 5 DONE 2026-07-22)

Implemented: `Measurer` interface + `Line` type in wrap.go (monospace is
the `Mono` measurer; all golden tests unchanged), `WrapLines`/`JustifyLines`
as the measured structured primitives, Fira Sans Regular+Bold embedded
(`pdf.Sans`/`pdf.SansBold`), `pdf.Measure` (milli-em widths),
`pdf.AvgAdvance`, `Page.Words` (TJ-array justification — note: PDF's Tw
word-spacing operator does NOT apply to 2-byte Identity-H text, so gaps
are TJ position adjustments in integer thousandths of an em), the
`.font mono|sans` layout attribute, and the broadsheet sans path (`typo`
struct; prose/headings/links measured, pre+tables stay mono at the size
where .width runes fill the column). `.width` now means "average
lowercase advances per line" under sans — same visual density contract.

**Stage 4 (proportional table cells) is REJECTED (2026-07-28)** — tables
and verbatim blocks stay monospace in both modes as deliberate
typography: mono tables in a sans page read as set data, the stage is
the most invasive remaining item (formatCell's pad-with-spaces dies,
Layout returns structured cells), and no document wants it. This is a
settled decision, not pending work. Everything below is the original
estimate, kept for context:

- **Metrics already exist.** `pdf.Width` (pdf/page.go, ~line 102) sums
  per-codepoint advances from parsed TTF `CIDWidths`; `pdf/ttf` does the
  parsing. The pipeline just needs to call it earlier than draw time.
- **The wrap DPs are unit-agnostic.** `raggedDP`/`justifyDP` (wrap.go)
  accumulate scalar word widths vs a scalar line width. Swap
  `runeLen(w.text)` for `measure(w.text)` and the Knuth-Plass structure
  carries over. Hyphenation is position-based — untouched.
- **The flow layer survives untouched.** `fblock`/`flow`/keeps/balancing
  measure height in *line counts*, valid while leading is uniform.

**The one real break: a line stops being a string.**
`wrapParagraphJustify` returns `[]string` and `justifyLine` justifies by
inserting literal spaces — impossible with variable widths. The wrapped
line becomes structured (words + interword gap, or words with x-offsets),
and that representation change ripples through APIs (not algorithms).

### Staged plan (each step ships with tests green)

1. **Measurer + fixed-point in wrap.go.** Introduce
   `type Measurer interface { ... }` (width of a word / word fragment);
   move widths to integer fixed-point (font-units×size or millipoints —
   NOT float64: DP cost comparisons and the byte-identical-PDF guarantee
   need determinism). Monospace becomes a Measurer (1 unit/rune) so the
   entire existing golden test suite keeps passing and pins behavior.
   Precompute word widths and hyphen-fragment widths at tokenization so
   `tryHyphenAt` stays cheap. (~1–2 days)
2. **Structured line type + text flattener.** Current space-insertion
   logic in `justifyLine` becomes the monospace renderer (integral gap
   distribution). Text backend output byte-identical to today. (~1 day)
3. **PDF measurer + drawing.** `drawColumn` draws per-word, or one run
   with the PDF `Tw` word-spacing operator (exists for exactly this —
   justification at draw time is nearly free). (~1 day)
4. **Tables.** `tbl.go` column widths become points; `formatCell`'s
   pad-with-spaces dies for PDF. `Layout` returns structured cells
   (wrapped lines + column x/width); text backend joins with padding, PDF
   backend positions. (~1 day)
5. **Geometry contract.** "Column holds exactly `.width` runes"
   (broadsheet.go, point-size derivation ~line 343) dissolves; redefine
   `.width` (e.g. as average-character-width units using the font's real
   average advance). Small code, real design decision. (~0.5 day)

### Deliberately out of scope

- **Kerning** (TTF kern/GPOS unparsed; per-codepoint advances are
  ditroff-era-respectable; the Measurer interface can grow pair adjustment
  later without touching the DPs).
- **Shaping/ligatures.**
- **Inline bold/italic spans** — styles stay per-line (`sline.style`) for
  now, keeping measurement single-font per paragraph; BUT give the
  structured word a style field from day one so inline markup slots in
  later.

## 4. Parked work and its triggers (reframed 2026-07-28)

Variable-width fonts are done (stages 1–3, 5; stage 4 rejected — see
§3). Nothing below is scheduled; every consumer today is first-party,
in one repo, feeding one binary, and building extension machinery ahead
of that demand is the register-soup essay's own sin. Each item instead
has a trigger, not a date:

- **Footnotes** — parked. The design is settled (§2: attachments +
  transactional per-column placement first, page-spanning via the
  reflow fixpoint with the monotone-eviction rule); its value today is
  as a written-down decision. Build when a real document demands notes.
- **Composer registry** — wait for the second real backend or the first
  external extender. `compose`'s switch is the right implementation at
  N=1.
- **`.alias` + doc-repo-as-module** — design only when repeated
  authoring pain appears in real documents; the packaging story rides
  with the registry.
- **Images** — parked; no image block exists. If a real document forces
  one, the boundary is settled (2026-07-31): **block-level only, height
  snapped to whole multiples of the leading, no floats, no inline
  images, no text runaround.** Block-level keeps the breaker's measure
  constant per paragraph, so wrap/justify never learn images exist;
  grid-snapping keeps flow's integer line-count model intact — an image
  is just an atomic `fblock` of whole-line height (caption = ordinary
  gray block via `keepNext`) — and preserves the cross-column baseline
  grid, a broadsheet virtue in itself. Everything past this line
  (runaround, floats, inline placement) is where the second 90% of the
  complexity hides, and none of it is needed for a broadsheet.
  Grayscale-at-ingest is the leaning: a stance, not a limitation.

## 5. The display list (decided 2026-07-31, not implemented)

The viewer discussions (native/phone via Gio, browser via wasm + canvas)
settled the backend boundary: a retained **display list** of positioned
draw ops. This refines, not reverses, §1's no-intermediate-language
decision — the list is Go values consumed in-process, with no
serialization requirement and no parser. What moves is the seam: today
`drawColumn` and the page chrome drive `pdf.Page` methods directly;
the decided form has them *emit op values*, and each backend consumes
the slice. Empirical basis: everything pica draws today goes through
six `pdf.Page` calls (`SetFont`, `Text`, `Words`, `Line`,
`Gray`/`StrokeGray`, `Link`) — the op set below is those six restated
as data.

Decisions, each with its load-bearing reason:

- **Data primary, not a renderer interface.** The two are duals but the
  duality is asymmetric: data→API is a range loop and a type switch;
  API→data requires every retained consumer to write a recorder whose
  output is an undesigned ad-hoc display list. And the viewer forces a
  retained form: layout runs once per reflow event, drawing runs every
  frame — the redraw loop must iterate values, not re-invoke the
  producer. Golden tests get `cmp.Diff` on integer ops instead of
  parsing PDF content streams; backends become pure functions.
- **Self-contained ops, no state machine.** `SetFont`/`Gray` stop being
  ops and become fields on each op. Compactness is worthless at a few
  hundred ops per page; independence buys viewport culling and damage
  redraw without replaying a prefix to reconstruct state.
- **Four ops, refuse a fifth: TextRun, Rule, Link, page structure**
  (a list is a slice of pages, each a slice of ops, page size on the
  page). No paths, no transforms, no clip, no RGB — each absence is a
  typeset design fact, and a general Bézier op would be PDF rebuilt
  with fewer tools. Images, if they ever come, are bounded by §4's
  image entry and would add one op plus a document-level resource
  table.
- **A TextRun is one composed line**, carrying origin, font, size,
  gray, a semantic tag distilled from `sline.style`, and per-word pen
  advances from run start in integer em-thousandths — computed once by
  the producer from the same Measurer that justified the line.
  Consumers place words with `x + dx·size/1000` and need no font
  metrics at all. This preserves `spread`'s integer determinism,
  keeps line identity (selection, search, accessibility want it), and
  word boxes fall out of consecutive offsets. Contract note: renderers
  must not clip runs to the column rect — the hanging hyphen
  deliberately protrudes.
- **Link stays a producer-computed rect + URL** (as `Page.Link`
  today), so tap hit-testing needs no metrics either.
- **Integer fixed-point throughout; y-down, origin top-left; y is the
  baseline.** Millipoints for page coordinates, em-thousandths within
  runs. Floats in the interchange invite platform-dependent formatting
  (the PDF backend already fights this with `ff`). PDF is the only
  y-up consumer and flips at its own boundary; the retarget confines
  today's y-up leak in `drawColumn` to the PDF emitter.
- **Tagged struct with a Kind field, not an Op interface** — four
  variants don't earn dynamic dispatch, and the tagged form serializes
  trivially if a wire use (server-render, cache) ever appears. Fonts
  stay the closed four-value enum: the face set is a design boundary
  worth keeping visible in the type.
- **Bottom interface only.** The display list is post-flow, coordinates
  final; it is *discarded* on any layout-affecting change. The archival
  format remains the source document. A viewer re-runs compose/flow per
  layout event (open, rotate, type-size change — full reflow, integer
  arithmetic, milliseconds, once per human gesture) and iterates ops
  per draw event (scroll, fling, damage — sustained at refresh rate).
  Continuous pinch-zoom: geometrically scale the stale list during the
  gesture, one real reflow on release.

Context worth keeping: a PDF content stream *is* a display list —
PostScript with computation deliberately removed — so the PDF backend
compiles our list into theirs, which is why it stays small; there is no
"program in the PDF" alternative (content streams cannot loop, and
reader JS has no drawing surface). The browser target is the same
architecture on another surface: the Go typesetter compiled to wasm,
a small canvas interpreter for the op set, HTML/CSS never doing layout.

Trigger: build with the first non-PDF backend or the start of viewer
work. Until then, `drawColumn`'s direct `pdf.Page` calls are the right
implementation at N=1 — reifying the ops today would be the speculative
machinery §4 warns against.

## 6. Open questions

Open only when their triggers fire; resolved ones removed (fixed-point
units landed as abstract integer measurer units — milli-ems at the PDF
layer; `.width` under sans is average lowercase advances, §3).

- Note placement default: per-column vs page-spanning (per-column is
  cheaper and cleaner; page-spanning matches the broadsheet identity).
- `.alias` exact syntax and interpolation rules.
- Registry shape: global `Register("table", handler)` vs a Renderer
  struct holding its command set (leaning the latter — global registries
  are the register-soup smell in miniature).
