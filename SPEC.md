# FACT — Format Specification v0.2

**FACT** is a line-oriented format for *facts about systems*, designed for AI agents rather than humans. A FACT file is an unordered set of lines, where each line is one complete, self-contained, typed fact. There is no nesting, no significant whitespace, no inter-line dependence, and no external schema: **the file is the schema.**

FACT exists primarily as a **projection format**: a generated, read-only index of a codebase (canonically: a Go module) that lets agents navigate and reason about code structure at grep cost. Configuration files are the degenerate — and fully supported — case where the facts *are* the whole system rather than a projection of one.

Recommended file extension: `.fact`
MIME type (provisional): `text/x-fact`

**Changes from v0.1:** segments and enum symbols are now case-sensitive `[a-zA-Z0-9_]` (§3, §4.1) — required because projected identifiers (Go) are case-sensitive and case is semantic; primary-purpose reframing (§1); projection profile added (§11); storage and commit convention for projections (§11.1); validation scope fixed to one file per package for projections (§5, §6.2 — per-package files share singleton keys and must not be concatenated for validation); third-party projection convention (§11.5); external-reference boundary rule (§6.4); findings from a real Go extraction simulation incorporated (§12).

---

## 1. Why FACT Exists

### 1.1 The primary purpose: code projection for agents

Agents working on a codebase spend most of their context budget on *navigation and comprehension* — finding the right declaration, tracing callers, discovering interface implementations — before any edit happens. Source code is the wrong medium for this phase:

- **Semantic relationships are implicit.** In Go, interface satisfaction is structural: a type that implements `Approver` never mentions `Approver`. No grep over source can answer "what implements this interface?" — the question requires type checking.
- **The unit of text is not the unit of meaning.** A struct's full story (fields, methods, satisfactions, callers of its methods) is scattered across files. Assembling it requires either loading whole packages into context or invoking heavyweight tooling per question.
- **Bodies dominate token cost.** Real packages are body-dominated (typically 5–10:1 over declarations), but most navigation questions are answered entirely by the declaration layer.

FACT solves this by **spending semantic analysis once, at generation time**. A generator (e.g., `go/ast` + `go/types`) type-checks the module and emits every declaration-layer fact — signatures, fields, method sets, computed interface satisfactions, resolved call edges, source locations — as self-contained FACT lines. Thereafter:

- Every navigation question is a prefix grep. "Everything about `Service`" = `grep '^type:Service\.' transfer/pkg.fact`. "What implements `Poster`?" = `grep 'implements.*Poster' pkg.fact`. "Who calls `validate`?" = `grep 'calls.*validate' pkg.fact`. Module-wide, the same queries run as `grep -r --include=pkg.fact`, where the printed file path supplies the package namespace (§11.1: the projection lives in the package directory, so the path is the qualifier).
- The projection is **read-only and regenerated on save** — a lens, never a second source of truth.
- Because serialization is canonical (§8), **the diff of the regenerated projection is the impact analysis** of a source edit: rename a function and the projection diff is exactly the renamed facts plus every updated caller list, with zero noise.

The resulting two-layer workflow: *navigate and scope on facts → follow `loc` pointers into the one relevant source region → edit source (where model priors are strongest) → regenerate → read the fact-diff as the impact report.*

FACT deliberately projects only the **declaration layer**. Function bodies are computation, not facts: flattening them (SSA-style) was evaluated and rejected — it multiplies token cost, reinvents compiler IR, and discards model fluency in the source language. Statement-level questions ("where is this field assigned?") are answered by the `loc` handoff, not by the projection. This division of labor is a design decision, not a limitation to be fixed.

### 1.2 The secondary purpose: configuration

A config file is the special case where there is no source to project — the facts are the system. All the projection properties carry over: single-line edits, unambiguous greps, loud validation, canonical diffs. The config use case is what originally motivated the format; the projection use case is what justifies it. Both profiles share one grammar.

---

## 2. Lexical Structure

### 2.1 File

