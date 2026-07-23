// Package ttf parses TrueType fonts and produces per-document glyph
// subsets for PDF embedding (CIDFontType2 / Identity-H).
package ttf

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// Parse parses raw TrueType font bytes into a TTFont ready for
// embedding and subsetting.
func Parse(raw []byte) (*TTFont, error) {
	return parseTTF(raw)
}


// TTFont holds parsed TrueType font data for PDF embedding.
// The full (unsubset) TTF is stored in Data; per-document subsetting
// is done at PDF build time via the Subset() method.
type TTFont struct {
	Tag            string // PDF resource name (e.g. "FR", "FB")
	PostScriptName string
	UnitsPerEm     uint16
	Ascent         int16
	Descent        int16
	CapHeight      int16
	ItalicAngle    float64
	BBox           [4]int16
	StemV          int
	Flags          int
	DefaultWidth   int            // scaled to PDF units (1000/unitsPerEm)
	CIDWidths      map[int]int    // per-CID advance widths, scaled to PDF units (all chars)
	CharToGID      map[int]uint16 // full cmap: codepoint → glyph ID (for subsetting)
	Data           []byte         // full (unsubset) TTF file bytes

	kern *kern // GPOS pair kerning; nil when the font has none
}

func readU16(b []byte, off int) uint16   { return binary.BigEndian.Uint16(b[off:]) }
func readI16(b []byte, off int) int16    { return int16(binary.BigEndian.Uint16(b[off:])) }
func readU32(b []byte, off int) uint32   { return binary.BigEndian.Uint32(b[off:]) }
func putU16(b []byte, off int, v uint16) { binary.BigEndian.PutUint16(b[off:], v) }
func putU32(b []byte, off int, v uint32) { binary.BigEndian.PutUint32(b[off:], v) }

type ttfTable struct {
	tag    string
	offset uint32
	length uint32
}

// parseTables extracts the table directory from raw TTF data.
func parseTables(raw []byte) (map[string]ttfTable, error) {
	if len(raw) < 12 {
		return nil, fmt.Errorf("font too small")
	}
	numTables := int(readU16(raw, 4))
	tables := make(map[string]ttfTable)
	for i := range numTables {
		rec := 12 + i*16
		tag := string(raw[rec : rec+4])
		tables[tag] = ttfTable{
			tag:    tag,
			offset: readU32(raw, rec+8),
			length: readU32(raw, rec+12),
		}
	}
	return tables, nil
}

func tableGet(raw []byte, tables map[string]ttfTable, tag string) ([]byte, error) {
	t, ok := tables[tag]
	if !ok {
		return nil, fmt.Errorf("missing table: %s", tag)
	}
	return raw[t.offset : t.offset+t.length], nil
}

