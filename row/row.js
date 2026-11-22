// row.js: ROW (repani.com/row) in JavaScript, beside the Go one. The
// reference is ROW.t; both implementations pass the same vectors
// (testdata/vectors.json). ES module, no dependencies.
//
// Values: null, boolean, number (an int), Decimal, Float, Day, Date
// (a datetime, UTC), TimeOfDay, Duration, string, Uint8Array, and
// Array (a row).

export class RowError extends Error {
  constructor(code, path, offset, line, message) {
    const where = (path ? ` at ${path.join(".")}` : "") + (line > 0 ? `, line ${line}` : offset >= 0 ? `, byte ${offset}` : "");
    super(`row: ${code}${where}: ${message}`);
    this.name = "RowError";
    this.code = code;
    this.path = path;
    this.offset = offset;
    this.line = line;
  }
}

// Decimal is mantissa x 10^-scale; the scale is part of the value.
export class Decimal {
  constructor(mantissa, scale) { this.mantissa = mantissa; this.scale = scale; }
}
// Float is a finite IEEE 754 binary64 number: a JavaScript number
// cannot say whether it is an int or a float, so floats are wrapped.
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

const MAX_STR = 127, BYTES = 0x80, MAX_BYTES = 63;
const NONE = 0xc0, FALSE = 0xc1, TRUE = 0xc2, INT = 0xc3, FLT = 0xc4, DATE = 0xc5, DT = 0xc6, TIME = 0xc7, DUR = 0xc8;
const LSTR = 0xc9, LBYTES = 0xca, LROW = 0xcb, DEC = 0xd0, SROW = 0xe0, MAX_SROW = 15;
const MAX_INT = Number.MAX_SAFE_INTEGER; // 2^53-1
const MAX_SCALE = 15, MIN_DAY = -719162, MAX_DAY = 2932896;
const MIN_DT = -62135596800000, MAX_DT = 253402300799999, MAX_TIME = 86399999;
const MAX_VARINT = 8, MAX_DEPTH = 4;

const te = new TextEncoder();
const td = new TextDecoder("utf-8", { fatal: true });

const fail = (code, path, msg) => new RowError(code, path ? [...path] : null, -1, 0, msg);
const inInt = (n) => Number.isSafeInteger(n);

// The calendar: Howard Hinnant's integer algorithms, the same as the
// Go implementation's.
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

// A string holding a lone surrogate is no UTF-8 text.
function validUTF16(s) {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const n = s.charCodeAt(i + 1);
      if (!(n >= 0xdc00 && n <= 0xdfff)) return false;
      i++;
    } else if (c >= 0xdc00 && c <= 0xdfff) return false;
  }
  return true;
}

// typeName is a decoded value's name in the tag registry, "" for one
// that is not a value.
export function typeName(v) {
  if (v === null) return "none";
  if (typeof v === "boolean") return "bool";
  if (typeof v === "number") return Number.isInteger(v) ? "int" : ""; // 1.5 is a Float's
  if (typeof v === "string") return "str";
  if (v instanceof Decimal) return "dec";
  if (v instanceof Float) return "f64";
  if (v instanceof Day) return "date";
  if (v instanceof Date) return "utc";
  if (v instanceof TimeOfDay) return "time";
  if (v instanceof Duration) return "dur";
  if (v instanceof Uint8Array) return "bin";
  if (Array.isArray(v)) return "row";
  return "";
}

