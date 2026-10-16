// row.js -- ROW v1 typed rows (ROW.t), the JavaScript twin of
// repani.com/row. ES module; runs in browsers and any engine with
// BigInt and TextDecoder. It imports ./lz4s.js, for compressed
// strings: serve lz4s.JS() (repani.com/lz4s) beside it.
//
//   encode(items) -> Uint8Array     decode(bytes) -> items
//   format(items) -> string         parse(line) -> items
//
// Items are JavaScript values of the row types:
//
//   string           a string          null           none
//   boolean          false, true       number         int (a safe integer)
//   Decimal          decimal           Float          float
//   Day              date              Date           datetime (an instant)
//   TimeOfDay        time              Duration       duration
//   Uint8Array       bytes             Array          list
//
// A float is always a Float, so a number is never ambiguous. Every
// failure is a RowError with ROW.t's code and the offset of the item at
// fault (bytes for binary, string index for text).

import { compress, decompress } from "./lz4s.js";

export class RowError extends Error {
  constructor(code, offset, message) {
    super(`row: ${code} at ${offset}: ${message}`);
    this.name = "RowError";
    this.code = code;
    this.offset = offset;
  }
}

// Decimal is mantissa x 10^-scale; the scale is part of the value.
export class Decimal {
  constructor(mantissa, scale) { this.mantissa = mantissa; this.scale = scale; }
}
// Float is a finite IEEE 754 binary64 number.
export class Float {
  constructor(value) { this.value = value; }
}
// Day is a date: days since 1970-01-01, proleptic Gregorian.
export class Day {
  constructor(days) { this.days = days; }
}
// TimeOfDay is milliseconds after midnight, no zone.
export class TimeOfDay {
  constructor(ms) { this.ms = ms; }
}
// Duration is a signed number of milliseconds.
export class Duration {
  constructor(ms) { this.ms = ms; }
}

const MAX_SHORT = 241;
const [CSTR, NONE, FALSE, TRUE, INT, DEC, FLT, DATE, DT, TIME, DUR, BYTES, LSTR, LIST] =
  Array.from({ length: 14 }, (_, i) => MAX_SHORT + 1 + i);
const MAX_INT = Number.MAX_SAFE_INTEGER; // 2^53-1
const MAX_SCALE = 15;
const MIN_DAY = -719162, MAX_DAY = 2932896;
const MIN_DT = -62135596800000, MAX_DT = 253402300799999;
const MAX_TIME = 86399999;
const MAX_VARINT = 8;
const MAX_EXPAND = 255; // bytes an lz4s stream yields at most per byte

const K = { STR: 0, NONE: 1, FALSE: 2, TRUE: 3, INT: 4, DEC: 5, FLT: 6, DATE: 7, DT: 8, TIME: 9, DUR: 10, BYTES: 11, LIST: 12 };

const te = new TextEncoder();
const td = new TextDecoder("utf-8", { fatal: true });

function err(code, off, msg) { return new RowError(code, off, msg); }
const isInt = (n) => Number.isSafeInteger(n);

// Days and civil dates, by Howard Hinnant's integer algorithms -- the
// same as repani.com/row's, and free of Date.UTC's 0-99 year trap.
function daysFromCivil(y, m, d) {
  if (m <= 2) y--;
  const era = Math.floor(y / 400);
  const yoe = y - era * 400;
  const mp = m > 2 ? m - 3 : m + 9;
  const doy = Math.floor((153 * mp + 2) / 5) + d - 1;
  const doe = yoe * 365 + Math.floor(yoe / 4) - Math.floor(yoe / 100) + doy;
  return era * 146097 + doe - 719468;
}
function civilFromDays(z) {
  z += 719468;
  const era = Math.floor(z / 146097);
  const doe = z - era * 146097;
  const yoe = Math.floor((doe - Math.floor(doe / 1460) + Math.floor(doe / 36524) - Math.floor(doe / 146096)) / 365);
  let y = yoe + era * 400;
  const doy = doe - (365 * yoe + Math.floor(yoe / 4) - Math.floor(yoe / 100));
  const mp = Math.floor((5 * doy + 2) / 153);
  const d = doy - Math.floor((153 * mp + 2) / 5) + 1;
  const m = mp < 10 ? mp + 3 : mp - 9;
  if (m <= 2) y++;
  return [y, m, d];
}
function daysIn(y, m) {
  if (m === 2) return y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28;
  return [4, 6, 9, 11].includes(m) ? 30 : 31;
}
const validCivil = (y, m, d) => y >= 1 && y <= 9999 && m >= 1 && m <= 12 && d >= 1 && d <= daysIn(y, m);

