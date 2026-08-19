package main

import (
	"strings"
	"testing"

	"repani.com/pica"
	"repani.com/pica/pdf"
)

// Regression: the justified reconstruction used to recompute the
// break after a hyphenated line with hyphenation disabled, so a
// line starting with a hyphen suffix could not itself end in a
// hyphen. In this Greek paragraph (long words, narrow measure)
// that produced a line stretched to 5.6 spaces per gap where the
// optimum ends it "συλλαβι-". Bound the worst-case gap instead of
// pinning exact breaks so cost tuning does not churn the test.
func TestJustifyLines_GreekNoGrotesqueGaps(t *testing.T) {
	para := "Ο αλγόριθμος που επιλέγει τα σημεία αλλαγής γραμμής είναι ο ίδιος για τα ελληνικά και τα αγγλικά: δυναμικός προγραμματισμός πάνω σε όλες τις δυνατές τομές της παραγράφου, με κόστος που τιμωρεί τα άνισα διαστήματα και τον περιττό συλλαβισμό. Τα πρότυπα συλλαβισμού για τα ελληνικά είναι ενσωματωμένα στο πρόγραμμα, όπως και τα αγγλικά, και δουλεύουν με τον ίδιο ακριβώς μηχανισμό."
	m := pdf.Measure(pdf.Sans)
	units := 36 * pdf.AvgAdvance(pdf.Sans)
	lines := typeset.JustifyLines(para, units, m)
	for i, ln := range lines[:len(lines)-1] {
		gaps := len(ln.Words) - 1
		if gaps <= 0 {
			continue
		}
		target := units
		if strings.HasSuffix(ln.Words[len(ln.Words)-1], "-") {
			target += typeset.HangHyphen(m)
		}
		perGap := float64(target-ln.Width) / float64(gaps)
		if perGap > 3*float64(m.Space()) {
			t.Errorf("line %d: per-gap slack %.1f exceeds 3 spaces: %v",
				i, perGap, strings.Join(ln.Words, " "))
		}
	}
	// The paragraph must not trail off in a bare hyphen fragment:
	// the final-hyphen penalty keeps the last line a whole word.
	prev := lines[len(lines)-2]
	if strings.HasSuffix(prev.Words[len(prev.Words)-1], "-") &&
		len(lines[len(lines)-1].Words) == 1 {
		t.Errorf("last line is a bare hyphen suffix: %v", lines[len(lines)-1].Words)
	}
}
