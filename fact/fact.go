// Package fact implements the FACT configuration format (see SPEC.t).
//
// A FACT file is an unordered set of single-line, self-contained, typed
// facts. Parsing is strictly line-local; Validate adds the set-level
// checks (duplicates, marker consistency, reference resolution).
package fact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// BaseKind enumerates the seven base types of SPEC §4.1.
type BaseKind int

const (
	Bool BaseKind = iota
	Int
	Float
	Str
	Datetime
	Enum
	Ref
)

// Type is one of the twenty-one legal type shapes of SPEC §4.2.
type Type struct {
	Raw      string // exact (canonical) type expression, e.g. "list(ref(step))"
	Base     BaseKind
	Optional bool     // T?
	List     bool     // list(T)
	Symbols  []string // Enum: legal symbols, in declared order
	RefKind  string   // Ref: the kind parameter
}

// Value holds a checked value as canonical scalar tokens.
type Value struct {
	None  bool
	List  bool
	Elems []string // canonical tokens; scalar values use Elems[0]
}

func (v Value) token() string {
	if v.None {
		return "none"
	}
	if v.List {
		return "[" + strings.Join(v.Elems, ", ") + "]"
	}
	return v.Elems[0]
}

// Fact is one parsed fact line.
type Fact struct {
	Key   string
	Type  Type
	Value Value
	Line  int // 1-based source line (or entry number for JSON input)
}

// String renders the fact in canonical single-line form (SPEC §2.2, §8).
func (f Fact) String() string {
	return f.Key + ": " + f.Type.Raw + " = " + f.Value.token()
}

// Error is a validation error with its normative code (SPEC §14).
type Error struct {
	Line int
	Code string
	Msg  string
}

func (e Error) Error() string {
	return fmt.Sprintf("line %d: %s: %s", e.Line, e.Code, e.Msg)
}

// Parse runs the line-local steps (SPEC §9 steps 1-4) over src.
// Lines that fail are reported and skipped; other lines are unaffected.
func Parse(src []byte) ([]Fact, []Error) {
	var facts []Fact
	var errs []Error
	lines := strings.Split(string(src), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, line := range lines {
		n := i + 1
		if strings.ContainsRune(line, '\r') {
			errs = append(errs, Error{n, "E001", "cannot lex line: carriage return (FACT uses LF line endings)"})
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		f, err := parseFact(line, n)
		if err != nil {
			errs = append(errs, *err)
			continue
		}
		facts = append(facts, f)
	}
	return facts, errs
}

// parseFact splits "key: type = value" and checks each part.
// The first '=' ends the type (types and keys never contain '='); the last
// ':' before it ends the key (types never contain ':').
func parseFact(line string, n int) (Fact, *Error) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return Fact{}, &Error{n, "E001", `cannot lex line: no "="`}
	}
	left, rawVal := line[:eq], strings.TrimSpace(line[eq+1:])
	colon := strings.LastIndexByte(left, ':')
	if colon < 0 {
		return Fact{}, &Error{n, "E001", `cannot lex line: no ":" before "="`}
	}
	key := strings.TrimSpace(left[:colon])
	if err := checkKey(key, n); err != nil {
		return Fact{}, err
	}
	t, terr := parseType(strings.TrimSpace(left[colon+1:]), n)
	if terr != nil {
		return Fact{}, terr
	}
	v, verr := checkValue(t, rawVal, n)
	if verr != nil {
		return Fact{}, verr
	}
	return Fact{Key: key, Type: t, Value: v, Line: n}, nil
}

