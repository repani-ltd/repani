// Format: the source formatter behind pica fmt. It refills the
// prose of a source document to a fixed source measure and changes
// nothing else, so the document it returns parses to the document
// it was given.
package pica

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"repani.com/typeset/wrap"
)

// FormatWidth is the source measure Format fills to, in runes. It
// is the editor's measure, not the page's: .width sets how wide a
// writer renders, and is often far narrower than any source line.
const FormatWidth = 80

// Format returns src with its prose refilled to FormatWidth: a
// paragraph, the text of an .item or .term, the body of a .quote,
// and a run of .rem lines (a comment refills as a group, each line
// its own .rem) each become greedy, ragged lines broken at spaces.
// The title, headings, command lines, and .pre and .table bodies
// are left as they are, blank lines included; line endings become
// LF, trailing spaces and tabs go, and the result ends in one
// newline.
//
// Refilling never hyphenates (an inserted hyphen would be content)
// and never breaks at a no-break space. A word is never moved to a
// line start where the parser would read the line as structure --
// a dot command, a heading mark, a rule -- so such a word stays on
// the end of the line before, and a word longer than the measure
// keeps an overlong line. A document that does not parse is
// returned unchanged with the parse error.
func Format(src string) (string, error) {
	if _, err := Parse(src); err != nil {
		return src, err
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(src, "\r\n", "\n"), "\n"), "\n")
	f := &formatter{}
	title := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)
		word := commandWord(trimmed)
		switch {
		case trimmed == "":
			f.flush()
			f.emit("")

		case word == ".rem":
			_, rest := splitCommand(trimmed)
			if rest == "" {
				f.flush()
				f.emit(".rem")
				continue
			}
			if !f.rem {
				f.flush()
				f.rem = true
			}
			f.words = append(f.words, wrap.Fields(rest)...)

		case !title:
			// The title is the first line that is neither blank nor
			// a comment; Parse has already refused a command there.
			title = true
			f.flush()
			f.emit(line)

		case word == ".pre" || word == ".table" || word == ".quote":
			// Parse guarantees the .end, so the body is lines[i+1:j].
			f.flush()
			f.emit(line)
			j := i + 1
			for strings.TrimSpace(lines[j]) != ".end" {
				j++
			}
			if word == ".quote" {
				f.quote(lines[i+1 : j])
			} else {
				f.out = append(f.out, lines[i+1:j]...)
			}
			f.emit(strings.TrimRight(lines[j], " \t"))
			i = j

		case word == ".item":
			_, rest := splitCommand(trimmed)
			f.flush()
			f.first = ".item "
			f.words = wrap.Fields(rest)

		case word != "" || isRule(trimmed) || strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## "):
			// Every other command, a rule, a heading: a line of its
			// own that ends any run (a .term's text is the run of
			// unmarked lines after it).
			f.flush()
			f.emit(line)

		default:
			// Prose: a paragraph line, or the continuation of an
			// .item or .term. A comment run ends here, though it did
			// not end the paragraph it sits in.
			if f.rem {
				f.flush()
			}
			f.words = append(f.words, wrap.Fields(trimmed)...)
		}
	}
	f.flush()
	for len(f.out) > 0 && f.out[len(f.out)-1] == "" {
		f.out = f.out[:len(f.out)-1]
	}
	return strings.Join(f.out, "\n") + "\n", nil
}

// formatter accumulates the run of words being refilled and the
// output lines.
type formatter struct {
	out   []string
	words []string // the run being refilled
	first string   // prefix of the run's first line (".item ")
	rem   bool     // the run is a comment: every line is a .rem
}

func (f *formatter) emit(line string) { f.out = append(f.out, line) }

// quote refills a .quote body: its runs of prose between blank
// lines, every line but the .attrib (which Parse keeps last).
func (f *formatter) quote(body []string) {
	for _, line := range body {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			f.flush()
			f.emit("")
		case commandWord(trimmed) == ".attrib":
			f.flush()
			f.emit(strings.TrimRight(line, " \t"))
		default:
			f.words = append(f.words, wrap.Fields(trimmed)...)
		}
	}
	f.flush()
}

// flush fills the run, if any, into the output.
func (f *formatter) flush() {
	if len(f.words) > 0 {
		first, next := f.first, ""
		if f.rem {
			first, next = ".rem ", ".rem "
		}
		f.out = append(f.out, fill(f.words, first, next, FormatWidth)...)
	}
	f.words, f.first, f.rem = nil, "", false
}

// fill sets words greedily in lines of at most width runes, the
// first prefixed by first and the rest by next. Words that may not
// be parted (see canBreak) travel as one unit, so the measure is
// exceeded only by a unit wider than it.
func fill(words []string, first, next string, width int) []string {
	units := []string{words[0]}
	for k := 1; k < len(words); k++ {
		if canBreak(words[k-1], words[k]) {
			units = append(units, words[k])
		} else {
			units[len(units)-1] += " " + words[k]
		}
	}
	var lines []string
	line := first + units[0]
	n := utf8.RuneCountInString(line)
	for _, u := range units[1:] {
		un := utf8.RuneCountInString(u)
		if n+1+un <= width {
			line += " " + u
			n += 1 + un
			continue
		}
		lines = append(lines, line)
		line = next + u
		n = utf8.RuneCountInString(line)
	}
	return append(lines, line)
}

// canBreak reports whether a line may end after word a and start
// with word b, so that the parser still reads both lines as the
// prose they were. The parser trims every line of Unicode space, so
// a space-edged word (a no-break space is a rune of a word) must
// not sit at a line edge; and a line that starts with a dot command,
// a heading mark or a rule is structure, not prose. The test is the
// strictest context's -- .attrib and .end, which matter only in a
// .quote, are dot commands everywhere -- so it needs no context.
func canBreak(a, b string) bool {
	last, _ := utf8.DecodeLastRuneInString(a)
	firstRune, _ := utf8.DecodeRuneInString(b)
	if unicode.IsSpace(last) || unicode.IsSpace(firstRune) {
		return false
	}
	return !isDotCommand(b) && b != "#" && b != "##" && b != "###" && !isRule(b)
}
