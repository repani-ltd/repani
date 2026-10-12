package row

import (
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Format returns a row's text notation (ROW.t section 5): its items
// in their one spelling, separated by single spaces. Items are the Go
// values Encode takes, and break the same rules.
func Format(items []any) (string, error) {
	vals := make([]val, len(items))
	for i, it := range items {
		var err error
		if vals[i], err = fromGo(it, false, 0); err != nil {
			return "", err
		}
	}
	return formatVals(vals), nil
}

func formatVals(vals []val) string {
	var sb strings.Builder
	for i, v := range vals {
		if i > 0 {
			sb.WriteByte(' ')
		}
		v.format(&sb)
	}
	return sb.String()
}

func (v val) format(sb *strings.Builder) {
	switch v.k {
	case kString:
		quote(sb, v.s)
	case kNone:
		sb.WriteString("none")
	case kFalse:
		sb.WriteString("false")
	case kTrue:
		sb.WriteString("true")
	case kInt:
		sb.WriteString(strconv.FormatInt(v.n, 10))
	case kDecimal:
		sb.WriteString(formatDecimal(v.n, v.scale))
	case kFloat:
		sb.WriteString(formatFloat(v.f))
	case kDate:
		sb.WriteString(formatDate(v.n))
	case kDatetime:
		day, ms := floorDiv(v.n, 86400000)
		sb.WriteString(formatDate(day) + "T" + formatTime(ms) + "Z")
	case kTime:
		sb.WriteString(formatTime(v.n))
	case kDuration:
		sb.WriteString(formatDuration(v.n))
	case kBytes:
		sb.WriteString("0x" + hex.EncodeToString(v.b))
	case kList:
		sb.WriteByte('[')
		for i, it := range v.list {
			if i > 0 {
				sb.WriteString(", ")
			}
			it.format(sb)
		}
		sb.WriteByte(']')
	}
}

// quote writes s as a JSON string with only the escapes JSON
// requires: " \ and control characters.
func quote(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				sb.WriteString(`\u00`)
				sb.WriteByte("0123456789abcdef"[r>>4])
				sb.WriteByte("0123456789abcdef"[r&15])
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

func formatDecimal(m int64, scale int) string {
	neg := m < 0
	if neg {
		m = -m
	}
	s := strconv.FormatInt(m, 10)
	if len(s) <= scale {
		s = strings.Repeat("0", scale+1-len(s)) + s
	}
	s = s[:len(s)-scale] + "." + s[len(s)-scale:]
	if neg {
		s = "-" + s
	}
	return s
}

// formatFloat is the shortest round-trip digits in exponent form:
// 9.05e1, 1e-3, 0e0.
func formatFloat(f float64) string {
	s := strconv.FormatFloat(f, 'e', -1, 64) // 9.05e+01
	i := strings.IndexByte(s, 'e')
	exp, _ := strconv.Atoi(s[i+1:])
	return s[:i] + "e" + strconv.Itoa(exp)
}

func formatDate(day int64) string {
	y, m, d := civilFromDays(day)
	return pad(y, 4) + "-" + pad(m, 2) + "-" + pad(d, 2)
}

func formatTime(ms int64) string {
	return pad(ms/3600000, 2) + ":" + pad(ms/60000%60, 2) + ":" + pad(ms/1000%60, 2) + "." + pad(ms%1000, 3)
}

func formatDuration(ms int64) string {
	var sb strings.Builder
	if ms < 0 {
		sb.WriteByte('-')
		ms = -ms
	}
	h, m, s := ms/3600000, ms%3600000/60000, ms%60000
	if h > 0 {
		sb.WriteString(strconv.FormatInt(h, 10) + "h")
	}
	if m > 0 {
		sb.WriteString(strconv.FormatInt(m, 10) + "m")
	}
	if s > 0 || h == 0 && m == 0 {
		sb.WriteString(strconv.FormatInt(s/1000, 10))
		if frac := s % 1000; frac > 0 {
			sb.WriteString("." + strings.TrimRight(pad(frac, 3), "0"))
		}
		sb.WriteByte('s')
	}
	return sb.String()
}

func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	if len(s) < width {
		s = strings.Repeat("0", width-len(s)) + s
	}
	return s
}

func floorDiv(a, b int64) (q, r int64) {
	q, r = a/b, a%b
	if r < 0 {
		q, r = q-1, r+b
	}
	return q, r
}

// Parse reads one row in text notation. Every value has one spelling:
// text that reads as a row but is spelt otherwise is a T002 *Error at
// the first byte that differs, whose message gives the canonical row;
// text that is no row is T001; a value outside its type is its R code.
// Offsets are bytes into line.
func Parse(line string) ([]any, error) {
	vals, err := parseVals(line)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v.toGo()
	}
	return out, nil
}

