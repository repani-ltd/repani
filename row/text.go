package row

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Labeled is one row of a labelled text (ROW.t 6.2).
type Labeled struct {
	Label string
	Row   Row
}

// Format returns the canonical text of r (ROW.t section 6), checking
// every value first: one line per item, each ended by a newline; the
// empty row is the empty text.
func Format(r Row) (string, error) {
	n, err := normItems(r, nil, 1)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	writeItems(&b, "", n, nil)
	return b.String(), nil
}

// FormatLabeled returns the canonical text of rows, in their order.
func FormatLabeled(rows []Labeled) (string, error) {
	var b strings.Builder
	seen := map[string]bool{}
	for _, l := range rows {
		if seen[l.Label] {
			return "", errAt("T005", nil, "label %q twice", l.Label)
		}
		seen[l.Label] = true
		n, err := normItems(l.Row, nil, 1)
		if err != nil {
			return "", err
		}
		var q strings.Builder
		quote(&q, l.Label)
		b.WriteString(q.String())
		b.WriteString(": row\n")
		q.WriteByte(' ')
		writeItems(&b, q.String(), n, nil)
	}
	return b.String(), nil
}

func writeItems(b *strings.Builder, prefix string, r Row, path []int) {
	p := append(clonePath(path), 0)
	for i, v := range r {
		p[len(p)-1] = i
		b.WriteString(prefix)
		b.WriteString(pathString(p))
		b.WriteString(": ")
		b.WriteString(typeName(v))
		if sub, ok := v.(Row); ok {
			b.WriteByte('\n')
			writeItems(b, prefix, sub, p)
			continue
		}
		if v != nil {
			b.WriteString(" = ")
			writeValue(b, v)
		}
		b.WriteByte('\n')
	}
}

// writeValue writes a normalised value's canonical spelling (ROW.t 6.5).
func writeValue(b *strings.Builder, v any) {
	switch x := v.(type) {
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int64:
		b.WriteString(group(strconv.FormatInt(x, 10)))
	case Decimal:
		b.WriteString(formatDecimal(x))
	case float64:
		b.WriteString(formatFloat(x))
	case Day:
		b.WriteString(formatDate(int64(x)))
	case time.Time:
		ms := x.UnixMilli()
		day, rem := floorDiv(ms, 86400000)
		b.WriteString(formatDate(day))
		b.WriteByte('T')
		b.WriteString(formatTime(rem))
		b.WriteByte('Z')
	case TimeOfDay:
		b.WriteString(formatTime(int64(x)))
	case Duration:
		b.WriteString(formatDuration(int64(x)))
	case string:
		quote(b, x)
	case []byte:
		b.WriteString("0x")
		const hexdigits = "0123456789abcdef"
		for _, c := range x {
			b.WriteByte(hexdigits[c>>4])
			b.WriteByte(hexdigits[c&0x0F])
		}
	}
}