// loneSurrogate is the index of s's first lone surrogate, or -1. A JS
// string holds UTF-16; only a lone surrogate has no UTF-8 form. A scan,
// not a regular expression: lookbehind is a syntax error before Safari
// 16.4, and one in this file would stop every page importing it.
function loneSurrogate(s) {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0xd800 || c > 0xdfff) continue;
    if (c <= 0xdbff) {
      const n = s.charCodeAt(i + 1);
      if (n >= 0xdc00 && n <= 0xdfff) { i++; continue; }
    }
    return i;
  }
  return -1;
}
const validUTF8 = (s) => loneSurrogate(s) < 0;

function checkFloat(f, off) {
  if (Number.isNaN(f)) throw err("R005", off, "float is NaN");
  if (!Number.isFinite(f)) throw err("R005", off, "float is infinite");
  if (Object.is(f, -0)) throw err("R005", off, "float is -0");
}

// fromJS checks a value against the rules and returns it as an
// internal {k, ...} item; off is the byte offset it would have.
function fromJS(v, inList, off) {
  if (v === null) return { k: K.NONE };
  switch (typeof v) {
    case "boolean": return { k: v ? K.TRUE : K.FALSE };
    case "string":
      if (!validUTF8(v)) throw err("R006", off, "string holds a lone surrogate");
      return { k: K.STR, s: v };
    case "number":
      if (!Number.isInteger(v)) throw new Error("row: a non-integer number; use Float or Decimal");
      if (!isInt(v)) throw err("R003", off, `int ${v} is outside ±(2^53-1)`);
      return { k: K.INT, n: v };
  }
  if (v instanceof Decimal) {
    if (!Number.isInteger(v.scale) || v.scale < 0 || v.scale > MAX_SCALE) throw err("R004", off, `decimal scale ${v.scale} is outside 0 to 15`);
    if (!isInt(v.mantissa)) throw err("R003", off, "decimal mantissa is outside ±(2^53-1)");
    return { k: K.DEC, n: v.mantissa, scale: v.scale };
  }
  if (v instanceof Float) {
    if (typeof v.value !== "number") throw new Error("row: Float holds a number");
    checkFloat(v.value, off);
    return { k: K.FLT, f: v.value };
  }
  if (v instanceof Day) {
    if (!Number.isInteger(v.days) || v.days < MIN_DAY || v.days > MAX_DAY) throw err("R003", off, `day ${v.days} is outside 0001-01-01 to 9999-12-31`);
    return { k: K.DATE, n: v.days };
  }
  if (v instanceof Date) {
    const ms = v.getTime();
    if (!Number.isInteger(ms) || ms < MIN_DT || ms > MAX_DT) throw err("R003", off, "datetime is outside years 0001 to 9999");
    return { k: K.DT, n: ms };
  }
  if (v instanceof TimeOfDay) {
    if (!Number.isInteger(v.ms) || v.ms < 0 || v.ms > MAX_TIME) throw err("R003", off, `time of day ${v.ms} ms is outside 0 to 86399999`);
    return { k: K.TIME, n: v.ms };
  }
  if (v instanceof Duration) {
    if (!isInt(v.ms)) throw err("R003", off, "duration is outside ±(2^53-1) ms");
    return { k: K.DUR, n: v.ms };
  }
  if (v instanceof Uint8Array) return { k: K.BYTES, b: v };
  if (Array.isArray(v)) {
    if (inList) throw err("R008", off, "a list holds a list");
    return { k: K.LIST, list: v.map((it) => fromJS(it, true, off)) };
  }
  throw new Error("row: " + Object.prototype.toString.call(v) + " is not a row type");
}

