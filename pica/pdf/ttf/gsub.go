// GSUB 'tnum' feature: tabular figures applied statically at parse
// time. The house style always sets figures on a fixed advance
// (DESIGN.t §6), so instead of a shaping engine the parser remaps
// every cmap entry covered by the feature's single-substitution
// lookups (type 1, plus type 7 extension wrappers) to its tabular
// variant, once. Widths, kerning, subsetting, and the PDF CID→GID
// map all flow from CharToGID, so they see only substituted glyphs.
// The feature → lookup → subtable walk (featureLookups,
// lookupSubtables) is shared with the GPOS parser in gpos.go.
package ttf

import "sort"

// featureLookups returns the offsets of the lookup tables referenced
// by every feature tagged tag in the GSUB or GPOS table g, in lookup
// index order (shaping order), each at most once.
func featureLookups(g []byte, tag string) []int {
	featOff := int(readU16(g, 6))
	lookOff := int(readU16(g, 8))

	idx := map[int]bool{}
	for i := range int(readU16(g, featOff)) {
		rec := featOff + 2 + i*6
		if string(g[rec:rec+4]) != tag {
			continue
		}
		fo := featOff + int(readU16(g, rec+4))
		for j := range int(readU16(g, fo+2)) {
			idx[int(readU16(g, fo+4+j*2))] = true
		}
	}
	order := make([]int, 0, len(idx))
	for li := range idx {
		order = append(order, li)
	}
	sort.Ints(order)

	numLookups := int(readU16(g, lookOff))
	var offs []int
	for _, li := range order {
		if li < numLookups {
			offs = append(offs, lookOff+int(readU16(g, lookOff+2+li*2)))
		}
	}
	return offs
}

// lookupSubtables returns the offsets of the subtables of lookup lo
// whose type is want, unwrapping extension lookups (type ext: the
// real type and a u32 offset to the real subtable follow the format
// word) and skipping every other type.
func lookupSubtables(g []byte, lo int, want, ext uint16) []int {
	typ := readU16(g, lo)
	var offs []int
	for s := range int(readU16(g, lo+4)) {
		so := lo + int(readU16(g, lo+6+s*2))
		t := typ
		if t == ext {
			t = readU16(g, so+2)
			so += int(readU32(g, so+4))
		}
		if t == want {
			offs = append(offs, so)
		}
	}
	return offs
}

// applyTnum rewrites charToGID in place. Like GPOS parsing, a
// malformed table degrades to no substitution.
func applyTnum(g []byte, charToGID map[int]uint16) {
	defer func() { _ = recover() }()
	// Original glyph → tabular variant; the first lookup that covers
	// a glyph wins, as in shaping order.
	subst := map[uint16]uint16{}
	for _, lo := range featureLookups(g, "tnum") {
		for _, so := range lookupSubtables(g, lo, 1, 7) {
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
