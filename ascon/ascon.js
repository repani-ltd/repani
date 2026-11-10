// ascon.js -- Ascon (NIST SP 800-232) in JavaScript: Ascon-XOF128,
// Ascon-CXOF128 and Ascon-AEAD128, the twin of repani.com/ascon, which
// embeds it (JS) so a server serves it from the module that owns it.
// ES module; runs in browsers and any engine with BigInt and DataView.
// The known-answer vectors of testdata/ pin it to the Go package
// (js_test.go).
//
// State is five 64-bit words as BigInt. Byte parameters and results
// are Uint8Array.

const M64 = 0xFFFFFFFFFFFFFFFFn;

// Ascon-XOF128 IV, LE uint64 (xof.go).
const XOF_IV = 0x0000080000CC0003n;
// Ascon-CXOF128 IV, LE uint64 (cxof.go).
const CXOF_IV = 0x0000080000CC0004n;
const CXOF_MAX_CUSTOM = 256;
// Ascon-AEAD128 IV, LE uint64 (aead.go).
const AEAD_IV = 0x00001000808C0001n;

export const AEAD_KEY_SIZE = 16;
export const AEAD_NONCE_SIZE = 16;
export const AEAD_TAG_SIZE = 16;
const AEAD_RATE = 16;

const te = new TextEncoder();

function ld64(b, off) {
  return new DataView(b.buffer, b.byteOffset + off, 8).getBigUint64(0, true);
}
function st64(b, off, v) {
  new DataView(b.buffer, b.byteOffset + off, 8).setBigUint64(0, v & M64, true);
}
function rotr(x, n) {
  return ((x >> n) | (x << (64n - n))) & M64;
}

// round is one round of the Ascon-p permutation.
function asconRound(s, i) {
  s[2] ^= BigInt(0xf0 - i * 0x10 + i);
  s[0] ^= s[4]; s[4] ^= s[3]; s[2] ^= s[1];
  const t0 = (s[0] ^ M64) & s[1];
  const t1 = (s[1] ^ M64) & s[2];
  const t2 = (s[2] ^ M64) & s[3];
  const t3 = (s[3] ^ M64) & s[4];
  const t4 = (s[4] ^ M64) & s[0];
  s[0] ^= t1; s[1] ^= t2; s[2] ^= t3; s[3] ^= t4; s[4] ^= t0;
  s[1] ^= s[0]; s[0] ^= s[4]; s[3] ^= s[2]; s[2] ^= M64;
  s[0] ^= rotr(s[0], 19n) ^ rotr(s[0], 28n);
  s[1] ^= rotr(s[1], 61n) ^ rotr(s[1], 39n);
  s[2] ^= rotr(s[2], 1n) ^ rotr(s[2], 6n);
  s[3] ^= rotr(s[3], 10n) ^ rotr(s[3], 17n);
  s[4] ^= rotr(s[4], 7n) ^ rotr(s[4], 41n);
}
function p12(s) { for (let i = 0; i < 12; i++) asconRound(s, i); }
function p8(s) { for (let i = 4; i < 12; i++) asconRound(s, i); }

// xof computes Ascon-XOF128 over data and returns outLen bytes.
export function xof(data, outLen) {
  const s = [XOF_IV, 0n, 0n, 0n, 0n];
  p12(s);
  hashAbsorb(s, data);
  return hashSqueeze(s, outLen);
}

// cxof computes Ascon-CXOF128 over data under the customization string
// z (a string or bytes, at most 256 bytes) and returns outLen bytes.
export function cxof(z, data, outLen) {
  const zb = typeof z === "string" ? te.encode(z) : z;
  if (zb.length > CXOF_MAX_CUSTOM) throw new Error("ascon: customization string longer than 256 bytes");
  const s = [CXOF_IV, 0n, 0n, 0n, 0n];
  p12(s);
  // The customization is framed by its own bit length, then absorbed
  // exactly like a message; after that CXOF is XOF.
  s[0] ^= BigInt(zb.length) * 8n;
  p12(s);
  hashAbsorb(s, zb);
  hashAbsorb(s, data);
  return hashSqueeze(s, outLen);
}

// hashAbsorb absorbs data: 10* padding into 8-byte blocks, p12 after
// every block.
function hashAbsorb(s, data) {
  const plen = (data.length + 8) & ~7;
  const padded = new Uint8Array(plen);
  padded.set(data);
  padded[data.length] = 0x01;
  for (let off = 0; off < plen; off += 8) {
    s[0] ^= ld64(padded, off);
    p12(s);
  }
}

// hashSqueeze returns outLen bytes: 8 bytes of s[0] per p12.
function hashSqueeze(s, outLen) {
  const out = new Uint8Array(outLen);
  const buf = new Uint8Array(8);
  let pos = 0;
  while (pos < outLen) {
    st64(buf, 0, s[0]);
    const n = Math.min(outLen - pos, 8);
    out.set(buf.subarray(0, n), pos);
    pos += n;
    if (pos < outLen) p12(s);
  }
  return out;
}