func parseTTF(raw []byte) (*TTFont, error) {
	tables, err := parseTables(raw)
	if err != nil {
		return nil, err
	}

	get := func(tag string) ([]byte, error) {
		return tableGet(raw, tables, tag)
	}

	font := &TTFont{Flags: 33, StemV: 80}

	// head
	head, err := get("head")
	if err != nil {
		return nil, err
	}
	font.UnitsPerEm = readU16(head, 18)
	font.BBox = [4]int16{readI16(head, 36), readI16(head, 38), readI16(head, 40), readI16(head, 42)}

	// hhea
	hhea, err := get("hhea")
	if err != nil {
		return nil, err
	}
	font.Ascent = readI16(hhea, 4)
	font.Descent = readI16(hhea, 6)
	numHMtx := int(readU16(hhea, 34))

	// maxp
	maxp, err := get("maxp")
	if err != nil {
		return nil, err
	}
	numGlyphs := int(readU16(maxp, 4))

	// OS/2 (optional)
	if os2, err := get("OS/2"); err == nil && readU16(os2, 0) >= 2 && len(os2) > 90 {
		font.CapHeight = readI16(os2, 88)
	}

	// post
	if post, err := get("post"); err == nil && len(post) >= 8 {
		font.ItalicAngle = float64(readI16(post, 4)) + float64(readU16(post, 6))/65536.0
	}

	// cmap — parse full character map
	cmap, err := get("cmap")
	if err != nil {
		return nil, err
	}
	charToGID := parseCmap(cmap)
	font.CharToGID = charToGID

	// hmtx
	hmtx, err := get("hmtx")
	if err != nil {
		return nil, err
	}
	gw := make([]uint16, numGlyphs)
	for i := range numGlyphs {
		if i < numHMtx {
			gw[i] = readU16(hmtx, i*4)
		} else {
			gw[i] = readU16(hmtx, (numHMtx-1)*4)
		}
	}

	// Default width (for monospace /DW in CID font)
	scale := float64(1000) / float64(font.UnitsPerEm)
	if gid, ok := charToGID[0x20]; ok && int(gid) < numGlyphs {
		font.DefaultWidth = int(float64(gw[gid]) * scale)
	} else {
		font.DefaultWidth = 600
	}

	// Build full CIDWidths map for all characters in cmap
	font.CIDWidths = make(map[int]int, len(charToGID))
	for ch, gid := range charToGID {
		if int(gid) < numGlyphs {
			font.CIDWidths[ch] = int(float64(gw[gid]) * scale)
		}
	}

	// GPOS pair kerning (optional)
	if gpos, err := get("GPOS"); err == nil {
		font.kern = parseKern(gpos)
	}

	// Store full TTF data (subsetting done per-document via Subset())
	font.Data = raw

	// Scale metrics to PDF units
	font.Ascent = int16(float64(font.Ascent) * scale)
	font.Descent = int16(float64(font.Descent) * scale)
	font.CapHeight = int16(float64(font.CapHeight) * scale)
	for i := range font.BBox {
		font.BBox[i] = int16(float64(font.BBox[i]) * scale)
	}

	// PostScript name
	if nt, err := get("name"); err == nil {
		font.PostScriptName = extractPSName(nt)
	}
	if font.PostScriptName == "" {
		font.PostScriptName = "FiraSans-Regular"
	}

	return font, nil
}

// parseCmap finds the format 4 subtable (platformID=3, encodingID=1)
// and returns a char→GID map for all characters in the font.
func parseCmap(cmap []byte) map[int]uint16 {
	m := make(map[int]uint16)
	n := int(readU16(cmap, 2))
	for i := range n {
		rec := 4 + i*8
		if readU16(cmap, rec) == 3 && readU16(cmap, rec+2) == 1 {
			off := int(readU32(cmap, rec+4))
			if readU16(cmap, off) == 4 {
				parseFmt4(cmap[off:], m)
			}
			break
		}
	}
	return m
}

func parseFmt4(sub []byte, m map[int]uint16) {
	if len(sub) < 14 {
		return
	}
	segCount := int(readU16(sub, 6)) / 2
	endOff := 14
	startOff := endOff + segCount*2 + 2 // +2 for reservedPad
	deltaOff := startOff + segCount*2
	rangeOff := deltaOff + segCount*2

	// Bounds check: ensure all segment arrays fit within the subtable
	if rangeOff+segCount*2 > len(sub) {
		return
	}

	for i := range segCount {
		ec := int(readU16(sub, endOff+i*2))
		sc := int(readU16(sub, startOff+i*2))
		delta := int(readI16(sub, deltaOff+i*2))
		roff := int(readU16(sub, rangeOff+i*2))
		if sc == 0xFFFF {
			break
		}
		for ch := sc; ch <= ec; ch++ {
			var gid uint16
			if roff == 0 {
				gid = uint16((ch + delta) & 0xFFFF)
			} else {
				addr := rangeOff + i*2 + roff + (ch-sc)*2
				if addr+2 <= len(sub) {
					gid = readU16(sub, addr)
					if gid != 0 {
						gid = uint16((int(gid) + delta) & 0xFFFF)
					}
				}
			}
			m[ch] = gid
		}
	}
}

