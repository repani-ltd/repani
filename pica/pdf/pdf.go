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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"repani.com/pica/pdf/ttf"
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

//go:embed fonts/FiraSans-Italic.ttf
var rawFiraSansItalic []byte

// Font identifies one of the embedded fonts by its PDF resource tag.
type Font string

const (
	Regular  Font = "R"  // Fira Mono Regular
	Bold     Font = "B"  // Fira Mono Bold
	Sans       Font = "S"  // Fira Sans Regular (proportional)
	SansBold   Font = "SB" // Fira Sans Bold (proportional)
	SansItalic Font = "SI" // Fira Sans Italic (proportional; emphasis)
)

// fontOrder defines a deterministic iteration order for all fonts.
var fontOrder = []Font{Regular, Bold, Sans, SansBold, SansItalic}

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
		{rawFiraSansItalic, SansItalic},
	} {
		f, err := ttf.Parse(e.raw)
		if err != nil {
			panic("pdf: parse embedded font: " + err.Error())
		}
		fontRegistry[e.font] = f
	}
}

// fontByID returns the parsed font for the given Font tag.
func fontByID(f Font) *ttf.TTFont { return fontRegistry[f] }

// Doc accumulates pages and builds the final PDF.
type Doc struct {
	Title    string
	Author   string
	Creator  string    // authoring application
	Producer string    // converting application
	Created  time.Time // CreationDate; the zero value omits it
	PageSize PageSize
	Compress bool

	pages []pageData
	used  map[Font]map[rune]bool
	forms []form
}

// form is a reusable vector drawing: a Form XObject shared by every
// page of the document, drawn with Page.Form.
type form struct {
	name    string
	w, h    float64
	content string
}

// AddForm registers a reusable vector drawing under name: content is
// a raw PDF content stream (path and colour operators) in a w x h
// user space, y up, origin bottom-left. The writer treats it as
// opaque; it is emitted once, shared by all pages, and placed with
// Page.Form. Names must be unique within the document.
func (d *Doc) AddForm(name string, w, h float64, content string) {
	d.forms = append(d.forms, form{name, w, h, content})
}

// pageData is a finished page: its content stream plus any link
// annotations.
type pageData struct {
	content string
	annots  []linkAnnot
}

// Add appends a finished page to the document, absorbing its
// rune-usage bookkeeping for font subsetting.
func (d *Doc) Add(p *Page) {
	d.pages = append(d.pages, pageData{content: p.buf.String(), annots: p.annots})
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

// Bytes builds the complete PDF: objects are written straight into
// the output as they are built, their offsets recorded for the xref.
func (d *Doc) Bytes() []byte {
	b := &builder{pos: []int{0}} // pos[0]: the xref free entry
	b.buf.WriteString(pdfHeader)

	w, h := d.PageSize.Dimensions()
	mbox := fmt.Sprintf("[ 0 0 %s %s ]", ff(w), ff(h))

	// 1. Reserve the pages tree and catalog IDs (forward
	// references); their objects are emitted last.
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
		name := subsetName(font.PostScriptName, used)
		toUnicodeID := b.add(pdfToUnicode(b.next(), used))
		fontFileID := b.add(pdfFontFile(b.next(), subset.Data))
		cidToGIDMapID := b.add(pdfCIDToGIDMap(b.next(), subset.CIDToGID))
		descriptorID := b.add(pdfFontDescriptor(b.next(), font, name, fontFileID))
		cidFontID := b.add(pdfCIDFont(b.next(), name, subset.Widths, font.DefaultWidth, descriptorID, cidToGIDMapID))
		fontType0IDs[i] = b.add(pdfType0Font(b.next(), name, cidFontID, toUnicodeID))
	}

	// 3. Form XObjects, then the resources shared by all pages.
	formIDs := make([]int, len(d.forms))
	for i, f := range d.forms {
		formIDs[i] = b.add(pdfForm(b.next(), f, d.Compress))
	}
	resourcesID := b.add(pdfResources(b.next(), embedded, fontType0IDs, d.forms, formIDs))

	// 4. Info. CreationDate only when the caller sets Created --
	// never the wall clock: identical input must produce
	// byte-identical PDFs, and the date is optional in the spec.
	infoID := b.add(pdfInfo(b.next(), d))

	// 5. Page/stream pairs, plus link annotations per page.
	kids := make([]int, len(d.pages))
	for i, pg := range d.pages {
		streamID := b.add(pdfStream(b.next(), pg.content, d.Compress))
		annotIDs := make([]int, len(pg.annots))
		for j, a := range pg.annots {
			annotIDs[j] = b.add(pdfLinkAnnot(b.next(), a))
		}
		kids[i] = b.add(pdfPage(b.next(), streamID, pagesID, resourcesID, annotIDs))
	}

	// 6. The reserved pages tree and catalog, emitted last.
	openAction := pagesID
	if len(kids) > 0 {
		openAction = kids[0]
	}
	b.set(pagesID, pdfPages(pagesID, len(d.pages), kids, mbox))
	b.set(catalogID, pdfCatalog(catalogID, openAction, pagesID))

	xrefPos := b.buf.Len()
	docID := fmt.Sprintf("%x", md5.Sum(b.buf.Bytes()))
	b.buf.Write(pdfXref(len(b.pos), b.pos, catalogID, infoID, docID, xrefPos))
	return b.buf.Bytes()
}

