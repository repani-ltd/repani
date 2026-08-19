// Package lz4s is LZ4s: the LZ4 sequence architecture with economics re-tuned
// for small texts. Token [W:1][L:3][M:4]; near matches (dist <=
// 256) cost 2 bytes and min match is 3.
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

	pos := 0
	for pos < len(src) {
		bestGain, bestLen, bestDist := 0, 0, 0
		maxLen := len(src) - pos
		lo := pos - 65536
		if lo < 0 {
			lo = 0
		}
		for cand := pos - 1; cand >= lo; cand-- {
			l := 0
			for l < maxLen && src[cand+l] == src[pos+l] {
				l++
			}
			if l < 3 {
				continue
			}
			dist := pos - cand
			cost := 2
			if dist > 256 {
				cost = 3
			}
			if l >= 17 {
				cost++ // length extension byte
			}
			if gain := l - cost; gain > bestGain {
				bestGain, bestLen, bestDist = gain, l, dist
			}
		}
		if bestGain > 0 {
			emit(bestLen, bestDist)
			pos += bestLen
		} else {
			lits = append(lits, src[pos])
			pos++
		}
	}
	if len(lits) > 0 {
		emit(0, 0)
	}
	return out
}

// Decompress decompresses into exactly dsize bytes.
func Decompress(comp []byte, dsize int) ([]byte, bool) {
	out := make([]byte, 0, dsize)
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
		start := len(out) - dist
		for j := 0; j < matchLen; j++ {
			out = append(out, out[start+j])
		}
	}
	if len(out) != dsize {
		return nil, false
	}
	return out, true
}