// SubsetResult holds the per-document font subset data needed for PDF embedding.
type SubsetResult struct {
	Data     []byte      // subset TTF bytes (only used glyphs)
	CIDToGID []byte      // binary CID→GID map for used codepoints
	Widths   map[int]int // per-CID widths for used codepoints only
}

// Subset creates a per-document font subset containing only the glyphs
// for the given codepoints. It reuses the internal subsetting machinery
// (addCompounds, subsetTTF) to produce a minimal TTF and CID→GID map.
func (f *TTFont) Subset(used map[rune]bool) (*SubsetResult, error) {
	raw := f.Data
	tables, err := parseTables(raw)
	if err != nil {
		return nil, err
	}

	get := func(tag string) ([]byte, error) {
		return tableGet(raw, tables, tag)
	}

	head, err := get("head")
	if err != nil {
		return nil, err
	}
	locaFmt := readI16(head, 50)

	maxp, err := get("maxp")
	if err != nil {
		return nil, err
	}
	numGlyphs := int(readU16(maxp, 4))

	glyf, err := get("glyf")
	if err != nil {
		return nil, err
	}
	loca, err := get("loca")
	if err != nil {
		return nil, err
	}
	offsets := readLoca(loca, locaFmt, numGlyphs)

	// Build keep set: GIDs for all used codepoints
	keep := map[uint16]bool{0: true} // always include .notdef
	maxCID := 0
	for r := range used {
		cp := int(r)
		if gid, ok := f.CharToGID[cp]; ok {
			keep[gid] = true
		}
		if cp > maxCID {
			maxCID = cp
		}
	}

	// Add compound glyph components
	addCompounds(glyf, offsets, keep, numGlyphs)

	// Build subset TTF
	subData := subsetTTF(raw, tables, glyf, offsets, keep, numGlyphs, locaFmt)

	// Build CIDToGID binary map
	cidToGID := make([]byte, (maxCID+1)*2)
	for r := range used {
		cp := int(r)
		if gid, ok := f.CharToGID[cp]; ok && cp <= maxCID {
			putU16(cidToGID, cp*2, gid)
		}
	}

	// Build widths map restricted to used codepoints
	widths := make(map[int]int, len(used))
	for r := range used {
		cp := int(r)
		if w, ok := f.CIDWidths[cp]; ok {
			widths[cp] = w
		}
	}

	return &SubsetResult{
		Data:     subData,
		CIDToGID: cidToGID,
		Widths:   widths,
	}, nil
}

func readLoca(loca []byte, format int16, n int) []uint32 {
	off := make([]uint32, n+1)
	for i := range n + 1 {
		if format == 0 {
			off[i] = uint32(readU16(loca, i*2)) * 2
		} else {
			off[i] = readU32(loca, i*4)
		}
	}
	return off
}

// addCompounds recursively includes component glyph IDs used by compound glyphs.
func addCompounds(glyf []byte, offsets []uint32, keep map[uint16]bool, n int) {
	changed := true
	for changed {
		changed = false
		for gid := uint16(0); gid < uint16(n); gid++ {
			if !keep[gid] {
				continue
			}
			s, e := offsets[gid], offsets[gid+1]
			if s >= e || int(e) > len(glyf) {
				continue
			}
			d := glyf[s:e]
			if len(d) < 12 || readI16(d, 0) >= 0 {
				continue // simple glyph or too short
			}
			// Compound glyph — parse component records
			off := 10
			for {
				if off+4 > len(d) {
					break
				}
				flags := readU16(d, off)
				comp := readU16(d, off+2)
				if !keep[comp] {
					keep[comp] = true
					changed = true
				}
				off += 4
				if flags&0x0001 != 0 { // ARG_1_AND_2_ARE_WORDS
					off += 4
				} else {
					off += 2
				}
				if flags&0x0008 != 0 { // WE_HAVE_A_SCALE
					off += 2
				} else if flags&0x0040 != 0 { // WE_HAVE_AN_X_AND_Y_SCALE
					off += 4
				} else if flags&0x0080 != 0 { // WE_HAVE_A_TWO_BY_TWO
					off += 8
				}
				if flags&0x0020 == 0 { // no MORE_COMPONENTS
					break
				}
			}
		}
	}
}