- Encoding: UTF-8, no BOM.
- Line separator: `\n` (LF). A trailing newline at end of file is required in canonical form.
- A file is a sequence of lines. Each line is exactly one of:
  - a **fact line**
  - a **comment line**: first non-space character is `#`; the entire line is ignored
  - a **blank line**: only whitespace; ignored, carries no semantics (grouping by blank lines is purely cosmetic)

### 2.2 Fact Line

```
key: type = value
```

- Exactly one `:` separates key from type (the first `:` that is not inside an instance marker, see §3).
- Exactly one `=` separates type from value (the first `=` at the top level of the type expression).
- Canonical spacing: no space before `:`, one space after `:`, one space before and after `=`. Parsers MUST accept arbitrary horizontal whitespace around `:` and `=`; serializers MUST emit canonical spacing.
- A fact line has no continuation. Multi-line values do not exist; newlines inside strings are written as the escape `\n`.
- No inline comments. `#` after a value is an error.

---

## 3. Keys

A key is a dot-separated path of segments:

```
segment(.segment)*
```

- **Segment characters: `[a-zA-Z0-9_]`, must start with a letter. Keys are case-sensitive.**
  *(Changed in v0.2. Projected identifier spaces — Go above all — are case-sensitive, and case carries meaning: Go exportedness is literally the case of the first letter. Lowercasing would collide `validate`/`Validate` and destroy the exportedness signal. This was discovered empirically when a working extractor violated the v0.1 grammar on its first run.)*
- **Casing convention (non-normative):** hand-authored config SHOULD use lowercase snake_case throughout — one canonical spelling of every key. Projections MUST use source-language casing verbatim — the projection is a mirror, and mirrors do not normalize.
- **Instance marker:** at most **one** segment in the path may take the form `kind:id`, where `kind` and `id` are each valid segment strings. This segment declares that the subtree rooted there is an *instance* (record) of *kind*. By convention, `kind` is lowercase even in projections (`type:Approver`, `func:Submit` — the kind vocabulary belongs to the generator, the id belongs to the source).
- Zero instance markers → the fact belongs to the singleton namespace.
- Two or more instance markers in one key → **error**. (This enforces "no nested records" at the key level. Nested identity is expressed by compound ids or refs, e.g. `method:Service_Settle`, never by nested markers.)
- The marker may appear at any segment position; validators MUST require that a given `kind:id` prefix appears at a consistent position across all facts of one instance.
- Dots are **namespacing, not structure**. `server.tls.enabled` does not imply an object `server.tls` exists. There is no tree; there is only the set of lines.

Examples:

```
server.tls.enabled                          → singleton (config profile)
route:transfer.method                       → instance "transfer" of kind "route"
pkg:transfer.type:Service.fields            → ILLEGAL (two markers) — see next line
pkg_transfer.type:Service.fields            → legal alternative...
```

**Projection namespacing note:** because only one marker is allowed, a projection of many packages either (a) emits one file per package with the package as a singleton prefix (`type:Service.fields` inside `transfer.fact`), or (b) folds the package into the kind or id (`type:transfer_Service.fields`) in a combined file. Option (a) is RECOMMENDED: one `pkg.fact` per package, mirroring the source tree. Whole-module queries are *read* operations over the tree (`grep -r --include=pkg.fact`), where the printed file path supplies the package namespace. Per-package projection files are **not** concatenatable into one validation set — each asserts the same singleton keys (`imports`, `pkg.path`) and same-named types collide — so validation scope is per file (§6.2).

---

## 4. Types

### 4.1 Base Types (six)

| Type | Values | Notes |
|---|---|---|
| `bool` | `true`, `false` | Sugar for `enum(true|false)`; semantically there are five base types |
| `int` | JSON integer syntax, 64-bit signed range | No width variants; the value is the truth |
| `float` | JSON number syntax with `.` or exponent | IEEE 754 double |
| `str` | JSON string syntax, double-quoted | JSON escaping rules exactly (`\"`, `\\`, `\n`, `\t`, `\uXXXX`) |
| `enum(a|b|c)` | One of the listed bare symbols | Symbols follow segment rules (`[a-zA-Z0-9_]`, start with letter, case-sensitive); at least one symbol; written unquoted in the value |
| `ref(kind)` | The instance marker of an instance of `kind`, written `kind:id` | See §6 |