// check throws unless v is a value of its type; depth is the level of
// the row v stands in, 1 for the value row.
function check(v, path, depth) {
  switch (typeName(v)) {
    case "none": case "bool": case "bin": return;
    case "int":
      if (!inInt(v)) throw fail("V001", path, `int ${v} outside ±(2^53-1)`);
      return;
    case "str":
      if (!validUTF16(v)) throw fail("V008", path, "string holds a lone surrogate");
      return;
    case "dec":
      if (!Number.isInteger(v.scale) || v.scale < 0 || v.scale > MAX_SCALE) throw fail("V002", path, `decimal scale ${v.scale} outside 0 to 15`);
      if (!inInt(v.mantissa)) throw fail("V002", path, `decimal mantissa ${v.mantissa} outside ±(2^53-1)`);
      return;
    case "f64":
      if (typeof v.value !== "number" || !Number.isFinite(v.value) || Object.is(v.value, -0)) throw fail("V003", path, `float ${v.value}: NaN, infinite or -0`);
      return;
    case "date":
      if (!Number.isInteger(v.days) || v.days < MIN_DAY || v.days > MAX_DAY) throw fail("V004", path, `date ${v.days} days outside 0001-01-01 to 9999-12-31`);
      return;
    case "utc": {
      const t = v.getTime();
      if (!(t >= MIN_DT && t <= MAX_DT)) throw fail("V005", path, "datetime outside 0001 to 9999");
      return;
    }
    case "time":
      if (!Number.isInteger(v.ms) || v.ms < 0 || v.ms > MAX_TIME) throw fail("V006", path, `time ${v.ms} ms outside one day`);
      return;
    case "dur":
      if (!Number.isInteger(v.ms)) throw fail("V007", path, "duration finer than a millisecond");
      if (!inInt(v.ms)) throw fail("V007", path, `duration ${v.ms} ms outside ±(2^53-1)`);
      return;
    case "row":
      if (depth >= MAX_DEPTH) throw fail("R005", path, "rows nested deeper than four levels");
      checkItems(v, path, depth + 1);
      return;
  }
  if (typeof v === "number") throw fail("V009", path, `${v} is not an integer (a float is a Float)`);
  throw fail("V009", path, `${Object.prototype.toString.call(v)} has no ROW type`);
}
// checkItems walks every index: a sparse array's hole is undefined, no value.
function checkItems(r, path, depth) {
  const p = [...(path ?? []), 0];
  for (let i = 0; i < r.length; i++) { p[p.length - 1] = i; check(r[i], p, depth); }
}

// The bytes.

function pushVarint(out, v) { // v a BigInt
  const b = [Number(v & 0x7fn)];
  for (v >>= 7n; v !== 0n; v >>= 7n) { v--; b.push(0x80 | Number(v & 0x7fn)); }
  for (let i = b.length - 1; i >= 0; i--) out.push(b[i]);
}
const zigzag = (n) => (n >= 0 ? BigInt(n) * 2n : -BigInt(n) * 2n - 1n);
const unzigzag = (u) => (u & 1n ? -((u + 1n) >> 1n) : u >> 1n);
const pushBytes = (out, b) => { for (const x of b) out.push(x); };

function pushItem(out, v) {
  switch (typeName(v)) {
    case "none": out.push(NONE); return;
    case "bool": out.push(v ? TRUE : FALSE); return;
    case "int": out.push(INT); pushVarint(out, zigzag(v)); return;
    case "dec": out.push(DEC | v.scale); pushVarint(out, zigzag(v.mantissa)); return;
    case "f64": {
      const b = new Uint8Array(8);
      new DataView(b.buffer).setFloat64(0, v.value, true);
      out.push(FLT); pushBytes(out, b); return;
    }
    case "date": out.push(DATE); pushVarint(out, zigzag(v.days)); return;
    case "utc": out.push(DT); pushVarint(out, zigzag(v.getTime())); return;
    case "time": out.push(TIME); pushVarint(out, BigInt(v.ms)); return;
    case "dur": out.push(DUR); pushVarint(out, zigzag(v.ms)); return;
    case "str": {
      const b = te.encode(v);
      if (b.length <= MAX_STR) out.push(b.length);
      else { out.push(LSTR); pushVarint(out, BigInt(b.length - MAX_STR - 1)); }
      pushBytes(out, b); return;
    }
    case "bin":
      if (v.length <= MAX_BYTES) out.push(BYTES | v.length);
      else { out.push(LBYTES); pushVarint(out, BigInt(v.length - MAX_BYTES - 1)); }
      pushBytes(out, v); return;
    case "row":
      if (v.length <= MAX_SROW) out.push(SROW | v.length);
      else { out.push(LROW); pushVarint(out, BigInt(v.length - MAX_SROW - 1)); }
      for (const x of v) pushItem(out, x);
      return;
  }
  throw new Error("row: an unchecked value reached the encoder"); // check refuses it first
}

// encode returns the bytes of a row (an Array), checking every value.
export function encode(row) {
  checkItems(row, null, 1);
  const out = [];
  pushVarint(out, BigInt(row.length));
  for (const v of row) pushItem(out, v);
  return Uint8Array.from(out);
}

