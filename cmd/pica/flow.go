// Column flow: pure distribution of flowable blocks into columns,
// counted in half-line units (capacity is in body lines). Shared by
// every paged presentation.
package main

// minKeep is the orphan/widow threshold: a split never leaves fewer
// than minKeep segments of a block on either side of a column break.
const minKeep = 2

// flow distributes blocks into columns of capacity(i) lines each.
// Splits happen only between segments, leaving at least minKeep
// segments on both sides; the repeated lead-in (table headers,
// .pre N) is re-emitted after each split. Atomic blocks move whole
// unless taller than an entire fresh column. A heading is never
// left at a column bottom without minKeep segments of what follows.
//
// Capacity is in body lines; internal accounting is in half-line
// units (a body line is 2, a table note line 1). Every block starts
// on a whole body line: placement pads an odd column height with a
// blank half-line first, so half-lines stay confined inside the
// block that made them and the cross-column baseline grid holds.
func flow(blocks []fblock, capacity func(int) int) [][]sline {
	var out [][]sline
	var cur []sline
	curH := 0 // height of cur in half-line units
	colIdx := 0

	closeCol := func() {
		out = append(out, cur)
		cur = nil
		curH = 0
		colIdx++
	}
	place := func(b fblock, upto int) {
		if curH%2 != 0 {
			cur = append(cur, sline{role: roleHalf})
			curH++
		}
		if len(cur) > 0 && !b.tight {
			cur = append(cur, sline{})
			curH += 2
		}
		for _, s := range b.segs[:upto] {
			cur = append(cur, s.lines...)
			curH += s.height()
		}
	}

	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		for {
			colCap := 2 * capacity(colIdx)
			sep := 0
			if len(cur) > 0 && !b.tight {
				sep = 2
			}
			snap := curH % 2
			avail := colCap - curH - snap - sep
			h := b.height()

			// Keep-with-next: the heading and the first minKeep
			// segments of the next block must fit together.
			if b.keepNext && i+1 < len(blocks) && len(cur) > 0 {
				next := blocks[i+1]
				need := h + 2
				for _, s := range next.segs[:min(minKeep, len(next.segs))] {
					need += s.height()
				}
				if avail < need {
					closeCol()
					continue
				}
			}

			if h <= avail {
				place(b, len(b.segs))
				break
			}

			k := splitSegs(b, avail)
			if k <= 0 {
				if len(cur) > 0 {
					closeCol()
					continue
				}
				// Top of an empty column and still no acceptable
				// split: force progress.
				k = forceSplit(b, avail)
			}
			place(b, k)
			b = b.rest(k)
			b.tight = false
			closeCol()
			if len(b.segs) == 0 {
				break
			}
		}
	}
	if len(cur) > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// fitSegs returns how many leading segments of b fit in avail
// half-line units.
func fitSegs(b fblock, avail int) int {
	k, units := 0, 0
	for k < len(b.segs) && units+b.segs[k].height() <= avail {
		units += b.segs[k].height()
		k++
	}
	return k
}

// splitSegs returns how many leading segments of b fit in avail
// half-line units under the orphan/widow rules, or 0 for "move
// whole".
func splitSegs(b fblock, avail int) int {
	if b.atomic {
		return 0
	}
	n := len(b.segs)
	// The first part must keep the repeated lead-in plus minKeep
	// content segments; the remainder keeps minKeep content segments.
	k := min(fitSegs(b, avail), n-minKeep)
	if k < b.repeat+minKeep {
		return 0
	}
	return k
}

// forceSplit fits as many segments as possible into an empty column,
// ignoring the keep rules. It always makes progress: at least one
// segment beyond the repeated lead-in goes down (otherwise rest()
// would reconstruct the identical block and flow would loop), and an
// atomic block taller than the column places whole, overflowing.
func forceSplit(b fblock, avail int) int {
	if b.atomic || len(b.segs) == 1 {
		return len(b.segs)
	}
	k := max(fitSegs(b, avail), b.repeat+1)
	return min(k, len(b.segs))
}

// rest returns the unplaced remainder after splitting at k, with the
// repeated lead-in re-attached. A block consumed to its end has no
// remainder (re-attaching the lead-in alone would loop forever).
func (b fblock) rest(k int) fblock {
	if k >= len(b.segs) {
		return fblock{}
	}
	segs := append(append([]seg{}, b.segs[:b.repeat]...), b.segs[k:]...)
	return fblock{segs: segs, repeat: b.repeat}
}