// builder writes PDF objects into buf as they arrive, recording each
// object's byte offset in pos (pos[0] is the unused xref free entry).
// Object IDs are assigned in order of reservation, not of emission:
// a reserved object is written wherever its data is set, and the
// xref table maps IDs to offsets regardless.
type builder struct {
	buf bytes.Buffer
	pos []int
}

// next is the ID the next add will return.
func (b *builder) next() int { return len(b.pos) }

// reserve allocates an object ID whose data is set later, for
// forward references (the pages tree needs its kids' IDs).
func (b *builder) reserve() int {
	b.pos = append(b.pos, 0)
	return len(b.pos) - 1
}

// add writes a fully-built object and returns its ID.
func (b *builder) add(data []byte) int {
	id := b.reserve()
	b.set(id, data)
	return id
}

// set writes the data of a previously reserved object.
func (b *builder) set(id int, data []byte) {
	b.pos[id] = b.buf.Len()
	b.buf.Write(data)
}

// ── PDF objects ─────────────────────────────────────────────────────

// obj builds a PDF object incrementally: a dictionary, closed by
// bytes(), optionally carrying a stream body, closed by stream().
type obj struct{ buf bytes.Buffer }

func newObj(id int) *obj {
	o := &obj{}
	fmt.Fprintf(&o.buf, "%d 0 obj\n<<\n", id)
	return o
}

func (o *obj) field(k, v string) {
	fmt.Fprintf(&o.buf, "/%s %s\n", k, v)
}

// bytes closes a plain dictionary object.
func (o *obj) bytes() []byte {
	o.buf.WriteString(">>\nendobj\n")
	return o.buf.Bytes()
}

// stream closes the dictionary and attaches body as the object's
// stream. The caller must have set /Length to len(body).
func (o *obj) stream(body []byte) []byte {
	o.buf.WriteString(">>\nstream\n")
	o.buf.Write(body)
	o.buf.WriteString("\nendstream\nendobj\n")
	return o.buf.Bytes()
}

func pdfCatalog(id, openActionRef, pagesRef int) []byte {
	o := newObj(id)
	o.field("OpenAction", fmt.Sprintf("[ %d 0 R /FitH null ]", openActionRef))
	o.field("Pages", fmt.Sprintf("%d 0 R", pagesRef))
	o.field("Type", "/Catalog")
	return o.bytes()
}

func pdfInfo(id int, d *Doc) []byte {
	o := newObj(id)
	if d.Author != "" {
		o.field("Author", textString(d.Author))
	}
	if !d.Created.IsZero() {
		o.field("CreationDate", d.Created.Format("(D:20060102)"))
	}
	if d.Creator != "" {
		o.field("Creator", textString(d.Creator))
	}
	if d.Producer != "" {
		o.field("Producer", textString(d.Producer))
	}
	if d.Title != "" {
		o.field("Title", textString(d.Title))
	}
	return o.bytes()
}

func pdfPages(id, pageCount int, kids []int, mediaBox string) []byte {
	o := newObj(id)
	o.field("Count", fmt.Sprintf("%d", pageCount))
	o.field("Kids", pdfKids(kids))
	o.field("MediaBox", mediaBox)
	o.field("Type", "/Pages")
	return o.bytes()
}

