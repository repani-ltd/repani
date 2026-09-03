// The fixture is the Go implementation's answer; this decoder must
// agree with it. Run: node --test raster_test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { TABLE, decode, text, html, links } from './raster.js';

const fixture = JSON.parse(readFileSync(new URL('./fixture.json', import.meta.url)));
const fromHex = h => Uint8Array.from(h.match(/../g).map(x => parseInt(x, 16)));

test('cell table', () => {
  assert.deepEqual(TABLE, fixture.table);
});

for (const page of fixture.pages) {
  test(page.name, () => {
    const panels = decode(fromHex(page.bytes), page);
    for (let p = 0; p < page.panels; p++) {
      for (let r = 0; r < page.rows; r++) {
        const cells = panels[p][r];
        assert.equal(text(cells), page.text[p][r], `text panel ${p} row ${r}`);
        assert.equal(html(cells), page.html[p][r], `html panel ${p} row ${r}`);
        assert.deepEqual(links(cells).map(l => ({ Col: l.col, Len: l.len, Target: l.target })), page.links[p][r], `links panel ${p} row ${r}`);
      }
    }
  });
}