func parseVals(line string) ([]val, error) {
	if !utf8.ValidString(line) {
		off := 0
		for off < len(line) {
			r, n := utf8.DecodeRuneInString(line[off:])
			if r == utf8.RuneError && n == 1 {
				break
			}
			off += n
		}
		return nil, errf("R006", off, "text is not valid UTF-8")
	}
	p := parser{s: line}
	var vals []val
	p.space()
	for p.pos < len(p.s) {
		v, err := p.item(false)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
		if p.pos < len(p.s) && !isSpace(p.s[p.pos]) {
			return nil, errf("T001", p.pos, "expected a space between items")
		}
		p.space()
	}
	if canon := formatVals(vals); canon != line {
		i := 0
		for i < len(canon) && i < len(line) && canon[i] == line[i] {
			i++
		}
		return nil, errf("T002", i, "not canonical; the row is spelt: %s", canon)
	}
	return vals, nil
}

type parser struct {
	s   string
	pos int
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

func (p *parser) space() {
	for p.pos < len(p.s) && isSpace(p.s[p.pos]) {
		p.pos++
	}
}

func (p *parser) item(inList bool) (val, error) {
	switch p.s[p.pos] {
	case '"':
		return p.str()
	case '[':
		if inList {
			return val{}, errf("R008", p.pos, "a list holds a list")
		}
		return p.list()
	}
	start := p.pos
	for p.pos < len(p.s) && !strings.ContainsRune(" \t,[]\"", rune(p.s[p.pos])) {
		p.pos++
	}
	if p.pos == start {
		return val{}, errf("T001", start, "expected an item")
	}
	return bare(p.s[start:p.pos], start)
}

func (p *parser) list() (val, error) {
	start := p.pos
	p.pos++ // [
	l := val{k: kList, list: []val{}}
	p.space()
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return l, nil
	}
	for {
		if p.pos >= len(p.s) {
			return val{}, errf("T001", start, "list is not closed")
		}
		v, err := p.item(true)
		if err != nil {
			return val{}, err
		}
		l.list = append(l.list, v)
		p.space()
		if p.pos >= len(p.s) {
			return val{}, errf("T001", start, "list is not closed")
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
			p.space()
		case ']':
			p.pos++
			return l, nil
		default:
			return val{}, errf("T001", p.pos, "expected , or ] in a list")
		}
	}
}

// str reads a JSON string, accepting every JSON spelling; the
// canonical comparison afterwards rejects all but one.
func (p *parser) str() (val, error) {
	start := p.pos
	p.pos++ // "
	var sb strings.Builder
	for {
		if p.pos >= len(p.s) {
			return val{}, errf("T001", start, "string is not closed")
		}
		c := p.s[p.pos]
		switch {
		case c == '"':
			p.pos++
			return val{k: kString, s: sb.String()}, nil
		case c != '\\':
			sb.WriteByte(c)
			p.pos++
			continue
		}
		if p.pos+1 >= len(p.s) {
			return val{}, errf("T001", start, "string is not closed")
		}
		e := p.s[p.pos+1]
		p.pos += 2
		switch e {
		case '"', '\\', '/':
			sb.WriteByte(e)
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'u':
			r, ok := p.hex4()
			if !ok {
				return val{}, errf("T001", start, `\u needs four hex digits`)
			}
			if utf16.IsSurrogate(r) {
				lo, ok := rune(0), false
				if r < 0xdc00 && strings.HasPrefix(p.s[p.pos:], `\u`) {
					p.pos += 2
					lo, ok = p.hex4()
				}
				if !ok || lo < 0xdc00 || lo > 0xdfff {
					return val{}, errf("R006", start, "string holds a lone surrogate")
				}
				r = utf16.DecodeRune(r, lo)
			}
			sb.WriteRune(r)
		default:
			return val{}, errf("T001", start, `unknown escape \%c`, e)
		}
	}
}

func (p *parser) hex4() (rune, bool) {
	if p.pos+4 > len(p.s) {
		return 0, false
	}
	n, err := strconv.ParseUint(p.s[p.pos:p.pos+4], 16, 32)
	if err != nil {
		return 0, false
	}
	p.pos += 4
	return rune(n), true
}

