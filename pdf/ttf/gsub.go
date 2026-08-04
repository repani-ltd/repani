// GSUB 'tnum' feature: tabular figures applied statically at parse
// time. The house style always sets figures on a fixed advance
// (DESIGN.md §6), so instead of a shaping engine the parser remaps
// every cmap entry covered by the feature's single-substitution
// lookups (type 1, plus type 7 extension wrappers) to its tabular
// variant, once. Widths, kerning, subsetting, and the PDF CID→GID
// map all flow from CharToGID, so they see only substituted glyphs.
package ttf

import "sort"

// applyTnum rewrites charToGID in place. Like GPOS parsing, a
// malformed table degrades to no substitution.
func applyTnum(g []byte, charToGID map[int]uint16) {
	defer func() { _ = recover() }()
	featOff := int(readU16(g, 6))
	lookOff := int(readU16(g, 8))

	idx := map[int]bool{}
	for i := range int(readU16(g, featOff)) {
		rec := featOff + 2 + i*6
		if string(g[rec:rec+4]) != "tnum" {
			continue
		}
		fo := featOff + int(readU16(g, rec+4))
		for j := range int(readU16(g, fo+2)) {
			idx[int(readU16(g, fo+4+j*2))] = true
		}
	}
	if len(idx) == 0 {
		return
	}
	order := make([]int, 0, len(idx))
	for li := range idx {
		order = append(order, li)
	}
	sort.Ints(order)

	// Original glyph → tabular variant; the first lookup that covers
	// a glyph wins, as in shaping order.
	subst := map[uint16]uint16{}
	numLookups := int(readU16(g, lookOff))
	for _, li := range order {
		if li >= numLookups {
			continue
		}
		lo := lookOff + int(readU16(g, lookOff+2+li*2))
		typ := readU16(g, lo)
		for s := range int(readU16(g, lo+4)) {
			so := lo + int(readU16(g, lo+6+s*2))
			t := typ
			if t == 7 { // extension substitution: real subtable behind a u32 offset
				t = readU16(g, so+2)
				so += int(readU32(g, so+4))
			}
			if t != 1 {
				continue
			}
			cov := parseCoverage(g, so+int(readU16(g, so+2)))
			switch readU16(g, so) {
			case 1:
				delta := int(readI16(g, so+4))
				for gid := range cov {
					if _, seen := subst[gid]; !seen {
						subst[gid] = uint16((int(gid) + delta) & 0xFFFF)
					}
				}
			case 2:
				n := int(readU16(g, so+4))
				for gid, ci := range cov {
					if _, seen := subst[gid]; !seen && ci < n {
						subst[gid] = readU16(g, so+6+ci*2)
					}
				}
			}
		}
	}

	for ch, gid := range charToGID {
		if ng, ok := subst[gid]; ok {
			charToGID[ch] = ng
		}
	}
}