class Decoder {
  constructor(b) { this.b = b; this.i = 0; }
  fail(code, path, off, msg) { return new RowError(code, path ? [...path] : null, off, 0, msg); }
  varint(path, off) {
    let v = 0n;
    for (let n = 0; ; n++) {
      if (this.i >= this.b.length) throw this.fail("R001", path, off, "the bytes end inside a varint");
      if (n === MAX_VARINT) throw this.fail("R003", path, off, "a varint longer than 8 bytes");
      const c = this.b[this.i++];
      v = n === 0 ? BigInt(c & 0x7f) : ((v + 1n) << 7n) | BigInt(c & 0x7f);
      if (!(c & 0x80)) return v;
    }
  }
  zz(path, off) { return unzigzag(this.varint(path, off)); }
  take(n, path, off) {
    if (n > BigInt(this.b.length - this.i)) throw this.fail("R001", path, off, `the bytes end inside a payload of ${n}`);
    const s = this.b.subarray(this.i, this.i + Number(n));
    this.i += Number(n);
    return s;
  }
  row(path, depth, count) {
    if (count === null) {
      const c = this.varint(path, this.i);
      if (c > BigInt(MAX_INT)) throw this.fail("V001", path, 0, `a count of ${c} outside 2^53-1`);
      count = Number(c);
    }
    const r = [];
    const p = [...(path ?? []), 0];
    for (let k = 0; k < count; k++) {
      p[p.length - 1] = k;
      r.push(this.item(p, depth));
    }
    return r;
  }
  // checked is a decoded value through check, the one check of a value
  // against its type, its error placed at the item's offset.
  checked(v, path, off) {
    try { check(v, path, 1); } catch (e) {
      if (!(e instanceof RowError)) throw e;
      throw this.fail(e.code, path, off, e.message.replace(/^row: \S+( at \S+)?: /, ""));
    }
    return v;
  }
  str(n, path, off) {
    const s = this.take(n, path, off);
    try { return td.decode(s); } catch { throw this.fail("V008", path, off, "string not valid UTF-8"); }
  }
  nested(count, path, depth, off) {
    if (depth >= MAX_DEPTH) throw this.fail("R005", path, off, "rows nested deeper than four levels");
    if (count > BigInt(MAX_INT)) throw this.fail("V001", path, off, `a count of ${count} outside 2^53-1`);
    return this.row(path, depth + 1, Number(count));
  }
  item(path, depth) {
    const off = this.i;
    if (this.i >= this.b.length) throw this.fail("R001", path, off, "the bytes end before the item");
    const t = this.b[this.i++];
    if (t <= MAX_STR) return this.str(BigInt(t), path, off);
    if (t < NONE) return this.take(BigInt(t - BYTES), path, off).slice();
    // a BigInt past 2^53 as a Number is inexact, but out of range all the same
    const num = (n) => Number(n);
    if (t >= DEC && t < SROW) return this.checked(new Decimal(num(this.zz(path, off)), t & 0x0f), path, off);
    if (t >= SROW && t <= SROW + MAX_SROW) return this.nested(BigInt(t & 0x0f), path, depth, off);
    switch (t) {
      case NONE: return null;
      case FALSE: return false;
      case TRUE: return true;
      case INT: return this.checked(num(this.zz(path, off)), path, off);
      case FLT: {
        const s = this.take(8n, path, off);
        return this.checked(new Float(new DataView(s.buffer, s.byteOffset, 8).getFloat64(0, true)), path, off);
      }
      case DATE: return this.checked(new Day(num(this.zz(path, off))), path, off);
      case DT: return this.checked(new Date(num(this.zz(path, off))), path, off);
      case TIME: return this.checked(new TimeOfDay(num(this.varint(path, off))), path, off);
      case DUR: return this.checked(new Duration(num(this.zz(path, off))), path, off);
      case LSTR: return this.str(this.varint(path, off) + BigInt(MAX_STR + 1), path, off);
      case LBYTES: return this.take(this.varint(path, off) + BigInt(MAX_BYTES + 1), path, off).slice();
      case LROW: return this.nested(this.varint(path, off) + BigInt(MAX_SROW + 1), path, depth, off);
    }
    throw this.fail("R004", path, off, `reserved tag 0x${t.toString(16).toUpperCase()}`);
  }
}

// decode returns the row the bytes hold; R002 if bytes follow it.
export function decode(bytes) {
  const d = new Decoder(bytes);
  const r = d.row(null, 1, null);
  if (d.i !== bytes.length) throw new RowError("R002", null, d.i, 0, `${bytes.length - d.i} bytes after the row`);
  return r;
}

// The text.

const pad = (n, w) => String(n).padStart(w, "0");

