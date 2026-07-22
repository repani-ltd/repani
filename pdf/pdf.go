// Minimal PDF 1.3 writer: object table, xref, compressed streams,
// and embedded TrueType fonts with per-document subsetting. See
// doc.go for scope and provenance.
package pdf

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/pavlos/typeset/pdf/ttf"
)

const pdfHeader = "%PDF-1.3\n%\xA5\xB1\xEB\n"

// Page dimensions in points.
const (
	a4W = 595.28
	a4H = 841.89

	letterW = 612.0
	letterH = 792.0

	a5W = 419.53
	a5H = 595.28
)

// PageSize identifies the paper size for a document.
type PageSize int

const (
	PageA4     PageSize = iota // default
	PageLetter                 // US Letter (8.5 x 11 in)
	PageA5                     // A5 (148 x 210 mm)
)

// Dimensions returns the page width and height in points.
func (ps PageSize) Dimensions() (w, h float64) {
	switch ps {
	case PageLetter:
		return letterW, letterH
	case PageA5:
		return a5W, a5H
	default:
		return a4W, a4H
	}
}

//go:embed fonts/FiraMono-Regular.ttf
var rawFiraMonoRegular []byte

//go:embed fonts/FiraMono-Bold.ttf
var rawFiraMonoBold []byte

//go:embed fonts/FiraSans-Regular.ttf
var rawFiraSansRegular []byte

//go:embed fonts/FiraSans-Bold.ttf
var rawFiraSansBold []byte

// Font identifies one of the embedded fonts by its PDF resource tag.
type Font string

const (
	Regular  Font = "R"  // Fira Mono Regular
	Bold     Font = "B"  // Fira Mono Bold
	Sans     Font = "S"  // Fira Sans Regular (proportional)
	SansBold Font = "SB" // Fira Sans Bold (proportional)
)

// fontOrder defines a deterministic iteration order for all fonts.
var fontOrder = []Font{Regular, Bold, Sans, SansBold}

// fontRegistry holds the parsed fonts, populated at init from the
// embedded TTF files.
var fontRegistry = map[Font]*ttf.TTFont{}

func init() {
	for _, e := range []struct {
		raw  []byte
		font Font
	}{
		{rawFiraMonoRegular, Regular},
		{rawFiraMonoBold, Bold},
		{rawFiraSansRegular, Sans},
		{rawFiraSansBold, SansBold},
	} {
		f, err := ttf.Parse(e.raw)
		if err != nil {
			panic("pdf: parse embedded font: " + err.Error())
		}
		f.Tag = string(e.font)
		fontRegistry[e.font] = f
	}
}

// fontByID returns the parsed font for the given Font tag.
func fontByID(f Font) *ttf.TTFont { return fontRegistry[f] }

// Doc accumulates pages and builds the final PDF.
type Doc struct {
	Title    string
	Creator  string
	PageSize PageSize
	Compress bool

	pages []pageData
	used  map[Font]map[rune]bool
}

// pageData is a finished page: its content stream plus any link
// annotations.
type pageData struct {
	content []byte
	annots  []linkAnnot
}

// Add appends a finished page to the document, absorbing its
// rune-usage bookkeeping for font subsetting.
func (d *Doc) Add(p *Page) {
	d.pages = append(d.pages, pageData{content: p.Bytes(), annots: p.annots})
	if d.used == nil {
		d.used = make(map[Font]map[rune]bool)
	}
	for font, runes := range p.used {
		if d.used[font] == nil {
			d.used[font] = make(map[rune]bool)
		}
		for r := range runes {
			d.used[font][r] = true
		}
	}
}