// group puts an underscore between every three digits of a number's
// whole part when it has five digits or more: 12345 is 12_345, 1234
// stays. s is an optional minus and digits.
func group(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	if len(s) < 5 {
		return sign + s
	}
	var b strings.Builder
	b.WriteString(sign)
	first := len(s) % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(s[:first])
	for i := first; i < len(s); i += 3 {
		b.WriteByte('_')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func formatDecimal(x Decimal) string {
	m := x.Mantissa
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	digits := strconv.FormatInt(m, 10)
	if x.Scale == 0 {
		return group(sign + digits)
	}
	for len(digits) <= x.Scale {
		digits = "0" + digits
	}
	cut := len(digits) - x.Scale
	return group(sign+digits[:cut]) + "." + digits[cut:]
}

// formatFloat writes the shortest digits that read back as f: plain
// when 1e-6 <= |f| < 1e21 or f is 0, else with an exponent, its plus
// sign and leading zeros dropped (ROW.t 6.5).
func formatFloat(f float64) string {
	a := math.Abs(f)
	if f == 0 || (a >= 1e-6 && a < 1e21) {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		whole, frac, hasPoint := strings.Cut(s, ".")
		if hasPoint {
			return group(whole) + "." + frac
		}
		return group(whole)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mant, exp, _ := strings.Cut(s, "e")
	sign := ""
	if exp[0] == '-' {
		sign = "-"
	}
	exp = strings.TrimLeft(exp[1:], "0")
	return mant + "e" + sign + exp
}

func formatDate(day int64) string {
	y, m, d := civilFromDays(day)
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

func formatTime(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func formatDuration(ms int64) string {
	if ms == 0 {
		return "0s"
	}
	var b strings.Builder
	if ms < 0 {
		b.WriteByte('-')
		ms = -ms
	}
	h, m, s, r := ms/3600000, ms/60000%60, ms/1000%60, ms%1000
	if h > 0 {
		b.WriteString(strconv.FormatInt(h, 10))
		b.WriteByte('h')
	}
	if m > 0 {
		b.WriteString(strconv.FormatInt(m, 10))
		b.WriteByte('m')
	}
	if s > 0 || r > 0 {
		b.WriteString(strconv.FormatInt(s, 10))
		if r > 0 {
			b.WriteByte('.')
			b.WriteString(strings.TrimRight(fmt.Sprintf("%03d", r), "0"))
		}
		b.WriteByte('s')
	}
	return b.String()
}

func floorDiv(a, b int64) (q, r int64) {
	q, r = a/b, a%b
	if r < 0 {
		q--
		r += b
	}
	return q, r
}

// quote writes s as a canonical string: escaping only the quote, the
// backslash and characters below U+0020.
func quote(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// Parse returns the row of an unlabelled text, its lines in any order
// and its values in any spelling their types read (ROW.t 6.3).
func Parse(text string) (Row, error) {
	lines, err := scan(text)
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		if l.labeled {
			return nil, &Error{Code: "T007", Offset: -1, Line: l.num, Msg: "a labelled line in an unlabelled text (ParseLabeled reads labelled ones)"}
		}
	}
	return assemble(lines, false)
}

// ParseLabeled returns the rows of a labelled text, in the order their
// labels first appear.
func ParseLabeled(text string) ([]Labeled, error) {
	lines, err := scan(text)
	if err != nil {
		return nil, err
	}
	var order []string
	byLabel := map[string][]line{}
	for _, l := range lines {
		if !l.labeled {
			return nil, &Error{Code: "T007", Offset: -1, Line: l.num, Msg: "an unlabelled line in a labelled text"}
		}
		if _, ok := byLabel[l.label]; !ok {
			order = append(order, l.label)
		}
		byLabel[l.label] = append(byLabel[l.label], l)
	}
	out := make([]Labeled, 0, len(order))
	for _, lab := range order {
		r, err := assemble(byLabel[lab], true)
		if err != nil {
			return nil, err
		}
		out = append(out, Labeled{Label: lab, Row: r})
	}
	return out, nil
}

// line is one parsed line: its item, or a row's own line (header).
type line struct {
	num     int
	labeled bool
	label   string
	path    []int
	header  bool
	value   any
}

func scan(text string) ([]line, error) {
	var out []line
	for num, s := range strings.Split(text, "\n") {
		if strings.Trim(s, " \t") == "" {
			continue
		}
		l, err := parseLine(s, num+1)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }
func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func skipSpace(s string, i int) int {
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	return i
}

func lineErr(code string, num int, path []int, format string, a ...any) *Error {
	return &Error{Code: code, Path: clonePath(path), Offset: -1, Line: num, Msg: fmt.Sprintf(format, a...)}
}

func parseLine(s string, num int) (line, error) {
	l := line{num: num}
	if !utf8.ValidString(s) {
		return l, lineErr("V008", num, nil, "the line is not valid UTF-8")
	}
	i := skipSpace(s, 0)
	if s[i] == '"' {
		lab, n, err := unquote(s[i:])
		if err != nil {
			return l, lineErr("T001", num, nil, "a broken label: %v", err)
		}
		if !utf8.ValidString(lab) {
			return l, lineErr("V008", num, nil, "a label not valid UTF-8")
		}
		l.labeled, l.label = true, lab
		i = skipSpace(s, i+n)
	}
	colon := strings.IndexByte(s[i:], ':')
	if colon < 0 {
		return l, lineErr("T001", num, nil, "no colon")
	}
	ptext := strings.TrimRight(s[i:i+colon], " \t")
	var path []int
	if ptext == "" && l.labeled {
		path = []int{} // the labelled row's own line
	} else {
		var err error
		if path, err = parsePath(ptext); err != nil {
			return l, lineErr("T002", num, nil, "%q: %v", ptext, err)
		}
	}
	l.path = path
	i = skipSpace(s, i+colon+1)
	j := i
	for j < len(s) && (isDigit(s[j]) || ('a' <= s[j] && s[j] <= 'z')) {
		j++
	}
	name := s[i:j]
	if name == "" {
		return l, lineErr("T001", num, path, "no type name")
	}
	k := skipSpace(s, j)
	rest := ""
	hasValue := false
	if k < len(s) {
		if s[k] != '=' {
			if !knownType(name) || j == k {
				return l, lineErr("T003", num, path, "unknown type name %q", s[i:strings.IndexAny(s[i:]+" ", " \t=")+i])
			}
			return l, lineErr("T001", num, path, "stray text after the type")
		}
		hasValue = true
		rest = strings.TrimRight(s[skipSpace(s, k+1):], " \t")
	}
	if !knownType(name) {
		return l, lineErr("T003", num, path, "unknown type name %q", name)
	}
	if len(path) == 0 && name != "row" {
		return l, lineErr("T002", num, nil, "a line without a path is its row's own, of type row")
	}
	if name == "none" || name == "row" {
		if hasValue {
			return l, lineErr("T004", num, path, "a value on a %s line", name)
		}
		if name == "row" {
			if len(path) == maxDepth {
				return l, lineErr("R005", num, path, "rows nested deeper than four levels")
			}
			l.header = true
		}
		return l, nil
	}
	if !hasValue || rest == "" {
		return l, lineErr("T004", num, path, "no value for %s", name)
	}
	v, err := parseValue(name, rest)
	if err != nil {
		e := err.(*Error)
		e.Line, e.Path, e.Offset = num, clonePath(path), -1
		return l, e
	}
	l.value = v
	return l, nil
}

func knownType(name string) bool {
	switch name {
	case "none", "bool", "int", "dec", "f64", "date", "utc", "time", "dur", "str", "bin", "row":
		return true
	}
	return false
}

func parsePath(s string) ([]int, error) {
	if s == "" {
		return nil, fmt.Errorf("an empty path")
	}
	parts := strings.Split(s, ".")
	if len(parts) > maxDepth {
		return nil, fmt.Errorf("more than four positions")
	}
	out := make([]int, len(parts))
	for k, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("an empty position")
		}
		for i := 0; i < len(p); i++ {
			if !isDigit(p[i]) {
				return nil, fmt.Errorf("%q is not digits", p)
			}
		}
		if len(p) > 1 && p[0] == '0' {
			return nil, fmt.Errorf("a leading zero in %q", p)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n > maxInt {
			return nil, fmt.Errorf("position %q too large", p)
		}
		out[k] = n
	}
	return out, nil
}

// assemble builds a row from its lines (ROW.t 6.1); a labelled row's
// lines include its own, with no path (6.2).
func assemble(lines []line, labelled bool) (Row, error) {
	byPath := map[string]*line{}
	for k := range lines {
		l := &lines[k]
		key := pathString(l.path)
		if prev, ok := byPath[key]; ok {
			return nil, lineErr("T005", l.num, l.path, "position given twice (also line %d)", prev.num)
		}
		byPath[key] = l
	}
	root, hasRoot := byPath[""]
	if labelled && !hasRoot {
		return nil, lineErr("T006", lines[0].num, nil, "row %q has no line of its own", lines[0].label)
	}
	rootLine := 0
	if hasRoot {
		rootLine = root.num
	}
	// Every item's row has its line, a row line.
	children := map[string][]*line{}
	for k := range lines {
		l := &lines[k]
		if len(l.path) == 0 {
			continue
		}
		parent := pathString(l.path[:len(l.path)-1])
		if len(l.path) > 1 {
			p, ok := byPath[parent]
			if !ok {
				return nil, lineErr("T006", l.num, l.path, "its row %s has no line", parent)
			}
			if !p.header {
				return nil, lineErr("T006", l.num, l.path, "its row %s is not a row", parent)
			}
		}
		children[parent] = append(children[parent], l)
	}
	var build func(key string, path []int, headerLine int) (Row, error)
	build = func(key string, path []int, headerLine int) (Row, error) {
		kids := children[key]
		sort.Slice(kids, func(a, b int) bool { return kids[a].path[len(path)] < kids[b].path[len(path)] })
		r := make(Row, len(kids))
		for i, l := range kids {
			if pos := l.path[len(path)]; pos != i {
				missing := append(clonePath(path), i)
				return nil, lineErr("T005", headerLine, missing, "position %s missing", pathString(missing))
			}
			if l.header {
				sub, err := build(pathString(l.path), l.path, l.num)
				if err != nil {
					return nil, err
				}
				r[i] = sub
			} else {
				r[i] = l.value
			}
		}
		return r, nil
	}
	return build("", nil, rootLine)
}

// parseValue reads a value in any spelling its type reads as the same
// value (ROW.t 6.5). Errors carry only the code and message; the
// caller adds where.
func parseValue(name, s string) (any, error) {
	bad := func(format string, a ...any) error {
		return &Error{Code: "T004", Offset: -1, Msg: fmt.Sprintf(format, a...)}
	}
	outside := func(code, format string, a ...any) error {
		return &Error{Code: code, Offset: -1, Msg: fmt.Sprintf(format, a...)}
	}
	switch name {
	case "bool":
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, bad("%q is not a bool", s)
	case "int":
		sign, digits, ok := readSigned(s)
		if !ok {
			return nil, bad("%q is not an int", s)
		}
		n, ok := new(big.Int).SetString(sign+digits, 10)
		if !ok {
			return nil, bad("%q is not an int", s)
		}
		if !n.IsInt64() || n.Int64() > maxInt || n.Int64() < -maxInt {
			return nil, outside("V001", "int %s outside ±(2^53-1)", s)
		}
		return n.Int64(), nil
	case "dec":
		return parseDecimal(s)
	case "f64":
		return parseFloat(s)
	case "date":
		day, ok, inRange := parseDate(s)
		if !ok {
			return nil, bad("%q is not a date", s)
		}
		if !inRange {
			return nil, outside("V004", "date %s outside 0001-01-01 to 9999-12-31", s)
		}
		return Day(day), nil
	case "utc":
		return parseDatetime(s)
	case "time":
		return parseTimeOfDay(s)
	case "dur":
		return parseDuration(s)
	case "str":
		v, n, err := unquote(s)
		if err != nil {
			return nil, bad("%v", err)
		}
		if n != len(s) {
			return nil, bad("text after the string")
		}
		if !utf8.ValidString(v) {
			return nil, outside("V008", "string not valid UTF-8")
		}
		return v, nil
	case "bin":
		if len(s) < 2 || s[0] != '0' || (s[1] != 'x' && s[1] != 'X') {
			return nil, bad("%q does not start 0x", s)
		}
		h := s[2:]
		if len(h)%2 != 0 {
			return nil, bad("%q has an odd number of hex digits", s)
		}
		out := make([]byte, len(h)/2)
		for i := range out {
			hi, ok1 := hexVal(h[2*i])
			lo, ok2 := hexVal(h[2*i+1])
			if !ok1 || !ok2 {
				return nil, bad("%q is not hex", s)
			}
			out[i] = hi<<4 | lo
		}
		return out, nil
	}
	return nil, bad("unknown type %q", name)
}

func hexVal(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// digitRun reads digits with single underscores between two digits,
// from s[i]; it returns them without the underscores and the index
// after the run. ok is false for an underscore not between digits.
func digitRun(s string, i int) (digits string, j int, ok bool) {
	var b strings.Builder
	j = i
	for j < len(s) {
		c := s[j]
		if isDigit(c) {
			b.WriteByte(c)
			j++
			continue
		}
		if c == '_' {
			if j == i || j+1 >= len(s) || !isDigit(s[j-1]) || !isDigit(s[j+1]) {
				return "", j, false
			}
			j++
			continue
		}
		break
	}
	return b.String(), j, true
}

// readSigned reads an optional sign and a digit run that is all of s.
func readSigned(s string) (sign, digits string, ok bool) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		if s[i] == '-' {
			sign = "-"
		}
		i++
	}
	digits, j, ok := digitRun(s, i)
	if !ok || digits == "" || j != len(s) {
		return "", "", false
	}
	return sign, digits, true
}

func parseDecimal(s string) (any, error) {
	bad := &Error{Code: "T004", Offset: -1, Msg: fmt.Sprintf("%q is not a decimal", s)}
	i := 0
	sign := ""
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		if s[i] == '-' {
			sign = "-"
		}
		i++
	}
	whole, j, ok := digitRun(s, i)
	if !ok {
		return nil, bad
	}
	frac := ""
	if j < len(s) && s[j] == '.' {
		var k int
		frac, k, ok = digitRun(s, j+1)
		if !ok {
			return nil, bad
		}
		j = k
	}
	if j != len(s) || whole+frac == "" {
		return nil, bad
	}
	if len(frac) > maxScale {
		return nil, &Error{Code: "V002", Offset: -1, Msg: fmt.Sprintf("decimal %s: scale %d above 15", s, len(frac))}
	}
	n, _ := new(big.Int).SetString(sign+whole+frac, 10)
	if !n.IsInt64() || n.Int64() > maxInt || n.Int64() < -maxInt {
		return nil, &Error{Code: "V002", Offset: -1, Msg: fmt.Sprintf("decimal %s: mantissa outside ±(2^53-1)", s)}
	}
	return Decimal{Mantissa: n.Int64(), Scale: len(frac)}, nil
}

func parseFloat(s string) (any, error) {
	bad := &Error{Code: "T004", Offset: -1, Msg: fmt.Sprintf("%q is not a float", s)}
	var b strings.Builder
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		b.WriteByte(s[i])
		i++
	}
	whole, j, ok := digitRun(s, i)
	if !ok {
		return nil, bad
	}
	b.WriteString(whole)
	frac := ""
	if j < len(s) && s[j] == '.' {
		frac, j, ok = digitRun(s, j+1)
		if !ok {
			return nil, bad
		}
		b.WriteByte('.')
		b.WriteString(frac)
	}
	if whole+frac == "" {
		return nil, bad
	}
	if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
		b.WriteByte('e')
		j++
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			b.WriteByte(s[j])
			j++
		}
		exp, k, ok := digitRun(s, j)
		if !ok || exp == "" {
			return nil, bad
		}
		b.WriteString(exp)
		j = k
	}
	if j != len(s) {
		return nil, bad
	}
	f, err := strconv.ParseFloat(b.String(), 64)
	if err != nil && !math.IsInf(f, 0) && f != 0 {
		return nil, bad
	}
	if math.IsInf(f, 0) || (f == 0 && math.Signbit(f)) {
		return nil, &Error{Code: "V003", Offset: -1, Msg: fmt.Sprintf("float %s is infinite or -0", s)}
	}
	return f, nil
}

