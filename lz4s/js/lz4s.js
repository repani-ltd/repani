// lz4s.js: the lz4s decoder (repani.com/lz4s), the JavaScript
// implementation beside the Go one; the fixture test holds the two to
// the same answers. ES module, no dependencies.

// undelta decodes comp, a stream made against base (a Uint8Array, or
// null for none), into exactly dsize bytes, and returns them as a
// Uint8Array, or null if the stream is malformed, truncated, or does
// not produce exactly dsize bytes. A wrong base decodes to wrong
// bytes: the caller names the base.
export function undelta(base, comp, dsize) {
  const b = base ? base.length : 0;
  if (dsize < 0) return null;
  const out = new Uint8Array(b + dsize);
  if (b) out.set(base, 0);
  let n = b, i = 0;
  const total = b + dsize;
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
  return b ? out.subarray(b) : out;
}

// decompress is undelta with no base.
export const decompress = (comp, dsize) => undelta(null, comp, dsize);
