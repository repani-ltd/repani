// raster.js: a decoder and DOM painter for raster pages (RASTER.t).
// A second implementation of the specification beside the Go one; the
// fixture test holds the two to the same answers. ES module, no
// dependencies, runs in a browser or in node.

// The cell table: the display character of every byte value. Blanks,
// ink codes and unassigned values are a space.
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

export const INK_FG = 0x80, INK_BG = 0x88, INK_LAST = 0x8f;
export const isInk = b => b >= INK_FG && b <= INK_LAST;

function apply(ink, b) {
  if (b >= INK_FG && b < INK_BG) return { fg: b - INK_FG, bg: ink.bg };
  if (b >= INK_BG && b <= INK_LAST) return { fg: ink.fg, bg: b - INK_BG };
  return ink;
}

// decodeRow turns one row's bytes into cells {glyph, fg, bg}: the
// tail (the codes at the very end) sets the opening ink; every other
// code sets the ink from its cell on and becomes an empty cell.
export function decodeRow(bytes) {
  let t = bytes.length;
  while (t > 0 && isInk(bytes[t - 1])) t--;
  let s = { fg: 0, bg: 0 };
  for (let x = t; x < bytes.length; x++) s = apply(s, bytes[x]);
  const cells = new Array(bytes.length);
  for (let x = 0; x < bytes.length; x++) {
    const b = bytes[x];
    if (x >= t) cells[x] = { glyph: 0, ...s };
    else if (isInk(b)) { s = apply(s, b); cells[x] = { glyph: 0, ...s }; }
    else cells[x] = { glyph: b, ...s };
  }
  return cells;
}

// decode turns a page's bytes into panels of rows of cells.
export function decode(bytes, { cols, rows, panels }) {
  if (bytes.length !== cols * rows * panels) throw new Error(`raster: ${bytes.length} bytes for ${cols}x${rows}x${panels}`);
  const out = [];
  for (let p = 0; p < panels; p++) {
    const panel = [];
    for (let r = 0; r < rows; r++) {
      const o = (p * rows + r) * cols;
      panel.push(decodeRow(bytes.subarray(o, o + cols)));
    }
    out.push(panel);
  }
  return out;
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

// paint writes one panel into a <pre> element, one child element per
// row, and calls onTap with a link's target when one is clicked or
// tapped. Painting again replaces only the rows whose rendering
// changed, so a page pushed to replace the one shown repaints in
// place and keeps focus on the rows it did not touch. Synchronous:
// call it from the handler that received the bytes so it lands in
// the current frame.
export function paint(pre, panel, onTap) {
  const rows = panel.map(html);
  if (pre.childElementCount !== rows.length) {
    pre.replaceChildren(...rows.map(h => { const d = document.createElement('div'); d.innerHTML = h; return d; }));
  } else {
    rows.forEach((h, i) => { const d = pre.children[i]; if (d.innerHTML !== h) d.innerHTML = h; });
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