function toJS(v) {
  switch (v.k) {
    case K.STR: return v.s;
    case K.NONE: return null;
    case K.FALSE: return false;
    case K.TRUE: return true;
    case K.INT: return v.n;
    case K.DEC: return new Decimal(v.n, v.scale);
    case K.FLT: return new Float(v.f);
    case K.DATE: return new Day(v.n);
    case K.DT: return new Date(v.n);
    case K.TIME: return new TimeOfDay(v.n);
    case K.DUR: return new Duration(v.n);
    case K.BYTES: return v.b;
  }
  return v.list.map(toJS);
}

// --- binary ---

// Varints go through BigInt: a zigzag value reaches 2^54-2, past the
// integers a double holds exactly.
function pushUvarint(out, u) {
  let x = BigInt(u);
  for (;;) {
    const b = Number(x & 0x7fn);
    x >>= 7n;
    if (x === 0n) { out.push(b); return; }
    out.push(b | 0x80);
  }
}
// smaller says whether a string of n bytes whose lz4s stream is c
// bytes is written compressed: when that item is the shorter, the tag
// and the length being common to both (ROW.t 3).
const smaller = (c, n) => { const v = []; pushUvarint(v, c); return v.length + c < n; };

const zigzag = (n) => (n >= 0 ? BigInt(n) * 2n : -BigInt(n) * 2n - 1n);
const unzigzag = (u) => (u & 1n ? -((u + 1n) >> 1n) : u >> 1n);

function append(out, v) {
  switch (v.k) {
    case K.STR: {
      const b = te.encode(v.s);
      if (b.length <= MAX_SHORT) {
        out.push(b.length);
      } else {
        const c = compress(b);
        if (smaller(c.length, b.length)) {
          out.push(CSTR);
          pushUvarint(out, b.length);
          pushUvarint(out, c.length);
          for (const x of c) out.push(x);
          return;
        }
        out.push(LSTR);
        pushUvarint(out, b.length);
      }
      for (const x of b) out.push(x);
      return;
    }
    case K.NONE: out.push(NONE); return;
    case K.FALSE: out.push(FALSE); return;
    case K.TRUE: out.push(TRUE); return;
    case K.INT: out.push(INT); pushUvarint(out, zigzag(v.n)); return;
    case K.DEC: out.push(DEC, v.scale); pushUvarint(out, zigzag(v.n)); return;
    case K.FLT: {
      const b = new Uint8Array(8);
      new DataView(b.buffer).setFloat64(0, v.f, true);
      out.push(FLT, ...b);
      return;
    }
    case K.DATE: out.push(DATE); pushUvarint(out, zigzag(v.n)); return;
    case K.DT: out.push(DT); pushUvarint(out, zigzag(v.n)); return;
    case K.TIME: out.push(TIME); pushUvarint(out, v.n); return;
    case K.DUR: out.push(DUR); pushUvarint(out, zigzag(v.n)); return;
    case K.BYTES: out.push(BYTES); pushUvarint(out, v.b.length); for (const x of v.b) out.push(x); return;
  }
  out.push(LIST);
  pushUvarint(out, v.list.length);
  for (const it of v.list) append(out, it);
}

// encode returns a row's bytes; a value outside its type's rules is a
// RowError with the R code decode would give its bytes.
export function encode(items) {
  const out = [];
  for (const it of items) append(out, fromJS(it, false, out.length));
  return Uint8Array.from(out);
}

// decode returns a row's items; a row that breaks a rule of ROW.t
// section 4 is a RowError with its code and the item's offset.
export function decode(bytes) {
  return decodeVals(bytes).map(toJS);
}

function decodeVals(b) {
  const d = { b, pos: 0 };
  const out = [];
  while (d.pos < b.length) out.push(item(d, false));
  return out;
}

