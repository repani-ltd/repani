// kv.js -- ASCON-KV v1 protocol client in a single JavaScript file.
//
// A faithful JS implementation of the layers defined by PROTOCOL.txt
// and CLIENT.txt, mirroring the Go packages ascon, vault, wire, store,
// and client. ES module; runs in browsers and Node (needs BigInt,
// DataView, and globalThis.crypto.getRandomValues).
//
// The full client protocol is implemented: REGISTER, and PROBE / PULL
// / PUSH reconciliation with the reference client's fast-path push and
// windowed walk, plus local Put/Delete writes and the merged live
// view. One deliberate difference from the Go client: registration is
// reactive when no register key is installed -- a keyless client syncs
// optimistically against an already-registered vault instead of
// failing, and only errors if the server answers UNKNOWN_VAULT
// (PROTOCOL.txt blesses REGISTER-on-UNKNOWN_VAULT as the recovery).
//
// All integers on the wire are little-endian. Byte parameters and
// results are Uint8Array unless stated otherwise; ts_ms values are
// BigInt (they are uint64 on the wire).

// ---------------------------------------------------------------------
// ASCON primitives (NIST SP 800-232): Ascon-XOF128 and Ascon-AEAD128.
// State is five 64-bit words as BigInt.

const M64 = 0xFFFFFFFFFFFFFFFFn;

// Ascon-XOF128 IV, LE uint64 (ascon/xof.go).
const XOF_IV = 0x0000080000CC0003n;
// Ascon-AEAD128 IV, LE uint64 (ascon/aead.go).
const AEAD_IV = 0x00001000808C0001n;

export const AEAD_KEY_SIZE = 16;
export const AEAD_NONCE_SIZE = 16;
export const AEAD_TAG_SIZE = 16;
const AEAD_RATE = 16;

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
  // Absorb: 10* padding into 8-byte blocks, p12 after every block.
  const plen = (data.length + 8) & ~7;
  const padded = new Uint8Array(plen);
  padded.set(data);
  padded[data.length] = 0x01;
  for (let off = 0; off < plen; off += 8) {
    s[0] ^= ld64(padded, off);
    p12(s);
  }
  // Squeeze: 8 bytes of s[0] per p12.
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
  if (key.length !== AEAD_KEY_SIZE) throw new Error("kv: AEAD key must be 16 bytes");
  if (nonce.length !== AEAD_NONCE_SIZE) throw new Error("kv: AEAD nonce must be 16 bytes");
}

// ---------------------------------------------------------------------
// Key derivation and nonces (vault/keys.go, vault/nonce.go).

export const ROOT_KEY_SIZE = 20;
export const REGISTER_KEY_SIZE = 16;
export const VAULT_ID_SIZE = 16;
export const ENTRY_ID_SIZE = 16;
export const NONCE_SIZE = 16;

const te = new TextEncoder();

function concatBytes(...parts) {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let off = 0;
  for (const p of parts) { out.set(p, off); off += p.length; }
  return out;
}

// derive16 computes XOF(dom || material, 16). dom is the 8-char ASCII
// domain separator from PROTOCOL.txt.
function derive16(dom, material) {
  return xof(concatBytes(te.encode(dom), material), 16);
}

export function deriveAEADKey(rootKey) { return derive16("kv/aead/", rootKey); }
export function deriveVaultID(rootKey) { return derive16("kv/vid1/", rootKey); }
export function deriveAuthKey(rootKey) { return derive16("kv/auth/", rootKey); }
export function deriveC2S(authKey) { return derive16("kv/c2s0/", authKey); }
export function deriveS2C(authKey) { return derive16("kv/s2c0/", authKey); }
export function deriveRegKey(registerKey) { return derive16("kv/reg0/", registerKey); }

// hedgedNonce derives a 16-byte nonce from a domain, optional context,
// a high-resolution timestamp, and 16 fresh random bytes.
function hedgedNonce(dom, context) {
  const r = new Uint8Array(16);
  globalThis.crypto.getRandomValues(r);
  const ts = new Uint8Array(8);
  // Fill the low nanosecond bits from a sub-millisecond monotonic clock so
  // the timestamp advances within a single millisecond. Date.now() alone
  // has ms granularity, which would defeat the RNG-failure hedge that
  // PROTOCOL.txt §Nonces relies on for entry nonces (a collision there
  // leaks the XOR of two secret values). random16 stays the primary guard.
  const frac = (typeof performance !== "undefined" && performance.now) ? performance.now() : 0;
  const subNs = BigInt(Math.floor((frac - Math.floor(frac)) * 1e6)); // 0..999999
  st64(ts, 0, BigInt(Date.now()) * 1000000n + subNs);
  return xof(concatBytes(te.encode(dom), context, ts, r), 16);
}

// frameNonce returns a hedged frame nonce bound to the vault.
export function frameNonce(vaultID) { return hedgedNonce("kv/rnon/", vaultID); }
// entryNonce returns a hedged entry nonce.
export function entryNonce() { return hedgedNonce("kv/enon/", new Uint8Array(0)); }

// ---------------------------------------------------------------------
// Entries (vault/entry.go). An entry is { tsMs: BigInt, nonce:
// Uint8Array(16), ct: Uint8Array }; ct includes the 16-byte tag.

export const ENTRY_HEADER_SIZE = 8 + NONCE_SIZE + 2;
export const MAX_ENTRY_BYTES = 64 * 1024;

export function entrySize(e) { return ENTRY_HEADER_SIZE + e.ct.length; }