### 4.2 Wrappers (two, non-composing)

| Wrapper | Meaning | Constraints |
|---|---|---|
| `T?` | Optional: value may be `none` | `T` must be a **base type**. `list(T)?` is illegal — the empty list `[]` is the "none of lists"; two spellings of absence are forbidden |
| `list(T)` | Ordered list, written `[v1, v2, ...]` | `T` must be a **base type**. `list(list(T))` is illegal. Empty list `[]` is valid |

**The type grammar is deliberately non-recursive.** The complete set of legal type expressions: six base types, six optional base types, six list-of-base types. Eighteen shapes total. Hard grammar rule, not style.

### 4.3 Deliberate exclusions

- **Integer widths, semantic string types (`path`, `url`, `duration`)**: conventions live in key names (`timeout_ms`, `cert_path`), not the grammar.
- **Maps/objects as values**: structure is expressed with instances and refs. The only compound value is the flat list.
- **Type inference**: forbidden. The annotation is the **domain of legal edits** — what a value may become — not a classifier of the current value. An agent editing a line it has never seen before must learn the legal replacements from that line alone.
- **Declared/named enum types**: enums are restated at every use site. Drift between copies is a one-pass lint; a central declaration would reintroduce inter-line dependence — the original sin this format exists to kill.

---

## 5. Data Model

- A file denotes an **unordered set of facts**. Line order carries no meaning. Concatenation of files with **disjoint** fact sets (followed by duplicate checking) is a valid merge primitive. Config files are disjoint by authorship, so config merges by concatenation; per-package projection files are *not* disjoint (each asserts the same singleton keys, e.g. `imports`) and compose by directory layout instead (§11.1), never by concatenation.
- **Duplicate keys are an error.** Not last-wins, not merge — error. Silent override is how bugs hide from agents.
- **Existence rule:** an instance `kind:id` exists iff at least one fact line contains that marker. Nothing exists by implication; empty records cannot exist.
- **Totality rule (config profile):** absence of a key is never a default. "Deliberately nothing" must be asserted: `route:health.auth: ref(policy)? = none`. *Absent* (nothing was decided — investigate) and *asserted none* (decided: nothing — trust) are different states, and the distinction is load-bearing for agents reading configs they did not write.
- **Completeness rule (projection profile):** a projection is total over its declared scope — every declaration in scope appears. Within that scope, absence of a fact *kind* means the generator does not emit it, never that the source lacks it. Generators MUST document their fact vocabulary (see §11.2).

---

## 6. References

### 6.1 Syntax and resolution

- A `ref(kind)` value is written as the target's instance marker: `kind:id` (e.g., `policy:maker`, `type:Poster`).
- A ref is valid iff at least one fact line exists containing the marker `kind:id` with the same `kind` as the ref's type parameter.
- The parameter is not redundant with the value: the value names the current inhabitant; the parameter names the **domain** — what may legally go there in a future edit.

### 6.2 Mechanics

- Referential integrity is checkable by prefix search over the line set. No tree construction, ever.
- Resolution scope is the *validation set*. **Config profile:** the file, or a deliberate concatenation of config files (keys are disjoint by authorship; a duplicate is then a real error). **Projection profile:** exactly one file — one package. Cross-package mentions are `str` values (§6.4) and are deliberately *not* checked by the FACT validator: their integrity is already guaranteed upstream by the host language's compiler plus the freshness gate (§11.1), and re-checking it here would be redundant.

### 6.3 Cycles and order

- Ref cycles are permitted by the format (sometimes meaningful); applications may impose acyclicity. Validators SHOULD offer an optional cycle check.
- Ordered relationships use `list(ref(kind))` — the list carries the order; the instances do not. "This pipeline is these steps in this order" is one fact and stays on one line.

### 6.4 The external-reference boundary *(new in v0.2)*

Projections inevitably mention symbols outside the projected scope (`fmt.Errorf`, `context.Context`). Refs to them would fail resolution. The rule:

