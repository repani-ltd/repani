// Runs row.js against the frozen vectors; js_test.go loads it in
// headless Chrome and reads the failures it returns.
import { encode, decode, format, parse, RowError } from "/row.js";

const unhex = (h) => Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16));
const tohex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

export async function run() {
  const v = await (await fetch("/vectors.json")).json();
  const fails = [];
  const check = (ok, msg) => { if (!ok) fails.push(msg); };
  const attempt = (f) => { try { return [f(), null]; } catch (e) { return [null, e]; } };
  for (const c of v.valid) {
    const [items, e1] = attempt(() => decode(unhex(c.hex)));
    if (e1) { fails.push(`decode ${c.hex}: ${e1.message}`); continue; }
    const [again, e2] = attempt(() => tohex(encode(items)));
    check(again === c.hex, `encode(decode(${c.hex})) = ${again} ${e2 ? e2.message : ""}`);
    const [text, e3] = attempt(() => format(items));
    check(text === c.text, `format(decode(${c.hex})) = ${JSON.stringify(text)} ${e3 ? e3.message : ""}; want ${JSON.stringify(c.text)}`);
    const [parsed, e4] = attempt(() => parse(c.text));
    if (e4) { fails.push(`parse ${JSON.stringify(c.text)}: ${e4.message}`); continue; }
    const [bytes, e5] = attempt(() => tohex(encode(parsed)));
    check(bytes === c.hex, `encode(parse(${JSON.stringify(c.text)})) = ${bytes} ${e5 ? e5.message : ""}; want ${c.hex}`);
  }
  for (const c of v.badBinary) {
    const [, e] = attempt(() => decode(unhex(c.hex)));
    check(e instanceof RowError && e.code === c.code && e.offset === c.offset,
      `decode ${c.hex}: ${e ? e.message : "no error"}; want ${c.code} at ${c.offset}`);
  }
  for (const c of v.badText) {
    const [, e] = attempt(() => parse(c.text));
    check(e instanceof RowError && e.code === c.code,
      `parse ${JSON.stringify(c.text)}: ${e ? e.message : "no error"}; want ${c.code}`);
  }
  return { fails, count: v.valid.length + v.badBinary.length + v.badText.length };
}