// entryHeader returns the 26-byte header used as AEAD associated data.
export function entryHeader(e) {
  const h = new Uint8Array(ENTRY_HEADER_SIZE);
  st64(h, 0, e.tsMs);
  h.set(e.nonce, 8);
  new DataView(h.buffer).setUint16(8 + NONCE_SIZE, e.ct.length, true);
  return h;
}

// marshalEntry serializes e: ts_ms(8) || nonce(16) || ct_len(2) || ct.
export function marshalEntry(e) {
  const out = new Uint8Array(entrySize(e));
  out.set(entryHeader(e), 0);
  out.set(e.ct, ENTRY_HEADER_SIZE);
  return out;
}

// unmarshalEntry parses one entry at b[off], returning {entry,
// consumed}. Throws on truncation.
export function unmarshalEntry(b, off = 0) {
  if (b.length - off < ENTRY_HEADER_SIZE) throw new Error("kv: entry bytes shorter than header");
  const tsMs = ld64(b, off);
  const nonce = b.slice(off + 8, off + 8 + NONCE_SIZE);
  const ctLen = new DataView(b.buffer, b.byteOffset + off + 8 + NONCE_SIZE, 2).getUint16(0, true);
  const total = ENTRY_HEADER_SIZE + ctLen;
  if (b.length - off < total) throw new Error("kv: entry ciphertext length mismatch");
  const ct = b.slice(off + ENTRY_HEADER_SIZE, off + total);
  return { entry: { tsMs, nonce, ct }, consumed: total };
}

// entryID computes the content address: XOF("kv/eid1/" || entry, 16).
export function entryID(e) {
  return xof(concatBytes(te.encode("kv/eid1/"), marshalEntry(e)), 16);
}

// sealEntry encrypts (key, value) into a fresh entry with timestamp
// tsMs (BigInt) and a hedged nonce. An empty or absent value denotes a
// tombstone. Plaintext: key_len(1) || key || value_len(2) || value.
export function sealEntry(aeadKey, tsMs, key, value) {
  value = value || new Uint8Array(0);
  if (key.length === 0) throw new Error("kv: key must be non-empty");
  if (key.length > 0xFF) throw new Error("kv: key exceeds 255 bytes");
  if (value.length > 0xFFFF) throw new Error("kv: value exceeds 65535 bytes");
  const pt = new Uint8Array(1 + key.length + 2 + value.length);
  pt[0] = key.length;
  pt.set(key, 1);
  new DataView(pt.buffer).setUint16(1 + key.length, value.length, true);
  pt.set(value, 1 + key.length + 2);
  const ctLen = pt.length + AEAD_TAG_SIZE;
  if (ctLen > 0xFFFF || ENTRY_HEADER_SIZE + ctLen > MAX_ENTRY_BYTES) {
    throw new Error("kv: entry exceeds MAX_ENTRY_BYTES");
  }
  const nonce = entryNonce();
  const ad = new Uint8Array(ENTRY_HEADER_SIZE);
  st64(ad, 0, tsMs);
  ad.set(nonce, 8);
  new DataView(ad.buffer).setUint16(8 + NONCE_SIZE, ctLen, true);
  const ct = aeadSeal(aeadKey, nonce, pt, ad);
  return { tsMs, nonce, ct };
}

// openEntry decrypts e and returns {key, value}, or null if the seal
// does not open. An empty value denotes a tombstone. Throws only on a
// plaintext that opened but is malformed.
export function openEntry(aeadKey, e) {
  const pt = aeadOpen(aeadKey, e.nonce, e.ct, entryHeader(e));
  if (pt === null) return null;
  if (pt.length < 3) throw new Error("kv: decrypted plaintext is malformed");
  const klen = pt[0];
  if (1 + klen + 2 > pt.length) throw new Error("kv: decrypted plaintext is malformed");
  const vlen = new DataView(pt.buffer, pt.byteOffset + 1 + klen, 2).getUint16(0, true);
  if (1 + klen + 2 + vlen !== pt.length) throw new Error("kv: decrypted plaintext is malformed");
  return { key: pt.slice(1, 1 + klen), value: pt.slice(1 + klen + 2) };
}

// ---------------------------------------------------------------------
// Canonical order and set hash (vault/setsync.go).

function bytesCompare(a, b) {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) {
    if (a[i] !== b[i]) return a[i] < b[i] ? -1 : 1;
  }
  return a.length - b.length;
}

// posAfter reports whether canonical position (ts, id) is strictly
// after (baseTs, baseID).
export function posAfter(ts, id, baseTs, baseID) {
  if (ts !== baseTs) return ts > baseTs;
  return bytesCompare(id, baseID) > 0;
}

// sortCanonical sorts [{entry, id}] pairs in place by (tsMs asc,
// entryID asc), the protocol's canonical order.
export function sortCanonical(pairs) {
  pairs.sort((a, b) => {
    if (a.entry.tsMs !== b.entry.tsMs) return a.entry.tsMs < b.entry.tsMs ? -1 : 1;
    return bytesCompare(a.id, b.id);
  });
}

// setHash returns the 16-byte fingerprint of an entry set:
// XOF("kv/sset/" || u32(N) || concat(entry_ids in canonical order), 16).
// sortedPairs must be [{entry, id}] in canonical order.
export function setHash(sortedPairs) {
  const buf = new Uint8Array(8 + 4 + sortedPairs.length * ENTRY_ID_SIZE);
  buf.set(te.encode("kv/sset/"), 0);
  new DataView(buf.buffer).setUint32(8, sortedPairs.length, true);
  for (let i = 0; i < sortedPairs.length; i++) {
    buf.set(sortedPairs[i].id, 12 + i * ENTRY_ID_SIZE);
  }
  return xof(buf, 16);
}