func pdfPage(id, contentID, parentID, resourcesID int, annotIDs []int) []byte {
	o := newObj(id)
	if len(annotIDs) > 0 {
		o.field("Annots", pdfKids(annotIDs))
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
	o.field("A", fmt.Sprintf("<< /S /URI /URI %s >>", uriString(a.url)))
	o.field("Border", "[ 0 0 0 ]")
	o.field("Rect", fmt.Sprintf("[ %s %s %s %s ]", ff(a.x0), ff(a.y0), ff(a.x1), ff(a.y1)))
	o.field("Subtype", "/Link")
	o.field("Type", "/Annot")
	return o.bytes()
}

// textString encodes s as a PDF text string (ISO 32000 7.9.2.2):
// ASCII text as a literal string with \, (, ), CR and LF escaped;
// anything else as UTF-16BE with a byte-order mark in a hex string,
// since literal strings are PDFDocEncoding, not UTF-8.
func textString(s string) string {
	for _, r := range s {
		if r >= 0x80 {
			var buf strings.Builder
			buf.WriteString("<FEFF")
			for _, u := range utf16.Encode([]rune(s)) {
				fmt.Fprintf(&buf, "%04X", u)
			}
			buf.WriteString(">")
			return buf.String()
		}
	}
	return literalString(s)
}

// uriString encodes a URI for a /URI action: 7-bit ASCII per ISO
// 32000 12.6.4.7, so non-ASCII bytes are percent-encoded (RFC 3986)
// rather than misread as PDFDocEncoding.
func uriString(u string) string {
	var buf strings.Builder
	for i := 0; i < len(u); i++ {
		if c := u[i]; c >= 0x80 {
			fmt.Fprintf(&buf, "%%%02X", c)
		} else {
			buf.WriteByte(c)
		}
	}
	return literalString(buf.String())
}

// literalString wraps ASCII s as a PDF literal string, escaping \,
// (, ), CR and LF.
func literalString(s string) string {
	var buf strings.Builder
	buf.WriteByte('(')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\', '(', ')':
			buf.WriteByte('\\')
			buf.WriteByte(c)
		case '\r':
			buf.WriteString(`\r`)
		case '\n':
			buf.WriteString(`\n`)
		default:
			buf.WriteByte(c)
		}
	}
	buf.WriteByte(')')
	return buf.String()
}

func pdfStream(id int, content string, compress bool) []byte {
	body := []byte(content)
	if compress {
		body = zlibCompress(body)
	}
	o := newObj(id)
	o.field("Length", strconv.Itoa(len(body)))
	if compress {
		o.field("Filter", "[ /FlateDecode ]")
	}
	return o.stream(body)
}

func pdfKids(ids []int) string {
	var buf strings.Builder
	buf.WriteString("[ ")
	for _, id := range ids {
		fmt.Fprintf(&buf, "%d 0 R ", id)
	}
	buf.WriteString("]")
	return buf.String()
}

func pdfXref(count int, objPos []int, rootID, infoID int, id string, xrefPos int) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "xref\n0 %d\n", count)
	buf.WriteString("0000000000 65535 f \n")
	for _, pos := range objPos[1:] {
		fmt.Fprintf(&buf, "%010d 00000 n \n", pos)
	}
	fmt.Fprintf(&buf, "trailer\n<<\n/Root %d 0 R\n/Size %d\n/Info %d 0 R\n/ID[<%s><%s>]\n>>\nstartxref\n%d\n%%%%EOF\n",
		rootID, count, infoID, id, id, xrefPos)
	return buf.Bytes()
}

func pdfResources(id int, fonts []Font, fontIDs []int, forms []form, formIDs []int) []byte {
	var fontDict strings.Builder
	fontDict.WriteString("<< ")
	for i, f := range fonts {
		fmt.Fprintf(&fontDict, "/%s %d 0 R ", string(f), fontIDs[i])
	}
	fontDict.WriteString(">>")
	o := newObj(id)
	o.field("Font", fontDict.String())
	if len(forms) > 0 {
		var xDict strings.Builder
		xDict.WriteString("<< ")
		for i, f := range forms {
			fmt.Fprintf(&xDict, "/%s %d 0 R ", f.name, formIDs[i])
		}
		xDict.WriteString(">>")
		o.field("XObject", xDict.String())
	}
	return o.bytes()
}

// pdfForm builds a Form XObject: the drawing's content stream in its
// own w x h bounding box.
func pdfForm(id int, f form, compress bool) []byte {
	o := newObj(id)
	o.field("Type", "/XObject")
	o.field("Subtype", "/Form")
	o.field("BBox", fmt.Sprintf("[ 0 0 %s %s ]", ff(f.w), ff(f.h)))
	body := []byte(f.content)
	if compress {
		body = zlibCompress(body)
		o.field("Filter", "/FlateDecode")
	}
	o.field("Length", strconv.Itoa(len(body)))
	return o.stream(body)
}

// subsetName returns the BaseFont/FontName of an embedded subset:
// the PostScript name behind the six-uppercase-letter subset tag ISO
// 32000 9.6.4 requires. The tag is derived from the used-rune set,
// so identical documents keep identical bytes.
func subsetName(psName string, used map[rune]bool) string {
	cps := make([]int, 0, len(used))
	for r := range used {
		cps = append(cps, int(r))
	}
	sort.Ints(cps)
	h := md5.New()
	for _, cp := range cps {
		fmt.Fprintf(h, "%d,", cp)
	}
	sum := h.Sum(nil)
	tag := make([]byte, 6)
	for i := range tag {
		tag[i] = 'A' + sum[i]%26
	}
	return string(tag) + "+" + psName
}