- **Intra-scope relationships use `ref(kind)`** — integrity is checked.
- **Extra-scope mentions use `str`** — by convention qualified as the source language writes them (`"fmt.Errorf"`).
- A generator MAY instead emit **stub facts** for external symbols (`extern:fmt_Errorf.lang_name: str = "fmt.Errorf"`) and ref them, buying uniform integrity at the cost of file size. Either choice MUST be applied consistently per fact kind and documented in the generator's vocabulary.

This boundary was discovered in simulation: call-edge facts emitted as `list(ref(func))` failed on the first stdlib call. Mixed-domain lists are illegal (a list has one element type), so a call-edge fact is either all-`str` (simple, unchecked) or all-`ref` with stubs (checked, heavier). v0.2 takes no side; it requires the choice be explicit.

---

## 7. Values — Syntax Summary

| Type | Example value |
|---|---|
| `bool` | `true` |
| `int` | `8443`, `-5` |
| `float` | `0.1`, `1.5e-3` |
| `str` | `"/etc/certs/server.pem"`, `"func(ctx context.Context) error"` |
| `enum(...)` | `struct` (bare symbol, unquoted) |
| `ref(kind)` | `type:Poster` |
| `T?` | any value of `T`, or the bare word `none` |
| `list(T)` | `[1, 2, 3]`, `["Balance", "Post"]`, `[type:Approver]`, `[]` |

- `none` is legal **only** when the type carries `?`.
- Canonical list separator: `, ` (comma-space). Trailing commas illegal.

**The content boundary.** Prose and blobs are not facts. A `str` value holds a short, single-conceptual-unit string (a path, a name, a signature, a one-line message); multi-paragraph prose, documents, and binary content live *outside* the fact set — as a sibling file, an archive member, or a store entry — and the fact set references or is paired with them by name. This is the same division of labor the projection profile makes for function bodies (§1.1, §11.3: declarations are facts, bodies are computation, `loc` is the handoff): structured data are facts, content is content, and the boundary is a handoff, not an encoding problem. Forcing prose into an escaped one-line `str` is legal but wrong for anything a human diffs or edits; adding multi-line values to the grammar is prohibited (Appendix A).

---

## 8. Canonical Form

Serializers MUST emit canonical form; validators SHOULD offer a canonical-form check.

1. Fact lines sorted **bytewise ascending** by full line content. (Case-sensitive keys sort bytewise: uppercase before lowercase. This is fine — canonical order is for determinism, not aesthetics.)
2. No comment lines, no blank lines in canonical output.
3. Canonical spacing per §2.2; canonical list separator `, `.
4. UTF-8, LF, exactly one trailing newline.

Consequences: independently materialized equal fact sets are **byte-identical**; equality is `sha256sum`; difference is a clean line diff with no moved-line noise. For projections this yields the flagship property: **regenerate after a source edit, and the projection diff is the impact analysis** — verified in simulation, where a function rename produced a 10-line diff that was exactly the renamed facts plus the one updated caller list.

---

## 9. Validation Algorithm

1. **Lex** each line independently (fact/comment/blank; split fact into key, type, value). Failures are per-line with line numbers; no cascading errors — a property of the stateless grammar.
2. **Key check:** segment rules (`[a-zA-Z0-9_]`, letter-first); at most one marker; consistent marker position per instance.
3. **Type check:** the expression is one of the eighteen legal shapes.
4. **Value check:** value inhabits the type's domain (enum symbol listed; `none` only under `?`; list elements inhabit the base type; JSON scalar syntax valid).
5. **Duplicate check:** no key twice in the validation set.
6. **Ref check:** every `ref(kind)` resolves within the validation set (§6.2).
7. *(Optional)*: cycle detection; enum-drift lint; canonical-form check.

Steps 1–5 are line-local or set-local; step 6 needs only the marker set. **No tree is ever built.** A complete validator is on the order of a hundred lines of Go; a complete Go projection generator (parse, typecheck, emit) is a few hundred lines against `go/ast` + `go/types`.

---

## 10. Canonical JSON Encoding (Interchange)

Pretrained models emit JSON far more fluently than any new format. FACT therefore defines a **bijective, lossless** JSON encoding for the generation step; `.fact` remains the on-disk truth. This contains training-data gravity to the one step where it helps.

