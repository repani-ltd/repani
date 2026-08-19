/*
Package fact implements FACT: a line-oriented format for facts
about systems, designed for AI agents. A FACT file is an unordered
set of lines; each line is one complete, self-contained, typed
fact. No nesting, no significant whitespace, no inter-line
dependence, no external schema: the file is the schema. Parsing is
line-local; Validate adds the set-level checks (duplicates, marker
consistency, reference resolution). This comment is the authoring
reference (printed by fact spec); the normative standard with
rationale, findings and grammar is SPEC.t in this directory.

# The line

	key: type = value        one fact
	# comment                dropped; blank lines dropped

Example:

	server.port: int = 8080
	route:transfer.method: enum(get|post) = post

# Keys

A key is a dot-separated path of segments. Segments are
[a-zA-Z0-9_], start with a letter, case-sensitive. Hand-authored
keys SHOULD be lowercase snake_case; projections keep source
casing verbatim. Dots are namespacing, not structure: server.tls.enabled
does not imply an object server.tls.

At most ONE segment may be an instance marker, kind:id (both valid
segments): the subtree rooted there is an instance (record) of
kind. Zero markers = the singleton namespace. A given instance
keeps one key prefix across all its facts. Nested records do not
exist; compound ids or refs express nested identity
(method:Service_Settle, never two markers).

# Types

Seven base types:

	bool        true, false
	int         64-bit signed integer
	float       IEEE 754 double (needs "." or exponent)
	str         double-quoted, JSON escaping exactly (\", \\, \n, \t, \uXXXX)
	datetime    2026-07-20 or 2026-07-20T09:30:00Z -- strict RFC 3339
	            subset, UTC only, uppercase T and Z, no offsets; the
	            two precisions are distinct values, both canonical
	enum(a|b|c) one of the listed bare symbols (segment rules;
	            "none" is reserved and cannot be a symbol)
	ref(kind)   the marker of an instance of kind, written kind:id

Two wrappers, non-composing: "T?" (optional: value may be none) and
"list(T)" (ordered, "[v1, v2]"; empty list is []). T must be a base
type; list(T)? and list(list(T)) are illegal. Twenty-one legal type
shapes in total. The annotation states what a value may legally
become, never inferred from the current value.

# References

A ref(kind) value must name an instance that exists in the same
file, and its kind must match. Validation scope is one file: refs
do not cross files.

# Canonical form

One spelling per value (0 not -0, no float exponent games), one
space around ":" and "=", facts sorted by key with instances
grouped. fact fmt prints it; fact fmt -w rewrites in place.
Canonical files make diffs the delta of meaning.

# Validation errors

	E001 line is not a fact/comment/blank (or CR line ending)
	E002 invalid key segment
	E003 more than one instance marker in a key
	E004 illegal type expression
	E005 value outside the type's domain
	E006 none on a non-optional type (lists have no none; use [])
	E007 duplicate key
	E008 unresolved reference
	E009 reference kind mismatch
	E010 inconsistent marker prefix for an instance
	W001 (lint) enum drift across same-suffix keys of one kind
	W002 (lint) valid but not canonical

Messages carry line numbers. fact validate prints them, one per
line; exit 0 means valid.

# JSON encoding

fact encode emits the canonical JSON interchange form (objects by
key path; datetime as string; type information carried where JSON
cannot express it); fact decode converts that JSON back to
canonical FACT. encode(decode(x)) and decode(encode(x)) are
identities on canonical inputs.

# Projections: pkg.fact

The primary use. fact project reads a Go package and emits its
declaration layer as FACT: types, fields, method sets, computed
interface satisfactions, signatures, resolved call edges, defining
files. Repos carry one pkg.fact per package; agents answer
navigation questions by grep instead of loading source:

	grep '^type:Doc\.' pkg.fact                       everything about a type
	grep -r --include=pkg.fact 'implements.*type:Doc' .   what implements it
	grep -r --include=pkg.fact 'calls.*DefaultLayout' .   who calls it
	grep '^func:DefaultLayout\.sig' pkg.fact              one signature

pkg.fact is generated and read-only: edit the Go source, never the
projection. fact project -check verifies freshness (the CI gate);
fact hook regenerates after edits under Claude Code.
*/
package fact