// Bytes builds the complete PDF.
func (d *Doc) Bytes() []byte {
	b := &builder{objs: [][]byte{nil}} // obj 0 placeholder (xref free entry)

	w, h := d.PageSize.Dimensions()
	mbox := fmt.Sprintf("[ 0 0 %s %s ]", ff(w), ff(h))

	// 1. Reserve pages tree and catalog (need forward references).
	pagesID := b.reserve()
	catalogID := b.reserve()

	// 2. Fonts -- six objects each, subset to the runes actually
	// used. Fonts with no used runes are skipped entirely.
	var embedded []Font
	for _, f := range fontOrder {
		if len(d.used[f]) > 0 {
			embedded = append(embedded, f)
		}
	}
	fontType0IDs := make([]int, len(embedded))
	for i, fid := range embedded {
		font := fontByID(fid)
		used := d.used[fid]
		subset, err := font.Subset(used)
		if err != nil {
			panic("pdf: font subset: " + err.Error())
		}
		toUnicodeID := b.add(pdfToUnicode(len(b.objs), used))
		fontFileID := b.add(pdfFontFile(len(b.objs), subset.Data))
		cidToGIDMapID := b.add(pdfCIDToGIDMap(len(b.objs), subset.CIDToGID))
		descriptorID := b.add(pdfFontDescriptor(len(b.objs), font, fontFileID))
		cidFontID := b.add(pdfCIDFont(len(b.objs), font, subset.Widths, font.DefaultWidth, descriptorID, cidToGIDMapID))
		fontType0IDs[i] = b.add(pdfType0Font(len(b.objs), font, cidFontID, toUnicodeID))
	}

	// 3. Resources shared by all pages.
	resourcesID := b.add(pdfResources(len(b.objs), embedded, fontType0IDs))

	// 4. Info. No CreationDate: identical input must produce
	// byte-identical PDFs, and the date is optional in the spec.
	infoID := b.add(pdfInfo(len(b.objs), d.Creator, d.Title))

	// 5. Page/stream pairs, plus link annotations per page.
	kids := make([]int, len(d.pages))
	for i, pg := range d.pages {
		streamID := b.add(pdfStream(len(b.objs), pg.content, d.Compress))
		annotIDs := make([]int, len(pg.annots))
		for j, a := range pg.annots {
			annotIDs[j] = b.add(pdfLinkAnnot(len(b.objs), a))
		}
		kids[i] = b.add(pdfPage(len(b.objs), streamID, pagesID, resourcesID, annotIDs))
	}

	// 6. Fill in the reserved pages tree and catalog.
	openAction := pagesID
	if len(kids) > 0 {
		openAction = kids[0]
	}
	b.set(pagesID, pdfPages(pagesID, len(d.pages), kids, mbox))
	b.set(catalogID, pdfCatalog(catalogID, openAction, pagesID))

	// Write all objects sequentially with tracked byte offsets.
	var buf bytes.Buffer
	buf.WriteString(pdfHeader)
	objPos := make([]int, len(b.objs))
	for i := 1; i < len(b.objs); i++ {
		objPos[i] = buf.Len()
		buf.Write(b.objs[i])
	}
	xrefPos := buf.Len()
	docID := fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
	buf.Write(pdfXref(len(b.objs), objPos, catalogID, infoID, docID, xrefPos))
	return buf.Bytes()
}

// builder collects PDF objects; objs[0] is the unused xref free entry.
type builder struct {
	objs [][]byte
}

// reserve allocates an object ID whose data is set later, for
// forward references (the pages tree needs its kids' IDs).
func (b *builder) reserve() int {
	id := len(b.objs)
	b.objs = append(b.objs, nil)
	return id
}

// add appends a fully-built object and returns its ID.
func (b *builder) add(data []byte) int {
	id := len(b.objs)
	b.objs = append(b.objs, data)
	return id
}

// set fills in a previously reserved object slot.
func (b *builder) set(id int, data []byte) { b.objs[id] = data }

// ── PDF object templates ────────────────────────────────────────────

const (
	tmplStream         = "%d 0 obj\n<< \n/Length %d\n"
	tmplFilter         = "/Filter [ /FlateDecode ]\n"
	tmplStreamS        = ">>\nstream\n"
	tmplKid            = "%d 0 R "
	tmplXrefH          = "xref\n0 %d\n"
	tmplXrefF          = "0000000000 65535 f \n"
	tmplXrefN          = "%010d 00000 n \n"
	tmplXrefT          = "trailer\n<<\n/Root %d 0 R\n/Size %d\n/Info %d 0 R\n/ID[<%s><%s>]\n>>\nstartxref\n%d\n%%%%EOF\n"
	tmplResourcesFont  = "/%s %d 0 R "
	tmplType0Font      = "%d 0 obj\n<<\n/Type /Font\n/Subtype /Type0\n/BaseFont /%s\n/Encoding /Identity-H\n/DescendantFonts [ %d 0 R ]\n/ToUnicode %d 0 R\n>>\nendobj\n"
	tmplCIDFont        = "%d 0 obj\n<<\n/Type /Font\n/Subtype /CIDFontType2\n/BaseFont /%s\n/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>\n/FontDescriptor %d 0 R\n/DW %d\n%s/CIDToGIDMap %d 0 R\n>>\nendobj\n"
	tmplFontDescriptor = "%d 0 obj\n<<\n/Type /FontDescriptor\n/FontName /%s\n/Flags %d\n/FontBBox [ %d %d %d %d ]\n/ItalicAngle %.1f\n/Ascent %d\n/Descent %d\n/CapHeight %d\n/StemV %d\n/FontFile2 %d 0 R\n>>\nendobj\n"
	tmplFontFileH      = "%d 0 obj\n<<\n/Length %d\n/Length1 %d\n/Filter /FlateDecode\n>>\nstream\n"
	tmplCIDToGIDMapH   = "%d 0 obj\n<<\n/Length %d\n/Filter /FlateDecode\n>>\nstream\n"
	tmplStreamEnd      = "\nendstream\nendobj\n"
	tmplToUnicodeH     = "%d 0 obj\n<<\n/Length %d\n>>\nstream\n"
)