A FACT file maps to a JSON array of fact objects, sorted by `key`:

```json
[
  {"key": "pkg.imports", "type": "list(str)", "value": ["context", "errors"]},
  {"key": "type:Poster.kind", "type": "enum(struct|iface|basic)", "value": "iface"},
  {"key": "type:MemLedger.implements", "type": "list(ref(type))", "value": ["type:Poster"]},
  {"key": "route:health.auth", "type": "ref(policy)?", "value": null}
]
```

- `key`/`type` are the exact fact-line strings. `value` maps: `bool`→boolean, `int`/`float`→number, `str`/enum symbol/ref marker→string, `none`→`null`, `list(T)`→array.
- The `type` field disambiguates decoding (a JSON string decodes as ref vs str vs enum according to the declared type).
- Round-trip guarantee: `decode(encode(F))` is canonically identical to `F`.

---

## 11. The Projection Profile (Go)

This section is the normative core of FACT's primary use case. It specifies the RECOMMENDED fact vocabulary for a Go declaration-layer projection; generators MAY extend it but MUST document extensions.

### 11.1 Layout, storage, and version control

**Location and name.** One projection file per package, named literally `pkg.fact`, stored **in the directory of the package it projects**, next to the source. The directory carries the package namespace (§11.2: the file identity is the singleton root), so the basename is constant: `**/pkg.fact` globs a whole module, and a moved package moves its projection with it.

**Generated marker.** A stored projection begins with exactly one header line, fixed verbatim:

```
# Code generated by fact project. DO NOT EDIT.
```

The header is an ordinary comment line (§2.1): every conforming parser already ignores it, and the file validates unchanged. It is not a fact and not part of the canonical fact set — but because the string is byte-fixed, whole-file byte comparison between conforming generators remains valid. The wording mirrors Go's generated-code convention so existing tooling recognizes the file as generated.

**Version control.** The projection MUST be committed, in the same commit as the source change it reflects (pre-commit hook, editor hook, or equivalent), and CI MUST verify freshness: regenerate on a clean checkout and require byte-identity with the committed file. Committing generated output is normally suspect — drift, a second source of truth — but canonical serialization (§8) closes that failure mode: staleness is a hash mismatch, mechanically detectable, never a judgment call. The reasons to commit are the point of the format:

- The regeneration diff **is** the impact analysis (§8, §12.4). It can only serve review if it appears in the change itself.
- Agents reading a fresh clone — or operating without a toolchain (review bots, sandboxed agents) — get the navigation layer at zero setup cost.
- Merge conflicts in `pkg.fact` are never resolved by hand: regenerate and commit.

**Review visibility.** Projections SHOULD NOT be marked as collapsed/hidden generated content in review tooling (e.g. `linguist-generated`). The projection diff is the payload of the review, not noise to suppress.

**Read-only lens.** The projection MUST NOT be hand-edited, and tooling SHOULD mark it read-only, for the same reason generated views everywhere must not be editable — a writable projection becomes a second source of truth. The CI freshness gate is the enforcement of lens-ness, not a substitute for it.

### 11.2 Fact vocabulary