// group puts an underscore between every three digits of a whole part
// of five digits or more.
function group(s) {
  let sign = "";
  if (s[0] === "-") { sign = "-"; s = s.slice(1); }
  if (s.length < 5) return sign + s;
  let first = s.length % 3 || 3;
  let out = s.slice(0, first);
  for (let i = first; i < s.length; i += 3) out += "_" + s.slice(i, i + 3);
  return sign + out;
}
function formatDecimal(v) {
  let m = v.mantissa, sign = "";
  if (m < 0) { sign = "-"; m = -m; }
  let digits = String(m);
  if (v.scale === 0) return group(sign + digits);
  while (digits.length <= v.scale) digits = "0" + digits;
  const cut = digits.length - v.scale;
  return group(sign + digits.slice(0, cut)) + "." + digits.slice(cut);
}
// formatFloat is JavaScript's own shortest spelling, plain from 1e-6
// up to 1e21, with the whole part grouped and an exponent's plus sign
// dropped.
function formatFloat(f) {
  const s = String(f);
  if (s.includes("e")) return s.replace("e+", "e");
  const [whole, frac] = s.split(".");
  return frac === undefined ? group(whole) : group(whole) + "." + frac;
}
function formatDate(day) {
  const [y, m, d] = civilFromDays(day);
  return `${pad(y, 4)}-${pad(m, 2)}-${pad(d, 2)}`;
}
function formatTime(ms) {
  return `${pad(Math.floor(ms / 3600000), 2)}:${pad(Math.floor(ms / 60000) % 60, 2)}:${pad(Math.floor(ms / 1000) % 60, 2)}.${pad(ms % 1000, 3)}`;
}
function formatDuration(ms) {
  if (ms === 0) return "0s";
  let out = "";
  if (ms < 0) { out = "-"; ms = -ms; }
  const h = Math.floor(ms / 3600000), m = Math.floor(ms / 60000) % 60, s = Math.floor(ms / 1000) % 60, r = ms % 1000;
  if (h > 0) out += `${h}h`;
  if (m > 0) out += `${m}m`;
  if (s > 0 || r > 0) out += String(s) + (r > 0 ? "." + pad(r, 3).replace(/0+$/, "") : "") + "s";
  return out;
}
const ESC = { '"': '\\"', "\\": "\\\\", "\b": "\\b", "\f": "\\f", "\n": "\\n", "\r": "\\r", "\t": "\\t" };
function quote(s) {
  let out = '"';
  for (const ch of s) {
    const c = ch.charCodeAt(0);
    if (ESC[ch]) out += ESC[ch];
    else if (c < 0x20) out += "\\u" + pad(c.toString(16), 4);
    else out += ch;
  }
  return out + '"';
}
const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

// writeValue is a checked value's canonical spelling (ROW.t 6.5).
function writeValue(v) {
  switch (typeName(v)) {
    case "bool": return String(v);
    case "int": return group(String(v));
    case "dec": return formatDecimal(v);
    case "f64": return formatFloat(v.value);
    case "date": return formatDate(v.days);
    case "utc": {
      const t = v.getTime();
      const day = Math.floor(t / 86400000);
      return `${formatDate(day)}T${formatTime(t - day * 86400000)}Z`;
    }
    case "time": return formatTime(v.ms);
    case "dur": return formatDuration(v.ms);
    case "str": return quote(v);
    case "bin": return "0x" + hex(v);
  }
}

function writeItems(lines, prefix, r, path) {
  r.forEach((v, i) => {
    const p = [...path, i];
    const name = typeName(v);
    if (name === "row") {
      lines.push(`${prefix}${p.join(".")}: row`);
      writeItems(lines, prefix, v, p);
    } else if (name === "none") lines.push(`${prefix}${p.join(".")}: none`);
    else lines.push(`${prefix}${p.join(".")}: ${name} = ${writeValue(v)}`);
  });
}

// format returns the canonical text of a row: one line per item, each
// ended by a newline; the empty row is "".
export function format(row) {
  checkItems(row, null, 1);
  const lines = [];
  writeItems(lines, "", row, []);
  return lines.map((l) => l + "\n").join("");
}

// formatLabeled returns the canonical text of [{label, row}, ...], in
// their order.
export function formatLabeled(rows) {
  const seen = new Set();
  const lines = [];
  for (const { label, row } of rows) {
    if (typeof label !== "string" || !validUTF16(label)) throw fail("V008", null, "a label that is no UTF-8 string");
    if (seen.has(label)) throw fail("T005", null, `label ${JSON.stringify(label)} twice`);
    seen.add(label);
    checkItems(row, null, 1);
    lines.push(`${quote(label)}: row`);
    writeItems(lines, quote(label) + " ", row, []);
  }
  return lines.map((l) => l + "\n").join("");
}

const lineErr = (code, line, path, msg) => new RowError(code, path ? [...path] : null, -1, line, msg);
const isSpace = (c) => c === " " || c === "\t";
const isDigit = (c) => c >= "0" && c <= "9";
const skip = (s, i) => { while (i < s.length && isSpace(s[i])) i++; return i; };
const KNOWN = new Set(["none", "bool", "int", "dec", "f64", "date", "utc", "time", "dur", "str", "bin", "row"]);