// fixed reads exactly n digits at s[i:].
func fixed(s string, i, n int) (int64, bool) {
	if i+n > len(s) {
		return 0, false
	}
	var v int64
	for k := i; k < i+n; k++ {
		if !isDigit(s[k]) {
			return 0, false
		}
		v = v*10 + int64(s[k]-'0')
	}
	return v, true
}

// parseDate reads YYYY-MM-DD: ok if it is a date, inRange if within
// 0001 to 9999 (a four-digit year is, but for 0000).
func parseDate(s string) (day int64, ok, inRange bool) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return 0, false, false
	}
	y, ok1 := fixed(s, 0, 4)
	m, ok2 := fixed(s, 5, 2)
	d, ok3 := fixed(s, 8, 2)
	if !ok1 || !ok2 || !ok3 || m < 1 || m > 12 || d < 1 || d > daysIn(max(y, 1), m) {
		return 0, false, false
	}
	if y < 1 {
		return 0, true, false
	}
	return daysFromCivil(y, m, d), true, true
}

// clock reads HH:MM[:SS[.fff...]] at the start of s: the milliseconds,
// whether a fraction finer than a millisecond was nonzero, the index
// after it, and ok. seconds says whether seconds are required.
func clock(s string, seconds bool) (ms int64, fine bool, j int, ok bool) {
	h, ok1 := fixed(s, 0, 2)
	if !ok1 || len(s) < 5 || s[2] != ':' {
		return 0, false, 0, false
	}
	mi, ok2 := fixed(s, 3, 2)
	if !ok2 || h > 23 || mi > 59 {
		return 0, false, 0, false
	}
	ms = (h*60 + mi) * 60000
	j = 5
	if j < len(s) && s[j] == ':' {
		sec, ok3 := fixed(s, 6, 2)
		if !ok3 || sec > 59 {
			if ok3 && sec == 60 {
				return 0, false, 8, false
			}
			return 0, false, 0, false
		}
		ms += sec * 1000
		j = 8
		if j < len(s) && s[j] == '.' {
			k := j + 1
			for k < len(s) && isDigit(s[k]) {
				k++
			}
			f := s[j+1 : k]
			if f == "" {
				return 0, false, 0, false
			}
			for len(f) < 3 {
				f += "0"
			}
			v, _ := strconv.ParseInt(f[:3], 10, 64)
			ms += v
			fine = strings.Trim(f[3:], "0") != ""
			j = k
		}
	} else if seconds {
		return 0, false, 0, false
	}
	return ms, fine, j, true
}

