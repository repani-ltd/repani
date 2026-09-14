// The fixture is the Go implementation's answer; this reader must
// agree with it. Run: node --test raster_test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { TABLE, read, text, html, links } from './raster.js';

const fixture = JSON.parse(readFileSync(new URL('./fixture.json', import.meta.url)));
const fromHex = h => Uint8Array.from((h.match(/../g) || []).map(x => parseInt(x, 16)));

test('cell table', () => {
  assert.deepEqual(TABLE, fixture.table);
});

for (const page of fixture.rasters) {
  test(page.name, () => {
    const rows = read(fromHex(page.bytes));
    assert.equal(rows.length, page.text.length, 'height');
    for (let r = 0; r < rows.length; r++) {
      const cells = rows[r];
      assert.equal(text(cells), page.text[r], `text row ${r}`);
      assert.equal(html(cells), page.html[r], `html row ${r}`);
      assert.deepEqual(links(cells).map(l => ({ Col: l.col, Len: l.len, Target: l.target })), page.links[r], `links row ${r}`);
    }
  });
}

test('stream folds: later records replace, length 0 clears', () => {
  const a = read(fromHex(fixture.stream.first));
  const b = read(fromHex(fixture.stream.second), a);
  assert.deepEqual(b.map(text), fixture.stream.text);
});