function uvarint(d, start) {
  let u = 0n;
  for (let i = 0; ; i++) {
    if (i === MAX_VARINT) throw err("R002", start, "a varint is longer than 8 bytes");
    if (d.pos >= d.b.length) throw err("R001", start, "a varint runs past the end");
    const c = d.b[d.pos++];
    u |= BigInt(c & 0x7f) << BigInt(7 * i);
    if (c < 0x80) {
      if (c === 0 && i > 0) throw err("R002", start, "a varint is longer than its shortest form");
      return u;
    }
  }
}

function signed(d, start, what) {
  const u = uvarint(d, start);
  if (u > 2n * BigInt(MAX_INT)) throw err("R003", start, `${what} is outside ±(2^53-1)`);
  return Number(unzigzag(u));
}

function length(d, start, what) {
  const u = uvarint(d, start);
  if (u > BigInt(MAX_INT)) throw err("R003", start, `${what} is outside 2^53-1`);
  if (u > BigInt(d.b.length - d.pos)) throw err("R001", start, `${what} ${u} runs past the end`);
  return Number(u);
}

function str(d, start, n) {
  const s = d.b.subarray(d.pos, d.pos + n);
  d.pos += n;
  try {
    return { k: K.STR, s: td.decode(s) };
  } catch {
    throw err("R006", start, "string is not valid UTF-8");
  }
}

function item(d, inList) {
  const start = d.pos;
  const tag = d.b[d.pos++];
  const left = () => d.b.length - d.pos;
  if (tag <= MAX_SHORT) {
    if (tag > left()) throw err("R001", start, `string of ${tag} bytes runs past the end`);
    return str(d, start, tag);
  }
  switch (tag) {
    case NONE: return { k: K.NONE };
    case FALSE: return { k: K.FALSE };
    case TRUE: return { k: K.TRUE };
    case INT: return { k: K.INT, n: signed(d, start, "int") };
    case DEC: {
      if (left() < 1) throw err("R001", start, "decimal runs past the end");
      const scale = d.b[d.pos++];
      if (scale > MAX_SCALE) throw err("R004", start, `decimal scale ${scale} is above 15`);
      return { k: K.DEC, n: signed(d, start, "decimal mantissa"), scale };
    }
    case FLT: {
      if (left() < 8) throw err("R001", start, "float runs past the end");
      const f = new DataView(d.b.buffer, d.b.byteOffset + d.pos, 8).getFloat64(0, true);
      d.pos += 8;
      checkFloat(f, start);
      return { k: K.FLT, f };
    }
    case DATE: {
      const n = signed(d, start, "date");
      if (n < MIN_DAY || n > MAX_DAY) throw err("R003", start, `day ${n} is outside 0001-01-01 to 9999-12-31`);
      return { k: K.DATE, n };
    }
    case DT: {
      const n = signed(d, start, "datetime");
      if (n < MIN_DT || n > MAX_DT) throw err("R003", start, `datetime ${n} ms is outside years 0001 to 9999`);
      return { k: K.DT, n };
    }
    case TIME: {
      const u = uvarint(d, start);
      if (u > BigInt(MAX_TIME)) throw err("R003", start, `time of day ${u} ms is outside 0 to 86399999`);
      return { k: K.TIME, n: Number(u) };
    }
    case DUR: return { k: K.DUR, n: signed(d, start, "duration") };
    case BYTES: {
      const n = length(d, start, "bytes length");
      const b = d.b.slice(d.pos, d.pos + n);
      d.pos += n;
      return { k: K.BYTES, b };
    }
    case LSTR: {
      const n = length(d, start, "string length");
      if (n <= MAX_SHORT) throw err("R007", start, `a long string of ${n} bytes could be a short one`);
      const bytes = d.b.subarray(d.pos, d.pos + n);
      const v = str(d, start, n);
      if (smaller(compress(bytes).length, n)) throw err("R007", start, `a long string of ${n} bytes could be a compressed one`);
      return v;
    }
    case CSTR: {
      const u = uvarint(d, start);
      if (u > BigInt(MAX_INT)) throw err("R003", start, `string length ${u} is outside 2^53-1`);
      const c = length(d, start, "stream length");
      const stream = d.b.subarray(d.pos, d.pos + c);
      d.pos += c;
      const n = Number(u);
      if (n <= MAX_SHORT) throw err("R007", start, `a compressed string of ${n} bytes could be a short one`);
      // A stream yields at most MAX_EXPAND bytes for each of its own,
      // so a longer claim is refused before anything is allocated.
      if (n > MAX_EXPAND * c) throw err("R009", start, `a stream of ${c} bytes cannot make ${n}`);
      const text = decompress(stream, n);
      if (text === null) throw err("R009", start, `the stream does not decompress to ${n} bytes`);
      let s;
      try { s = td.decode(text); } catch { throw err("R006", start, "string is not valid UTF-8"); }
      const again = compress(text);
      if (again.length !== c || again.some((x, i) => x !== stream[i])) throw err("R009", start, "the stream is not lz4s.Compress of its string");
      if (!smaller(c, n)) throw err("R007", start, `a compressed string of ${n} bytes could be a long one`);
      return { k: K.STR, s };
    }
  }
  // LIST
  if (inList) throw err("R008", start, "a list holds a list");
  const n = length(d, start, "list count");
  const list = [];
  for (let i = 0; i < n; i++) {
    if (d.pos >= d.b.length) throw err("R001", start, `list of ${n} items runs past the end`);
    list.push(item(d, true));
  }
  return { k: K.LIST, list };
}