Within a package file, the package namespace is the singleton root (no `pkg:` marker needed; the file identity carries it). Kinds: `type`, `func`, `method`, `field` (nested id inside a type's fact keys via compound naming), `var`, `const`, `iface` method entries.

| Fact | Type | Meaning |
|---|---|---|
| `pkg.path` | `str` | Import path of the projected package — self-identification, since the file identity otherwise carries the namespace |
| `pkg.version` | `str` | Module version (`"v1.2.3"`); emitted only when the projected package belongs to a versioned module (third-party, §11.5) |
| `imports` | `list(str)` | Import paths, sorted |
| `type:T.kind` | `enum(struct|iface|basic)` | Underlying kind |
| `type:T.loc` | `str` | `"file.go:line"` — the handoff pointer into source |
| `type:T.fields` | `list(str)` | Field names, declaration order |
| `type:T.field_F_type` | `str` | Field F's type, source-qualified |
| `type:T.methods` | `list(str)` | Method set (pointer-receiver superset), sorted |
| `type:T.implements` | `list(ref(type))` | **Computed** satisfactions (via `types.Implements`) against all in-scope interfaces — the query grep cannot answer from source |
| `type:T.method_M_sig` | `str` | Interface method signature (iface kinds) |
| `func:F.sig` / `method:R_M.sig` | `str` | Full signature, source-qualified |
| `func:F.loc` / `method:R_M.loc` | `str` | Definition site |
| `func:F.exported` | `bool` | Case-derived; projected explicitly so agents need not apply Go rules |
| `func:F.calls` | `list(str)` | Resolved callees (through interfaces: the *static* callee, e.g. `"ledger.Poster.Post"`), sorted, deduplicated. See §6.4 for the str-vs-ref choice |
| `method:R_M.receiver` | `str` | Receiver type name |

*(Compound ids like `field_F_type` and `method:R_M` exist because keys admit only one marker (§3); nested identity is flattened into the id. This is deliberate: it preserves the one-grep-per-entity property — `grep '^type:Service\.'` returns fields, methods, satisfactions, everything.)*

### 11.3 Division of labor (normative intent)

- The projection answers **navigation** questions: what exists, what shape it has, what relates to what, where it lives.
- The projection does **not** answer **statement-level** questions (where is a field assigned, what does this branch do). Its answer to those is the `loc` pointer. Agents edit source, not facts.
- After any source edit: regenerate, and read the projection diff as the impact report.

### 11.4 Token economics (measured, honest)

From simulation on a three-package module: for **declaration-heavy** code, the projection can *exceed* source size (measured: 2,184 vs 1,142 tokens on a decl-only toy) — for interface-only packages, reading source directly is cheaper. Projection cost scales with declaration count; source cost scales with body size. Adding one realistic 300-line function body grew source by ~1,850 tokens and the projection by **4 facts (~60 tokens)**. Real packages are body-dominated 5–10:1; there the projection is smaller by roughly that factor, while *also* answering questions source cannot. Generators SHOULD NOT be evaluated on toy modules.

### 11.5 Third-party projections (packages you do not own)

Dependencies can be projected too. Their facts differ from first-party facts in one structural way: they are pinned to an **immutable module version** — they cannot drift, and never change until the dependency is upgraded. The convention:

- **Never write into the dependency's tree.** The module cache is read-only and shared; `vendor/` is regenerated by tooling. §11.1's next-to-source rule applies only to packages you own.
- **Storage:** a mirror tree at the consuming module's root, keyed by import path, with **no version in the path**:

  ```
  facts/github.com/shopspring/decimal/pkg.fact
  facts/google.golang.org/grpc/credentials/pkg.fact
  ```

  The version is recorded *inside* the file as `pkg.version` (§11.2). Version-free paths are load-bearing: a dependency upgrade then diffs as line-level fact changes — **the diff of a version bump is the API impact analysis of the upgrade** — instead of a whole-file delete-and-add.
- **Commit facts for direct dependencies** (or an explicit allowlist); generate transitive ones on demand. The freshness gate is mechanical: `pkg.version` must match the module's dependency graph, and regeneration from the (read-only, pinned) source must be byte-identical.
- **Machine-wide cache** for uncommitted projections, keyed by `module@version` (the module-cache analog). Facts for a given `module@version` are generated once, ever, and are shareable across projects and machines.
- **Trust:** canonical serialization (§8) makes the bytes of a projection of `module@version` a universal constant. Pregenerated third-party facts from any source are verifiable by regenerate-and-compare, or pinned by hash (the `go.sum` analog). No generator host needs to be trusted.
- Each dependency's fact set stands alone: its refs resolve within its own files (§6.2), and the consuming module refers to its symbols as `str` (§6.4). Provenance travels in `pkg.path`/`pkg.version`. Note that committed third-party facts redistribute a derived index of the dependency's declared API surface; they carry their provenance and are not a copy of the source.

---

## 12. Simulation Findings (v0.2 evidence base)

A real extractor (`go/ast` + `go/types`, ~250 lines) was run against a realistic three-package banking module (ledger/approval/transfer, maker-checker flow). Findings, all incorporated above:

1. **Interface satisfaction is the killer query.** Source grep for `Approver` found only the declaration; the projection answered "what implements it" in one grep because satisfaction was computed at generation. *(→ §1.1, §11.2 `implements`)*
2. **Case is semantic in projected spaces.** The extractor violated v0.1's lowercase rule on its first emitted line. *(→ §3 case-sensitivity change — the headline change of v0.2)*
3. **Call edges hit the external-reference boundary immediately** (`fmt.Errorf` has no facts). *(→ §6.4)*
4. **Canonical regeneration diff = impact analysis.** A rename produced exactly the semantic blast radius as a 10-line diff. *(→ §8)*
5. **Token economics invert on decl-heavy code.** *(→ §11.4)*
6. **The body blind spot behaved as designed** — assignment-site questions correctly fall through to `loc`. *(→ §11.3)*

---

## 13. Grammar (EBNF)

```ebnf
file          = { line } ;
line          = fact_line | comment_line | blank_line ;
comment_line  = ws , "#" , { any_char } , eol ;
blank_line    = ws , eol ;

fact_line     = key , ws , ":" , ws , type , ws , "=" , ws , value , ws , eol ;

key           = segment_or_marker , { "." , segment_or_marker } ;
                (* at most one segment_or_marker may be a marker *)
segment_or_marker = segment | marker ;
marker        = segment , ":" , segment ;
segment       = letter , { letter | digit | "_" } ;

type          = base_type
              | base_type , "?"
              | "list(" , base_type , ")" ;
base_type     = "bool" | "int" | "float" | "str"
              | "enum(" , symbol , { "|" , symbol } , ")"
              | "ref(" , segment , ")" ;
symbol        = letter , { letter | digit | "_" } ;

value         = scalar | "none" | list_value ;
                (* "none" legal only for optional types;
                   list_value legal only for list types *)
list_value    = "[" , ws , [ scalar , { ws , "," , ws , scalar } ] , ws , "]" ;
scalar        = json_bool | json_int | json_float | json_string
              | symbol            (* enum value *)
              | marker ;          (* ref value: kind:id *)

letter        = "a" | ... | "z" | "A" | ... | "Z" ;   (* v0.2: case-sensitive *)
digit         = "0" | ... | "9" ;
ws            = { " " | "\t" } ;
eol           = "\n" ;
```

No recursive production exists: `type` does not reference itself, list elements are scalars only, key depth is namespacing without structure.

---

## 14. Error Catalogue (normative messages)

| Code | Condition | Example message |
|---|---|---|
| `E001` | Line is not a fact/comment/blank | `line 12: cannot lex line` |
| `E002` | Invalid key segment | `line 3: segment "9lives" must start with a letter` |
| `E003` | Multiple instance markers | `line 7: key contains two markers ("pkg:transfer" and "type:Service")` |
| `E004` | Illegal type expression | `line 9: "list(list(int))" — wrappers do not compose` |
| `E005` | Value outside type domain | `line 5: "put" is not in enum(get|post)` |
| `E006` | `none` on non-optional type | `line 8: none requires optional type (add "?")` |
| `E007` | Duplicate fact | `line 15: duplicate of key "server.port" (first at line 2)` |
| `E008` | Unresolved reference | `line 11: ref(policy) "policy:makerr" — no such instance` |
| `E009` | Ref kind mismatch | `line 11: "step:make" is a step, expected ref(policy)` |
| `E010` | Inconsistent marker position | `instance "route:transfer": marker at segment 1 and segment 2 across lines` |
| `W001` | *(lint)* Enum drift | `key suffix "method" under kind "route" has differing symbol sets` |
| `W002` | *(lint)* Non-canonical form | `file is valid but not canonically sorted/spaced` |

---

## 15. Implementation Checklist

For an agent implementing FACT support (suggested order):

1. **Lexer/parser** — line classifier + fact-line splitter. Stateless; lines processed independently.
2. **Type-expression parser** — eighteen legal shapes; reject everything else.
3. **Value checker** — per-type domain validation, JSON-compatible scalars.
4. **Set validator** — duplicates (E007), marker consistency (E010), ref resolution (E008/E009) over the validation set.
5. **Canonical serializer** — sort, strip, normalize (§8).
6. **JSON encoder/decoder** — bijective (§10), with round-trip property test.
7. **Go projection generator** — parse + typecheck (`go/ast`, `go/types`), emit §11.2 vocabulary; regenerate-on-save hook; read-only output.
8. **Lints** — enum drift, canonical check, optional ref cycles.
9. **Property tests** — `decode(encode(F)) == canonical(F)`; concatenation of disjoint sets validates; every single-line mutation validates or yields exactly one error; projection regeneration on an unchanged source tree is byte-identical.

Items 1–5: ~100 lines of Go. Item 7: a few hundred lines. Both verified at these scales in simulation.

---

## Appendix A — Design Decisions Ledger

Tested and **rejected** (do not re-litigate without new evidence):

| Proposal | Verdict | Reason |
|---|---|---|
| Sections (`[server]`) | Rejected | A line's meaning would depend on a distant header; grep hits become ambiguous |
| External schema | Rejected | Two sources of truth; the annotation-as-domain property does the schema's work inline |
| Type inference | Rejected | The annotation is the edit domain, not a classifier |
| Drop `ref(kind)` parameter | Rejected | Value names today's inhabitant; parameter names tomorrow's legal edits |
| Normalize lists into back-refs | Rejected | Ordered membership is one atomic fact |
| Drop `bool` | Rejected (kept as sugar) | Files exist millions of times, the spec once |
| Nested wrappers | **Adopted ban** | Type grammar finite (18 shapes), non-recursive all the way down |
| Canonical form | **Adopted** | Hash equality; clean diffs; projection-diff-as-impact-analysis |
| Last-wins duplicates | Rejected | Silent override hides bugs from agents |
| Multi-line string values (heredocs, continuations) | **Rejected** | Every load-bearing property hangs on one line = one fact: bytewise line sorting for canonical form, stateless line-local lexing, grep hits being complete facts, the single-line edit primitive. Prose crosses the content boundary (§7) as a sibling file/archive member, never as grammar |
| Defaults by key absence | Rejected | Absent vs asserted-none = investigate vs trust |
| Lowercase-only keys (v0.1) | **Reversed in v0.2** | Projected identifiers are case-sensitive and case is semantic (Go exportedness); an actual extractor violated the rule on first run |
| Full Go-source conversion (bodies as facts) | Rejected | Reinvents compiler IR at ~8× tokens while discarding model fluency in Go; declarations are facts, bodies are computation |
| `list(ref(func))` for call edges | Deferred (§6.4) | External symbols break resolution; per-generator choice between all-str and stub-facts |
| Package-qualified keys (one leading `pkg:` marker; module-relative mangled ids; module = validation set) | **Deferred** | Would make per-package files concatenatable, enable module-wide validation, and make cross-package refs expressible (incl. config→code refs, and qualified ref values) — at a per-line token tax on every projection. Unneeded by interactive agents: grep's printed file path already qualifies (§1.1), and the compiler plus freshness gate already guarantee cross-package integrity (§6.2). **Adopt if/when fine-tune training-corpus generation begins** — a format baked into model weights cannot be changed afterwards, and that is the one consumer for whom self-contained module-wide lines, single-artifact module diffs, and a millisecond module-wide validator (hallucination gate) pay for the tax |

## Appendix B — Naming

**FACT**: each line is one fact. Induced vocabulary is exact: *assert a fact*, *retract a fact*, *duplicate fact*, *a projection is the set of facts about a codebase*. Rejected: anything containing "ML" or "-on" — the format is not a markup language or object notation, and the name must not wear the family costume of what it replaces.

---

*Spec v0.2. Open items for v0.3: include/overlay mechanism for config environment variants (likely canonical-set union with explicit override markers, not implicit layering); §6.4 resolution after field experience with stub facts; package-qualified keys remain deferred with an explicit trigger condition (Appendix A) — the per-file validation scope of §6.2 is the resolution until a training-corpus consumer exists; projection vocabularies for further fact sources (SQL schemas, OpenAPI, protobuf — each is a declaration layer awaiting projection); formal test-vector suite.*
