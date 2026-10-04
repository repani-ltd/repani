FACT — Format Specification v0.4

FACT is a line-oriented format for facts about systems, designed for AI agents rather than humans. A FACT file is an unordered set of lines, where each line is one complete, self-contained, typed fact. There is no nesting, no significant whitespace, no inter-line dependence, and no external schema: the file is the schema.

FACT exists primarily as a projection format: a generated, read-only index of a codebase (canonically: a Go module) that lets agents navigate and reason about code structure at grep cost. Configuration files are the degenerate — and fully supported — case where the facts are the whole system rather than a projection of one.

Recommended file extension: .fact
Authoring reference: fact spec (doc.go's package comment, embedded
in the binary) is what an agent needs to read and write FACT; this
document is the normative standard behind it.
MIME type (provisional): text/x-fact

Changes from v0.1: segments and enum symbols are now case-sensitive [a-zA-Z0-9_] (§3, §4.1) — required because projected identifiers (Go) are case-sensitive and case is semantic; primary-purpose reframing (§1); projection profile added (§11); storage and commit convention for projections (§11.1); validation scope fixed to one file per package for projections (§5, §6.2 — per-package files share singleton keys and must not be concatenated for validation); third-party projection convention (§11.5); external-reference boundary rule (§6.4); findings from a real Go extraction simulation incorporated (§12).

Changes from v0.3: comment lines are removed from the grammar (§2.1, §8, §9, §13, §14, Appendix A). A line is now a fact line or a blank line; a line whose first non-space character is # is E001. The construct was not neutral -- canonical form has always required comments to be dropped (§8), so fact fmt -w silently deleted them, taking nine live lines of run instructions out of a working config in the repos that use this format, and one project's own documentation already told its authors not to use comments because the parser discards them. §8 could not be relaxed to preserve them without giving up byte-equality for equal fact sets, so the construct went instead: what a file says about itself is prose, and §7's content boundary already sends prose to a sibling document. Blank lines are unaffected (sorting loses no information). Version bumped rather than amended in place, since v0.3 files carrying comments are not v0.4 files. Non-normative authoring guidance added on choosing which dimension of data goes in a key prefix, an id, or a ref value (§3); the value claim of §1.1 reordered to put line-local validation ahead of greppability, with the evidence; a bulk boundary and admission test for a whole file added (§7); the row-format alternative and its one-way derived view recorded (Appendix A).

Changes from v0.2: datetime added as a seventh base type (§4.1) — a strict RFC 3339 subset, UTC only, two unquoted literal forms (2026-07-20, 2026-09-01T09:30:00Z), both canonical as written; the type grammar grows to twenty-one shapes (§4.2, §13); JSON encoding maps datetime to string with the existing type-field disambiguation (§10); rejected design variants recorded (Appendix A).

---

# 1. Why FACT Exists

## 1.1 The purpose: configuration and data

FACT is a format for the files a program reads and a person writes: a station's configuration, a squad, an event log, a ledger, a registry. What it asks of a file is what makes such files trustworthy, in the order the properties have proved to matter: a declared type on every value, so a mistake is caught on the line that made it and a reader never guesses; references that must resolve, so a misspelled name is an error at load rather than an empty value; duplicate keys refused, so nothing is silently overridden; one fact per line, so a file is greppable and diffable line by line; and a canonical form, so two files with the same facts are the same bytes. The value against JSON is not expressiveness -- FACT has less -- but that every line stands alone and every mistake is reported with a line number.

