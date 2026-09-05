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
// A stream may be made against a BASE: bytes both sides already
// hold, which the decoder places before its output so that a
// distance may reach into them. Delta and Undelta are Compress and
// Decompress with a base; with an empty base they are the same
// functions, and the stream is the same frame either way. The base
// is the caller's contract: a stream decoded against the wrong base
// is plausible garbage, which the decoder cannot tell, so a protocol
// that sends deltas names the base (a checksum of it) beside them.
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
// Compress is greedy: at every position it takes the match with the
// best gain over emitting its bytes as literals, nearest first on a
// tie, or emits a literal. Candidates come from a hash chain over
// three-byte prefixes, visited near to far, which is exactly the
// positions where a match can begin. It is meant for texts of a few
// bytes to a few KiB, where a page compresses in a fraction of a
// millisecond; it is not a bulk compressor. Its output is the
// canonical encoding: one parse rule, no tuning.
package lz4s

// Compress compresses src self-referentially (no base).
func Compress(src []byte) []byte { return Delta(nil, src) }

// Delta compresses src against base: the stream's distances may
// reach into base, which the decoder must hold. Delta(nil, src) is
// Compress(src), byte for byte.
func Delta(base, src []byte) []byte {
	var out, lits []byte
	all := src
	if len(base) > 0 {
		all = make([]byte, 0, len(base)+len(src))
		all = append(append(all, base...), src...)
	}
	start := len(base)
	src = all

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

	// The hash chain: head[h] is one past the latest position whose
	// three bytes hash to h (zero: none), prev[i] the same for the
	// position before i. The table has one entry per input byte,
	// rounded up: on a page the chains are real candidates, the
	// interiors of earlier blank runs, and a larger table buys
	// nothing.
	hashBits := 10
	for 1<<hashBits < len(src) && hashBits < 16 {
		hashBits++
	}
	head := make([]int32, 1<<hashBits)
	prev := make([]int32, len(src))
	hash := func(i int) uint32 {
		v := uint32(src[i])<<16 | uint32(src[i+1])<<8 | uint32(src[i+2])
		return (v * 2654435761) >> (32 - hashBits)
	}
	insert := func(i int) {
		if i+3 <= len(src) {
			h := hash(i)
			prev[i] = head[h]
			head[h] = int32(i + 1)
		}
	}
	// The stream is at most the input plus one token per 7 literals
	// and the run extensions; one allocation covers it.
	out = make([]byte, 0, len(src)-start+(len(src)-start)/7+16)
	lits = make([]byte, 0, min(len(src)-start, 256))

	// The base is in the chain before the first position of src.
	for i := 0; i < start; i++ {
		insert(i)
	}
	pos := start
	for pos < len(src) {
		bestGain, bestLen, bestDist := 0, 0, 0
		maxLen := len(src) - pos
		lo := pos - 65536
		if lo < 0 {
			lo = 0
		}
		first := 0
		if maxLen >= 3 {
			first = int(head[hash(pos)])
		}
		for cand := first - 1; cand >= lo && bestLen < maxLen; cand = int(prev[cand]) - 1 {
			// A candidate that differs at bestLen cannot beat the
			// best so far (a tie never wins, see the gain test).
			if src[cand+bestLen] != src[pos+bestLen] {
				continue
			}
			l := 0
			for l < maxLen && src[cand+l] == src[pos+l] {
				l++
			}
			if l < 3 {
				continue
			}
			// Cost of taking the match versus emitting its bytes as
			// literals: one token (the match closes the pending
			// literal run, so the bytes after it need a fresh token
			// that a longer literal run would not have), the offset,
			// and the exact run of match-ext bytes.
			dist := pos - cand
			cost := 2
			if dist > 256 {
				cost = 3
			}
			if l >= 17 {
				cost += 1 + (l-17)/255
			}
			if gain := l - cost; gain > bestGain {
				bestGain, bestLen, bestDist = gain, l, dist
			}
		}
		if bestGain > 0 {
			emit(bestLen, bestDist)
			for i := pos; i < pos+bestLen; i++ {
				insert(i)
			}
			pos += bestLen
		} else {
			lits = append(lits, src[pos])
			insert(pos)
			pos++
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
func Decompress(comp []byte, dsize int) (out []byte, ok bool) { return Undelta(nil, comp, dsize) }

// Undelta decompresses a stream made by Delta against the same
// base, into exactly dsize bytes. The base is placed before the
// output, so a distance may reach into it; the result excludes it.
func Undelta(base, comp []byte, dsize int) (out []byte, ok bool) {
	if dsize < 0 {
		return nil, false
	}
	out = append(make([]byte, 0, len(base)+dsize), base...)
	dsize += len(base)
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
	return out[len(base):], true
}
