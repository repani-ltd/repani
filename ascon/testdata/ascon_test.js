// ascon_test.js -- runs ascon.js against the known-answer vectors
// the Go package is tested with (LWC_*_KAT_*.txt here), in a browser;
// js_test.go drives it.
import { xof, cxof, aeadSeal, aeadOpen } from "/ascon.js";

const hex = (s) => Uint8Array.from((s.match(/../g) ?? []).map((b) => parseInt(b, 16)));
const toHex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("").toUpperCase();

// kat reads a KAT file into its records, each {Field: "HEX", ...}.
async function kat(name) {
  const text = await (await fetch("/kat/" + name)).text();
  return text.split(/\n\s*\n/).filter((b) => b.trim()).map((b) =>
    Object.fromEntries(b.trim().split("\n").map((l) => l.split(" = ").map((x) => (x ?? "").trim()))));
}

export async function run() {
  const fails = [];
  let count = 0;
  const check = (what, got, want) => {
    count++;
    if (got !== want) fails.push(`${what}: got ${got}, want ${want}`);
  };
  for (const v of await kat("LWC_XOF_KAT_128_512.txt")) {
    check(`XOF ${v.Count}`, toHex(xof(hex(v.Msg), 64)), v.MD);
  }
  for (const v of await kat("LWC_CXOF_KAT_128_512.txt")) {
    check(`CXOF ${v.Count}`, toHex(cxof(hex(v.Z), hex(v.Msg), 64)), v.MD);
  }
  for (const v of await kat("LWC_AEAD_KAT_128_128.txt")) {
    const ct = aeadSeal(hex(v.Key), hex(v.Nonce), hex(v.PT), hex(v.AD));
    check(`AEAD seal ${v.Count}`, toHex(ct), v.CT);
    const pt = aeadOpen(hex(v.Key), hex(v.Nonce), ct, hex(v.AD));
    check(`AEAD open ${v.Count}`, pt && toHex(pt), v.PT);
    if (ct.length) {
      ct[0] ^= 1;
      check(`AEAD forged ${v.Count}`, aeadOpen(hex(v.Key), hex(v.Nonce), ct, hex(v.AD)), null);
    }
  }
  return { fails, count };
}