// ---------------------------------------------------------------------
// Wire (wire/wire.go): the single-envelope frame and its payloads.

export const VERSION = 0x01;
export const FRAME_HEADER_SIZE = 1 + 1 + VAULT_ID_SIZE + NONCE_SIZE;

export const KIND_REGISTER = 0x01;
export const KIND_SYNC = 0x02;
export const KIND_RESP = 0x03;

export const OP_PROBE = 0x00;
export const OP_PULL = 0x01;
export const OP_PUSH = 0x02;

export const STATUS_OK = 0x00;
export const STATUS_DIFFER = 0x01;
export const STATUS_QUOTA = 0x02;
export const STATUS_BAD_REQUEST = 0x03;

export const ERR_BAD_FRAME = 0xE0;
export const ERR_BAD_VERSION = 0xE1;
export const ERR_BAD_KIND = 0xE2;
export const ERR_UNKNOWN_VAULT = 0xE3;
export const ERR_BAD_AUTH = 0xE4;
export const ERR_TRY_LATER = 0xE5;

export const CURSOR_SIZE = 8 + ENTRY_ID_SIZE;
export const PAGE_BYTES = 256 * 1024;
export const MAX_FRAME_BYTES = 1 << 20;

function buildFrameHeader(kind, vaultID, nonce) {
  const h = new Uint8Array(FRAME_HEADER_SIZE);
  h[0] = VERSION;
  h[1] = kind;
  h.set(vaultID, 2);
  h.set(nonce, 2 + VAULT_ID_SIZE);
  return h;
}

// sealFrame builds a full frame: the 34-byte cleartext header followed
// by AEAD(channelKey, nonce, ad=header, pt=payload).
export function sealFrame(kind, vaultID, nonce, channelKey, payload) {
  const header = buildFrameHeader(kind, vaultID, nonce);
  return concatBytes(header, aeadSeal(channelKey, nonce, payload, header));
}

// parseFrameHeader splits a frame into its cleartext header fields and
// the trailing sealed bytes, validating only length and version.
export function parseFrameHeader(b) {
  if (b.length < FRAME_HEADER_SIZE + AEAD_TAG_SIZE) throw new Error("kv: truncated frame");
  if (b[0] !== VERSION) throw new Error("kv: unsupported frame version");
  return {
    kind: b[1],
    vaultID: b.slice(2, 2 + VAULT_ID_SIZE),
    nonce: b.slice(2 + VAULT_ID_SIZE, FRAME_HEADER_SIZE),
    sealed: b.subarray(FRAME_HEADER_SIZE),
  };
}

// openFrame opens sealed under channelKey, reconstructing the AD from
// the header fields. Returns the payload or null if the seal does not
// open.
export function openFrame(channelKey, kind, vaultID, nonce, sealed) {
  return aeadOpen(channelKey, nonce, sealed, buildFrameHeader(kind, vaultID, nonce));
}

// marshalSync encodes a SYNC payload from {op, setHash} (PROBE),
// {op, curTs, curID} (PULL), or {op, entries} (PUSH).
export function marshalSync(req) {
  switch (req.op) {
    case OP_PROBE: {
      const out = new Uint8Array(1 + 16);
      out[0] = OP_PROBE;
      out.set(req.setHash, 1);
      return out;
    }
    case OP_PULL: {
      const out = new Uint8Array(1 + CURSOR_SIZE);
      out[0] = OP_PULL;
      st64(out, 1, req.curTs);
      out.set(req.curID, 9);
      return out;
    }
    case OP_PUSH:
      return concatBytes(new Uint8Array([OP_PUSH]), marshalEntryList(req.entries));
    default:
      throw new Error("kv: unknown sync op");
  }
}

// marshalEntryList encodes push_count(4) || entries.
function marshalEntryList(entries) {
  const parts = [new Uint8Array(4)];
  new DataView(parts[0].buffer).setUint32(0, entries.length, true);
  for (const e of entries) parts.push(marshalEntry(e));
  return concatBytes(...parts);
}

// parseEntryList decodes push_count(4) || entries, rejecting trailing
// bytes.
function parseEntryList(b) {
  if (b.length < 4) throw new Error("kv: malformed payload");
  const n = new DataView(b.buffer, b.byteOffset, 4).getUint32(0, true);
  const out = [];
  let off = 4;
  for (let i = 0; i < n; i++) {
    const { entry, consumed } = unmarshalEntry(b, off);
    out.push(entry);
    off += consumed;
  }
  if (off !== b.length) throw new Error("kv: trailing bytes after entries");
  return out;
}

// parseResp decodes a RESP payload into {status, reqNonce, entries}.
export function parseResp(payload) {
  if (payload.length < 1 + NONCE_SIZE) throw new Error("kv: malformed payload");
  return {
    status: payload[0],
    reqNonce: payload.slice(1, 1 + NONCE_SIZE),
    entries: parseEntryList(payload.subarray(1 + NONCE_SIZE)),
  };
}

// statusText labels a sealed status for errors and logs.
export function statusText(s) {
  switch (s) {
    case STATUS_OK: return "ok";
    case STATUS_DIFFER: return "differ";
    case STATUS_QUOTA: return "quota";
    case STATUS_BAD_REQUEST: return "bad-request";
    default: return "status(0x" + s.toString(16).padStart(2, "0") + ")";
  }
}

