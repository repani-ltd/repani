package client

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

	"repani.com/kv/vault"
)

// ExportCSV writes the current live key/value state to w as a CSV
// with header "Key,Value". The rows are sorted by key for stable
// output. Tombstoned (deleted) keys are omitted. This format is the
// intentional common subset with quietcasting's qvlt — a qvlt import
// reads exactly this shape.
//
// Values are written verbatim. Binary or non-UTF-8 values will not
// round-trip cleanly through CSV; such vaults should prefer a
// byte-preserving transport (e.g., a raw copy of entries.log).
func (c *Client) ExportCSV(w io.Writer) error {
	m := c.replay()
	keys := make([]string, 0, len(m))
	for k, kv := range m {
		if kv.tomb {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"Key", "Value"}); err != nil {
		return err
	}
	for _, k := range keys {
		if err := cw.Write([]string{k, string(m[k].value)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ImportCSV reads CSV from r and writes each row into the vault.
// The header is inspected case-insensitively for "Key" and "Value"
// columns; extra columns (e.g., the User/Bucket/Uid that qvlt
// exports) are ignored. Rows with an empty key or an empty value are
// skipped — an empty value is a protocol-level tombstone and must
// not be created silently by an import.
//
// Rows are committed in durable batches (one fsync per batch rather
// than per row), so a large import costs milliseconds, not minutes.
//
// Returns the number of rows durably written (after the header). The
// caller must Sync to propagate these writes to the server.
func (c *Client) ImportCSV(r io.Reader) (int, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate variable-width rows

	header, err := cr.Read()
	if err == io.EOF {
		return 0, fmt.Errorf("client: csv is empty")
	}
	if err != nil {
		return 0, fmt.Errorf("client: csv header: %w", err)
	}

	keyCol, valCol := -1, -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "key":
			keyCol = i
		case "value":
			valCol = i
		}
	}
	if keyCol < 0 || valCol < 0 {
		return 0, fmt.Errorf("client: csv header must include Key and Value columns: got %v", header)
	}

	const importChunk = 512 // rows per durable batch (one fsync each)
	n := 0
	var batch []vault.Entry
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := c.recordBatch(batch); err != nil {
			return fmt.Errorf("client: import flush: %w", err)
		}
		n += len(batch)
		batch = batch[:0]
		return nil
	}
	row := 1 // for error messages; header is row 1
	for {
		row++
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, fmt.Errorf("client: csv row %d: %w", row, err)
		}
		if keyCol >= len(rec) || valCol >= len(rec) {
			continue
		}
		key := rec[keyCol]
		val := rec[valCol]
		if key == "" || val == "" {
			continue
		}
		e, err := vault.Seal(c.aead, c.nextTs(), []byte(key), WrapValue(fmtRaw, []byte(val)))
		if err != nil {
			return n, fmt.Errorf("client: seal row %d key=%q: %w", row, key, err)
		}
		batch = append(batch, e)
		if len(batch) >= importChunk {
			if err := flush(); err != nil {
				return n, err
			}
		}
	}
	if err := flush(); err != nil {
		return n, err
	}
	return n, nil
}
