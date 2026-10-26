package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"repani.com/kv/client"
)

// exportCSV writes the vault's live keys and values to w as CSV with
// the header "Key,Value", sorted by key. Values are written verbatim:
// a binary or non-UTF-8 value does not round-trip through CSV.
func exportCSV(c *client.Client, w io.Writer) error {
	all, err := c.All()
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"Key", "Value"}); err != nil {
		return err
	}
	for _, e := range all {
		if err := cw.Write([]string{string(e.Key), string(e.Value)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// importChunk is how many rows go in one durable batch (one fsync).
const importChunk = 512

// importCSV writes the rows of the CSV in r into the vault and returns
// how many it wrote. The header names a Key and a Value column, in any
// case; other columns are ignored. A row with an empty key or value is
// skipped: an empty value is a tombstone, never made by an import. A
// key that appears twice takes its later row. The caller syncs.
func importCSV(c *client.Client, r io.Reader) (int, error) {
	// Excel's and Numbers' "CSV UTF-8" begins with a byte-order mark,
	// dropped here so the first header still reads as Key.
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && string(b) == "\xef\xbb\xbf" {
		br.Discard(3)
	}
	cr := csv.NewReader(br)
	cr.FieldsPerRecord = -1 // rows may vary in width
	header, err := cr.Read()
	if err == io.EOF {
		return 0, fmt.Errorf("csv is empty")
	}
	if err != nil {
		return 0, fmt.Errorf("csv header: %w", err)
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
		return 0, fmt.Errorf("csv header must include Key and Value columns: got %v", header)
	}

	n := 0
	chunk := map[string][]byte{}
	flush := func() error {
		if err := c.PutMany(chunk); err != nil {
			return err
		}
		n += len(chunk)
		clear(chunk)
		return nil
	}
	for row := 2; ; row++ { // the header is row 1
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, fmt.Errorf("csv row %d: %w", row, err)
		}
		if keyCol >= len(rec) || valCol >= len(rec) || rec[keyCol] == "" || rec[valCol] == "" {
			continue
		}
		chunk[rec[keyCol]] = []byte(rec[valCol])
		if len(chunk) >= importChunk {
			if err := flush(); err != nil {
				return n, err
			}
		}
	}
	err = flush()
	return n, err
}
