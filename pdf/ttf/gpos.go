// GPOS 'kern' feature parsing: pair-positioning XAdvance adjustments
// (lookup type 2, both subtable formats, plus type 9 extension
// wrappers). Kerning is applied at measurement and content-stream
// time as TJ adjustments, so the GPOS table itself never needs to
// reach the PDF viewer.
package ttf

import (
	"math"
	"math/bits"
	"sort"
)

// kernSub is one PairPos subtable. Exactly one of pairs (format 1)
// or matrix (format 2) is set. Values are XAdvance in font units.
type kernSub struct {
	coverage map[uint16]int // first glyph -> coverage index

	pairs []map[uint16]int16 // fmt 1: coverage index -> second glyph -> adv

	class1, class2 map[uint16]uint16 // fmt 2: glyph -> class (absent = 0)
	class2Count    int
	matrix         []int16 // class1Count x class2Count advances
}

// kernLookup is one GPOS lookup: within it the first subtable whose
// coverage and pair match wins; values accumulate across lookups.
type kernLookup struct{ subs []kernSub }

// kern holds a font's parsed kerning state plus the pair cache. The
// cache is not safe for concurrent use, like the rest of the package.
type kern struct {
	lookups []kernLookup
	cache   map[uint32]int
}

// Kern returns the kerning adjustment between codepoints a and b in
// PDF units (thousandths of an em), or 0 when the pair does not
// kern. Values are negative when the pair tightens.
func (f *TTFont) Kern(a, b rune) int {
	if f.kern == nil || len(f.kern.lookups) == 0 || a > 0xFFFF || b > 0xFFFF {
		return 0
	}
	k := f.kern
	key := uint32(a)<<16 | uint32(b)
	if v, ok := k.cache[key]; ok {
		return v
	}
	v := 0
	ga, oka := f.CharToGID[int(a)]
	gb, okb := f.CharToGID[int(b)]
	if oka && okb {
		total := 0
		for _, lk := range k.lookups {
			for i := range lk.subs {
				if adv, ok := lk.subs[i].adjust(ga, gb); ok {
					total += adv
					break
				}
			}
		}
		v = int(math.Round(float64(total) * 1000 / float64(f.UnitsPerEm)))
	}
	if k.cache == nil {
		k.cache = make(map[uint32]int)
	}
	k.cache[key] = v
	return v
}

// adjust returns the subtable's XAdvance for the glyph pair and
// whether the pair matched (a matched pair with value 0 still stops
// the lookup's subtable chain).
func (s *kernSub) adjust(a, b uint16) (int, bool) {
	ci, ok := s.coverage[a]
	if !ok {
		return 0, false
	}
	if s.pairs != nil {
		if v, ok := s.pairs[ci][b]; ok {
			return int(v), true
		}
		return 0, false
	}
	c1 := int(s.class1[a])
	c2 := int(s.class2[b])
	return int(s.matrix[c1*s.class2Count+c2]), true
}

// parseKern extracts the lookups referenced by any 'kern' feature.
// GPOS layout is offset-chained binary; a malformed table degrades
// to no kerning rather than failing the font parse.
func parseKern(g []byte) (k *kern) {
	defer func() {
		if recover() != nil {
			k = nil
		}
	}()
	featOff := int(readU16(g, 6))
	lookOff := int(readU16(g, 8))

	idx := map[int]bool{}
	for i := range int(readU16(g, featOff)) {
		rec := featOff + 2 + i*6
		if string(g[rec:rec+4]) != "kern" {
			continue
		}
		fo := featOff + int(readU16(g, rec+4))
		for j := range int(readU16(g, fo+2)) {
			idx[int(readU16(g, fo+4+j*2))] = true
		}
	}
	if len(idx) == 0 {
		return nil
	}
	order := make([]int, 0, len(idx))
	for li := range idx {
		order = append(order, li)
	}
	sort.Ints(order)

	numLookups := int(readU16(g, lookOff))
	var lookups []kernLookup
	for _, li := range order {
		if li >= numLookups {
			continue
		}
		lo := lookOff + int(readU16(g, lookOff+2+li*2))
		typ := readU16(g, lo)
		var lk kernLookup
		for s := range int(readU16(g, lo+4)) {
			so := lo + int(readU16(g, lo+6+s*2))
			t := typ
			if t == 9 { // extension positioning: real subtable behind a u32 offset
				t = readU16(g, so+2)
				so += int(readU32(g, so+4))
			}
			if t != 2 {
				continue
			}
			lk.subs = append(lk.subs, parsePairPos(g, so))
		}
		if len(lk.subs) > 0 {
			lookups = append(lookups, lk)
		}
	}
	if len(lookups) == 0 {
		return nil
	}
	return &kern{lookups: lookups}
}

