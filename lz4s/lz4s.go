// Package lz4s is LZ4s: the LZ4 sequence architecture with economics re-tuned
// for small texts. Token [W:1][L:3][M:4]; near matches (dist <=
// 256) cost 2 bytes and min match is 3.
//
// # Stream format
//
// A compressed stream is a sequence of sequences, each a run of
// literals followed by an optional back-reference:
//
//	token [lit-ext] literals [offset [match-ext]]
//
// token is one byte, msb first: W (bit 7) selects the offset width,
// L (bits 6-4) is the literal length, M (bits 3-0) the match length.
//
//   - L in 0..6 is the literal count; L = 7 means 7 plus lit-ext.
//   - M = 0 means no match, and W, offset and match-ext are absent.
//     Otherwise the match length is M+2 (3..16 for M in 1..14);
//     M = 15 means 17 plus match-ext.
//   - lit-ext and match-ext are 255-run extensions: a run of 0xFF
//     bytes terminated by one byte below 0xFF, the length being the
//     sum of all bytes (so 0 < 255 is one byte 0x00; 255 is 0xFF
//     0x00; 300 is 0xFF 0x2D).
//   - offset is the match distance minus 1: W = 0 stores one byte
//     (distance 1..256), W = 1 stores two bytes little-endian
//     (distance 1..65536). The distance must not exceed the bytes
//     output so far; overlapping copies (distance < length) are
//     byte-serial, as in LZ4.
//
// There is no header and no end marker: the decoder is given the
// exact decompressed size and accepts the stream iff it produces
// exactly that many bytes without running past either buffer. That
// is the only framing check; tokens that produce no output (0x00,
// or any L = 0, M = 0 token, including trailing ones) are permitted
// no-ops, so the mapping from bytes to output is many-to-one.
// The decoder is the wire contract; Compress emits one canonical
// encoding of it.
//
// # Compressor
//
// Compress parses optimally: a hash chain over three-byte prefixes
// visits, at every position, the places in the 64 KiB window where a
// match can begin, near to far, keeping each match that is longer
// than every nearer one; a dynamic programme over (position, pending
// literal run) then chooses the sequences that minimise the stream's
// bytes under the frame's exact costs. A match of 256 or more is
// taken whole, so a long run costs one scan, not one per cell. It is
// meant for texts of a few bytes to a few KiB, where a page
// compresses in well under a millisecond; it is not a bulk
// compressor. The bytes it emits are canonical for this parser; the
// decoder is the contract, and a better parser would be a new
// canonical encoding of the same frame.
package lz4s

// Compress compresses src self-referentially (no dictionary).
func Compress(src []byte) []byte {
	var out []byte
	var lits []byte

	emit := func(matchLen, dist int) {
		var token byte
		l := len(lits)
		if l >= 7 {
			token |= 7 << 4
		} else {
			token |= byte(l) << 4
		}
		m := 0
		if matchLen > 0 {
			m = matchLen - 2
			if m >= 15 {
				m = 15
			}
			token |= byte(m)
		}
		wide := dist > 256
		if matchLen > 0 && wide {
			token |= 0x80
		}
		out = append(out, token)
		if l >= 7 {
			v := l - 7
			for v >= 255 {
				out = append(out, 255)
				v -= 255
			}
			out = append(out, byte(v))
		}
		out = append(out, lits...)
		lits = lits[:0]
		if matchLen > 0 {
			if wide {
				out = append(out, byte(dist-1), byte((dist-1)>>8))
			} else {
				out = append(out, byte(dist-1))
			}
			if matchLen-2 >= 15 {
				v := matchLen - 17
				for v >= 255 {
					out = append(out, 255)
					v -= 255
				}
				out = append(out, byte(v))
			}
		}
	}

	for _, sq := range parse(src) {
		lits = append(lits, src[sq.pos:sq.pos+sq.lits]...)
		if sq.l > 0 {
			emit(sq.l, sq.dist)
		}
	}
	if len(lits) > 0 {
		emit(0, 0)
	}
	return out
}

// Decompress decompresses comp into exactly dsize bytes; ok is
// false if comp is malformed, truncated, or does not produce exactly
// dsize bytes (see the package doc for what is accepted).
func Decompress(comp []byte, dsize int) (out []byte, ok bool) {
	if dsize < 0 {
		return nil, false
	}
	out = make([]byte, 0, dsize)
	i := 0
	for i < len(comp) {
		token := comp[i]
		i++
		l := int(token >> 4 & 7)
		if l == 7 {
			for {
				if i >= len(comp) {
					return nil, false
				}
				b := comp[i]
				i++
				l += int(b)
				if b != 255 {
					break
				}
			}
		}
		if l > 0 {
			if i+l > len(comp) || len(out)+l > dsize {
				return nil, false
			}
			out = append(out, comp[i:i+l]...)
			i += l
		}
		m := int(token & 0x0F)
		if m == 0 {
			continue
		}
		matchLen := m + 2
		var dist int
		if token&0x80 != 0 {
			if i+2 > len(comp) {
				return nil, false
			}
			dist = int(comp[i]) | int(comp[i+1])<<8
			i += 2
		} else {
			if i+1 > len(comp) {
				return nil, false
			}
			dist = int(comp[i])
			i++
		}
		dist++
		if m == 15 {
			matchLen = 17
			for {
				if i >= len(comp) {
					return nil, false
				}
				b := comp[i]
				i++
				matchLen += int(b)
				if b != 255 {
					break
				}
			}
		}
		if dist > len(out) || len(out)+matchLen > dsize {
			return nil, false
		}
		// Byte-serial forward copy: correct for overlapping matches.
		n := len(out)
		out = out[:n+matchLen]
		for j, k := n, n-dist; j < n+matchLen; j, k = j+1, k+1 {
			out[j] = out[k]
		}
	}
	if len(out) != dsize {
		return nil, false
	}
	return out, true
}