var (
	reDatetime = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})\.(\d{3})Z$`)
	reDate     = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	reTime     = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2})\.(\d{3})$`)
	reDuration = regexp.MustCompile(`^(-?)(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)(?:\.(\d{1,3}))?s)?$`)
	reFloat    = regexp.MustCompile(`^-?\d+(?:\.\d+)?[eE][+-]?\d+$`)
	reDecimal  = regexp.MustCompile(`^(-?)(\d+)\.(\d*)$`)
	reInt      = regexp.MustCompile(`^(-?)(\d+)$`)
	reBytes    = regexp.MustCompile(`^0[xX]((?:[0-9a-fA-F]{2})*)$`)
)

// bare reads an unquoted item: a keyword, number, date, time,
// duration or bytes.
func bare(t string, off int) (val, error) {
	switch t {
	case "none":
		return val{k: kNone}, nil
	case "false":
		return val{k: kFalse}, nil
	case "true":
		return val{k: kTrue}, nil
	}
	if m := reBytes.FindStringSubmatch(t); m != nil {
		b, _ := hex.DecodeString(m[1])
		return val{k: kBytes, b: b}, nil
	}
	if m := reDatetime.FindStringSubmatch(t); m != nil {
		day, err := civil(m[1], m[2], m[3], off)
		if err != nil {
			return val{}, err
		}
		ms, err := clock(m[4], m[5], m[6], m[7], off)
		if err != nil {
			return val{}, err
		}
		return val{k: kDatetime, n: day*86400000 + ms}, nil
	}
	if m := reDate.FindStringSubmatch(t); m != nil {
		day, err := civil(m[1], m[2], m[3], off)
		return val{k: kDate, n: day}, err
	}
	if m := reTime.FindStringSubmatch(t); m != nil {
		ms, err := clock(m[1], m[2], m[3], m[4], off)
		return val{k: kTime, n: ms}, err
	}
	if m := reDuration.FindStringSubmatch(t); m != nil && (m[2] != "" || m[3] != "" || m[4] != "") {
		return duration(m, off)
	}
	if reFloat.MatchString(t) {
		// The syntax is the regexp's; a range error is only the value
		// rounding to an infinity (R005 below) or to zero (then not
		// canonical), as JavaScript's Number reads it.
		f, _ := strconv.ParseFloat(t, 64)
		if err := checkFloat(f, off); err != nil {
			return val{}, err
		}
		return val{k: kFloat, f: f}, nil
	}
	if m := reDecimal.FindStringSubmatch(t); m != nil {
		if len(m[3]) > maxScale {
			return val{}, errf("R004", off, "decimal scale %d is above 15", len(m[3]))
		}
		n, err := digits(m[2]+m[3], off, "decimal mantissa")
		if err != nil {
			return val{}, err
		}
		if m[1] == "-" {
			n = -n
		}
		return val{k: kDecimal, n: n, scale: len(m[3])}, nil
	}
	if m := reInt.FindStringSubmatch(t); m != nil {
		n, err := digits(m[2], off, "int")
		if err != nil {
			return val{}, err
		}
		if m[1] == "-" {
			n = -n
		}
		return val{k: kInt, n: n}, nil
	}
	return val{}, errf("T001", off, "%q is not an item", t)
}

// digits reads a run of decimal digits within 2^53-1.
func digits(s string, off int, what string) (int64, error) {
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > maxInt {
		return 0, errf("R003", off, "%s is outside ±(2^53-1)", what)
	}
	return n, nil
}

func num(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func civil(y, m, d string, off int) (int64, error) {
	yy, mm, dd := num(y), num(m), num(d)
	if !validCivil(yy, mm, dd) {
		return 0, errf("R003", off, "%s-%s-%s is not a date from 0001-01-01 to 9999-12-31", y, m, d)
	}
	return daysFromCivil(yy, mm, dd), nil
}

func clock(h, m, s, ms string, off int) (int64, error) {
	hh, mm, ss := num(h), num(m), num(s)
	if hh > 23 || mm > 59 || ss > 59 {
		return 0, errf("R003", off, "%s:%s:%s is not a time of day", h, m, s)
	}
	return ((hh*60+mm)*60+ss)*1000 + num(ms), nil
}

func duration(m []string, off int) (val, error) {
	var total int64
	for i, unit := range []int64{3600000, 60000, 1000} {
		if m[2+i] == "" {
			continue
		}
		n, err := digits(m[2+i], off, "duration")
		if err != nil {
			return val{}, err
		}
		if n > maxInt/unit {
			return val{}, errf("R003", off, "duration is outside ±(2^53-1) ms")
		}
		total += n * unit
	}
	if frac := m[5]; frac != "" {
		total += num((frac + "00")[:3])
	}
	if total > maxInt {
		return val{}, errf("R003", off, "duration is outside ±(2^53-1) ms")
	}
	if m[1] == "-" {
		total = -total
	}
	return val{k: kDuration, n: total}, nil
}
