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
	                         blank lines dropped

There are no comments: a line starting with # is E001 (removed in
v0.4, because canonical form dropped them and fmt -w therefore
deleted them). What a file says about itself -- what consumes it,
how to run it, why a key holds the value it does -- goes in a
sibling document, like any other prose.

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

Choosing where a dimension goes. One marker per key means a squad's
players are nested by prefix (liverpool.player:dalglish), folded into
the id (player:t26_01), or related by a value (player:dalglish.team:
ref(team) = team:liverpool). All three take one grep, so grep does not
decide it; what changes does. The frozen dimension goes in the prefix
or the id -- an instance keeps one prefix, so a nested player cannot
change clubs without rewriting every line of the instance, and a folded
id is a string that is neither checked nor renameable. The mutable
dimension goes in a ref value: a transfer is one line, a misspelled
club is E008 rather than a silently unaffiliated player, and the grep
is exact ('= team:liverpool') where a bare str would collide with any
field holding the same word. State a membership once, on one side
only -- both directions are one grep, since a ref value is the same
token as the marker, and no validator can see two copies disagree.
Order decides the side: a set that changes lives on the member, an
ordered membership lives on the container as list(ref(member)).

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

	E001 line is neither a fact nor blank (a # line, or CR endings)
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

# Data files

The use: configuration and data that a program reads and a person
writes -- a station's settings, a squad, an event log, a ledger.
One fact per line makes a file greppable and diffable line by
line, references fail loudly at load when they name nothing, and
canonical form makes two files comparable byte for byte. Validate
before shipping: fact validate FILE reports every error with its
line; a program loads through Load (Parse + Validate) and binds
with Unmarshal, so a misspelled reference is an error at load,
never an empty value.

What does NOT belong in a fact file. Every property above is
per-line -- the type as the domain of legal edits, the error on
the line that caused it, the one-line edit, the prefix grep --
so a file whose lines are never read, grepped or edited one at
a time pays for all of it and collects nothing. Bulk
observations that only move as a block are an array, not a
fact set: keep them in a bulk format as a sibling file and
reference it by name, the same handoff prose and blobs take. A
generator about to emit a million facts should ask first
whether anything will ever read one of its lines alone.
*/
package fact