func parseTimeOfDay(s string) (any, error) {
	ms, fine, j, ok := clock(s, false)
	if !ok || j != len(s) {
		return nil, &Error{Code: "T004", Offset: -1, Msg: fmt.Sprintf("%q is not a time", s)}
	}
	if fine {
		return nil, &Error{Code: "V006", Offset: -1, Msg: fmt.Sprintf("time %s finer than a millisecond", s)}
	}
	return TimeOfDay(ms), nil
}

func parseDatetime(s string) (any, error) {
	bad := &Error{Code: "T004", Offset: -1, Msg: fmt.Sprintf("%q is not a datetime", s)}
	if len(s) < 11 || (s[10] != 'T' && s[10] != 't' && s[10] != ' ') {
		return nil, bad
	}
	day, ok, inRange := parseDate(s[:10])
	if !ok {
		return nil, bad
	}
	rest := s[11:]
	ms, fine, j, ok := clock(rest, true)
	if !ok {
		if j == 8 {
			return nil, &Error{Code: "V005", Offset: -1, Msg: fmt.Sprintf("datetime %s: a leap second", s)}
		}
		return nil, bad
	}
	zone := rest[j:]
	var offset int64
	switch {
	case zone == "Z" || zone == "z":
	case len(zone) == 6 && (zone[0] == '+' || zone[0] == '-') && zone[3] == ':':
		oh, ok1 := fixed(zone, 1, 2)
		om, ok2 := fixed(zone, 4, 2)
		if !ok1 || !ok2 || oh > 23 || om > 59 {
			return nil, bad
		}
		offset = (oh*60 + om) * 60000
		if zone[0] == '-' {
			offset = -offset
		}
	default:
		return nil, bad
	}
	if fine {
		return nil, &Error{Code: "V005", Offset: -1, Msg: fmt.Sprintf("datetime %s finer than a millisecond", s)}
	}
	if !inRange && day == 0 {
		return nil, &Error{Code: "V005", Offset: -1, Msg: fmt.Sprintf("datetime %s outside 0001 to 9999", s)}
	}
	t := day*86400000 + ms - offset
	if t < minDatetime || t > maxDatetime {
		return nil, &Error{Code: "V005", Offset: -1, Msg: fmt.Sprintf("datetime %s outside 0001 to 9999", s)}
	}
	return time.UnixMilli(t).UTC(), nil
}

