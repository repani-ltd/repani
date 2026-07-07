package main

import (
	"fmt"
	"strings"
	"testing"
)

func mkLines(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return out
}

func fixedCap(n int) func(int) int { return func(int) int { return n } }

// used verifies no column exceeds its capacity and returns flattened
// non-blank content for order checks.
func checkCols(t *testing.T, cols [][]string, capacity func(int) int) []string {
	t.Helper()
	var flat []string
	for i, col := range cols {
		if len(col) > capacity(i) {
			t.Fatalf("column %d holds %d lines, capacity %d", i, len(col), capacity(i))
		}
		for _, ln := range col {
			if strings.TrimSpace(ln) != "" {
				flat = append(flat, ln)
			}
		}
	}
	return flat
}

func TestParseBlocks(t *testing.T) {
	lines := []string{
		"# Heading", "",
		"para line one", "para line two", "", "",
		"Hour  Level", "----  -----", "01:00 +0.1", "02:00 +0.2",
	}
	blocks := parseBlocks(lines)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}
	if !blocks[0].heading || blocks[1].heading || blocks[2].heading {
		t.Error("heading detection wrong")
	}
	if blocks[0].table || blocks[1].table || !blocks[2].table {
		t.Error("table detection wrong")
	}
}

func TestFlow_OrphanNeverSingleLineAtBottom(t *testing.T) {
	// A 2-line filler then a 6-line paragraph into capacity 4: after
	// filler + separator, only 1 line of space remains -- an orphan.
	// The paragraph must move whole to the next column, splitting
	// there if needed.
	blocks := []block{
		{lines: mkLines("fill", 2)},
		{lines: mkLines("para", 6)},
	}
	cols := flow(blocks, fixedCap(4))
	checkCols(t, cols, fixedCap(4))
	if len(cols[0]) != 2 {
		t.Fatalf("column 0 = %v, want just the filler", cols[0])
	}
	if got := cols[1]; len(got) != 4 || got[0] != "para1" {
		t.Fatalf("column 1 = %v, want para1..para4", got)
	}
	// Continuation carries >= minKeep lines.
	if got := cols[2]; len(got) != 2 || got[0] != "para5" {
		t.Fatalf("column 2 = %v, want para5..para6", got)
	}
}

func TestFlow_WidowNeverSingleLineAtTop(t *testing.T) {
	// 5-line paragraph into capacity 4: a naive split would put 4
	// lines then a lone widow. The split must hold back a line.
	blocks := []block{{lines: mkLines("p", 5)}}
	cols := flow(blocks, fixedCap(4))
	checkCols(t, cols, fixedCap(4))
	if len(cols) != 2 {
		t.Fatalf("columns = %d, want 2", len(cols))
	}
	if len(cols[0]) != 3 || len(cols[1]) != 2 {
		t.Fatalf("split %d/%d, want 3/2", len(cols[0]), len(cols[1]))
	}
}

func TestFlow_TinyBlocksNeverSplit(t *testing.T) {
	blocks := []block{
		{lines: mkLines("a", 3)},
		{lines: mkLines("b", 3)},
	}
	cols := flow(blocks, fixedCap(5))
	checkCols(t, cols, fixedCap(5))
	// b (3 lines) does not fit after a+separator (5-3-1=1 line
	// free) and must not split: whole block to column 2.
	if len(cols) != 2 || len(cols[1]) != 3 {
		t.Fatalf("cols = %v", cols)
	}
}

func TestFlow_HeadingKeepsWithNext(t *testing.T) {
	// Heading lands with exactly 1 line of space after the filler:
	// it must not sit alone at the column bottom.
	blocks := []block{
		{lines: mkLines("fill", 4)},
		{lines: []string{"# Heading"}, heading: true},
		{lines: mkLines("body", 4)},
	}
	cols := flow(blocks, fixedCap(6))
	checkCols(t, cols, fixedCap(6))
	if len(cols[0]) != 4 {
		t.Fatalf("column 0 = %v, want only filler", cols[0])
	}
	if cols[1][0] != "# Heading" || cols[1][2] != "body1" {
		t.Fatalf("column 1 = %v, want heading atop its body", cols[1])
	}
}