// obj builds a PDF dictionary object incrementally.
type obj struct{ buf bytes.Buffer }

func newObj(id int) *obj {
	o := &obj{}
	fmt.Fprintf(&o.buf, "%d 0 obj\n<<\n", id)
	return o
}

func (o *obj) field(k, v string) {
	fmt.Fprintf(&o.buf, "/%s %s\n", k, v)
}

func (o *obj) bytes() []byte {
	o.buf.WriteString(">>\nendobj\n")
	return o.buf.Bytes()
}

func pdfCatalog(id, openActionRef, pagesRef int) []byte {
	o := newObj(id)
	o.field("OpenAction", fmt.Sprintf("[ %d 0 R /FitH null ]", openActionRef))
	o.field("Pages", fmt.Sprintf("%d 0 R", pagesRef))
	o.field("Type", "/Catalog")
	return o.bytes()
}

func pdfInfo(id int, creator, title string) []byte {
	o := newObj(id)
	if creator != "" {
		o.field("Creator", fmt.Sprintf("(%s)", creator))
	}
	if title != "" {
		o.field("Title", fmt.Sprintf("(%s)", title))
	}
	return o.bytes()
}

func pdfPages(id, pageCount int, kids []int, mediaBox string) []byte {
	o := newObj(id)
	o.field("Count", fmt.Sprintf("%d", pageCount))
	o.field("Kids", string(pdfKids(kids)))
	o.field("MediaBox", mediaBox)
	o.field("Type", "/Pages")
	return o.bytes()
}

func pdfPage(id, contentID, parentID, resourcesID int, annotIDs []int) []byte {
	o := newObj(id)
	if len(annotIDs) > 0 {
		o.field("Annots", string(pdfKids(annotIDs)))
	}
	o.field("Contents", fmt.Sprintf("%d 0 R", contentID))
	o.field("Parent", fmt.Sprintf("%d 0 R", parentID))
	o.field("Resources", fmt.Sprintf("%d 0 R", resourcesID))
	o.field("Type", "/Page")
	return o.bytes()
}

// pdfLinkAnnot builds a /Link annotation with a URI action and no
// visible border.
func pdfLinkAnnot(id int, a linkAnnot) []byte {
	o := newObj(id)
	o.field("A", fmt.Sprintf("<< /S /URI /URI (%s) >>", escapeString(a.url)))
	o.field("Border", "[ 0 0 0 ]")
	o.field("Rect", fmt.Sprintf("[ %s %s %s %s ]", ff(a.x0), ff(a.y0), ff(a.x1), ff(a.y1)))
	o.field("Subtype", "/Link")
	o.field("Type", "/Annot")
	return o.bytes()
}

// escapeString escapes a PDF literal string's special characters.
func escapeString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	return r.Replace(s)
}

func pdfStream(id int, content []byte, compress bool) []byte {
	body := content
	if compress {
		body = zlibCompress(content)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplStream, id, len(body))
	if compress {
		buf.WriteString(tmplFilter)
	}
	buf.WriteString(tmplStreamS)
	buf.Write(body)
	buf.WriteString(tmplStreamEnd)
	return buf.Bytes()
}

func pdfKids(ids []int) []byte {
	var buf bytes.Buffer
	buf.WriteString("[ ")
	for _, id := range ids {
		fmt.Fprintf(&buf, tmplKid, id)
	}
	buf.WriteString("]")
	return buf.Bytes()
}

func pdfXref(count int, objPos []int, rootID, infoID int, id string, xrefPos int) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplXrefH, count)
	fmt.Fprintf(&buf, tmplXrefF)
	for _, pos := range objPos[1:] {
		fmt.Fprintf(&buf, tmplXrefN, pos)
	}
	fmt.Fprintf(&buf, tmplXrefT, rootID, count, infoID, id, id, xrefPos)
	return buf.Bytes()
}

func pdfResources(id int, fonts []Font, fontIDs []int) []byte {
	var fontDict bytes.Buffer
	for i, f := range fonts {
		fmt.Fprintf(&fontDict, tmplResourcesFont, string(f), fontIDs[i])
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d 0 obj\n<<\n/Font << %s>>\n>>\nendobj\n", id, fontDict.String())
	return buf.Bytes()
}

func pdfType0Font(id int, font *ttf.TTFont, cidFontID, toUnicodeID int) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplType0Font, id, font.PostScriptName, cidFontID, toUnicodeID)
	return buf.Bytes()
}