Why validation leads that list, ahead of the grep property the format was sold on. The writer of a data file is now usually a generator that makes several mistakes per run and is repaired line by line, and the checks above are what make its mistakes local: seeded one at a time into a 1600-record agent-generated corpus, a duplicate record, an enum typo and a dangling ref were each caught with no schema, on their own line, while every other error in the file still reported (§9's no-cascading rule). A row format catches none of the three without an external schema, and a syntax error in a host-language table or a JSON document masks the whole file. Greppability is what makes the format pleasant to work in afterwards, and it is real -- a prefix grep returns a whole entity with every field labeled, and canonical order makes that block contiguous -- but it is one-dimensional: a hit is a complete fact, never a complete record, so a query over two fields at once costs a grep per conjunct and a join. That is the format's one standing cost, it falls on analysis rather than on lookup, and Appendix A records what was weighed against it.

FACT began (2026-08) as a projection format for Go declarations, so that agents could answer navigation questions by grep instead of reading source, with a generator, a per-package pkg.fact, hooks and a freshness gate. That machinery was retired on 2026-09-06 after measurement: the projections were half the size of the source they described, touched half of all commits, and the one use agents made of them, an index of signatures, is served by go doc -u -short from source, synchronously and without a file. The evidence and the argument are in §12; the profile that section 11 defined is recorded there as retired. What survives is this format, whose properties were never about Go.

## 1.2 Configuration

A config file is the special case where there is no source to project — the facts are the system. All the projection properties carry over: single-line edits, unambiguous greps, loud validation, canonical diffs. The config use case is what originally motivated the format; the projection use case is what justifies it. Both profiles share one grammar.

---

# 2. Lexical Structure

## 2.1 File

.item Encoding: UTF-8, no BOM.
.item Line separator: \n (LF). A trailing newline at end of file is required in canonical form.
.item A file is a sequence of lines. Each line is exactly one of:
.item -- a fact line
.item -- a blank line: only whitespace; ignored, carries no semantics (grouping by blank lines is purely cosmetic)
.item There are no comments (removed in v0.4). A line whose first non-space character is # is E001, with a message naming the removal. Nothing in a FACT file is addressed to a reader rather than a parser: what a file says about itself -- what consumes it, how to run it, why a key holds the value it does -- is prose, and prose lives in a sibling document under the content boundary of §7. The construct was removed rather than kept because canonical form drops comments (§8), so the format's own formatter destroyed them; keeping them would have cost byte-equality for equal fact sets, which is the property §8 exists for.

## 2.2 Fact Line

.pre
key: type = value
.end

.item Exactly one : separates key from type (the first : that is not inside an instance marker, see §3).
.item Exactly one = separates type from value (the first = at the top level of the type expression).
.item Canonical spacing: no space before :, one space after :, one space before and after =. Parsers MUST accept arbitrary horizontal whitespace around : and =; serializers MUST emit canonical spacing.
.item A fact line has no continuation. Multi-line values do not exist; newlines inside strings are written as the escape \n.
.item No inline comments. # after a value is an error.

---

# 3. Keys

A key is a dot-separated path of segments:

.pre
segment(.segment)*
.end

.item Segment characters: [a-zA-Z0-9_], must start with a letter. Keys are case-sensitive. (Changed in v0.2. Projected identifier spaces — Go above all — are case-sensitive, and case carries meaning: Go exportedness is literally the case of the first letter. Lowercasing would collide validate/Validate and destroy the exportedness signal. This was discovered empirically when a working extractor violated the v0.1 grammar on its first run.)
.item Casing convention (non-normative): hand-authored config SHOULD use lowercase snake_case throughout — one canonical spelling of every key. Projections MUST use source-language casing verbatim — the projection is a mirror, and mirrors do not normalize.
.item Instance marker: at most one segment in the path may take the form kind:id, where kind and id are each valid segment strings. This segment declares that the subtree rooted there is an instance (record) of kind. By convention, kind is lowercase even in projections (type:Approver, func:Submit — the kind vocabulary belongs to the generator, the id belongs to the source).
.item Zero instance markers → the fact belongs to the singleton namespace.
.item Two or more instance markers in one key → error. (This enforces "no nested records" at the key level. Nested identity is expressed by compound ids or refs, e.g. method:Service_Settle, never by nested markers.)
.item The marker may appear at any segment position; validators MUST require that a given kind:id marker appears under one key prefix — the same segments before it, hence the same position — across all facts of one instance. The subtree rooted at the marker is the instance; an instance has one root, not one root per namespace.
.item Dots are namespacing, not structure. server.tls.enabled does not imply an object server.tls exists. There is no tree; there is only the set of lines.

Choosing the dimension (non-normative authoring guidance). Data has more dimensions than a key has places to put them, and the one-marker rule forces a choice: a squad's players can be nested under the club by prefix (liverpool.player:dalglish), folded into the id (player:t26_01), or related by a value (player:dalglish.team: ref(team) = team:liverpool). All three are greppable in one pass — a prefix grep, an id-prefix grep, and a value grep respectively — so grep does not decide it. What decides it is which dimension changes:

.item Put the FROZEN dimension in the prefix or the id. The marker-prefix rule above welds an instance to its prefix, so a player nested under a club cannot change clubs without rewriting every line of the instance; an id-folded dimension (player:t26_01) is a string, so it is neither checked, refable, nor renameable. Both are right only for a dimension that will not move -- a historical XI, a package in a projection.
.item Put the MUTABLE dimension in a ref value. A transfer is then one line, a misspelled club is E008 rather than a silently unaffiliated player, and the club becomes an instance with facts of its own. Prefer ref(kind) over a bare str here: the value is a marker, so the grep for it is exact ('= team:liverpool') where a string would collide with any nickname or city field holding the same word.
.item State a membership ONCE. Both directions are one grep -- a ref value is the same token as the marker, so grepping player:dalglish finds his own facts and any list naming him -- so the second copy buys nothing and cannot be checked: no validator can see a team's squad list disagreeing with the players' own team facts. Order decides the side: a set that changes lives on the member (.team, one-line edits), an ordered membership lives on the container as list(ref(member)), which §6.3 makes one atomic fact.

Examples:

.pre
server.tls.enabled                          → singleton (config profile)
route:transfer.method                       → instance "transfer" of kind "route"
pkg:transfer.type:Service.fields            → ILLEGAL (two markers) — see next line
pkg_transfer.type:Service.fields            → legal alternative...
.end

Projection namespacing note: because only one marker is allowed, a projection of many packages either (a) emits one file per package with the package as a singleton prefix (type:Service.fields inside transfer.fact), or (b) folds the package into the kind or id (type:transfer_Service.fields) in a combined file. Option (a) is RECOMMENDED: one pkg.fact per package, mirroring the source tree. Whole-module queries are read operations over the tree (grep -r --include=pkg.fact), where the printed file path supplies the package namespace. Per-package projection files are not concatenatable into one validation set — each asserts the same singleton keys (imports, pkg.path) and same-named types collide — so validation scope is per file (§6.2).

---

# 4. Types

## 4.1 Base Types (seven)

Type — Values — Notes:

.item bool — true, false — Sugar for enum(true|false); semantically there are six base types
.item int — JSON integer syntax, 64-bit signed range — No width variants; the value is the truth; -0 is the value 0 and canonicalizes to 0 (one spelling per value, §8)
.item float — JSON number syntax with . or exponent — IEEE 754 double
.item str — JSON string syntax, double-quoted — JSON escaping rules exactly (\", \\, \n, \t, \uXXXX)
.item datetime — 2026-07-20 or 2026-09-01T09:30:00Z, unquoted — Strict RFC 3339 subset, UTC only; two forms, both canonical as written; see below (new in v0.3)
.item enum(a|b|c) — One of the listed bare symbols — Symbols follow segment rules ([a-zA-Z0-9_], start with letter, case-sensitive); at least one symbol; written unquoted in the value. The word none is reserved (it is the absence value of optional types, §7) and is not a legal symbol: an enum listing it is E004, since the symbol could never be written as a value
.item ref(kind) — The instance marker of an instance of kind, written kind:id — See §6

The datetime type (new in v0.3). A UTC point in time or a calendar date, written unquoted in exactly two forms:

.pre
2026-07-20              full-date   (day precision)
2026-07-20T09:30:00Z    date-time   (second precision, UTC)
.end

.item Strict RFC 3339 subset. Fixed-width zero-padded fields; uppercase T and Z only. No offsets — +02:00, and even +00:00, are E005: FACT datetimes are UTC by definition, and the Z travels with the token so that consumers who never read this spec parse it as UTC rather than local time (a Z-less date-time token reads as local time across mainstream ecosystems). No fractional seconds. Values must be calendar-valid: Gregorian month lengths, leap years honored, hours 00–23, minutes and seconds 00–59 (no leap second 60), years 0001–9999. Every violation is E005.
.item Both forms are canonical as written. A full-date does not normalize to midnight: 2026-07-20 and 2026-07-20T00:00:00Z are distinct values — a day is not an instant. One annotation deliberately admits both precisions: per the annotation-as-edit-domain rule (§4.3), a datetime key tells an editing agent that widening a date to a timestamp (or narrowing back) is a legal edit.
.item Bytewise order is chronological order, within and across both forms — a consequence of fixed-width big-endian fields with no fractions. Datetime values therefore inherit canonical-form determinism (§8) with zero normalization rules, and a value grep such as '= 2026-07' is a temporal range query.
.item Semantics stop at the token. FACT defines the value space and nothing more: no datetime arithmetic, no equality across precisions, no durations, no time zones. Interpretation belongs to the consumer's datetime library, which every host language provides. This is what keeps the type's semantics closed — no tzdata dependency, no political time — the test every other semantic-type candidate fails (§4.3).

## 4.2 Wrappers (two, non-composing)

.table 8L 28L *L
^Wrapper | Meaning | Constraints
T? | Optional: value may be none | T must be a base type. list(T)? is illegal — the empty list [] is the "none of lists"; two spellings of absence are forbidden
list(T) | Ordered list, written [v1, v2, ...] | T must be a base type. list(list(T)) is illegal. Empty list [] is valid
.end

The type grammar is deliberately non-recursive. The complete set of legal type expressions: seven base types, seven optional base types, seven list-of-base types. Twenty-one shapes total. Hard grammar rule, not style.

## 4.3 Deliberate exclusions

.item Integer widths, semantic string types (path, url, duration): conventions live in key names (timeout_ms, cert_path), not the grammar. datetime (v0.3) is the deliberate exception: alone among semantic-type candidates its value space has a closed, finite, canonicalizable grammar with no external dependency, so it passes the same test the original six base types pass and path/url/duration fail.
.item Maps/objects as values: structure is expressed with instances and refs. The only compound value is the flat list.
.item Type inference: forbidden. The annotation is the domain of legal edits — what a value may become — not a classifier of the current value. An agent editing a line it has never seen before must learn the legal replacements from that line alone.
.item Declared/named enum types: enums are restated at every use site. Drift between copies is a one-pass lint; a central declaration would reintroduce inter-line dependence — the original sin this format exists to kill.

---

# 5. Data Model

.item A file denotes an unordered set of facts. Line order carries no meaning. Concatenation of files with disjoint fact sets (followed by duplicate checking) is a valid merge primitive. Config files are disjoint by authorship, so config merges by concatenation; per-package projection files are not disjoint (each asserts the same singleton keys, e.g. imports) and compose by directory layout instead (§11.1), never by concatenation.
.item Duplicate keys are an error. Not last-wins, not merge — error. Silent override is how bugs hide from agents.
.item Existence rule: an instance kind:id exists iff at least one fact line contains that marker. Nothing exists by implication; empty records cannot exist.
.item Totality rule (config profile): absence of a key is never a default. "Deliberately nothing" must be asserted: route:health.auth: ref(policy)? = none. Absent (nothing was decided — investigate) and asserted none (decided: nothing — trust) are different states, and the distinction is load-bearing for agents reading configs they did not write. The rule is stated for keys because only keys carry values: a namespace (a dotted prefix, the subtree a language binding maps to a nested record) has no none — by the existence rule nothing exists by implication, so a binding that serializes an absent record can only omit its subtree, and a reader cannot tell that from a record nobody decided about. Where "decided: no record" must be readable, assert it with an explicit optional key.
.item Completeness rule (projection profile): a projection is total over its declared scope — every declaration in scope appears. Within that scope, absence of a fact kind means the generator does not emit it, never that the source lacks it. Generators MUST document their fact vocabulary (see §11.2).

---

# 6. References

## 6.1 Syntax and resolution

.item A ref(kind) value is written as the target's instance marker: kind:id (e.g., policy:maker, type:Poster).
.item A ref is valid iff at least one fact line exists containing the marker kind:id with the same kind as the ref's type parameter.
.item The parameter is not redundant with the value: the value names the current inhabitant; the parameter names the domain — what may legally go there in a future edit.

## 6.2 Mechanics

.item Referential integrity is checkable by prefix search over the line set. No tree construction, ever.
.item Resolution scope is the validation set. Config profile: the file, or a deliberate concatenation of config files (keys are disjoint by authorship; a duplicate is then a real error). Projection profile: exactly one file — one package. Cross-package mentions are str values (§6.4) and are deliberately not checked by the FACT validator: their integrity is already guaranteed upstream by the host language's compiler plus the freshness gate (§11.1), and re-checking it here would be redundant.

## 6.3 Cycles and order

.item Ref cycles are permitted by the format (sometimes meaningful); applications may impose acyclicity. Validators SHOULD offer an optional cycle check.
.item Ordered relationships use list(ref(kind)) — the list carries the order; the instances do not. "This pipeline is these steps in this order" is one fact and stays on one line.

## 6.4 The external-reference boundary (new in v0.2)

Projections inevitably mention symbols outside the projected scope (fmt.Errorf, context.Context). Refs to them would fail resolution. The rule:

.item Intra-scope relationships use ref(kind) — integrity is checked.
.item Extra-scope mentions use str — by convention qualified as the source language writes them ("fmt.Errorf").
.item A generator MAY instead emit stub facts for external symbols (extern:fmt_Errorf.lang_name: str = "fmt.Errorf") and ref them, buying uniform integrity at the cost of file size. Either choice MUST be applied consistently per fact kind and documented in the generator's vocabulary.

This boundary was discovered in simulation: call-edge facts emitted as list(ref(func)) failed on the first stdlib call. Mixed-domain lists are illegal (a list has one element type), so a call-edge fact is either all-str (simple, unchecked) or all-ref with stubs (checked, heavier). v0.2 takes no side; it requires the choice be explicit.

---

# 7. Values — Syntax Summary

.table 10L *L
^Type | Example value
bool | true
int | 8443, -5
float | 0.1, 1.5e-3
str | "/etc/certs/server.pem", "func(ctx context.Context) error"
datetime | 2026-07-20, 2026-09-01T09:30:00Z (unquoted; two forms, §4.1)
enum(...) | struct (bare symbol, unquoted)
ref(kind) | type:Poster
T? | any value of T, or the bare word none
list(T) | [1, 2, 3], ["Balance", "Post"], [type:Approver], []
.end

.item none is legal only when the type carries ?.
.item Canonical list separator: ", " (comma-space). Trailing commas illegal.

The content boundary. Prose and blobs are not facts. A str value holds a short, single-conceptual-unit string (a path, a name, a signature, a one-line message); multi-paragraph prose, documents, and binary content live outside the fact set — as a sibling file, an archive member, or a store entry — and the fact set references or is paired with them by name. This is the same division of labor the projection profile makes for function bodies (§1.1, §11.3: declarations are facts, bodies are computation, file is the handoff): structured data are facts, content is content, and the boundary is a handoff, not an encoding problem. Forcing prose into an escaped one-line str is legal but wrong for anything a human diffs or edits; adding multi-line values to the grammar is prohibited (Appendix A).

The bulk boundary, and the admission test for a FILE. The same division applies in the other direction, and it is a test a generator must pass before it emits: a fact set is admitted when its lines are read, grepped or edited one at a time, and refused when it is bulk observations that only ever move as a block. Every property this format charges for is per-line -- the annotation as edit domain, the line-local error, the single-line edit, the prefix grep -- so a file nobody queries by line pays the whole tax and collects nothing. The cost is measurable: across the repos that use FACT, 98.4% of all fact lines (1,138,462 of 1,156,425) are three generated bulk files, one of them a character-model background distribution of 1,058,994 facts and 36 MB whose bytes are 80% key and type framing and which costs 1.9 s to validate on every load, is never grepped and has never had a line edited. By file count the format is used as intended; by line count it is not, and both are true of the same tree. Such data is an array, and an array belongs in the handoff this section already defines -- a sibling file in a bulk format, referenced by name -- exactly as a function body does (§11.3) and prose does above. The admission test for vocabulary (§4.3) asks whether a type's semantics are closed; the admission test for a file asks whether anything will ever read one of its lines alone.

---

# 8. Canonical Form

Serializers MUST emit canonical form; validators SHOULD offer a canonical-form check.

.item 1. Fact lines sorted bytewise ascending by full line content. (Case-sensitive keys sort bytewise: uppercase before lowercase. This is fine — canonical order is for determinism, not aesthetics.)
.item 2. No blank lines in canonical output. (Through v0.3 this item also dropped comment lines, which is why they were removed from the grammar; see §2.1.)
.item 3. Canonical spacing per §2.2; canonical list separator ", ".
.item 4. UTF-8, LF, exactly one trailing newline.

Consequences: independently materialized equal fact sets are byte-identical; equality is sha256sum; difference is a clean line diff with no moved-line noise. For projections this yields the flagship property: regenerate after a source edit, and the projection diff is the impact analysis — verified in simulation, where a function rename produced a 10-line diff that was exactly the renamed facts plus the one updated caller list.

---

# 9. Validation Algorithm

.item 1. Lex each line independently (fact or blank; split fact into key, type, value; a # line is E001, §2.1). Failures are per-line with line numbers; no cascading errors — a property of the stateless grammar.
.item 2. Key check: segment rules ([a-zA-Z0-9_], letter-first); at most one marker; consistent marker prefix per instance.
.item 3. Type check: the expression is one of the twenty-one legal shapes.
.item 4. Value check: value inhabits the type's domain (enum symbol listed; none only under ?; list elements inhabit the base type; JSON scalar syntax valid; datetime form, calendar validity, and Z per §4.1).
.item 5. Duplicate check: no key twice in the validation set.
.item 6. Ref check: every ref(kind) resolves within the validation set (§6.2).
.item 7. (Optional): cycle detection; enum-drift lint; canonical-form check.

Steps 1–5 are line-local or set-local; step 6 needs only the marker set. No tree is ever built. A complete validator is on the order of a hundred lines of Go; a complete Go projection generator (parse, typecheck, emit) is a few hundred lines against go/ast + go/types.

---

# 10. Canonical JSON Encoding (Interchange)

Pretrained models emit JSON far more fluently than any new format. FACT therefore defines a bijective, lossless JSON encoding for the generation step; .fact remains the on-disk truth. This contains training-data gravity to the one step where it helps.

A FACT file maps to a JSON array of fact objects, sorted by key:

.pre
[
  {"key": "cert.expires", "type": "datetime", "value": "2026-09-01T09:30:00Z"},
  {"key": "pkg.imports", "type": "list(str)", "value": ["context", "errors"]},
  {"key": "type:Poster.kind", "type": "enum(struct|iface|basic)", "value": "iface"},
  {"key": "type:MemLedger.implements", "type": "list(ref(type))", "value": ["type:Poster"]},
  {"key": "route:health.auth", "type": "ref(policy)?", "value": null}
]
.end

.item key/type are the exact fact-line strings. value maps: bool→boolean, int/float→number, str/enum symbol/ref marker/datetime→string, none→null, list(T)→array.
.item The type field disambiguates decoding (a JSON string decodes as ref vs str vs enum vs datetime according to the declared type).
.item Round-trip guarantee: decode(encode(F)) is canonically identical to F.

---

# 11. The Projection Profile (Go) -- retired 2026-09-06

This section defined a vocabulary for projecting a Go package's declaration layer into a pkg.fact file beside its source (kinds type, func, method, const and var; signatures, fields, method sets, computed interface satisfactions, resolved call edges, defining files; a fixed generated-file header; the projection committed with the source change and gated for freshness; third-party packages projected into a facts/ mirror keyed by import path). It is retired, the generator and hooks removed, and no file conforms to it; the section number is kept so that the ledgers citing it still resolve. The reasons, measured on the repos that used it for eighteen days:

.item Agents used the projection as an index of signatures and for nothing else; the call edges and computed satisfactions, the facts that justified a format of their own, were not queried. go doc -u -short serves the index from source, synchronously, works while a package does not yet compile, and needs no file.
.item The stored projections were half the size of the source they described, touched half of all commits, and needed a post-edit hook, a pre-commit hook and a check gate to stay coherent with the compiler's own knowledge.
.item The premise that held was the one about the LSP: gopls under agent-speed editing answers from stale snapshots without saying so, and agents fall back to the compiler. The answer is oracles that read source synchronously when asked, not a cached projection that must be kept fresh.

The profile's design record and its measurements remain in §12 as evidence.

---

# 12. Findings (evidence base)

## 12.1 Simulation (v0.2)

A real extractor (go/ast + go/types, ~250 lines) was run against a realistic three-package banking module (ledger/approval/transfer, maker-checker flow). Findings, all incorporated above:

.item 1. Interface satisfaction is the killer query. Source grep for Approver found only the declaration; the projection answered "what implements it" in one grep because satisfaction was computed at generation. (→ §1.1, §11.2 implements)
.item 2. Case is semantic in projected spaces. The extractor violated v0.1's lowercase rule on its first emitted line. (→ §3 case-sensitivity change — the headline change of v0.2)
.item 3. Call edges hit the external-reference boundary immediately (fmt.Errorf has no facts). (→ §6.4)
.item 4. Canonical regeneration diff = impact analysis. A rename produced exactly the semantic blast radius as a 10-line diff. (→ §8)
.item 5. Token economics invert on decl-heavy code. (→ §11.4)
.item 6. The body blind spot behaved as designed — assignment-site questions correctly fall through to the file handoff. (→ §11.3)

## 12.2 Field measurements (v0.3, four projected modules)

Measured across every committed projection in four real modules (typesetting library, encrypted KV store, weather station, this toolchain — 21 pkg.fact files, 18 non-trivial packages):

.item 7. Compression is ~2×, not 5–10×. Source is 1.3–3.2× the projection by bytes across all 18 packages with the complete vocabulary (2–3.2× before method call edges and const/var facts were added). (→ §11.4, which corrects the earlier extrapolation)
.item 8. calls is the workhorse; implements is sparse but unique. Of 100 implements facts, 4 are non-empty — interface-light codebases barely exercise the flagship simulation query. Meanwhile 287 calls facts are non-empty, and the module-wide reverse call lookup (grep -r --include=pkg.fact 'calls.*"pkg\.') is the highest-frequency agent query in practice. Simulation finding 1 named the uniquely grep-impossible query; field use names the everyday one. (→ §1.1)
.item 9. Freshness holds only where regeneration is automated. Every projection measured was byte-fresh — in repos where an editor/agent hook regenerates on save. §11.1's commit-and-verify discipline is load-bearing, not ceremonial: an unhooked consumer drifts silently, and a stale projection is worse than none.

---

# 13. Grammar (EBNF)

.pre
file          = { line } ;
line          = fact_line | blank_line ;
blank_line    = ws , eol ;
                (* no comment_line: removed in v0.4, §2.1 *)

fact_line     = key , ws , ":" , ws , type , ws , "=" , ws , value , ws , eol ;

key           = segment_or_marker , { "." , segment_or_marker } ;
                (* at most one segment_or_marker may be a marker *)
segment_or_marker = segment | marker ;
marker        = segment , ":" , segment ;
segment       = letter , { letter | digit | "_" } ;

type          = base_type
              | base_type , "?"
              | "list(" , base_type , ")" ;
base_type     = "bool" | "int" | "float" | "str" | "datetime"
              | "enum(" , symbol , { "|" , symbol } , ")"
              | "ref(" , segment , ")" ;
symbol        = letter , { letter | digit | "_" } ;
                (* except the reserved word "none", §4.1 *)

value         = scalar | "none" | list_value ;
                (* "none" legal only for optional types;
                   list_value legal only for list types *)
list_value    = "[" , ws , [ scalar , { ws , "," , ws , scalar } ] , ws , "]" ;
scalar        = json_bool | json_int | json_float | json_string
              | symbol            (* enum value *)
              | marker            (* ref value: kind:id *)
              | datetime_lit ;

datetime_lit  = full_date , [ "T" , clock , "Z" ] ;
                (* calendar-valid, UTC only; see §4.1 *)
full_date     = digit , digit , digit , digit , "-" ,
                digit , digit , "-" , digit , digit ;
clock         = digit , digit , ":" , digit , digit , ":" , digit , digit ;

letter        = "a" | ... | "z" | "A" | ... | "Z" ;   (* v0.2: case-sensitive *)
digit         = "0" | ... | "9" ;
ws            = { " " | "\t" } ;
eol           = "\n" ;
.end

No recursive production exists: type does not reference itself, list elements are scalars only, key depth is namespacing without structure.

---

# 14. Error Catalogue (normative messages)

Code — Condition — Example message:

.item E001 — Line is neither a fact nor blank — line 12: cannot lex line / line 1: cannot lex line: comments are not part of the format (removed in v0.4) — what a file says about itself belongs in a sibling document
.item E002 — Invalid key segment — line 3: segment "9lives" must start with a letter / line 4: segment "tls-mode" contains characters outside [a-zA-Z0-9_]
.item E003 — Multiple instance markers — line 7: key contains two markers ("pkg:transfer" and "type:Service")
.item E004 — Illegal type expression — line 9: "list(list(int))" is not one of the twenty-one legal type shapes (wrappers do not compose) / line 10: "enum(none|some)": none is reserved and cannot be an enum symbol
.item E005 — Value outside type domain — line 5: "put" is not in enum(get|post) / line 6: "2026-09-01T09:30:00+02:00" — datetime is UTC only (write Z)
.item E006 — none on non-optional type — line 8: none requires optional type (add "?") / line 9 (list type): lists have no none; use []
.item E007 — Duplicate fact — line 15: duplicate of key "server.port" (first at line 2)
.item E008 — Unresolved reference — line 11: ref(policy) "policy:makerr" — no such instance
.item E009 — Ref kind mismatch — line 11: "step:make" is a step, expected ref(policy)
.item E010 — Inconsistent marker prefix — line 7: instance "route:transfer": inconsistent marker prefix ("route:transfer" vs "api.route:transfer")
.item W001 — (lint) Enum drift — key suffix "method" under kind "route" has differing symbol sets
.item W002 — (lint) Non-canonical form — file is valid but not canonically sorted/spaced

---

# 15. Implementation Checklist

For an agent implementing FACT support (suggested order):

.item 1. Lexer/parser — line classifier + fact-line splitter. Stateless; lines processed independently.
.item 2. Type-expression parser — twenty-one legal shapes; reject everything else.
.item 3. Value checker — per-type domain validation, JSON-compatible scalars, datetime calendar/UTC check (§4.1).
.item 4. Set validator — duplicates (E007), marker consistency (E010), ref resolution (E008/E009) over the validation set.
.item 5. Canonical serializer — sort, strip, normalize (§8).
.item 6. JSON encoder/decoder — bijective (§10), with round-trip property test.
.item 7. Go projection generator — parse + typecheck (go/ast, go/types), emit §11.2 vocabulary; regenerate-on-save hook; read-only output.
.item 8. Lints — enum drift, canonical check, optional ref cycles.
.item 9. Property tests — decode(encode(F)) == canonical(F); concatenation of disjoint sets validates; every single-line mutation validates or yields exactly one error; projection regeneration on an unchanged source tree is byte-identical.

Items 1–5: ~100 lines of Go. Item 7: a few hundred lines. Both verified at these scales in simulation.

---

# Appendix A — Design Decisions Ledger

Tested and rejected (do not re-litigate without new evidence):

.table 24L 16L *L
^Proposal | Verdict | Reason
Sections ([server]) | Rejected | A line's meaning would depend on a distant header; grep hits become ambiguous
External schema | Rejected | Two sources of truth; the annotation-as-domain property does the schema's work inline
Type inference | Rejected | The annotation is the edit domain, not a classifier
Drop ref(kind) parameter | Rejected | Value names today's inhabitant; parameter names tomorrow's legal edits
Normalize lists into back-refs | Rejected | Ordered membership is one atomic fact
Drop bool | Rejected (kept as sugar) | Files exist millions of times, the spec once
Nested wrappers | Adopted ban | Type grammar finite (21 shapes), non-recursive all the way down
Canonical form | Adopted | Hash equality; clean diffs; projection-diff-as-impact-analysis
Last-wins duplicates | Rejected | Silent override hides bugs from agents
Comment lines | Removed (v0.4) | Admitted in v0.1 without a consumer, and never coherent with §8: canonical form drops comments, so fact fmt -w silently deleted them -- nine live lines of run instructions in a working config, and 264 lines across 26 of the 48 fact files in these repos. The format's own formatter destroying a construct is worse than not having it, and one project's documentation already told authors not to use comments because the parser discards them. §8 could not be relaxed to keep them (equal fact sets must produce equal bytes), and the alternative of making fmt -w refuse on a commented file would leave the construct as a permanent exception to canonical form. Prose about a file is prose: §7's boundary sends it to a sibling document, and a fact file now holds nothing addressed to a reader rather than a parser. Blank lines stay -- sorting them away loses no information
Multi-line string values (heredocs, continuations) | Rejected | Every load-bearing property hangs on one line = one fact: bytewise line sorting for canonical form, stateless line-local lexing, grep hits being complete facts, the single-line edit primitive. Prose crosses the content boundary (§7) as a sibling file/archive member, never as grammar
Defaults by key absence | Rejected | Absent vs asserted-none = investigate vs trust
Lowercase-only keys (v0.1) | Reversed in v0.2 | Projected identifiers are case-sensitive and case is semantic (Go exportedness); an actual extractor violated the rule on first run
Full Go-source conversion (bodies as facts) | Rejected | Reinvents compiler IR at ~8× tokens while discarding model fluency in Go; declarations are facts, bodies are computation
list(ref(func)) for call edges | Deferred (§6.4) | External symbols break resolution; per-generator choice between all-str and stub-facts
Line numbers in the source handoff (loc = "file.go:line") | Reversed | Line numbers are the only contract-independent data the projection carried: any edit above a declaration churned them, polluting the regeneration diff that §11.1 makes the impact report. Replaced by file — the defining file plus the symbol name locates a declaration in one grep, and the projection now changes iff the declaration layer changes (§11.2 churn invariant)
date and datetime as two base types | Rejected (v0.3) | One datetime annotation whose domain spans both precisions is the annotation-as-edit-domain rule at work; two types would force every key author to predict whether precision will ever be needed
Full-date normalizes to midnight (2026-07-20 → 2026-07-20T00:00:00Z) | Rejected (v0.3) | Meaning-changing, unlike float normalization (5.0→5): a due date is a day, not an instant. Both forms are canonical as written
Datetime offsets, local date-times, fractional seconds | Rejected (v0.3) | Offsets and local forms import tzdata and political time — the open semantics that keep datetime out of every minimal format; a Z-less token parses as local time across ecosystems, so the Z must travel with the value; fractions would break bytewise-order = chronological-order
Row formats for record data (TSV with a typed header and a validating CLI; a table of structs in the host language) | Rejected (v0.3, on field evidence) | Cheaper per record and better on record-shaped greps — a hit is a whole row, where a FACT hit is one field of one and a conjunctive query is a join. Rejected on error locality, measured on a 1600-record agent-generated corpus (nyx): the writer of record data is a lossy generator repaired line by line, and per-line self-description is what makes its mistakes detectable where they were made. Seeded one at a time, fact validate caught a duplicate record (E007), an enum typo (E005) and a dangling ref (E008) with no schema; a row format catches none of the three without an external schema, which §4.3 and this ledger already reject, and its header states the contract at a distance from the line being written. Blast radius decides it: one unterminated string in the equivalent Go table masked every other error in the file, and a JSON syntax error does the same, where §9's no-cascading rule reports the bad line and keeps reporting. TSV additionally cannot express asserted absence (the empty cell is both none and forgotten), has no escape standard (a generated tab or newline silently reshapes a row, where str is JSON-quoted), and cannot grow a field without an encoded list inside a column or a variable width. The one check a row format wins is arity — a short row — and that is the class fact validate misses; the schema-free answer is a lint, not a schema (W003, parked in TASKS.t). Rejected as an authoring format, a row form is nonetheless admitted ONE-WAY as a derived view (fact rows, parked with W003): the conjunctive query is the one place a row form is genuinely cheaper, awk and cut are everywhere jq is not, and a rendered view carries no authority — nothing reads it back, so it is a render of the fact set the way pica text is a render of a document, and an importer is prohibited for every reason in this row
Package-qualified keys (one leading pkg: marker; module-relative mangled ids; module = validation set) | Deferred | Would make per-package files concatenatable, enable module-wide validation, and make cross-package refs expressible (incl. config→code refs, and qualified ref values) — at a per-line token tax on every projection. Unneeded by interactive agents: grep's printed file path already qualifies (§1.1), and the compiler plus freshness gate already guarantee cross-package integrity (§6.2). Adopt if/when fine-tune training-corpus generation begins — a format baked into model weights cannot be changed afterwards, and that is the one consumer for whom self-contained module-wide lines, single-artifact module diffs, and a millisecond module-wide validator (hallucination gate) pay for the tax
.end

# Appendix B — Naming

FACT: each line is one fact. Induced vocabulary is exact: assert a fact, retract a fact, duplicate fact, a projection is the set of facts about a codebase. Rejected: anything containing "ML" or "-on" — the format is not a markup language or object notation, and the name must not wear the family costume of what it replaces.

---

Spec v0.3. Open items for v0.4: include/overlay mechanism for config environment variants (likely canonical-set union with explicit override markers, not implicit layering); §6.4 resolution after field experience with stub facts; package-qualified keys remain deferred with an explicit trigger condition (Appendix A) — the per-file validation scope of §6.2 is the resolution until a training-corpus consumer exists; projection vocabularies for further fact sources (SQL schemas, OpenAPI, protobuf — each is a declaration layer awaiting projection); formal test-vector suite.

.width 92
.font sans