// A sequence is the parser's unit: lits literals from pos, then a
// match of length l at distance dist (l == 0: literals only).
type sequence struct{ pos, lits, l, dist int }

// litExt is the literal count at which the token overflows into an
// extension byte; matchExt the match length at which M does.
const (
	litExt   = 7
	matchExt = 17
	runCap   = 8 // pending literal runs beyond this cost alike (the 255-run extension is ignored)
	minMatch = 3
	window   = 65536
	niceLen  = 256 // a match this long is taken whole: the positions it covers are not searched
)

// cost is the bytes a match of length l at distance dist adds to
// the stream: its token, its offset and its extension.
func cost(l, dist int) int {
	c := 2
	if dist > 256 {
		c = 3
	}
	if l >= matchExt {
		c += 1 + (l-matchExt)/255
	}
	return c
}

// parse chooses the sequences of src that minimise the stream.
func parse(src []byte) []sequence {
	n := len(src)
	if n == 0 {
		return nil
	}
	// candidates[i]: the matches from i worth considering, scanning
	// the window from near to far and keeping a match only when it is
	// longer than every nearer one (a farther match no longer than a
	// nearer one costs the same or more for nothing). The list is
	// therefore strictly increasing in length, and short.
	// Positions are visited through a hash chain over three-byte
	// prefixes, near to far: exactly the positions where a match of
	// minMatch or more can begin.
	type cand struct{ l, dist int }
	cands := make([][]cand, n)
	const hashBits = 14
	head := make([]int32, 1<<hashBits)
	prev := make([]int32, n)
	for i := range head {
		head[i] = -1
	}
	hash := func(i int) uint32 {
		v := uint32(src[i])<<16 | uint32(src[i+1])<<8 | uint32(src[i+2])
		return (v * 2654435761) >> (32 - hashBits)
	}
	skipTo := 0 // positions before this are inside a nice match
	for i := range n {
		maxLen := n - i
		if maxLen < minMatch {
			break
		}
		h := hash(i)
		if i < skipTo {
			prev[i] = head[h]
			head[h] = int32(i)
			continue
		}
		lo := max(i-window, 0)
		var best []cand
		longest := minMatch - 1
		for c := int(head[h]); c >= lo && longest < maxLen; c = int(prev[c]) {
			// cannot beat the longest so far: differs where it would
			// have to match
			if src[c+longest] != src[i+longest] {
				continue
			}
			l := 0
			for l < maxLen && src[c+l] == src[i+l] {
				l++
			}
			if l > longest {
				best = append(best, cand{l, i - c})
				longest = l
			}
		}
		cands[i] = best
		if longest >= niceLen {
			skipTo = i + longest
		}
		prev[i] = head[h]
		head[h] = int32(i)
	}
	// best[i][r]: the least bytes that reach position i with r pending
	// literals; from[i][r] the step that did.
	type step struct{ run, l, dist int }
	const inf = 1 << 30
	best := make([][runCap + 1]int, n+1)
	from := make([][runCap + 1]step, n+1)
	for i := range best {
		for r := range best[i] {
			best[i][r] = inf
		}
	}
	best[0][0] = 0
	for i := 0; i < n; i++ {
		for r := 0; r <= runCap; r++ {
			c := best[i][r]
			if c == inf {
				continue
			}
			// a literal: one byte, and the extension byte when the run
			// reaches litExt
			nr, lc := r+1, 1
			if nr == litExt {
				lc++
			}
			nr = min(nr, runCap)
			if c+lc < best[i+1][nr] {
				best[i+1][nr] = c + lc
				from[i+1][nr] = step{r, 0, 0}
			}
			// a match: the candidate's full length, and every shorter
			// length below the extension, which may end at a better place
			for _, cd := range cands[i] {
				try := func(l int) {
					mc := cost(l, cd.dist)
					if c+mc < best[i+l][0] {
						best[i+l][0] = c + mc
						from[i+l][0] = step{r, l, cd.dist}
					}
				}
				try(cd.l)
				for l := minMatch; l < cd.l && l <= matchExt; l++ {
					try(l)
				}
			}
		}
	}
	// the end: pending literals need a closing token
	endR, endC := 0, inf
	for r := 0; r <= runCap; r++ {
		c := best[n][r]
		if r > 0 {
			c++
		}
		if c < endC {
			endC, endR = c, r
		}
	}
	// walk back, then forward into sequences
	var steps []step
	for i, r := n, endR; i > 0; {
		st := from[i][r]
		steps = append(steps, st)
		if st.l == 0 {
			i--
		} else {
			i -= st.l
		}
		r = st.run
	}
	var seqs []sequence
	pos, lits := 0, 0
	for k := len(steps) - 1; k >= 0; k-- {
		st := steps[k]
		if st.l == 0 {
			lits++
			continue
		}
		seqs = append(seqs, sequence{pos, lits, st.l, st.dist})
		pos += lits + st.l
		lits = 0
	}
	if lits > 0 {
		seqs = append(seqs, sequence{pos, lits, 0, 0})
	}
	return seqs
}