// unquote reads a JSON string at the start of s: [value, bytes taken].
function unquote(s) {
  if (s[0] !== '"') throw new Error("no opening quote");
  let out = "";
  let i = 1;
  const SIMPLE = { '"': '"', "\\": "\\", "/": "/", b: "\b", f: "\f", n: "\n", r: "\r", t: "\t" };
  while (i < s.length) {
    const c = s[i];
    if (c === '"') return [out, i + 1];
    if (c.charCodeAt(0) < 0x20) throw new Error("a raw control character in a string");
    if (c !== "\\") { out += c; i++; continue; }
    const e = s[i + 1];
    if (e === undefined) throw new Error("a string ends in a backslash");
    i += 2;
    if (e in SIMPLE) { out += SIMPLE[e]; continue; }
    if (e === "u") {
      const h = s.slice(i, i + 4);
      if (!/^[0-9a-fA-F]{4}$/.test(h)) throw new Error("a broken \\u escape");
      out += String.fromCharCode(parseInt(h, 16));
      i += 4;
      continue;
    }
    throw new Error(`an unknown escape \\${e}`);
  }
  throw new Error("no closing quote");
}

function parsePath(s) {
  if (s === "") throw new Error("an empty path");
  const parts = s.split(".");
  if (parts.length > MAX_DEPTH) throw new Error("more than four positions");
  return parts.map((p) => {
    if (p === "") throw new Error("an empty position");
    if (![...p].every(isDigit)) throw new Error(`"${p}" is not digits`);
    if (p.length > 1 && p[0] === "0") throw new Error(`a leading zero in "${p}"`);
    const n = Number(p);
    if (!inInt(n)) throw new Error(`position "${p}" too large`);
    return n;
  });
}

function parseLine(s, num) {
  const l = { num, labeled: false, label: "", path: null, header: false, value: undefined };
  let i = skip(s, 0);
  if (s[i] === '"') {
    let lab, n;
    try { [lab, n] = unquote(s.slice(i)); } catch (e) { throw lineErr("T001", num, null, `a broken label: ${e.message}`); }
    if (!validUTF16(lab)) throw lineErr("V008", num, null, "a label not valid UTF-8");
    l.labeled = true; l.label = lab;
    i = skip(s, i + n);
  }
  const colon = s.indexOf(":", i);
  if (colon < 0) throw lineErr("T001", num, null, "no colon");
  const ptext = s.slice(i, colon).replace(/[ \t]+$/, "");
  if (ptext === "" && l.labeled) l.path = []; // the labelled row's own line
  else {
    try { l.path = parsePath(ptext); } catch (e) { throw lineErr("T002", num, null, `${JSON.stringify(ptext)}: ${e.message}`); }
  }
  const path = l.path;
  i = skip(s, colon + 1);
  let j = i;
  while (j < s.length && (isDigit(s[j]) || (s[j] >= "a" && s[j] <= "z"))) j++;
  const name = s.slice(i, j);
  if (name === "") throw lineErr("T001", num, path, "no type name");
  const k = skip(s, j);
  let rest = "", hasValue = false;
  if (k < s.length) {
    if (s[k] !== "=") {
      if (!KNOWN.has(name) || j === k) throw lineErr("T003", num, path, `unknown type name ${JSON.stringify(s.slice(i).split(/[ \t=]/)[0])}`);
      throw lineErr("T001", num, path, "stray text after the type");
    }
    hasValue = true;
    rest = s.slice(skip(s, k + 1)).replace(/[ \t]+$/, "");
  }
  if (!KNOWN.has(name)) throw lineErr("T003", num, path, `unknown type name ${JSON.stringify(name)}`);
  if (path.length === 0 && name !== "row") throw lineErr("T002", num, null, "a line without a path is its row's own, of type row");
  if (name === "none" || name === "row") {
    if (hasValue) throw lineErr("T004", num, path, `a value on a ${name} line`);
    if (name === "row") {
      if (path.length === MAX_DEPTH) throw lineErr("R005", num, path, "rows nested deeper than four levels");
      l.header = true;
    } else l.value = null;
    return l;
  }
  if (!hasValue || rest === "") throw lineErr("T004", num, path, `no value for ${name}`);
  try { l.value = readValue(name, rest); } catch (e) {
    if (e instanceof RowError) throw lineErr(e.code, num, path, e.message.replace(/^row: \S+: /, ""));
    throw e;
  }
  return l;
}

function scan(text) {
  const out = [];
  text.split("\n").forEach((s, n) => {
    if (s.replace(/[ \t]/g, "") === "") return;
    out.push(parseLine(s, n + 1));
  });
  return out;
}