// vrSize is the byte size of a ValueRecord for the given value format.
func vrSize(vf uint16) int { return bits.OnesCount16(vf) * 2 }

// vrXAdvance extracts the XAdvance field (bit 0x0004) of the
// ValueRecord at off, or 0 when the format does not carry one.
func vrXAdvance(g []byte, off int, vf uint16) int16 {
	if vf&0x0004 == 0 {
		return 0
	}
	return readI16(g, off+bits.OnesCount16(vf&0x0003)*2)
}

// parsePairPos parses one PairPos subtable at offset so.
func parsePairPos(g []byte, so int) kernSub {
	sub := kernSub{coverage: parseCoverage(g, so+int(readU16(g, so+2)))}
	vf1 := readU16(g, so+4)
	vf2 := readU16(g, so+6)
	switch readU16(g, so) {
	case 1:
		n := int(readU16(g, so+8))
		rec := 2 + vrSize(vf1) + vrSize(vf2)
		sub.pairs = make([]map[uint16]int16, n)
		for i := range n {
			pso := so + int(readU16(g, so+10+i*2))
			cnt := int(readU16(g, pso))
			m := make(map[uint16]int16, cnt)
			for p := range cnt {
				r := pso + 2 + p*rec
				m[readU16(g, r)] = vrXAdvance(g, r+2, vf1)
			}
			sub.pairs[i] = m
		}
	case 2:
		sub.class1 = parseClassDef(g, so+int(readU16(g, so+8)))
		sub.class2 = parseClassDef(g, so+int(readU16(g, so+10)))
		c1n := int(readU16(g, so+12))
		c2n := int(readU16(g, so+14))
		rec := vrSize(vf1) + vrSize(vf2)
		sub.class2Count = c2n
		sub.matrix = make([]int16, c1n*c2n)
		for i := range sub.matrix {
			sub.matrix[i] = vrXAdvance(g, so+16+i*rec, vf1)
		}
	}
	return sub
}

// parseCoverage returns glyph -> coverage index for both formats.
func parseCoverage(g []byte, off int) map[uint16]int {
	cov := make(map[uint16]int)
	switch readU16(g, off) {
	case 1:
		for i := range int(readU16(g, off+2)) {
			cov[readU16(g, off+4+i*2)] = i
		}
	case 2:
		for i := range int(readU16(g, off+2)) {
			r := off + 4 + i*6
			start, end, sci := int(readU16(g, r)), int(readU16(g, r+2)), int(readU16(g, r+4))
			for gid := start; gid <= end; gid++ {
				cov[uint16(gid)] = sci + gid - start
			}
		}
	}
	return cov
}

// parseClassDef returns glyph -> class, storing only nonzero classes
// (absent glyphs are class 0 by definition).
func parseClassDef(g []byte, off int) map[uint16]uint16 {
	cd := make(map[uint16]uint16)
	switch readU16(g, off) {
	case 1:
		start := readU16(g, off+2)
		for i := range int(readU16(g, off+4)) {
			if c := readU16(g, off+6+i*2); c != 0 {
				cd[start+uint16(i)] = c
			}
		}
	case 2:
		for i := range int(readU16(g, off+2)) {
			r := off + 4 + i*6
			s, e, c := int(readU16(g, r)), int(readU16(g, r+2)), readU16(g, r+4)
			if c == 0 {
				continue
			}
			for gid := s; gid <= e; gid++ {
				cd[uint16(gid)] = c
			}
		}
	}
	return cd
}