// IsSegment reports whether s is a legal key segment / enum symbol:
// [a-zA-Z][a-zA-Z0-9_]*, case-sensitive (SPEC §3, v0.2).
func IsSegment(s string) bool {
	if s == "" || !isLetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !isLetter(c) && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func checkKey(key string, n int) *Error {
	var markers []string
	for _, seg := range strings.Split(key, ".") {
		kind, id, isMarker := strings.Cut(seg, ":")
		if !isMarker {
			if !IsSegment(seg) {
				return &Error{n, "E002", fmt.Sprintf("segment %q violates [a-zA-Z][a-zA-Z0-9_]*", seg)}
			}
			continue
		}
		if !IsSegment(kind) || !IsSegment(id) {
			return &Error{n, "E002", fmt.Sprintf("marker %q is not kind:id with valid segments", seg)}
		}
		markers = append(markers, seg)
	}
	if len(markers) > 1 {
		return &Error{n, "E003", fmt.Sprintf("key contains two markers (%q and %q)", markers[0], markers[1])}
	}
	return nil
}

// keyMarker returns the instance marker ("kind:id") in key and the key
// prefix up to and including it, or "" for singleton keys.
func keyMarker(key string) (marker, prefix string) {
	segs := strings.Split(key, ".")
	for i, seg := range segs {
		if strings.Contains(seg, ":") {
			return seg, strings.Join(segs[:i+1], ".")
		}
	}
	return "", ""
}

// parseType recognizes the twenty-one legal shapes and rejects everything else.
func parseType(s string, n int) (Type, *Error) {
	bad := func() *Error {
		return &Error{n, "E004", fmt.Sprintf("%q is not one of the twenty-one legal type shapes (wrappers do not compose)", s)}
	}
	t := Type{Raw: s}
	inner := s
	switch {
	case strings.HasSuffix(inner, "?"):
		t.Optional = true
		inner = inner[:len(inner)-1]
	case strings.HasPrefix(inner, "list(") && strings.HasSuffix(inner, ")"):
		t.List = true
		inner = inner[len("list(") : len(inner)-1]
	}
	switch {
	case inner == "bool":
		t.Base = Bool
	case inner == "int":
		t.Base = Int
	case inner == "float":
		t.Base = Float
	case inner == "str":
		t.Base = Str
	case inner == "datetime":
		t.Base = Datetime
	case strings.HasPrefix(inner, "enum(") && strings.HasSuffix(inner, ")"):
		t.Base = Enum
		t.Symbols = strings.Split(inner[len("enum("):len(inner)-1], "|")
		for _, sym := range t.Symbols {
			if !IsSegment(sym) {
				return Type{}, bad()
			}
		}
	case strings.HasPrefix(inner, "ref(") && strings.HasSuffix(inner, ")"):
		t.Base = Ref
		t.RefKind = inner[len("ref(") : len(inner)-1]
		if !IsSegment(t.RefKind) {
			return Type{}, bad()
		}
	default:
		return Type{}, bad()
	}
	return t, nil
}

// checkValue verifies that raw inhabits t's domain and canonicalizes it.
func checkValue(t Type, raw string, n int) (Value, *Error) {
	if raw == "none" {
		if !t.Optional {
			return Value{}, &Error{n, "E006", `none requires optional type (add "?")`}
		}
		return Value{None: true}, nil
	}
	if t.List {
		if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
			return Value{}, &Error{n, "E005", fmt.Sprintf("%q is not a list value of %s", raw, t.Raw)}
		}
		elems, ok := splitList(raw[1 : len(raw)-1])
		if !ok {
			return Value{}, &Error{n, "E005", fmt.Sprintf("malformed list %q (unterminated string, empty element, or trailing comma)", raw)}
		}
		out := make([]string, len(elems))
		for i, e := range elems {
			c, err := checkScalar(t, e, n)
			if err != nil {
				return Value{}, err
			}
			out[i] = c
		}
		return Value{List: true, Elems: out}, nil
	}
	c, err := checkScalar(t, raw, n)
	if err != nil {
		return Value{}, err
	}
	return Value{Elems: []string{c}}, nil
}