// --- text ---

// format returns a row's text notation (ROW.t section 5).
export function format(items) {
  return formatVals(items.map((it) => fromJS(it, false, 0)));
}

function formatVals(vals) { return vals.map(fmt).join(" "); }

const pad = (n, w) => String(n).padStart(w, "0");

function fmt(v) {
  switch (v.k) {
    case K.STR: return quote(v.s);
    case K.NONE: return "none";
    case K.FALSE: return "false";
    case K.TRUE: return "true";
    case K.INT: return String(v.n);
    case K.DEC: return formatDecimal(v.n, v.scale);
    case K.FLT: return v.f.toExponential().replace("e+", "e");
    case K.DATE: return formatDate(v.n);
    case K.DT: {
      const day = Math.floor(v.n / 86400000);
      return formatDate(day) + "T" + formatTime(v.n - day * 86400000) + "Z";
    }
    case K.TIME: return formatTime(v.n);
    case K.DUR: return formatDuration(v.n);
    case K.BYTES: return "0x" + Array.from(v.b, (x) => x.toString(16).padStart(2, "0")).join("");
  }
  return "[" + v.list.map(fmt).join(", ") + "]";
}

const ESC = { '"': '\\"', "\\": "\\\\", "\b": "\\b", "\f": "\\f", "\n": "\\n", "\r": "\\r", "\t": "\\t" };
function quote(s) {
  let out = '"';
  for (const ch of s) {
    const c = ch.codePointAt(0);
    if (ESC[ch]) out += ESC[ch];
    else if (c < 0x20) out += "\\u00" + c.toString(16).padStart(2, "0");
    else out += ch;
  }
  return out + '"';
}

function formatDecimal(m, scale) {
  const neg = m < 0;
  let s = String(Math.abs(m));
  if (s.length <= scale) s = "0".repeat(scale + 1 - s.length) + s;
  s = s.slice(0, s.length - scale) + "." + s.slice(s.length - scale);
  return neg ? "-" + s : s;
}

function formatDate(day) {
  const [y, m, d] = civilFromDays(day);
  return pad(y, 4) + "-" + pad(m, 2) + "-" + pad(d, 2);
}

function formatTime(ms) {
  return pad(Math.floor(ms / 3600000), 2) + ":" + pad(Math.floor(ms / 60000) % 60, 2) + ":" +
    pad(Math.floor(ms / 1000) % 60, 2) + "." + pad(ms % 1000, 3);
}