// subsetTTF zeroes out unused glyphs in glyf/loca and rebuilds the TTF.
func subsetTTF(raw []byte, tables map[string]ttfTable, glyf []byte, offsets []uint32, keep map[uint16]bool, n int, locaFmt int16) []byte {
	var newGlyf []byte
	newOffsets := make([]uint32, n+1)
	for i := range n {
		newOffsets[i] = uint32(len(newGlyf))
		if keep[uint16(i)] {
			s, e := offsets[i], offsets[i+1]
			if s < e && int(e) <= len(glyf) {
				newGlyf = append(newGlyf, glyf[s:e]...)
				// Short loca format requires 2-byte aligned offsets
				if locaFmt == 0 && len(newGlyf)%2 != 0 {
					newGlyf = append(newGlyf, 0)
				}
			}
		}
	}
	newOffsets[n] = uint32(len(newGlyf))
	// Pad glyf to 4-byte boundary
	for len(newGlyf)%4 != 0 {
		newGlyf = append(newGlyf, 0)
	}

	// Build new loca
	var newLoca []byte
	if locaFmt == 0 {
		newLoca = make([]byte, (n+1)*2)
		for i := range n + 1 {
			putU16(newLoca, i*2, uint16(newOffsets[i]/2))
		}
	} else {
		newLoca = make([]byte, (n+1)*4)
		for i := range n + 1 {
			putU32(newLoca, i*4, newOffsets[i])
		}
	}

	return buildTTF(raw, tables, newGlyf, newLoca)
}

// buildTTF reassembles a TTF file, replacing glyf and loca with new data.
func buildTTF(raw []byte, tables map[string]ttfTable, newGlyf, newLoca []byte) []byte {
	// Sort tables by original offset to maintain file order
	var ts []ttfTable
	for _, t := range tables {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].offset < ts[j].offset })

	nt := len(ts)
	dirEnd := 12 + nt*16
	dataStart := (dirEnd + 3) &^ 3 // align to 4 bytes

	var data []byte
	dir := make([]byte, nt*16)

	for i, t := range ts {
		var td []byte
		switch t.tag {
		case "glyf":
			td = newGlyf
		case "loca":
			td = newLoca
		default:
			td = raw[t.offset : t.offset+t.length]
		}

		off := dataStart + len(data)
		rec := i * 16
		copy(dir[rec:], []byte(t.tag))
		putU32(dir, rec+4, tblChecksum(td))
		putU32(dir, rec+8, uint32(off))
		putU32(dir, rec+12, uint32(len(td)))

		data = append(data, td...)
		// Pad each table to 4-byte boundary
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}

	out := make([]byte, 0, dataStart+len(data))
	out = append(out, raw[:12]...) // offset table header
	out = append(out, dir...)
	for len(out) < dataStart {
		out = append(out, 0)
	}
	out = append(out, data...)
	return out
}

func tblChecksum(d []byte) uint32 {
	p := make([]byte, (len(d)+3)&^3)
	copy(p, d)
	var s uint32
	for i := 0; i < len(p); i += 4 {
		s += readU32(p, i)
	}
	return s
}

func extractPSName(nt []byte) string {
	if len(nt) < 6 {
		return ""
	}
	count := int(readU16(nt, 2))
	strOff := int(readU16(nt, 4))
	for i := range count {
		rec := 6 + i*12
		if rec+12 > len(nt) {
			break
		}
		if readU16(nt, rec) == 3 && readU16(nt, rec+6) == 6 {
			l := int(readU16(nt, rec+8))
			o := strOff + int(readU16(nt, rec+10))
			if o+l <= len(nt) {
				return decodeUTF16BE(nt[o : o+l])
			}
		}
	}
	return ""
}

func decodeUTF16BE(b []byte) string {
	var out []byte
	for i := 0; i+1 < len(b); i += 2 {
		if ch := readU16(b, i); ch < 128 {
			out = append(out, byte(ch))
		}
	}
	return string(out)
}
