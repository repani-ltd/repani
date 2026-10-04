package raster

import (
	"fmt"
	"unicode/utf8"
)

// Append appends the encoding of each record to b, checking each
// first; on an error it returns b as it was.
func Append(b []byte, recs ...Record) ([]byte, error) {
	start := len(b)
	for _, rec := range recs {
		var err error
		switch rec := rec.(type) {
		case Row:
			if err = rec.Check(); err == nil {
				b = appendRow(b, rec)
			}
		case Count:
			if err = rec.Check(); err == nil {
				b = append(b, countKind, byte(rec))
			}
		default:
			err = fmt.Errorf("raster: record of type %T", rec)
		}
		if err != nil {
			return b[:start], err
		}
	}
	return b, nil
}

// appendRow writes row role k (end fg bg tlen target)×k text.
func appendRow(b []byte, r Row) []byte {
	b = append(b, byte(r.Index), byte(r.Role), byte(len(r.Segments)))
	for _, s := range r.Segments {
		b = append(b, byte(s.End), byte(s.FG), byte(s.BG), byte(len(s.Target)))
		b = append(b, s.Target...)
	}
	for _, s := range r.Segments {
		b = append(b, s.Text...)
	}
	return b
}

// Decode decodes a sequence of records, checking each as Append
// does, so that what decodes is canonical and re-encodes to the same
// bytes. Every byte offset in an error counts from the start of b.
func Decode(b []byte) ([]Record, error) {
	var recs []Record
	for off := 0; off < len(b); {
		rec, next, err := decodeRecord(b, off)
		if err != nil {
			return recs, fmt.Errorf("raster: record at byte %d: %w", off, err)
		}
		recs = append(recs, rec)
		off = next
	}
	return recs, nil
}

// decodeRecord decodes the record at byte off of b and returns the
// offset after it.
func decodeRecord(b []byte, off int) (Record, int, error) {
	d := decoder{b: b, off: off}
	kind := d.byte()
	if kind == countKind {
		c := Count(d.byte())
		return c, d.off, d.err
	}
	r := Row{Index: int(kind), Role: Role(d.byte())}
	k := int(d.byte())
	if d.err == nil && (k == 0 || k > Width) {
		return nil, 0, fmt.Errorf("row %d: %d segments, not 1 to %d", r.Index, k, Width)
	}
	r.Segments = make([]Segment, k)
	start := 0
	for i := range r.Segments {
		s := &r.Segments[i]
		s.Start, s.End = start, int(d.byte())
		s.FG, s.BG = Color(d.byte()), Color(d.byte())
		s.Target = string(d.bytes(int(d.byte())))
		if d.err == nil && (s.End <= s.Start || s.End > Width) {
			return nil, 0, fmt.Errorf("row %d: segment %d ends at column %d after starting at %d", r.Index, i, s.End, s.Start)
		}
		start = s.End
	}
	if d.err == nil && start != Width {
		return nil, 0, fmt.Errorf("row %d: segments end at column %d, not %d", r.Index, start, Width)
	}
	for i := range r.Segments {
		s := &r.Segments[i]
		s.Text = d.text(s.End - s.Start)
	}
	if d.err != nil {
		return nil, 0, d.err
	}
	if err := r.Check(); err != nil {
		return nil, 0, err
	}
	return r, d.off, nil
}

// decoder reads fields from a record, holding the first error.
type decoder struct {
	b   []byte
	off int
	err error
}

func (d *decoder) byte() byte {
	if d.err != nil {
		return 0
	}
	if d.off >= len(d.b) {
		d.err = fmt.Errorf("truncated at byte %d", d.off)
		return 0
	}
	d.off++
	return d.b[d.off-1]
}

func (d *decoder) bytes(n int) []byte {
	if d.err != nil {
		return nil
	}
	if d.off+n > len(d.b) {
		d.err = fmt.Errorf("truncated at byte %d", len(d.b))
		return nil
	}
	d.off += n
	return d.b[d.off-n : d.off]
}

// text reads exactly n code points.
func (d *decoder) text(n int) string {
	if d.err != nil {
		return ""
	}
	start := d.off
	for range n {
		if d.off >= len(d.b) {
			d.err = fmt.Errorf("text truncated at byte %d", d.off)
			return ""
		}
		c, size := utf8.DecodeRune(d.b[d.off:])
		if c == utf8.RuneError && size <= 1 {
			if !utf8.FullRune(d.b[d.off:]) {
				d.err = fmt.Errorf("text truncated at byte %d", len(d.b))
			} else {
				d.err = fmt.Errorf("text: invalid UTF-8 at byte %d", d.off)
			}
			return ""
		}
		d.off += size
	}
	return string(d.b[start:d.off])
}