function formatDuration(ms) {
  let out = ms < 0 ? "-" : "";
  const a = Math.abs(ms);
  const h = Math.floor(a / 3600000), m = Math.floor((a % 3600000) / 60000), s = a % 60000;
  if (h > 0) out += h + "h";
  if (m > 0) out += m + "m";
  if (s > 0 || (h === 0 && m === 0)) {
    out += Math.floor(s / 1000);
    const frac = s % 1000;
    if (frac > 0) {
      let f = pad(frac, 3);
      while (f.endsWith("0")) f = f.slice(0, -1);
      out += "." + f;
    }
    out += "s";
  }
  return out;
}

// parse reads one row in text notation: T001 for text that is no row,
// T002 (at the first differing index, the message giving the canonical
// row) for a row spelt other than canonically, an R code for a value
// outside its type.
export function parse(line) {
  return parseVals(line).map(toJS);
}

const isSpace = (c) => c === " " || c === "\t";

function parseVals(line) {
  const bad = loneSurrogate(line);
  if (bad >= 0) throw err("R006", bad, "text is not valid UTF-8");
  const p = { s: line, pos: 0 };
  const vals = [];
  space(p);
  while (p.pos < p.s.length) {
    vals.push(parseItem(p, false));
    if (p.pos < p.s.length && !isSpace(p.s[p.pos])) throw err("T001", p.pos, "expected a space between items");
    space(p);
  }
  const canon = formatVals(vals);
  if (canon !== line) {
    let i = 0;
    while (i < canon.length && i < line.length && canon[i] === line[i]) i++;
    throw err("T002", i, "not canonical; the row is spelt: " + canon);
  }
  return vals;
}

function space(p) { while (p.pos < p.s.length && isSpace(p.s[p.pos])) p.pos++; }

function parseItem(p, inList) {
  const c = p.s[p.pos];
  if (c === '"') return parseStr(p);
  if (c === "[") {
    if (inList) throw err("R008", p.pos, "a list holds a list");
    return parseList(p);
  }
  const start = p.pos;
  while (p.pos < p.s.length && !' \t,[]"'.includes(p.s[p.pos])) p.pos++;
  if (p.pos === start) throw err("T001", start, "expected an item");
  return bare(p.s.slice(start, p.pos), start);
}

function parseList(p) {
  const start = p.pos++;
  const list = [];
  space(p);
  if (p.s[p.pos] === "]") { p.pos++; return { k: K.LIST, list }; }
  for (;;) {
    if (p.pos >= p.s.length) throw err("T001", start, "list is not closed");
    list.push(parseItem(p, true));
    space(p);
    if (p.pos >= p.s.length) throw err("T001", start, "list is not closed");
    const c = p.s[p.pos];
    if (c === ",") { p.pos++; space(p); }
    else if (c === "]") { p.pos++; return { k: K.LIST, list }; }
    else throw err("T001", p.pos, "expected , or ] in a list");
  }
}

const SIMPLE = { '"': '"', "\\": "\\", "/": "/", b: "\b", f: "\f", n: "\n", r: "\r", t: "\t" };

function parseStr(p) {
  const start = p.pos++;
  let out = "";
  for (;;) {
    if (p.pos >= p.s.length) throw err("T001", start, "string is not closed");
    const c = p.s[p.pos];
    if (c === '"') { p.pos++; return { k: K.STR, s: out }; }
    if (c !== "\\") { out += c; p.pos++; continue; }
    if (p.pos + 1 >= p.s.length) throw err("T001", start, "string is not closed");
    const e = p.s[p.pos + 1];
    p.pos += 2;
    if (e in SIMPLE) { out += SIMPLE[e]; continue; }
    if (e !== "u") throw err("T001", start, "unknown escape \\" + e);
    let r = hex4(p);
    if (r === null) throw err("T001", start, "\\u needs four hex digits");
    if (r >= 0xd800 && r <= 0xdfff) {
      let lo = null;
      if (r < 0xdc00 && p.s.startsWith("\\u", p.pos)) { p.pos += 2; lo = hex4(p); }
      if (lo === null || lo < 0xdc00 || lo > 0xdfff) throw err("R006", start, "string holds a lone surrogate");
      r = 0x10000 + ((r - 0xd800) << 10) + (lo - 0xdc00);
    }
    out += String.fromCodePoint(r);
  }
}