// parseDuration reads parts h, m, s and ms, in that order, each once,
// each a number that may have a fraction, the total whole milliseconds.
func parseDuration(s string) (any, error) {
	bad := &Error{Code: "T004", Offset: -1, Msg: fmt.Sprintf("%q is not a duration", s)}
	i := 0
	neg := false
	if i < len(s) && s[i] == '-' {
		neg = true
		i++
	}
	units := []struct {
		name string
		ms   int64
	}{{"h", 3600000}, {"m", 60000}, {"s", 1000}, {"ms", 1}}
	total := new(big.Rat)
	next := 0
	parts := 0
	for i < len(s) {
		whole, j, ok := digitRun(s, i)
		if !ok {
			return nil, bad
		}
		frac := ""
		if j < len(s) && s[j] == '.' {
			frac, j, ok = digitRun(s, j+1)
			if !ok {
				return nil, bad
			}
		}
		if whole+frac == "" {
			return nil, bad
		}
		k := j
		for k < len(s) && 'a' <= s[k] && s[k] <= 'z' {
			k++
		}
		unit := s[j:k]
		u := -1
		for n := next; n < len(units); n++ {
			if units[n].name == unit {
				u = n
				break
			}
		}
		if u < 0 {
			return nil, bad
		}
		next = u + 1
		num, ok := new(big.Rat).SetString(whole + "." + frac + "0")
		if !ok {
			return nil, bad
		}
		total.Add(total, num.Mul(num, new(big.Rat).SetInt64(units[u].ms)))
		parts++
		i = k
	}
	if parts == 0 {
		return nil, bad
	}
	if !total.IsInt() {
		return nil, &Error{Code: "V007", Offset: -1, Msg: fmt.Sprintf("duration %s finer than a millisecond", s)}
	}
	n := total.Num()
	if neg {
		n.Neg(n)
	}
	if !n.IsInt64() || n.Int64() > maxInt || n.Int64() < -maxInt {
		return nil, &Error{Code: "V007", Offset: -1, Msg: fmt.Sprintf("duration %s outside ±(2^53-1) ms", s)}
	}
	return Duration(n.Int64()), nil
}