// preChannelText labels a pre-channel error byte.
export function preChannelText(b) {
  switch (b) {
    case ERR_BAD_FRAME: return "bad-frame";
    case ERR_BAD_VERSION: return "bad-version";
    case ERR_BAD_KIND: return "bad-kind";
    case ERR_UNKNOWN_VAULT: return "unknown-vault";
    case ERR_BAD_AUTH: return "bad-auth";
    case ERR_TRY_LATER: return "try-later";
    default: return "pre-channel(0x" + b.toString(16).padStart(2, "0") + ")";
  }
}

// ---------------------------------------------------------------------
// Store (store/store.go, store/memstore.go). A store is an unordered
// entry set keyed by entry id with a canonical-order view. The client
// awaits every store method, so an implementation may be async;
// MemStore is the synchronous reference and IDBStore the persistent
// IndexedDB-backed implementation.
//
// Contract: add(entry) -> bool (false if already present);
// has(idHex) -> bool; len() -> number; sortedPairs() -> [{entry, id}]
// in canonical order (callers must not mutate); setHash() ->
// Uint8Array(16); entriesAfter(afterTs, afterID, budget) -> [{entry,
// id}] strictly after the cursor in canonical order, accumulating
// until the next entry would exceed budget bytes but always at least
// one if any remain.

function idHex(id) {
  let s = "";
  for (let i = 0; i < id.length; i++) s += id[i].toString(16).padStart(2, "0");
  return s;
}

export class MemStore {
  constructor() {
    this.byID = new Map(); // idHex -> {entry, id}
    this.sorted = null;    // cached canonical [{entry, id}], null when stale
    this.hash = null;      // cached set hash, null when stale
  }
  add(entry, id = entryID(entry)) {
    const h = idHex(id);
    if (this.byID.has(h)) return false;
    this.byID.set(h, { entry, id });
    this.sorted = null;
    this.hash = null;
    return true;
  }
  has(h) { return this.byID.has(h); }
  len() { return this.byID.size; }
  sortedPairs() {
    if (this.sorted === null) {
      this.sorted = Array.from(this.byID.values());
      sortCanonical(this.sorted);
    }
    return this.sorted;
  }
  setHash() {
    if (this.hash === null) this.hash = setHash(this.sortedPairs());
    return this.hash;
  }
  entriesAfter(afterTs, afterID, budget) {
    const sorted = this.sortedPairs();
    // Binary search for the first position strictly after the cursor.
    let lo = 0, hi = sorted.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (posAfter(sorted[mid].entry.tsMs, sorted[mid].id, afterTs, afterID)) hi = mid;
      else lo = mid + 1;
    }
    const out = [];
    let used = 0;
    for (let i = lo; i < sorted.length; i++) {
      const sz = entrySize(sorted[i].entry);
      if (out.length > 0 && used + sz > budget) break;
      out.push(sorted[i]);
      used += sz;
    }
    return out;
  }
}

// IDBStore is the persistent Store: an IndexedDB object store of
// marshaled entry bytes keyed by entry id, mirrored into a MemStore
// that serves every read. This is the same split as the Go LogStore
// (durable log + in-memory index): IndexedDB provides durability,
// memory provides the canonical view. add() resolves only on the
// write transaction's completion, so the client's page-commit
// guarantee ("committed to the log before the cursor advances")
// holds across a killed tab, like the Go log's fsync.
//
// Open one database per vault -- name it by VaultID (e.g. "kv-" +
// hex of the id) so credentials never share a namespace. Adds are
// content-addressed and idempotent, so concurrent tabs on the same
// database cannot corrupt it, but each tab's in-memory view only
// sees its own session's entries until its next sync or reload.
export class IDBStore {
  // Internal; use IDBStore.open(name).
  constructor(db, mem) {
    this.db = db;
    this.mem = mem;
  }

