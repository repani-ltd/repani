// lz4s.js: lz4s (repani.com/lz4s) in JavaScript, beside the Go one:
// the decoder and the compressor; the fixture test holds the two to
// the same answers. ES module, no dependencies.

// decompress decodes comp into exactly dsize bytes, and returns them
// as a Uint8Array, or null if the stream is malformed, truncated, or
// does not produce exactly dsize bytes.
export function decompress(comp, dsize) {
  if (dsize < 0) return null;
  const out = new Uint8Array(dsize);
  let n = 0, i = 0;
  const total = dsize;
  const ext = v => {
    for (;;) {
      if (i >= comp.length) return -1;
      const x = comp[i++];
      v += x;
      if (x !== 255) return v;
    }
  };
  while (i < comp.length) {
    const token = comp[i++];
    let l = (token >> 4) & 7;
    if (l === 7) { l = ext(l); if (l < 0) return null; }
    if (l) {
      if (i + l > comp.length || n + l > total) return null;
      out.set(comp.subarray(i, i + l), n);
      i += l; n += l;
    }
    const m = token & 0x0f;
    if (m === 0) continue;
    let ml = m + 2;
    let dist;
    if (token & 0x80) {
      if (i + 2 > comp.length) return null;
      dist = (comp[i] | (comp[i + 1] << 8)) + 1;
      i += 2;
    } else {
      if (i + 1 > comp.length) return null;
      dist = comp[i++] + 1;
    }
    if (m === 15) { ml = ext(17); if (ml < 0) return null; }
    if (dist > n || n + ml > total) return null;
    // byte-serial copy: correct for overlapping matches
    for (let j = n, k = n - dist; j < n + ml; j++, k++) out[j] = out[k];
    n += ml;
  }
  if (n !== total) return null;
  return out;
}

// compress is Go's lz4s.Compress: the same greedy parse over the same
// hash chain, so the fixture test can compare their streams. src is a
// Uint8Array; so is the result.
export function compress(src) {
  const n = src.length;
  const out = [];
  let lits = [];
  const ext = v => {
    for (; v >= 255; v -= 255) out.push(255);
    out.push(v);
  };
  const emit = (matchLen, dist) => {
    const l = lits.length;
    let token = (l >= 7 ? 7 : l) << 4;
    const wide = dist > 256;
    if (matchLen > 0) {
      token |= Math.min(matchLen - 2, 15);
      if (wide) token |= 0x80;
    }
    out.push(token);
    if (l >= 7) ext(l - 7);
    for (const x of lits) out.push(x);
    lits = [];
    if (matchLen > 0) {
      if (wide) out.push((dist - 1) & 0xff, (dist - 1) >> 8);
      else out.push(dist - 1);
      if (matchLen - 2 >= 15) ext(matchLen - 17);
    }
  };
  // The hash chain: head[h] is one past the latest position whose
  // three bytes hash to h (zero: none), prev[i] the same for the
  // position before i.
  let hashBits = 10;
  while (1 << hashBits < n && hashBits < 16) hashBits++;
  const head = new Int32Array(1 << hashBits);
  const prev = new Int32Array(n);
  const hash = i => Math.imul((src[i] << 16) | (src[i + 1] << 8) | src[i + 2], 2654435761 | 0) >>> (32 - hashBits);
  const insert = i => {
    if (i + 3 <= n) {
      const h = hash(i);
      prev[i] = head[h];
      head[h] = i + 1;
    }
  };
  let pos = 0;
  while (pos < n) {
    let bestGain = 0, bestLen = 0, bestDist = 0;
    const maxLen = n - pos;
    const lo = Math.max(pos - 65536, 0);
    const first = maxLen >= 3 ? head[hash(pos)] : 0;
    for (let cand = first - 1; cand >= lo && bestLen < maxLen; cand = prev[cand] - 1) {
      if (src[cand + bestLen] !== src[pos + bestLen]) continue;
      let l = 0;
      while (l < maxLen && src[cand + l] === src[pos + l]) l++;
      if (l < 3) continue;
      const dist = pos - cand;
      let cost = dist > 256 ? 3 : 2;
      if (l >= 17) cost += 1 + Math.floor((l - 17) / 255);
      const gain = l - cost;
      if (gain > bestGain) { bestGain = gain; bestLen = l; bestDist = dist; }
    }
    if (bestGain > 0) {
      emit(bestLen, bestDist);
      for (let i = pos; i < pos + bestLen; i++) insert(i);
      pos += bestLen;
    } else {
      lits.push(src[pos]);
      insert(pos);
      pos++;
    }
  }
  if (lits.length > 0) emit(0, 0);
  return Uint8Array.from(out);
}
