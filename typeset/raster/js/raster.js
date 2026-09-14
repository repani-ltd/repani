// raster.js: a reader and DOM painter for rasters (RASTER.t). A
// second implementation of the specification beside the Go one; the
// fixture test holds the two to the same answers. ES module, no
// dependencies, runs in a browser or in node.

export const COLS = 40, MAX_ROWS = 1024;

// The cell table: the display character of every glyph byte. Blanks
// and unassigned values are a space.
export const TABLE = (() => {
  const t = new Array(256).fill(' ');
  const set = (at, s) => { let i = at; for (const r of s) t[i++] = r; };
  set(0x01, '─│←↑→↓░▒▓█°±×÷•·');
  for (let b = 0x20; b <= 0x7e; b++) t[b] = String.fromCharCode(b);
  t[0x7f] = '€';
  set(0x90, '☀☁☂☾❄↯⚠');
  set(0x97, '‘’“”–—');
  set(0x9d, '☺☹♥★✓✗●○£');
  set(0xc0, 'αβγδεζηθικλμνξοπρςστυφχψω');
  set(0xd9, 'άέήίόύώϊϋΐΰ');
  set(0xe4, 'ΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩ');
  set(0xfc, '«»…―');
  return t;
})();

const blankRow = () => Array.from({ length: COLS }, () => ({ glyph: 0, fg: 0, bg: 0 }));

// read folds a stream of row records into rows: an array indexed by
// row, each COLS cells {glyph, fg, bg}, as long as the highest row
// read plus one. A record is a little-endian 16-bit header, the row
// in its high ten bits and the length N in its low six, then N
// glyph bytes, then N ink bytes (background high nibble, foreground
// low); cells past N are blank. A row repeated replaces its earlier
// value; a record of length 0 clears its row. Records may be applied
// onto existing rows, for an update.
export function read(bytes, rows = []) {
  let at = 0;
  while (at < bytes.length) {
    if (bytes.length - at < 2) throw new Error(`raster: byte ${at}: record header cut short`);
    const h = bytes[at] | bytes[at + 1] << 8;
    const i = h >> 6, n = h & 0x3f;
    at += 2;
    if (n > COLS) throw new Error(`raster: byte ${at - 2}: row ${i} has length ${n}`);
    if (bytes.length - at < 2 * n) throw new Error(`raster: byte ${at - 2}: row ${i} cut short`);
    while (rows.length <= i) rows.push(blankRow());
    const row = blankRow();
    for (let k = 0; k < n; k++) {
      const ink = bytes[at + n + k];
      if (ink & 0x88) throw new Error(`raster: byte ${at + n + k}: ink ${ink} is not two palette indices`);
      row[k] = { glyph: bytes[at + k], fg: ink & 0x07, bg: ink >> 4 };
    }
    rows[i] = row;
    at += 2 * n;
  }
  return rows;
}

const blank = c => c.glyph === 0 || c.glyph === 0x20;

// links returns a row's links: an opening bracket to the next closing
// bracket, brackets included, the text between them the target.
export function links(cells) {
  const out = [];
  for (let x = 0; x < cells.length; x++) {
    if (cells[x].glyph !== 0x5b) continue; // [
    let end = -1;
    for (let y = x + 1; y < cells.length; y++) if (cells[y].glyph === 0x5d) { end = y; break; } // ]
    if (end < 0) break;
    if (end > x + 1) out.push({ col: x, len: end - x + 1, target: cells.slice(x + 1, end).map(c => TABLE[c.glyph]).join('') });
    x = end;
  }
  return out;
}

// text renders a row plain, trimmed on the right.
export function text(cells) {
  let end = cells.length;
  while (end > 0 && blank(cells[end - 1])) end--;
  return cells.slice(0, end).map(c => TABLE[c.glyph]).join('');
}

const escape = s => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&#34;').replace(/'/g, '&#39;');
const same = (a, b) => a.fg === b.fg && a.bg === b.bg;

// html renders a row as the Go renderer does: <span class="fN bM">
// per ink run, bare text in default ink, an <a href="#target"> around
// every link, brackets included.
export function html(cells) {
  let out = '';
  let open = { fg: 0, bg: 0 }, inSpan = false;
  const closeSpan = () => { if (inSpan) { out += '</span>'; inSpan = false; } };
  const ls = links(cells);
  let li = 0, linkEnd = -1;
  for (let x = 0; x < cells.length; x++) {
    const cell = cells[x];
    if (li < ls.length && ls[li].col === x) {
      closeSpan();
      out += `<a href="#${escape(ls[li].target)}">`;
      linkEnd = x + ls[li].len;
      li++;
    }
    let s = { fg: cell.fg, bg: cell.bg };
    if (blank(cell)) s = cell.bg === open.bg ? open : { fg: 0, bg: cell.bg };
    if (!same(s, open) || (!inSpan && (s.fg !== 0 || s.bg !== 0))) {
      closeSpan();
      if (s.fg !== 0 || s.bg !== 0) { out += `<span class="f${s.fg} b${s.bg}">`; inSpan = true; }
      open = s;
    }
    out += escape(TABLE[cell.glyph]);
    if (x + 1 === linkEnd) { closeSpan(); out += '</a>'; open = { fg: 0, bg: 0 }; linkEnd = -1; }
  }
  closeSpan();
  return out;
}

// paint writes rows into a <pre> element, one child element per row,
// and calls onTap with a link's target when one is clicked or tapped.
// Painting again replaces only the rows whose rendering changed, so
// rows pushed to replace the ones shown repaint in place and keep
// focus on the rows they did not touch. Synchronous: call it from the
// handler that received the bytes so it lands in the current frame.
export function paint(pre, rows, onTap) {
  const html_ = rows.map(html);
  if (pre.childElementCount !== html_.length) {
    pre.replaceChildren(...html_.map(h => { const d = document.createElement('div'); d.innerHTML = h; return d; }));
  } else {
    html_.forEach((h, i) => { const d = pre.children[i]; if (d.innerHTML !== h) d.innerHTML = h; });
  }
  if (onTap && !pre.rasterTap) {
    pre.rasterTap = true;
    pre.addEventListener('click', e => {
      const a = e.target.closest('a');
      if (!a) return;
      e.preventDefault();
      onTap(decodeURIComponent(a.getAttribute('href').slice(1)), a);
    });
  }
}