  // open opens (creating if needed) the named database and loads
  // every persisted entry into memory.
  static async open(name) {
    const db = await new Promise((resolve, reject) => {
      const req = indexedDB.open(name, 1);
      req.onupgradeneeded = () => {
        if (!req.result.objectStoreNames.contains("entries")) {
          req.result.createObjectStore("entries");
        }
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    // Yield to a future schema upgrade from another context.
    db.onversionchange = () => db.close();
    const mem = new MemStore();
    await new Promise((resolve, reject) => {
      const tx = db.transaction("entries", "readonly");
      const cur = tx.objectStore("entries").openCursor();
      cur.onsuccess = () => {
        const c = cur.result;
        if (!c) return;
        // Skip a corrupt row rather than letting the throw stall the
        // cursor and silently resolve with a truncated store. A dropped
        // entry is re-fetched on the next sync; a truncated load is not.
        try {
          mem.add(unmarshalEntry(new Uint8Array(c.value)).entry);
        } catch (e) {
          if (typeof console !== "undefined") console.warn("kv: skipping corrupt stored entry:", e);
        }
        c.continue();
      };
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
    return new IDBStore(db, mem);
  }

  // add inserts the entry in memory and, if new, persists it before
  // resolving. One transaction per entry keeps the commit guarantee
  // simple; if a large first sync ever needs it, the refinement is an
  // addMany() batching one transaction per pulled page.
  async add(entry) {
    const id = entryID(entry);
    if (!this.mem.add(entry, id)) return false;
    const bytes = marshalEntry(entry);
    await new Promise((resolve, reject) => {
      const tx = this.db.transaction("entries", "readwrite");
      tx.objectStore("entries").put(bytes, idHex(id));
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
    return true;
  }

  has(h) { return this.mem.has(h); }
  len() { return this.mem.len(); }
  sortedPairs() { return this.mem.sortedPairs(); }
  setHash() { return this.mem.setHash(); }
  entriesAfter(afterTs, afterID, budget) { return this.mem.entriesAfter(afterTs, afterID, budget); }

  close() { this.db.close(); }
}

// ---------------------------------------------------------------------
// Value encoding (CLIENT.txt): value = fmt(1) || payload; a tombstone
// is the protocol's empty value with no fmt byte.

export const FMT_RAW = 0x00;

export function wrapValue(fmtTag, value) {
  const out = new Uint8Array(1 + value.length);
  out[0] = fmtTag;
  out.set(value, 1);
  return out;
}

export function parseValue(stored) {
  if (stored.length === 0) return { fmt: 0, value: null, tomb: true };
  return { fmt: stored[0], value: stored.subarray(1), tomb: false };
}

// ---------------------------------------------------------------------
// Client (client/client.go): local Put/Delete on the store, REGISTER,
// the full PROBE / PULL / PUSH sync, and the merged live view.

// defaultSyncWindowMs is the reconcile look-back: on a plain sync the
// client walks [frontier - window, inf).
const DEFAULT_SYNC_WINDOW_MS = 10 * 60 * 1000;

// maxPullPages is an absolute backstop against a server fabricating an
// endless entry stream; an honest walk ends with an empty page.
const MAX_PULL_PAGES = 1 << 20;

const MAX_ROUNDS = 64;

const ZERO_ID = new Uint8Array(ENTRY_ID_SIZE);

function preChannelError(stage, pre) {
  return new Error("kv: " + stage + ": server returned " + preChannelText(pre));
}

export class Client {
  // rootKey: Uint8Array(32). store: see the Store contract above.
  // transport: async (frameBytes) -> responseBytes.
  // opts.syncWindowMs overrides the reconcile look-back window.
  constructor(rootKey, store, transport, opts = {}) {
    if (rootKey.length !== ROOT_KEY_SIZE) throw new Error("kv: root key must be 20 bytes");
    this.aeadKey = deriveAEADKey(rootKey);
    this.vaultID = deriveVaultID(rootKey);
    this.authKey = deriveAuthKey(rootKey);
    this.c2s = deriveC2S(this.authKey);
    this.s2c = deriveS2C(this.authKey);
    this.store = store;
    this.transport = transport;
    this.windowMs = opts.syncWindowMs || DEFAULT_SYNC_WINDOW_MS;
    this.lastStats = null;
    this.trace = opts.trace || null; // optional (msg: string) -> void
    this.registered = false; // belief; embedders may persist and restore it
    this.hasReg = false;
    this.regChan = null; // K_reg, valid iff hasReg
    this.recent = []; // entries written since the last sync (fast-path push)
    this.lastTs = 0n;
  }

  tracef(msg) { if (this.trace) this.trace(msg); }

  // setRegisterKey installs the register-gate secret used to seal a
  // REGISTER frame. Pass null to clear.
  setRegisterKey(registerKey) {
    if (registerKey === null) {
      this.hasReg = false;
      this.regChan = null;
    } else {
      this.hasReg = true;
      this.regChan = deriveRegKey(registerKey);
    }
  }

  // --- local writes ---

  // nextTs returns a strictly-increasing millisecond timestamp.
  nextTs() {
    let now = BigInt(Date.now());
    if (now <= this.lastTs) now = this.lastTs + 1n;
    this.lastTs = now;
    return now;
  }

  // put writes (key, value) with the raw format tag; strings are UTF-8
  // encoded. The stored value is fmt(1)||value.
  async put(key, value) { return this.putFmt(key, value, FMT_RAW); }

  // putFmt writes (key, value) under an explicit CLIENT.txt format tag.
  async putFmt(key, value, fmtTag) {
    if (typeof key === "string") key = te.encode(key);
    if (typeof value === "string") value = te.encode(value);
    if (value.length === 0) throw new Error("kv: empty value is a tombstone; use delete()");
    await this.record(sealEntry(this.aeadKey, this.nextTs(), key, wrapValue(fmtTag, value)));
  }

  // delete writes a tombstone (an empty value, with no fmt tag).
  async delete(key) {
    if (typeof key === "string") key = te.encode(key);
    await this.record(sealEntry(this.aeadKey, this.nextTs(), key, null));
  }

  // record adds a locally-created entry to the store and remembers it
  // for the next sync's fast-path push.
  async record(e) {
    await this.store.add(e);
    this.recent.push(e);
  }

  // --- sync ---

  // sync reconciles the local store with the server. It registers if
  // needed (given a register key), then: fast path -- push exactly the
  // entries written since the last sync and PROBE; otherwise walk a
  // suffix window (PULL then dedup-tolerant PUSH), widening from a
  // recent window to a full walk across rounds until a PROBE matches.
  // Returns {roundTrips, reqBytes, respBytes, pushed, pushedBytes,
  // pulled, pulledBytes}.
  async sync() {
    const st = { roundTrips: 0, reqBytes: 0, respBytes: 0, pushed: 0, pushedBytes: 0, pulled: 0, pulledBytes: 0 };
    const recent = this.recent;
    this.recent = [];
    try {
      for (let round = 0; round < MAX_ROUNDS; round++) {
        if (!this.registered && this.hasReg) await this.register(st);

        // Fast path: push just-written entries before the first probe.
        if (round === 0 && recent.length > 0) {
          const pre = await this.pushEntries(recent, "PUSH(new)", st);
          if (pre === ERR_UNKNOWN_VAULT) { this.unknownVault(); continue; }
          if (pre !== 0) throw preChannelError("push", pre);
        }

        const p = await this.probe(st);
        if (p.pre === ERR_UNKNOWN_VAULT) { this.unknownVault(); continue; }
        if (p.pre !== 0) throw preChannelError("probe", p.pre);
        if (p.inSync) return st;

        const anchor = await this.reconcileAnchor(round);
        this.tracef("RECONCILE round " + round + ": window from ts=" + anchor.ts);

        let pre = await this.pullFrom(anchor.ts, anchor.id, st);
        if (pre === ERR_UNKNOWN_VAULT) { this.unknownVault(); continue; }
        if (pre !== 0) throw preChannelError("pull", pre);

        pre = await this.pushFrom(anchor.ts, anchor.id, st);
        if (pre === ERR_UNKNOWN_VAULT) { this.unknownVault(); continue; }
        if (pre !== 0) throw preChannelError("push", pre);
        // Loop back to PROBE to confirm convergence (and catch entries
        // a concurrent writer added mid-walk).
      }
      throw new Error("kv: sync did not converge within round budget");
    } finally {
      this.lastStats = st;
    }
  }

  // unknownVault handles a pre-channel UNKNOWN_VAULT: re-register on
  // the next round when a register key is available, otherwise fail.
  unknownVault() {
    if (!this.hasReg) throw new Error("kv: vault not registered on server and no register key installed");
    this.registered = false;
  }

  // register establishes the channel by adopting the client's AuthKey
  // on the server, sealed under K_reg.
  async register(st) {
    const { pre, resp } = await this.roundtrip(KIND_REGISTER, this.regChan, this.authKey, st);
    if (pre === ERR_BAD_AUTH) throw new Error("kv: registration rejected - wrong register key");
    if (pre !== 0) throw preChannelError("register", pre);
    if (resp.status !== STATUS_OK) throw new Error("kv: register status " + statusText(resp.status));
    this.tracef("REGISTER -> ok");
    this.registered = true;
  }

  // reconcileAnchor returns the walk anchor for a round: round 0 looks
  // back one window below the frontier; later rounds walk from zero.
  async reconcileAnchor(round) {
    if (round !== 0) return { ts: 0n, id: ZERO_ID };
    const sorted = await this.store.sortedPairs();
    if (sorted.length > 0) {
      const fTs = sorted[sorted.length - 1].entry.tsMs;
      const w = BigInt(this.windowMs);
      if (fTs > w) return { ts: fTs - w, id: ZERO_ID };
    }
    return { ts: 0n, id: ZERO_ID };
  }

  // probe returns {inSync, pre}: whether the set hashes match, or the
  // pre-channel error byte when the server sent one.
  async probe(st) {
    const payload = marshalSync({ op: OP_PROBE, setHash: await this.store.setHash() });
    const { pre, resp } = await this.roundtrip(KIND_SYNC, this.c2s, payload, st);
    if (pre !== 0) return { inSync: false, pre };
    switch (resp.status) {
      case STATUS_OK:
        this.tracef("PROBE -> in sync");
        return { inSync: true, pre: 0 };
      case STATUS_DIFFER:
        this.tracef("PROBE -> differ");
        return { inSync: false, pre: 0 };
      default:
        throw new Error("kv: probe status " + statusText(resp.status));
    }
  }

  // pullFrom walks the server's set from the anchor cursor, committing
  // each page to the store, and returns a pre-channel byte (0 = none).
  // Every returned entry is stored verbatim (even one that fails to
  // open) so the local set matches the server's. The cursor must
  // advance strictly on every page.
  async pullFrom(anchorTs, anchorID, st) {
    let curTs = anchorTs, curID = anchorID;
    for (let page = 0; ; page++) {
      if (page >= MAX_PULL_PAGES) throw new Error("kv: pull exceeded page budget (misbehaving server)");
      const payload = marshalSync({ op: OP_PULL, curTs, curID });
      const { pre, resp } = await this.roundtrip(KIND_SYNC, this.c2s, payload, st);
      if (pre !== 0) return pre;
      if (resp.status !== STATUS_OK) throw new Error("kv: pull status " + statusText(resp.status));
      if (resp.entries.length === 0) return 0;
      const last = resp.entries[resp.entries.length - 1];
      const lastID = entryID(last);
      if (!posAfter(last.tsMs, lastID, curTs, curID)) {
        throw new Error("kv: server pull did not advance the cursor");
      }
      let bytesIn = 0;
      for (const e of resp.entries) {
        // Enforce the entry-size invariant on ingest: an honest server
        // never holds an oversized entry, so a larger one is a hostile
        // or corrupt relay.
        if (entrySize(e) > MAX_ENTRY_BYTES) {
          throw new Error("kv: pulled entry exceeds MAX_ENTRY_BYTES");
        }
        bytesIn += entrySize(e);
        await this.store.add(e);
      }
      st.pulled += resp.entries.length;
      st.pulledBytes += bytesIn;
      this.tracef("PULL -> " + resp.entries.length + " entries (" + bytesIn + " B)");
      curTs = last.tsMs;
      curID = lastID;
    }
  }

  // pushFrom walks the client's own entries from the anchor cursor in
  // byte-bounded batches and pushes each; dedup-tolerant, the server
  // drops what it already holds. Returns a pre-channel byte (0 = none).
  async pushFrom(anchorTs, anchorID, st) {
    let curTs = anchorTs, curID = anchorID;
    for (;;) {
      const batch = await this.store.entriesAfter(curTs, curID, PAGE_BYTES);
      if (batch.length === 0) return 0;
      const pre = await this.sendPush(batch.map((p) => p.entry), "PUSH", st);
      if (pre !== 0) return pre;
      const last = batch[batch.length - 1];
      curTs = last.entry.tsMs;
      curID = last.id;
    }
  }

  // pushEntries pushes an explicit list of entries in byte-bounded
  // batches -- the fast path for freshly-written local entries.
  async pushEntries(entries, label, st) {
    let i = 0;
    while (i < entries.length) {
      const batch = [];
      let used = 0;
      while (i < entries.length) {
        const sz = entrySize(entries[i]);
        if (batch.length > 0 && used + sz > PAGE_BYTES) break;
        batch.push(entries[i]);
        used += sz;
        i++;
      }
      const pre = await this.sendPush(batch, label, st);
      if (pre !== 0) return pre;
    }
    return 0;
  }

  // sendPush pushes one batch, accounting and tracing it.
  async sendPush(batch, label, st) {
    const payload = marshalSync({ op: OP_PUSH, entries: batch });
    const { pre, resp } = await this.roundtrip(KIND_SYNC, this.c2s, payload, st);
    if (pre !== 0) return pre;
    if (resp.status !== STATUS_OK) throw new Error("kv: push status " + statusText(resp.status));
    let bytesOut = 0;
    for (const e of batch) bytesOut += entrySize(e);
    st.pushed += batch.length;
    st.pushedBytes += bytesOut;
    this.tracef(label + " -> " + batch.length + " entries (" + bytesOut + " B)");
    return 0;
  }

  // roundtrip seals a request frame under sealKey, sends it, and opens
  // the response under K_s2c, verifying the echoed request nonce
  // (anti-stitch). Returns {pre, resp}: a 1-byte response is an
  // unauthenticated pre-channel error surfaced in pre (resp null).
  async roundtrip(kind, sealKey, payload, st) {
    const nonce = frameNonce(this.vaultID);
    const frame = sealFrame(kind, this.vaultID, nonce, sealKey, payload);
    const respBytes = await this.transport(frame);
    st.roundTrips++;
    st.reqBytes += frame.length;
    st.respBytes += respBytes.length;
    if (respBytes.length === 1) {
      return { pre: respBytes[0], resp: null };
    }
    const h = parseFrameHeader(respBytes);
    if (h.kind !== KIND_RESP || bytesCompare(h.vaultID, this.vaultID) !== 0) {
      throw new Error("kv: unexpected response frame");
    }
    const rpayload = openFrame(this.s2c, KIND_RESP, h.vaultID, h.nonce, h.sealed);
    if (rpayload === null) throw new Error("kv: response did not open");
    const resp = parseResp(rpayload);
    if (bytesCompare(resp.reqNonce, nonce) !== 0) {
      throw new Error("kv: response nonce does not match request");
    }
    return { pre: 0, resp };
  }

  // --- reads (last-writer-wins merge, client/client.go §replay) ---

  // replay decrypts every entry in canonical order; later entries
  // override earlier ones. Entries that fail to open are silently
  // skipped (they stay in the store so the set still matches the
  // server's). Returns Map(latin1 key -> {key, value, fmt, ts,
  // tomb}); ts is the winning entry's ts_ms as BigInt.
  async replay() {
    const out = new Map();
    for (const { entry } of await this.store.sortedPairs()) {
      let kva;
      try {
        kva = openEntry(this.aeadKey, entry);
      } catch {
        continue;
      }
      if (kva === null) continue;
      const { fmt, value, tomb } = parseValue(kva.value);
      out.set(idHex(kva.key), { key: kva.key, value, fmt, ts: entry.tsMs, tomb });
    }
    return out;
  }

  // get returns {value, fmt, ts} for key (Uint8Array or string), or
  // null.
  async get(key) {
    if (typeof key === "string") key = te.encode(key);
    const kv = (await this.replay()).get(idHex(key));
    if (!kv || kv.tomb) return null;
    return { value: kv.value, fmt: kv.fmt, ts: kv.ts };
  }

  // list returns the live keys as Uint8Arrays, sorted bytewise.
  async list() {
    const out = [];
    for (const kv of (await this.replay()).values()) {
      if (!kv.tomb) out.push(kv.key);
    }
    out.sort(bytesCompare);
    return out;
  }

  // all returns every live pair as [{key, value, fmt, ts}], sorted by
  // key. fmt is the CLIENT.txt format tag; value has the tag
  // stripped; ts is the winning entry's ts_ms as BigInt.
  async all() {
    const out = [];
    for (const kv of (await this.replay()).values()) {
      if (!kv.tomb) out.push({ key: kv.key, value: kv.value, fmt: kv.fmt, ts: kv.ts });
    }
    out.sort((a, b) => bytesCompare(a.key, b.key));
    return out;
  }
}

// ---------------------------------------------------------------------
// Transport (client/transport.go): POST each sealed frame to
// <base>/v1/sync. Returns an async (frame) -> Uint8Array. Clients MUST
// set a request timeout and cap each response body (PROTOCOL.txt
// §Transport); opts.timeoutMs defaults to 60s.

export function httpTransport(base, opts = {}) {
  const url = base.replace(/\/+$/, "") + "/v1/sync";
  const timeoutMs = opts.timeoutMs || 60000;
  const fetchFn = opts.fetch || globalThis.fetch;
  return async (frame) => {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), timeoutMs);
    try {
      const resp = await fetchFn(url, {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream" },
        body: frame,
        signal: ctl.signal,
      });
      if (!resp.ok) throw new Error("kv: server returned HTTP " + resp.status);
      // Reject an over-cap body by its declared length before buffering it.
      const clen = resp.headers.get("content-length");
      if (clen !== null && Number(clen) > MAX_FRAME_BYTES) {
        throw new Error("kv: response exceeds frame cap");
      }
      const body = new Uint8Array(await resp.arrayBuffer());
      if (body.length > MAX_FRAME_BYTES) throw new Error("kv: response exceeds frame cap");
      return body;
    } finally {
      clearTimeout(timer);
    }
  };
}

// ---------------------------------------------------------------------
// Pairing (vault/pairing.go): RootKey as 32 Crockford base32 chars
// (8 groups of 4, no padding bits).

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const CROCKFORD_DEC = (() => {
  const m = new Int8Array(128).fill(-1);
  for (let i = 0; i < CROCKFORD.length; i++) {
    m[CROCKFORD.charCodeAt(i)] = i;
    m[CROCKFORD.toLowerCase().charCodeAt(i)] = i;
  }
  // Crockford aliases.
  m["O".charCodeAt(0)] = 0; m["o".charCodeAt(0)] = 0;
  m["I".charCodeAt(0)] = 1; m["i".charCodeAt(0)] = 1;
  m["L".charCodeAt(0)] = 1; m["l".charCodeAt(0)] = 1;
  return m;
})();

// crockfordEncode encodes bytes as Crockford base32: 8 chars per full
// 5-byte group, then the tail bytes MSB-aligned with the trailing
// padding bits truncated. Matches vault/pairing.go for both key sizes.
function crockfordEncode(b) {
  let out = "";
  const full = Math.floor(b.length / 5);
  for (let g = 0; g < full; g++) {
    let v = 0n;
    for (let i = 0; i < 5; i++) v = (v << 8n) | BigInt(b[g * 5 + i]);
    for (let i = 7; i >= 0; i--) out += CROCKFORD[Number((v >> BigInt(i * 5)) & 0x1Fn)];
  }
  const r = b.length - full * 5;
  if (r > 0) {
    const chars = Math.ceil(r * 8 / 5);
    let v = 0n;
    for (let i = 0; i < r; i++) v = (v << 8n) | BigInt(b[full * 5 + i]);
    v <<= BigInt(chars * 5 - r * 8); // MSB-align into the char grid
    for (let i = chars - 1; i >= 0; i--) out += CROCKFORD[Number((v >> BigInt(i * 5)) & 0x1Fn)];
  }
  return out;
}

// crockfordDecode parses a Crockford base32 string into byteLen bytes.
// Whitespace and dashes are ignored; the padding bits must be zero.
function crockfordDecode(s, byteLen) {
  const raw = s.replace(/[\s-]/g, "");
  const full = Math.floor(byteLen / 5);
  const r = byteLen - full * 5;
  const tailChars = r > 0 ? Math.ceil(r * 8 / 5) : 0;
  if (raw.length !== full * 8 + tailChars) throw new Error("kv: malformed pairing blob");
  const dec = (i) => {
    const d = CROCKFORD_DEC[raw.charCodeAt(i)] ?? -1;
    if (d < 0) throw new Error("kv: malformed pairing blob");
    return d;
  };
  const out = new Uint8Array(byteLen);
  for (let g = 0; g < full; g++) {
    let v = 0n;
    for (let i = 0; i < 8; i++) v = (v << 5n) | BigInt(dec(g * 8 + i));
    for (let i = 0; i < 5; i++) out[g * 5 + i] = Number((v >> BigInt((4 - i) * 8)) & 0xFFn);
  }
  if (r > 0) {
    let v = 0n;
    for (let i = 0; i < tailChars; i++) v = (v << 5n) | BigInt(dec(full * 8 + i));
    const pad = BigInt(tailChars * 5 - r * 8);
    if ((v & ((1n << pad) - 1n)) !== 0n) throw new Error("kv: malformed pairing blob");
    v >>= pad;
    for (let i = 0; i < r; i++) out[full * 5 + i] = Number((v >> BigInt((r - 1 - i) * 8)) & 0xFFn);
  }
  return out;
}

// encodeRootKey returns the Crockford base32 encoding of a 20-byte
// RootKey (32 chars, no padding bits).
export function encodeRootKey(rk) {
  if (rk.length !== ROOT_KEY_SIZE) throw new Error("kv: root key must be 20 bytes");
  return crockfordEncode(rk);
}

// decodeRootKey parses the 32-char Crockford base32 RootKey form.
export function decodeRootKey(s) { return crockfordDecode(s, ROOT_KEY_SIZE); }

// encodeRegisterKey returns the Crockford base32 encoding of a 16-byte
// RegisterKey (26 chars).
export function encodeRegisterKey(rk) {
  if (rk.length !== REGISTER_KEY_SIZE) throw new Error("kv: register key must be 16 bytes");
  return crockfordEncode(rk);
}

// decodeRegisterKey parses the 26-char Crockford base32 RegisterKey
// form.
export function decodeRegisterKey(s) { return crockfordDecode(s, REGISTER_KEY_SIZE); }

// encodeBackup packs a RootKey and optional RegisterKey into a single
// blob: 32 chars alone, 58 chars combined.
export function encodeBackup(rootKey, registerKey = null) {
  if (registerKey === null) return encodeRootKey(rootKey);
  return encodeRootKey(rootKey) + encodeRegisterKey(registerKey);
}

// decodeBackup parses either backup shape, returning {rootKey,
// registerKey} with registerKey null when the blob had none.
export function decodeBackup(s) {
  const raw = s.replace(/[\s-]/g, "");
  switch (raw.length) {
    case 32:
      return { rootKey: decodeRootKey(raw), registerKey: null };
    case 58:
      return { rootKey: decodeRootKey(raw.slice(0, 32)), registerKey: decodeRegisterKey(raw.slice(32)) };
    default:
      throw new Error("kv: malformed pairing blob");
  }
}