func TestFlow_TableSplitRepeatsHeader(t *testing.T) {
	table := block{
		lines: append([]string{"Hour  Level", "----  -----"}, mkLines("row", 10)...),
		table: true,
	}
	cols := flow([]block{table}, fixedCap(6))
	checkCols(t, cols, fixedCap(6))
	if len(cols) < 2 {
		t.Fatalf("expected a split, got %d column(s)", len(cols))
	}
	for i, col := range cols {
		if col[0] != "Hour  Level" || col[1] != "----  -----" {
			t.Fatalf("column %d does not start with the header: %v", i, col)
		}
		if dataRows := len(col) - 2; dataRows < minKeep {
			t.Fatalf("column %d has %d data rows, want >= %d", i, dataRows, minKeep)
		}
	}
	// All 10 data rows present, in order, exactly once.
	var rows []string
	for _, col := range cols {
		rows = append(rows, col[2:]...)
	}
	for i, r := range rows {
		if want := fmt.Sprintf("row%d", i+1); r != want {
			t.Fatalf("data row %d = %q, want %q", i, r, want)
		}
	}
	if len(rows) != 10 {
		t.Fatalf("data rows = %d, want 10", len(rows))
	}
}

func TestFlow_BlockTallerThanColumnForceSplits(t *testing.T) {
	blocks := []block{{lines: mkLines("x", 20)}}
	cols := flow(blocks, fixedCap(6))
	flat := checkCols(t, cols, fixedCap(6))
	if len(flat) != 20 {
		t.Fatalf("lines preserved = %d, want 20", len(flat))
	}
}

func TestFlow_NothingLostOrReordered(t *testing.T) {
	var blocks []block
	var want []string
	for b := 0; b < 12; b++ {
		n := 1 + (b*7)%9
		lines := mkLines(fmt.Sprintf("b%d_", b), n)
		blk := block{lines: lines, heading: n == 1 && b < 11}
		if n >= 4 && b%3 == 0 {
			blk.table = true // exercise header re-attachment paths
			blk.lines = append([]string{"HDR", "---"}, lines...)
		}
		blocks = append(blocks, blk)
		want = append(want, blk.lines...)
	}
	cols := flow(blocks, fixedCap(7))
	flat := checkCols(t, cols, fixedCap(7))
	// Every original line appears, in order (repeated table headers
	// are extras, so check subsequence).
	wi := 0
	for _, ln := range flat {
		if wi < len(want) && ln == want[wi] {
			wi++
		}
	}
	if wi != len(want) {
		t.Fatalf("only %d of %d lines survived in order", wi, len(want))
	}
}

func TestSplitMasthead(t *testing.T) {
	text := "\nTHE GAZETTE\n\nBody line.\n"
	m, body := splitMasthead(text, "", false)
	if m != "THE GAZETTE" || len(body) != 1 || body[0] != "Body line." {
		t.Fatalf("m=%q body=%v", m, body)
	}
	m, body = splitMasthead(text, "OVERRIDE", false)
	if m != "OVERRIDE" || len(body) != 4 {
		t.Fatalf("override: m=%q body=%v", m, body)
	}
	m, body = splitMasthead(text, "", true)
	if m != "" || len(body) != 4 {
		t.Fatalf("nomast: m=%q body=%v", m, body)
	}
}

func TestBroadsheetEndToEnd(t *testing.T) {
	var b strings.Builder
	b.WriteString("E2E TEST SHEET\n\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "# Section %d\n\n", i)
		for j := 0; j < 6; j++ {
			fmt.Fprintf(&b, "line %d of section %d padded to width\n", j, i)
		}
		b.WriteString("\n")
	}
	masthead, body := splitMasthead(b.String(), "", false)
	out, err := broadsheet(masthead, body, 0 /* A4 */, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "%PDF-1.3") {
		t.Fatal("not a PDF")
	}
	if pages := strings.Count(s, "/Type /Page\n"); pages < 2 {
		t.Fatalf("pages = %d, want multi-page", pages)
	}
}
