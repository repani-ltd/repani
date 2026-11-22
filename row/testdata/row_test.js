// Runs row.js against the vectors; js_test.go loads it in headless
// Chrome and reads the failures it returns.
import { encode, decode, format, parse, formatLabeled, parseLabeled, parseValue, formatValue, typeName, Decimal, Float, RowError } from "/row.js";

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
  // parseValue and formatValue read and write each value as parse and
  // format do, and refuse a bad one with the code parse gives it.
  const valueLine = /^[ \t]*([0-9.]+)[ \t]*:[ \t]*([a-z0-9]+)[ \t]*=[ \t]*(.*?)[ \t]*$/;
  const at = (r, p) => p.split(".").reduce((x, i) => x[Number(i)], r);
  let values = 0;
  for (const text of [...v.valid.map((c) => c.text), ...v.lenient.map((c) => c.text)]) {
    const row = parse(text);
    for (const l of text.split("\n")) {
      const m = valueLine.exec(l);
      if (!m) continue;
      const want = at(row, m[1]);
      const [got, e] = attempt(() => parseValue(m[2], m[3]));
      if (e) { fails.push(`parseValue(${m[2]}, ${JSON.stringify(m[3])}): ${e.message}`); continue; }
      check(tohex(encode([got])) === tohex(encode([want])) && typeName(got) === m[2],
        `parseValue(${m[2]}, ${JSON.stringify(m[3])}) is not what parse reads`);
      const canon = format([want]).replace(/\n$/, "").split(" = ").slice(1).join(" = ");
      const [s, e2] = attempt(() => formatValue(want));
      check(s === canon, `formatValue = ${JSON.stringify(s)} ${e2 ? e2.message : ""}; want ${JSON.stringify(canon)}`);
      values++;
    }
  }
  for (const c of v.badText) {
    const t = c.text.replace(/\n$/, "");
    const m = valueLine.exec(t);
    if (!m || t.includes("\n") || c.path !== "0") continue;
    const [, e] = attempt(() => parseValue(m[2], m[3]));
    check(e instanceof RowError && e.code === c.code, `parseValue(${m[2]}, ${JSON.stringify(m[3])}): ${e ? e.message : "no error"}; want ${c.code}`);
  }
  for (const [ty, tx, cd] of [["none", "", "T004"], ["row", "x", "T004"], ["int", "", "T004"], ["abc", "1", "T003"]]) {
    const [, e] = attempt(() => parseValue(ty, tx));
    check(e instanceof RowError && e.code === cd, `parseValue(${ty}, ${JSON.stringify(tx)}): ${e ? e.message : "no error"}; want ${cd}`);
  }
  // the value is checked before its kind, as in Go: a row holding NaN is V003
  for (const [val, cd] of [[null, "T004"], [[], "T004"], [new Decimal(1, 16), "V002"], [{}, "V009"], [[new Float(NaN)], "V003"]]) {
    const [, e] = attempt(() => formatValue(val));
    check(e instanceof RowError && e.code === cd, `formatValue(${JSON.stringify(val)}): ${e ? e.message : "no error"}; want ${cd}`);
  }
  // what no vector can hold: a program's values
  const refuse = (what, f, cd) => {
    const [, e] = attempt(f);
    check(e instanceof RowError && e.code === cd, `${what}: ${e ? e.message : "no error"}; want ${cd}`);
  };
  refuse("encode of a sparse array", () => encode([1, , 2]), "V009"); // eslint-disable-line no-sparse-arrays
  refuse("format of a sparse array", () => format([1, , 2]), "V009"); // eslint-disable-line no-sparse-arrays
  refuse("encode of 1.5", () => encode([1.5]), "V009");
  refuse("a label with a lone surrogate", () => formatLabeled([{ label: "\ud800", row: [] }]), "V008");
  check(typeName(1.5) === "", `typeName(1.5) = ${JSON.stringify(typeName(1.5))}, want "" (a float is a Float)`);
  check(values > 100, `only ${values} value lines checked`);
  const count = v.valid.length + v.labeled.length + v.lenient.length + v.badBinary.length + v.badText.length + v.badLabeled.length;
  return { fails, count };
}