// assemble builds a row from its lines; a labelled row's lines include
// its own, with no path.
function assemble(lines, labelled) {
  const byPath = new Map();
  for (const l of lines) {
    const key = l.path.join(".");
    if (byPath.has(key)) throw lineErr("T005", l.num, l.path, `position given twice (also line ${byPath.get(key).num})`);
    byPath.set(key, l);
  }
  const root = byPath.get("");
  if (labelled && !root) throw lineErr("T006", lines[0].num, null, `row ${JSON.stringify(lines[0].label)} has no line of its own`);
  const children = new Map();
  for (const l of lines) {
    if (l.path.length === 0) continue;
    const parent = l.path.slice(0, -1).join(".");
    if (l.path.length > 1) {
      const p = byPath.get(parent);
      if (!p) throw lineErr("T006", l.num, l.path, `its row ${parent} has no line`);
      if (!p.header) throw lineErr("T006", l.num, l.path, `its row ${parent} is not a row`);
    }
    if (!children.has(parent)) children.set(parent, []);
    children.get(parent).push(l);
  }
  const build = (key, path) => {
    const kids = (children.get(key) ?? []).slice().sort((a, b) => a.path[path.length] - b.path[path.length]);
    return kids.map((l, i) => {
      if (l.path[path.length] !== i) {
        // at the line after the gap: a top-level row has no line of its own
        const missing = [...path, i];
        throw lineErr("T005", l.num, missing, `position ${missing.join(".")} missing`);
      }
      return l.header ? build(l.path.join("."), l.path) : l.value;
    });
  };
  return build("", []);
}

// parse returns the row of an unlabelled text, its lines in any order
// and its values in any spelling their types read.
export function parse(text) {
  const lines = scan(text);
  for (const l of lines) {
    if (l.labeled) throw lineErr("T007", l.num, null, "a labelled line in an unlabelled text (parseLabeled reads labelled ones)");
  }
  return assemble(lines, false);
}

// parseLabeled returns [{label, row}, ...] of a labelled text, in the
// order its labels first appear.
export function parseLabeled(text) {
  const order = [];
  const by = new Map();
  for (const l of scan(text)) {
    if (!l.labeled) throw lineErr("T007", l.num, null, "an unlabelled line in a labelled text");
    if (!by.has(l.label)) { order.push(l.label); by.set(l.label, []); }
    by.get(l.label).push(l);
  }
  return order.map((label) => ({ label, row: assemble(by.get(label), true) }));
}

// The values (ROW.t 6.5): any spelling the type reads as the same value.

const bad = (msg) => fail("T004", null, msg);
const outside = (code, msg) => fail(code, null, msg);

// digitRun reads digits with single underscores between two digits:
// [digits, index after] or null for a stray underscore.
function digitRun(s, i) {
  let out = "", j = i;
  while (j < s.length) {
    const c = s[j];
    if (isDigit(c)) { out += c; j++; continue; }
    if (c === "_") {
      if (j === i || !isDigit(s[j - 1] ?? "") || !isDigit(s[j + 1] ?? "")) return null;
      j++;
      continue;
    }
    break;
  }
  return [out, j];
}

function parseInt53(s) {
  let i = 0, sign = 1n;
  if (s[0] === "+" || s[0] === "-") { if (s[0] === "-") sign = -1n; i = 1; }
  const run = digitRun(s, i);
  if (!run || run[0] === "" || run[1] !== s.length) throw bad(`${JSON.stringify(s)} is not an int`);
  const n = sign * BigInt(run[0]);
  if (n > BigInt(MAX_INT) || n < -BigInt(MAX_INT)) throw outside("V001", `int ${s} outside ±(2^53-1)`);
  return Number(n) || 0;
}

function parseDecimal(s) {
  const no = () => bad(`${JSON.stringify(s)} is not a decimal`);
  let i = 0, sign = 1n;
  if (s[0] === "+" || s[0] === "-") { if (s[0] === "-") sign = -1n; i = 1; }
  const w = digitRun(s, i);
  if (!w) throw no();
  let [whole, j] = w, frac = "";
  if (s[j] === ".") {
    const f = digitRun(s, j + 1);
    if (!f) throw no();
    [frac, j] = f;
  }
  if (j !== s.length || whole + frac === "") throw no();
  if (frac.length > MAX_SCALE) throw outside("V002", `decimal ${s}: scale ${frac.length} above 15`);
  const m = sign * BigInt(whole + frac);
  if (m > BigInt(MAX_INT) || m < -BigInt(MAX_INT)) throw outside("V002", `decimal ${s}: mantissa outside ±(2^53-1)`);
  return new Decimal(Number(m) || 0, frac.length);
}