// splitList splits the bracket-stripped body of a list value on top-level
// commas (commas inside string elements do not split).
func splitList(body string) ([]string, bool) {
	if strings.TrimSpace(body) == "" {
		return nil, true
	}
	var elems []string
	var cur strings.Builder
	inStr, esc := false, false
	for _, r := range body {
		switch {
		case inStr:
			cur.WriteRune(r)
			if esc {
				esc = false
			} else if r == '\\' {
				esc = true
			} else if r == '"' {
				inStr = false
			}
		case r == '"':
			inStr = true
			cur.WriteRune(r)
		case r == ',':
			elems = append(elems, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if inStr {
		return nil, false
	}
	elems = append(elems, strings.TrimSpace(cur.String()))
	for _, e := range elems {
		if e == "" {
			return nil, false
		}
	}
	return elems, true
}

// checkScalar validates one scalar token against t's base type and returns
// its canonical spelling.
func checkScalar(t Type, tok string, n int) (string, *Error) {
	bad := func() *Error {
		return &Error{n, "E005", fmt.Sprintf("%q is not a value of %s", tok, t.Raw)}
	}
	switch t.Base {
	case Bool:
		if tok == "true" || tok == "false" {
			return tok, nil
		}
	case Int:
		if !isJSONInt(tok) {
			return "", bad()
		}
		if _, err := strconv.ParseInt(tok, 10, 64); err != nil {
			return "", &Error{n, "E005", fmt.Sprintf("%q overflows 64-bit signed integer", tok)}
		}
		return tok, nil
	case Float:
		if !isJSONNumber(tok) {
			return "", bad()
		}
		v, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			return "", &Error{n, "E005", fmt.Sprintf("%q overflows IEEE 754 double", tok)}
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case Str:
		if c, ok := canonString(tok); ok {
			return c, nil
		}
	case Datetime:
		if isDatetime(tok) {
			return tok, nil
		}
		if datetimeZoneDefect(tok) {
			return "", &Error{n, "E005", fmt.Sprintf("%q — datetime is UTC only (write Z)", tok)}
		}
	case Enum:
		if slices.Contains(t.Symbols, tok) {
			return tok, nil
		}
		return "", &Error{n, "E005", fmt.Sprintf("%q is not in %s", tok, t.Raw)}
	case Ref:
		kind, id, isMarker := strings.Cut(tok, ":")
		if !isMarker || !IsSegment(kind) || !IsSegment(id) {
			return "", bad()
		}
		if kind != t.RefKind {
			return "", &Error{n, "E009", fmt.Sprintf("%q is a %s, expected ref(%s)", tok, kind, t.RefKind)}
		}
		return tok, nil
	}
	return "", bad()
}

// isDatetime reports whether tok is a legal datetime value (SPEC §4.1):
// "YYYY-MM-DD" or "YYYY-MM-DDTHH:MM:SSZ", calendar-valid, years 0001-9999,
// no offsets, no fractional seconds. Both forms are canonical as written.
func isDatetime(tok string) bool {
	if len(tok) == 10 {
		return validFullDate(tok)
	}
	return len(tok) == 20 && validFullDate(tok[:10]) && tok[10] == 'T' &&
		validClock(tok[11:19]) && tok[19] == 'Z'
}

// datetimeZoneDefect reports whether tok is a well-formed date-time whose
// only defect is the zone suffix (missing Z, lowercase z, or a numeric
// offset) — the mistake worth a specific message.
func datetimeZoneDefect(tok string) bool {
	if len(tok) < 19 || !validFullDate(tok[:10]) || tok[10] != 'T' || !validClock(tok[11:19]) {
		return false
	}
	rest := tok[19:]
	return rest == "" || rest == "z" || rest[0] == '+' || rest[0] == '-'
}

// validFullDate checks "YYYY-MM-DD": digits in place, year 0001-9999,
// Gregorian month lengths, leap years honored.
func validFullDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	y, ok1 := atoiFixed(s[0:4])
	m, ok2 := atoiFixed(s[5:7])
	d, ok3 := atoiFixed(s[8:10])
	if !ok1 || !ok2 || !ok3 || y == 0 || m < 1 || m > 12 || d < 1 {
		return false
	}
	days := [13]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[m]
	if m == 2 && y%4 == 0 && (y%100 != 0 || y%400 == 0) {
		days = 29
	}
	return d <= days
}

// validClock checks "HH:MM:SS": hours 00-23, minutes and seconds 00-59
// (no leap second 60).
func validClock(s string) bool {
	if len(s) != 8 || s[2] != ':' || s[5] != ':' {
		return false
	}
	h, ok1 := atoiFixed(s[0:2])
	mi, ok2 := atoiFixed(s[3:5])
	se, ok3 := atoiFixed(s[6:8])
	return ok1 && ok2 && ok3 && h <= 23 && mi <= 59 && se <= 59
}

// atoiFixed parses an all-digit string (leading zeros expected).
func atoiFixed(s string) (int, bool) {
	v := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

// isJSONInt reports whether s is JSON integer syntax: -?(0|[1-9][0-9]*).
func isJSONInt(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	if s[0] == '0' {
		return len(s) == 1
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isJSONNumber reports whether s is JSON number syntax:
// -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][-+]?[0-9]+)?
func isJSONNumber(s string) bool {
	i := 0
	digits := func() bool { // consume [0-9]+
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i > start
	}
	if i < len(s) && s[i] == '-' {
		i++
	}
	if i >= len(s) {
		return false
	}
	if s[i] == '0' {
		i++
	} else if !digits() {
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		if !digits() {
			return false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '-' || s[i] == '+') {
			i++
		}
		if !digits() {
			return false
		}
	}
	return i == len(s)
}

// canonString checks tok is a JSON string literal and re-encodes it
// canonically (deterministic escaping, HTML escaping off).
func canonString(tok string) (string, bool) {
	var s string
	if err := json.Unmarshal([]byte(tok), &s); err != nil || !strings.HasPrefix(tok, `"`) {
		return "", false
	}
	return Quote(s), true
}

// Quote renders s as a canonical FACT str value token (a JSON string
// literal, deterministic escaping, HTML escaping off). For programs
// that generate fact lines.
func Quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil { // strings never fail to encode
		panic("fact.Quote: " + err.Error())
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// Validate runs the set-level checks (SPEC §9 steps 5-6): duplicates (E007),
// marker-position consistency (E010), and reference resolution (E008).
func Validate(facts []Fact) []Error {
	var errs []Error
	firstLine := make(map[string]int)   // key -> first line
	prefixes := make(map[string]string) // marker -> key prefix through marker
	markers := make(map[string]bool)    // set of existing instance markers
	for _, f := range facts {
		if first, dup := firstLine[f.Key]; dup {
			errs = append(errs, Error{f.Line, "E007", fmt.Sprintf("duplicate of key %q (first at line %d)", f.Key, first)})
		} else {
			firstLine[f.Key] = f.Line
		}
		marker, prefix := keyMarker(f.Key)
		if marker == "" {
			continue
		}
		markers[marker] = true
		if prev, seen := prefixes[marker]; seen && prev != prefix {
			errs = append(errs, Error{f.Line, "E010", fmt.Sprintf("instance %q: inconsistent marker position (%q vs %q)", marker, prev, prefix)})
		} else if !seen {
			prefixes[marker] = prefix
		}
	}
	for _, f := range facts {
		if f.Type.Base != Ref || f.Value.None {
			continue
		}
		for _, tok := range f.Value.Elems {
			if !markers[tok] {
				errs = append(errs, Error{f.Line, "E008", fmt.Sprintf("ref(%s) %q — no such instance", f.Type.RefKind, tok)})
			}
		}
	}
	sort.Slice(errs, func(i, j int) bool { return errs[i].Line < errs[j].Line })
	return errs
}

// Canonical serializes facts in canonical form (SPEC §8): lines sorted
// bytewise, canonical spacing, LF, one trailing newline.
func Canonical(facts []Fact) []byte {
	if len(facts) == 0 {
		return nil
	}
	lines := make([]string, len(facts))
	for i, f := range facts {
		lines[i] = f.String()
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n") + "\n")
}