// aeadSeal encrypts and authenticates pt with associated data ad,
// returning ciphertext||tag (pt.length + 16 bytes).
export function aeadSeal(key, nonce, pt, ad) {
  checkAEADArgs(key, nonce);
  const k0 = ld64(key, 0), k1 = ld64(key, 8);
  const s = [AEAD_IV, k0, k1, ld64(nonce, 0), ld64(nonce, 8)];
  p12(s);
  s[3] ^= k0; s[4] ^= k1;
  aeadAbsorbAD(s, ad);
  const out = new Uint8Array(pt.length + AEAD_TAG_SIZE);
  // Encrypt full 16-byte blocks.
  let pos = 0;
  while (pt.length - pos >= AEAD_RATE) {
    s[0] ^= ld64(pt, pos);
    s[1] ^= ld64(pt, pos + 8);
    st64(out, pos, s[0]);
    st64(out, pos + 8, s[1]);
    p8(s);
    pos += AEAD_RATE;
  }
  // Final partial block with 10* padding.
  const buf = new Uint8Array(AEAD_RATE);
  buf.set(pt.subarray(pos));
  buf[pt.length - pos] = 0x01;
  s[0] ^= ld64(buf, 0);
  s[1] ^= ld64(buf, 8);
  st64(buf, 0, s[0]);
  st64(buf, 8, s[1]);
  out.set(buf.subarray(0, pt.length - pos), pos);
  aeadFinalize(s, k0, k1, out, pt.length);
  return out;
}

// aeadOpen decrypts and authenticates ciphertext||tag. Returns the
// plaintext, or null if authentication fails.
export function aeadOpen(key, nonce, ciphertext, ad) {
  checkAEADArgs(key, nonce);
  if (ciphertext.length < AEAD_TAG_SIZE) return null;
  const ct = ciphertext.subarray(0, ciphertext.length - AEAD_TAG_SIZE);
  const tag = ciphertext.subarray(ciphertext.length - AEAD_TAG_SIZE);
  const k0 = ld64(key, 0), k1 = ld64(key, 8);
  const s = [AEAD_IV, k0, k1, ld64(nonce, 0), ld64(nonce, 8)];
  p12(s);
  s[3] ^= k0; s[4] ^= k1;
  aeadAbsorbAD(s, ad);
  const pt = new Uint8Array(ct.length);
  let pos = 0;
  while (ct.length - pos >= AEAD_RATE) {
    const c0 = ld64(ct, pos), c1 = ld64(ct, pos + 8);
    st64(pt, pos, s[0] ^ c0);
    st64(pt, pos + 8, s[1] ^ c1);
    s[0] = c0; s[1] = c1;
    p8(s);
    pos += AEAD_RATE;
  }
  const t = ct.length - pos;
  const keystream = new Uint8Array(AEAD_RATE);
  st64(keystream, 0, s[0]);
  st64(keystream, 8, s[1]);
  for (let i = 0; i < t; i++) pt[pos + i] = keystream[i] ^ ct[pos + i];
  const pad = new Uint8Array(AEAD_RATE);
  pad.set(pt.subarray(pos, pos + t));
  pad[t] = 0x01;
  s[0] ^= ld64(pad, 0);
  s[1] ^= ld64(pad, 8);
  const computed = new Uint8Array(AEAD_TAG_SIZE);
  aeadFinalize(s, k0, k1, computed, 0);
  // No-early-exit compare (JS offers no constant-time guarantee; this
  // avoids the obvious timing shortcut).
  let diff = 0;
  for (let i = 0; i < AEAD_TAG_SIZE; i++) diff |= computed[i] ^ tag[i];
  if (diff !== 0) return null;
  return pt;
}

function aeadAbsorbAD(s, ad) {
  if (ad && ad.length > 0) {
    let pos = 0;
    while (ad.length - pos >= AEAD_RATE) {
      s[0] ^= ld64(ad, pos);
      s[1] ^= ld64(ad, pos + 8);
      p8(s);
      pos += AEAD_RATE;
    }
    const buf = new Uint8Array(AEAD_RATE);
    buf.set(ad.subarray(pos));
    buf[ad.length - pos] = 0x01;
    s[0] ^= ld64(buf, 0);
    s[1] ^= ld64(buf, 8);
    p8(s);
  }
  // Domain separation between AD and plaintext.
  s[4] ^= 0x8000000000000000n;
}

function aeadFinalize(s, k0, k1, dst, off) {
  s[2] ^= k0; s[3] ^= k1;
  p12(s);
  s[3] ^= k0; s[4] ^= k1;
  st64(dst, off, s[3]);
  st64(dst, off + 8, s[4]);
}

function checkAEADArgs(key, nonce) {
  if (key.length !== AEAD_KEY_SIZE) throw new Error("ascon: AEAD key must be 16 bytes");
  if (nonce.length !== AEAD_NONCE_SIZE) throw new Error("ascon: AEAD nonce must be 16 bytes");
}