func pdfCIDFont(id int, font *ttf.TTFont, widths map[int]int, defaultW int, descriptorID, cidToGIDMapID int) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplCIDFont, id, font.PostScriptName, descriptorID, defaultW, pdfWidthsArray(widths, defaultW), cidToGIDMapID)
	return buf.Bytes()
}

// pdfWidthsArray builds the /W array for CID fonts, mapping CID
// values to advance widths. Format: /W [ cid1 [ w1 w2 ... ] ... ]
func pdfWidthsArray(widths map[int]int, defaultW int) string {
	type cidWidth struct{ cid, width int }
	var cws []cidWidth
	for cid, w := range widths {
		if w != defaultW {
			cws = append(cws, cidWidth{cid, w})
		}
	}
	if len(cws) == 0 {
		return ""
	}
	sort.Slice(cws, func(i, j int) bool { return cws[i].cid < cws[j].cid })

	// Group consecutive CIDs into runs: startCID [ w1 w2 w3 ... ]
	var buf bytes.Buffer
	buf.WriteString("/W [ ")
	i := 0
	for i < len(cws) {
		start := cws[i].cid
		j := i
		for j < len(cws) && cws[j].cid == start+(j-i) {
			j++
		}
		fmt.Fprintf(&buf, "%d [ ", start)
		for k := i; k < j; k++ {
			fmt.Fprintf(&buf, "%d ", cws[k].width)
		}
		buf.WriteString("] ")
		i = j
	}
	buf.WriteString("]\n")
	return buf.String()
}

func pdfFontDescriptor(id int, font *ttf.TTFont, fontFileID int) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplFontDescriptor,
		id, font.PostScriptName, font.Flags,
		font.BBox[0], font.BBox[1], font.BBox[2], font.BBox[3],
		font.ItalicAngle, font.Ascent, font.Descent, font.CapHeight, font.StemV, fontFileID)
	return buf.Bytes()
}

func pdfFontFile(id int, data []byte) []byte {
	compressed := zlibCompress(data)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplFontFileH, id, len(compressed), len(data))
	buf.Write(compressed)
	buf.WriteString(tmplStreamEnd)
	return buf.Bytes()
}

func pdfCIDToGIDMap(id int, data []byte) []byte {
	compressed := zlibCompress(data)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplCIDToGIDMapH, id, len(compressed))
	buf.Write(compressed)
	buf.WriteString(tmplStreamEnd)
	return buf.Bytes()
}

// zlibCompress compresses data. Panics on error: writes to an
// in-memory buffer cannot fail except through a programming bug.
func zlibCompress(data []byte) []byte {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		panic("pdf: zlib write: " + err.Error())
	}
	if err := w.Close(); err != nil {
		panic("pdf: zlib close: " + err.Error())
	}
	return b.Bytes()
}

// buildToUnicodeCMap generates a ToUnicode CMap for the used runes,
// merging consecutive codepoints into bfrange entries (max 100 per
// block, per the PDF spec).
func buildToUnicodeCMap(used map[rune]bool) string {
	cps := make([]int, 0, len(used))
	for r := range used {
		cps = append(cps, int(r))
	}
	sort.Ints(cps)

	type bfrange struct{ start, end int }
	var ranges []bfrange
	i := 0
	for i < len(cps) {
		start := cps[i]
		end := start
		for i+1 < len(cps) && cps[i+1] == end+1 {
			i++
			end = cps[i]
		}
		ranges = append(ranges, bfrange{start, end})
		i++
	}

	var buf bytes.Buffer
	buf.WriteString(`/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
`)
	for j := 0; j < len(ranges); j += 100 {
		end := min(j+100, len(ranges))
		chunk := ranges[j:end]
		fmt.Fprintf(&buf, "%d beginbfrange\n", len(chunk))
		for _, r := range chunk {
			fmt.Fprintf(&buf, "<%04X> <%04X> <%04X>\n", r.start, r.end, r.start)
		}
		buf.WriteString("endbfrange\n")
	}
	buf.WriteString(`endcmap
CMapCurrentMap CMapName def
end
end`)
	return buf.String()
}

func pdfToUnicode(id int, used map[rune]bool) []byte {
	cmap := buildToUnicodeCMap(used)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, tmplToUnicodeH, id, len(cmap))
	buf.WriteString(cmap)
	buf.WriteString(tmplStreamEnd)
	return buf.Bytes()
}