function hex4(p) {
  const h = p.s.slice(p.pos, p.pos + 4);
  if (h.length !== 4 || ![...h].every(isHex)) return null;
  p.pos += 4;
  return parseInt(h, 16);
}

// The bare items' shapes, matched by hand, with no regular expressions
// (the Go twin is the same scan): some engines reject regexp syntax that
// others accept, and one rejected literal stops the whole module. Each
// match returns submatches in the manner of RegExp exec, m[0] the whole
// token and "" for a group that is absent, or null. Digits are ASCII 0-9.

const isDigit = (c) => c >= "0" && c <= "9";
const isHex = (c) => isDigit(c) || (c >= "a" && c <= "f") || (c >= "A" && c <= "F");

// digitRun is the end of the run of digits in t from i.
function digitRun(t, i) {
  while (i < t.length && isDigit(t[i])) i++;
  return i;
}

// minus reads an optional leading "-": the group, and where the rest begins.
const minus = (t) => (t[0] === "-" ? ["-", 1] : ["", 0]);

// Fixed shapes: each "d" one digit, any other character itself; the
// groups are the runs of "d".
const SHAPE = { datetime: "dddd-dd-ddTdd:dd:dd.dddZ", date: "dddd-dd-dd", time: "dd:dd:dd.ddd" };

function matchShape(t, shape) {
  if (t.length !== shape.length) return null;
  const m = [t];
  let start = -1;
  for (let i = 0; i <= shape.length; i++) {
    if (i < shape.length && shape[i] === "d") {
      if (!isDigit(t[i])) return null;
      if (start < 0) start = i;
      continue;
    }
    if (start >= 0) { m.push(t.slice(start, i)); start = -1; }
    if (i < shape.length && t[i] !== shape[i]) return null;
  }
  return m;
}

// matchDuration: -? (N h)? (N m)? (N (.F)? s)?, F one to three digits;
// m[1] the sign, m[2], m[3], m[4] hours, minutes, seconds, m[5] the
// fraction. Every part may be absent, so the empty token matches; the
// caller asks for one of m[2..4].
function matchDuration(t) {
  const m = [t, "", "", "", "", ""];
  let i;
  [m[1], i] = minus(t);
  ["h", "m"].forEach((unit, g) => {
    const j = digitRun(t, i);
    if (j > i && t[j] === unit) { m[2 + g] = t.slice(i, j); i = j + 1; }
  });
  const j = digitRun(t, i);
  if (j > i) {
    let k = j;
    if (t[k] === ".") {
      const e = digitRun(t, k + 1);
      if (e - (k + 1) < 1 || e - (k + 1) > 3) return null;
      m[5] = t.slice(k + 1, e);
      k = e;
    }
    if (t[k] !== "s") return null;
    m[4] = t.slice(i, j);
    i = k + 1;
  }
  return i === t.length ? m : null;
}

// isFloat: -? D+ (. D+)? [eE] [+-]? D+.
function isFloat(t) {
  const i = minus(t)[1];
  let j = digitRun(t, i);
  if (j === i) return false;
  if (t[j] === ".") {
    const k = digitRun(t, j + 1);
    if (k === j + 1) return false;
    j = k;
  }
  if (t[j] !== "e" && t[j] !== "E") return false;
  j++;
  if (t[j] === "+" || t[j] === "-") j++;
  const k = digitRun(t, j);
  return k > j && k === t.length;
}

// matchDecimal: -? D+ . D*; m[1] the sign, m[2] the whole digits, m[3]
// the fraction's.
function matchDecimal(t) {
  const [s, i] = minus(t);
  const j = digitRun(t, i);
  if (j === i || t[j] !== "." || digitRun(t, j + 1) !== t.length) return null;
  return [t, s, t.slice(i, j), t.slice(j + 1)];
}