func pdfType0Font(id int, name string, cidFontID, toUnicodeID int) []byte {
	o := newObj(id)
	o.field("Type", "/Font")
	o.field("Subtype", "/Type0")
	o.field("BaseFont", "/"+name)
	o.field("Encoding", "/Identity-H")
	o.field("DescendantFonts", fmt.Sprintf("[ %d 0 R ]", cidFontID))
	o.field("ToUnicode", fmt.Sprintf("%d 0 R", toUnicodeID))
	return o.bytes()
}

func pdfCIDFont(id int, name string, widths map[int]int, defaultW int, descriptorID, cidToGIDMapID int) []byte {
	o := newObj(id)
	o.field("Type", "/Font")
	o.field("Subtype", "/CIDFontType2")
	o.field("BaseFont", "/"+name)
	o.field("CIDSystemInfo", "<< /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>")
	o.field("FontDescriptor", fmt.Sprintf("%d 0 R", descriptorID))
	o.field("DW", strconv.Itoa(defaultW))
	if w := pdfWidthsArray(widths, defaultW); w != "" {
		o.field("W", w)
	}
	o.field("CIDToGIDMap", fmt.Sprintf("%d 0 R", cidToGIDMapID))
	return o.bytes()
}

// pdfWidthsArray builds the value of the /W array for CID fonts,
// mapping CID values to advance widths, consecutive CIDs grouped
// into runs: [ cid1 [ w1 w2 ... ] ... ]. Empty when every width
// equals the default.
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

	var buf bytes.Buffer
	buf.WriteString("[ ")
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
	buf.WriteString("]")
	return buf.String()
}

func pdfFontDescriptor(id int, font *ttf.TTFont, name string, fontFileID int) []byte {
	o := newObj(id)
	o.field("Type", "/FontDescriptor")
	o.field("FontName", "/"+name)
	o.field("Flags", strconv.Itoa(font.Flags))
	o.field("FontBBox", fmt.Sprintf("[ %d %d %d %d ]", font.BBox[0], font.BBox[1], font.BBox[2], font.BBox[3]))
	o.field("ItalicAngle", fmt.Sprintf("%.1f", font.ItalicAngle))
	o.field("Ascent", strconv.Itoa(int(font.Ascent)))
	o.field("Descent", strconv.Itoa(int(font.Descent)))
	o.field("CapHeight", strconv.Itoa(int(font.CapHeight)))
	o.field("StemV", strconv.Itoa(font.StemV))
	o.field("FontFile2", fmt.Sprintf("%d 0 R", fontFileID))
	return o.bytes()
}

func pdfFontFile(id int, data []byte) []byte {
	compressed := zlibCompress(data)
	o := newObj(id)
	o.field("Length", strconv.Itoa(len(compressed)))
	o.field("Length1", strconv.Itoa(len(data)))
	o.field("Filter", "/FlateDecode")
	return o.stream(compressed)
}

func pdfCIDToGIDMap(id int, data []byte) []byte {
	compressed := zlibCompress(data)
	o := newObj(id)
	o.field("Length", strconv.Itoa(len(compressed)))
	o.field("Filter", "/FlateDecode")
	return o.stream(compressed)
}

// zlibCompress compresses data at BestSpeed (content streams and
// font subsets are small; the PDF is built once per document). One
// writer is reused across calls: allocating a deflater per stream
// costs more than the streams. Panics on error: writes to an
// in-memory buffer cannot fail except through a programming bug.
var (
	zlibMu sync.Mutex
	zlibW  *zlib.Writer
)

func zlibCompress(data []byte) []byte {
	zlibMu.Lock()
	defer zlibMu.Unlock()
	var b bytes.Buffer
	if zlibW == nil {
		zlibW, _ = zlib.NewWriterLevel(&b, zlib.BestSpeed)
	} else {
		zlibW.Reset(&b)
	}
	w := zlibW
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
// block, per the PDF spec). A range may differ only in its last
// byte, so runs break at every xxFF boundary.
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
		for i+1 < len(cps) && cps[i+1] == end+1 && end&0xFF != 0xFF {
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
CMapName currentdict /CMap defineresource pop
end
end`)
	return buf.String()
}

func pdfToUnicode(id int, used map[rune]bool) []byte {
	cmap := buildToUnicodeCMap(used)
	o := newObj(id)
	o.field("Length", strconv.Itoa(len(cmap)))
	return o.stream([]byte(cmap))
}