function parseFloatValue(s) {
  const no = () => bad(`${JSON.stringify(s)} is not a float`);
  let i = 0, t = "";
  if (s[0] === "+" || s[0] === "-") { t += s[0]; i = 1; }
  const w = digitRun(s, i);
  if (!w) throw no();
  let [whole, j] = w, frac = "";
  t += whole;
  if (s[j] === ".") {
    const f = digitRun(s, j + 1);
    if (!f) throw no();
    [frac, j] = f;
    t += "." + frac;
  }
  if (whole + frac === "") throw no();
  if (s[j] === "e" || s[j] === "E") {
    t += "e";
    j++;
    if (s[j] === "+" || s[j] === "-") t += s[j++];
    const e = digitRun(s, j);
    if (!e || e[0] === "") throw no();
    t += e[0];
    j = e[1];
  }
  if (j !== s.length) throw no();
  if (t.startsWith(".") || t.startsWith("+.") || t.startsWith("-.")) t = t.replace(".", "0.");
  const f = Number(t);
  if (!Number.isFinite(f) || Object.is(f, -0)) throw outside("V003", `float ${s} is infinite or -0`);
  return new Float(f);
}

function fixed(s, i, n) {
  const t = s.slice(i, i + n);
  return t.length === n && [...t].every(isDigit) ? Number(t) : null;
}

// parseDate reads the date YYYY-MM-DD at the start of s, a year of four
// digits or more: [day, length, inRange], or null if it is no date. Year
// 0 is a leap year, as the proleptic calendar has it; a year past 99999
// is far enough beyond to stand as 100000.
function parseDate(s) {
  const k = s.indexOf("-");
  if (k < 4 || s.length < k + 6 || s[k + 3] !== "-") return null;
  const y = k <= 5 ? fixed(s, 0, k) : [...s.slice(0, k)].every(isDigit) ? 100000 : null;
  const m = fixed(s, k + 1, 2), d = fixed(s, k + 4, 2);
  if (y === null || m === null || d === null || m < 1 || m > 12 || d < 1 || d > daysIn(y, m)) return null;
  return [daysFromCivil(y, m, d), k + 6, y >= 1 && y <= 9999];
}

// clock reads HH:MM[:SS[.f...]] at the start of s: {ms, fine, j} or
// null, or {leap: true}.
function clock(s, seconds) {
  const h = fixed(s, 0, 2), mi = fixed(s, 3, 2);
  if (h === null || s[2] !== ":" || mi === null || h > 23 || mi > 59) return null;
  let ms = (h * 60 + mi) * 60000, j = 5, fine = false;
  if (s[j] === ":") {
    const sec = fixed(s, 6, 2);
    if (sec === 60) return { leap: true };
    if (sec === null || sec > 59) return null;
    ms += sec * 1000;
    j = 8;
    if (s[j] === ".") {
      let k = j + 1;
      while (k < s.length && isDigit(s[k])) k++;
      let f = s.slice(j + 1, k);
      if (f === "") return null;
      while (f.length < 3) f += "0";
      ms += Number(f.slice(0, 3));
      fine = f.slice(3).replace(/0/g, "") !== "";
      j = k;
    }
  } else if (seconds) return null;
  return { ms, fine, j };
}

function parseTime(s) {
  const c = clock(s, false);
  if (!c || c.leap || c.j !== s.length) throw bad(`${JSON.stringify(s)} is not a time`);
  if (c.fine) throw outside("V006", `time ${s} finer than a millisecond`);
  return new TimeOfDay(c.ms);
}

function parseDatetime(s) {
  const no = () => bad(`${JSON.stringify(s)} is not a datetime`);
  const d = parseDate(s);
  if (!d || s.length <= d[1] || !"Tt ".includes(s[d[1]])) throw no();
  const rest = s.slice(d[1] + 1);
  const c = clock(rest, true);
  if (!c) throw no();
  if (c.leap) throw outside("V005", `datetime ${s}: a leap second`);
  const zone = rest.slice(c.j);
  let offset = 0;
  if (zone === "Z" || zone === "z") offset = 0;
  else if (zone.length === 6 && "+-".includes(zone[0]) && zone[3] === ":") {
    const oh = fixed(zone, 1, 2), om = fixed(zone, 4, 2);
    if (oh === null || om === null || oh > 23 || om > 59) throw no();
    offset = (oh * 60 + om) * 60000 * (zone[0] === "-" ? -1 : 1);
  } else throw no();
  if (c.fine) throw outside("V005", `datetime ${s} finer than a millisecond`);
  // in range by the instant, whatever the year written: an offset can
  // carry 0000-12-31 into 0001
  const t = d[0] * 86400000 + c.ms - offset;
  if (t < MIN_DT || t > MAX_DT) throw outside("V005", `datetime ${s} outside 0001 to 9999`);
  return new Date(t);
}