// unquote reads a JSON string at the start of s and returns it and the
// bytes it took.
func unquote(s string) (string, int, error) {
	if s == "" || s[0] != '"' {
		return "", 0, fmt.Errorf("no opening quote")
	}
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"':
			return b.String(), i + 1, nil
		case c < 0x20:
			return "", 0, fmt.Errorf("a raw control character in a string")
		case c != '\\':
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(s) {
			return "", 0, fmt.Errorf("a string ends in a backslash")
		}
		e := s[i+1]
		i += 2
		switch e {
		case '"', '\\', '/':
			b.WriteByte(e)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			r, ok := hex4(s, i)
			if !ok {
				return "", 0, fmt.Errorf("a broken \\u escape")
			}
			i += 4
			if utf16.IsSurrogate(r) {
				if r < 0xDC00 && i+6 <= len(s) && s[i] == '\\' && s[i+1] == 'u' {
					if r2, ok := hex4(s, i+2); ok && r2 >= 0xDC00 && r2 <= 0xDFFF {
						b.WriteRune(utf16.DecodeRune(r, r2))
						i += 6
						continue
					}
				}
				// A lone surrogate is no character: invalid UTF-8.
				b.WriteString("\xed\xa0\x80")
				continue
			}
			b.WriteRune(r)
		default:
			return "", 0, fmt.Errorf("an unknown escape \\%c", e)
		}
	}
	return "", 0, fmt.Errorf("no closing quote")
}

func hex4(s string, i int) (rune, bool) {
	if i+4 > len(s) {
		return 0, false
	}
	var r rune
	for k := i; k < i+4; k++ {
		v, ok := hexVal(s[k])
		if !ok {
			return 0, false
		}
		r = r<<4 | rune(v)
	}
	return r, true
}
