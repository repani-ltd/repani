package raster

// Page is what a renderer that takes updates holds: a height and the
// rows below it, a row never written being blank. The zero Page is
// empty.
type Page struct {
	rows []Row
}

// Height returns the page's row count.
func (p *Page) Height() int { return len(p.rows) }

// Row returns row i, 0 <= i < Height.
func (p *Page) Row(i int) Row { return p.rows[i] }

// Apply checks every record, then applies them in order: a row
// writes its row, raising the height to cover it; a count sets the
// height. If any record is invalid, none is applied.
func (p *Page) Apply(recs ...Record) error {
	if _, err := Append(nil, recs...); err != nil {
		return err
	}
	for _, rec := range recs {
		switch rec := rec.(type) {
		case Row:
			p.resize(max(p.Height(), rec.Index+1))
			p.rows[rec.Index] = rec
		case Count:
			p.resize(int(rec))
		}
	}
	return nil
}

// resize removes the rows from h on, or grows the page to h with
// blank rows.
func (p *Page) resize(h int) {
	if h <= len(p.rows) {
		clear(p.rows[h:])
		p.rows = p.rows[:h]
		return
	}
	for i := len(p.rows); i < h; i++ {
		p.rows = append(p.rows, Blank(i))
	}
}

// Records returns the page as a whole page in its one encoding: a
// count of 0, the non-blank rows in ascending order, and the height.
// Applied to a Page in any state, it leaves that Page equal to p.
func (p *Page) Records() []Record {
	recs := []Record{Count(0)}
	for _, r := range p.rows {
		if !r.IsBlank() {
			recs = append(recs, r)
		}
	}
	return append(recs, Count(p.Height()))
}
