// Runs row.js against the vectors; js_test.go loads it in headless
// Chrome and reads the failures it returns.
import { encode, decode, format, parse, formatLabeled, parseLabeled, RowError } from "/row.js";

const unhex = (h) => Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16));
const tohex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
const path = (e) => (e.path ? e.path.join(".") : "");

export async function run() {
  const v = await (await fetch("/vectors.json")).json();
  const fails = [];
  const check = (ok, msg) => { if (!ok) fails.push(msg); };
  const attempt = (f) => { try { return [f(), null]; } catch (e) { return [null, e]; } };
  for (const c of v.valid) {
    const [row, e1] = attempt(() => decode(unhex(c.hex)));
    if (e1) { fails.push(`decode ${c.hex}: ${e1.message}`); continue; }
    const [again, e2] = attempt(() => tohex(encode(row)));
    check(again === c.hex, `encode(decode(${c.hex})) = ${again} ${e2 ? e2.message : ""}`);
    const [text, e3] = attempt(() => format(row));
    check(text === c.text, `format(decode(${c.hex})) = ${JSON.stringify(text)} ${e3 ? e3.message : ""}; want ${JSON.stringify(c.text)}`);
    const [parsed, e4] = attempt(() => parse(c.text));
    if (e4) { fails.push(`parse ${JSON.stringify(c.text)}: ${e4.message}`); continue; }
    const [bytes, e5] = attempt(() => tohex(encode(parsed)));
    check(bytes === c.hex, `encode(parse(${JSON.stringify(c.text)})) = ${bytes} ${e5 ? e5.message : ""}; want ${c.hex}`);
  }
  for (const c of v.labeled) {
    const rows = c.rows.map((r) => ({ label: r.label, row: decode(unhex(r.hex)) }));
    const [text, e1] = attempt(() => formatLabeled(rows));
    check(text === c.text, `formatLabeled = ${JSON.stringify(text)} ${e1 ? e1.message : ""}; want ${JSON.stringify(c.text)}`);
    const [got, e2] = attempt(() => parseLabeled(c.text));
    if (e2) { fails.push(`parseLabeled ${JSON.stringify(c.text)}: ${e2.message}`); continue; }
    check(got.length === c.rows.length, `parseLabeled: ${got.length} rows, want ${c.rows.length}`);
    got.forEach((g, i) => {
      const h = tohex(encode(g.row));
      check(g.label === c.rows[i].label && h === c.rows[i].hex, `row ${i} = ${g.label} ${h}, want ${c.rows[i].label} ${c.rows[i].hex}`);
    });
  }
  for (const c of v.lenient) {
    const [text, e] = attempt(() => format(parse(c.text)));
    check(text === c.canonical, `format(parse(${JSON.stringify(c.text)})) = ${JSON.stringify(text)} ${e ? e.message : ""}; want ${JSON.stringify(c.canonical)}`);
  }
  for (const c of v.badBinary) {
    const [, e] = attempt(() => decode(unhex(c.hex)));
    check(e instanceof RowError && e.code === c.code && path(e) === c.path,
      `decode ${c.hex}: ${e ? e.message : "no error"}; want ${c.code} at "${c.path}"`);
  }
  for (const c of v.badText) {
    const [, e] = attempt(() => parse(c.text));
    check(e instanceof RowError && e.code === c.code && path(e) === c.path && e.line === c.line,
      `parse ${JSON.stringify(c.text)}: ${e ? e.message : "no error"}; want ${c.code} at "${c.path}" line ${c.line}`);
  }
  for (const c of v.badLabeled) {
    const [, e] = attempt(() => parseLabeled(c.text));
    check(e instanceof RowError && e.code === c.code && path(e) === c.path && e.line === c.line,
      `parseLabeled ${JSON.stringify(c.text)}: ${e ? e.message : "no error"}; want ${c.code} at "${c.path}" line ${c.line}`);
  }
  const count = v.valid.length + v.labeled.length + v.lenient.length + v.badBinary.length + v.badText.length + v.badLabeled.length;
  return { fails, count };
}