// parseDuration reads parts h, m, s and ms in that order, each once;
// the total must be whole milliseconds. Exact, in BigInt thousandths
// scaled by the fraction's length.
function parseDuration(s) {
  const no = () => bad(`${JSON.stringify(s)} is not a duration`);
  let i = 0, neg = false;
  if (s[0] === "-") { neg = true; i = 1; }
  const UNITS = [["h", 3600000n], ["m", 60000n], ["s", 1000n], ["ms", 1n]];
  let num = 0n, den = 1n, next = 0, parts = 0;
  while (i < s.length) {
    const w = digitRun(s, i);
    if (!w) throw no();
    let [whole, j] = w, frac = "";
    if (s[j] === ".") {
      const f = digitRun(s, j + 1);
      if (!f) throw no();
      [frac, j] = f;
    }
    if (whole + frac === "") throw no();
    let k = j;
    while (k < s.length && s[k] >= "a" && s[k] <= "z") k++;
    const unit = s.slice(j, k);
    let u = -1;
    for (let n = next; n < UNITS.length; n++) if (UNITS[n][0] === unit) { u = n; break; }
    if (u < 0) throw no();
    next = u + 1;
    // num/den += (whole.frac) * unit
    const d = 10n ** BigInt(frac.length);
    const part = BigInt((whole || "0") + frac) * UNITS[u][1];
    num = num * d + part * den;
    den *= d;
    parts++;
    i = k;
  }
  if (parts === 0) throw no();
  if (num % den !== 0n) throw outside("V007", `duration ${s} finer than a millisecond`);
  let ms = num / den;
  if (neg) ms = -ms;
  if (ms > BigInt(MAX_INT) || ms < -BigInt(MAX_INT)) throw outside("V007", `duration ${s} outside ±(2^53-1) ms`);
  return new Duration(Number(ms) || 0);
}

function readValue(name, s) {
  switch (name) {
    case "bool":
      if (s === "true") return true;
      if (s === "false") return false;
      throw bad(`${JSON.stringify(s)} is not a bool`);
    case "int": return parseInt53(s);
    case "dec": return parseDecimal(s);
    case "f64": return parseFloatValue(s);
    case "date": {
      const d = parseDate(s);
      if (!d || d[1] !== s.length) throw bad(`${JSON.stringify(s)} is not a date`);
      if (!d[2]) throw outside("V004", `date ${s} outside 0001-01-01 to 9999-12-31`);
      return new Day(d[0]);
    }
    case "utc": return parseDatetime(s);
    case "time": return parseTime(s);
    case "dur": return parseDuration(s);
    case "str": {
      let v, n;
      try { [v, n] = unquote(s); } catch (e) { throw bad(e.message); }
      if (n !== s.length) throw bad("text after the string");
      if (!validUTF16(v)) throw outside("V008", "string not valid UTF-8");
      return v;
    }
    case "bin": {
      if (!/^0[xX]/.test(s)) throw bad(`${JSON.stringify(s)} does not start 0x`);
      const h = s.slice(2);
      if (h.length % 2) throw bad(`${JSON.stringify(s)} has an odd number of hex digits`);
      if (!/^[0-9a-fA-F]*$/.test(h)) throw bad(`${JSON.stringify(s)} is not hex`);
      return Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16));
    }
  }
  throw bad(`unknown type ${name}`);
}

// parseValue returns the value text spells in the type named type
// (ROW.t 6.5): one value as it stands after the = of a line, with no
// spaces around it, in any spelling its type reads, never altered to
// fit. none and row have no value text (T004); an unknown type name is
// T003.
export function parseValue(type, text) {
  if (!KNOWN.has(type)) throw fail("T003", null, `unknown type name ${JSON.stringify(type)}`);
  if (type === "none" || type === "row") throw bad(`a ${type} has no value text`);
  if (text === "") throw bad(`no value for ${type}`);
  return readValue(type, text);
}

// formatValue returns the canonical text of one value (ROW.t 6.5),
// checking it first as encode does. none and a row have no value text
// (T004): a row's text is format's.
export function formatValue(v) {
  check(v, null, 1); // first, as Go's: a row holding NaN is V003
  const t = typeName(v);
  if (t === "none" || t === "row") throw bad(`a ${t} has no value text`);
  return writeValue(v);
}