// matchInt: -? D+; m[1] the sign, m[2] the digits.
function matchInt(t) {
  const [s, i] = minus(t);
  const j = digitRun(t, i);
  return j === i || j !== t.length ? null : [t, s, t.slice(i)];
}

// matchBytes: 0x or 0X, then hex digits in pairs; m[1] the hex.
function matchBytes(t) {
  if (t[0] !== "0" || (t[1] !== "x" && t[1] !== "X") || t.length % 2 !== 0) return null;
  for (let i = 2; i < t.length; i++) if (!isHex(t[i])) return null;
  return [t, t.slice(2)];
}

function bare(t, off) {
  if (t === "none") return { k: K.NONE };
  if (t === "false") return { k: K.FALSE };
  if (t === "true") return { k: K.TRUE };
  let m;
  if ((m = matchBytes(t))) {
    const b = new Uint8Array(m[1].length / 2);
    for (let i = 0; i < b.length; i++) b[i] = parseInt(m[1].slice(2 * i, 2 * i + 2), 16);
    return { k: K.BYTES, b };
  }
  if ((m = matchShape(t, SHAPE.datetime))) {
    const day = civil(m[1], m[2], m[3], off);
    return { k: K.DT, n: day * 86400000 + clock(m[4], m[5], m[6], m[7], off) };
  }
  if ((m = matchShape(t, SHAPE.date))) return { k: K.DATE, n: civil(m[1], m[2], m[3], off) };
  if ((m = matchShape(t, SHAPE.time))) return { k: K.TIME, n: clock(m[1], m[2], m[3], m[4], off) };
  if ((m = matchDuration(t)) && (m[2] || m[3] || m[4])) return duration(m, off);
  if (isFloat(t)) {
    const f = Number(t);
    checkFloat(f, off);
    return { k: K.FLT, f };
  }
  if ((m = matchDecimal(t))) {
    if (m[3].length > MAX_SCALE) throw err("R004", off, `decimal scale ${m[3].length} is above 15`);
    const n = digits(m[2] + m[3], off, "decimal mantissa");
    return { k: K.DEC, n: m[1] === "-" ? -n : n, scale: m[3].length };
  }
  if ((m = matchInt(t))) {
    const n = digits(m[2], off, "int");
    return { k: K.INT, n: m[1] === "-" ? -n : n };
  }
  throw err("T001", off, JSON.stringify(t) + " is not an item");
}

// digits reads a run of decimal digits within 2^53-1, exactly.
function digits(s, off, what) {
  let z = 0;
  while (s[z] === "0") z++;
  s = s.slice(z);
  if (s === "") return 0;
  if (s.length > 16 || BigInt(s) > BigInt(MAX_INT)) throw err("R003", off, `${what} is outside ±(2^53-1)`);
  return Number(s);
}

function civil(y, m, d, off) {
  const [yy, mm, dd] = [Number(y), Number(m), Number(d)];
  if (!validCivil(yy, mm, dd)) throw err("R003", off, `${y}-${m}-${d} is not a date from 0001-01-01 to 9999-12-31`);
  return daysFromCivil(yy, mm, dd);
}

function clock(h, m, s, ms, off) {
  const [hh, mm, ss] = [Number(h), Number(m), Number(s)];
  if (hh > 23 || mm > 59 || ss > 59) throw err("R003", off, `${h}:${m}:${s} is not a time of day`);
  return ((hh * 60 + mm) * 60 + ss) * 1000 + Number(ms);
}

function duration(m, off) {
  let total = 0n;
  [3600000n, 60000n, 1000n].forEach((unit, i) => {
    if (m[2 + i]) total += BigInt(m[2 + i]) * unit;
  });
  if (m[5]) total += BigInt((m[5] + "00").slice(0, 3));
  if (total > BigInt(MAX_INT)) throw err("R003", off, "duration is outside ±(2^53-1) ms");
  const n = Number(total);
  return { k: K.DUR, n: m[1] === "-" ? -n : n };
}
