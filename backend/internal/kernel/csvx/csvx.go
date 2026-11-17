// Package csvx reads and writes CSV files safely: exported cells cannot run as spreadsheet formulas,
// and imports are bounded.
package csvx

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SafeCell stops a spreadsheet from treating exported text as a formula. A cell starting with one
// of these characters could run commands when someone opens the file.
func SafeCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r', '|', '%':
		return "'" + s
	}
	return s
}

// Write writes rows with every cell made safe.
func Write(header []string, rows [][]string) ([]byte, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, r := range rows {
		safe := make([]string, len(r))
		for i, c := range r {
			safe[i] = SafeCell(c)
		}
		if err := w.Write(safe); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

// Row is one data row, keyed by lower-cased column heading.
type Row struct {
	Line   int
	Fields map[string]string
}

// Read parses a CSV with a header row into at most maxRows rows. Every column in required must be
// present. Values are trimmed.
func Read(data []byte, required []string, maxRows int) ([]Row, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true // Spreadsheet exports often have stray quotes inside fields.
	header, err := r.Read()
	if err != nil {
		return nil, errors.New("the file has no header row")
	}
	cols := make([]string, len(header))
	seen := map[string]bool{}
	for i, h := range header {
		cols[i] = strings.ToLower(strings.TrimSpace(h))
		seen[cols[i]] = true
	}
	for _, c := range required {
		if !seen[c] {
			return nil, fmt.Errorf("the file has no column named %q", c)
		}
	}
	var out []Row
	for line := 2; ; line++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d could not be read", line)
		}
		if len(out) >= maxRows {
			return nil, fmt.Errorf("the file has more than %d rows", maxRows)
		}
		row := Row{Line: line, Fields: map[string]string{}}
		for i, v := range rec {
			if i < len(cols) && cols[i] != "" {
				row.Fields[cols[i]] = strings.TrimSpace(v)
			}
		}
		out = append(out, row)
	}
	return out, nil
}
