// The fixture is the Go implementation's answer; this decoder must
// agree with it. Run: node --test lz4s_test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { undelta, decompress } from './lz4s.js';

const fixture = JSON.parse(readFileSync(new URL('./fixture.json', import.meta.url)));
const hex = h => Uint8Array.from((h.match(/../g) || []).map(x => parseInt(x, 16)));

for (const p of fixture.pages) {
  test(p.name, () => {
    const page = hex(p.page);
    assert.deepEqual(decompress(hex(p.comp), page.length), page);
    assert.equal(decompress(hex(p.comp), page.length - 1), null, 'wrong size');
    assert.equal(decompress(hex(p.comp).subarray(0, -3), page.length), null, 'truncated');
  });
}
for (const d of fixture.deltas) {
  test(`${d.src} against ${d.base}`, () => {
    const base = hex(fixture.pages.find(p => p.name === d.base).page);
    const src = hex(fixture.pages.find(p => p.name === d.src).page);
    assert.deepEqual(undelta(base, hex(d.delta), src.length), src);
    assert.equal(undelta(null, hex(d.delta), src.length), null, 'no base');
  });
}
test('known answers', () => {
  for (const k of fixture.known) assert.deepEqual(decompress(hex(k.comp), hex(k.src).length), hex(k.src));
  assert.deepEqual(decompress(new Uint8Array([0x00, 0x00]), 0), new Uint8Array(0));
  assert.equal(decompress(new Uint8Array([0x00]), 1), null);
});
