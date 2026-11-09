package domain

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

// WriteCSV writes rows with every cell made safe.
func WriteCSV(header []string, rows [][]string) ([]byte, error) {
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

// MaxImportRows bounds one import.
const MaxImportRows = 10000

// ImportRow is one parsed row, keyed by our field names.
type ImportRow struct {
	Line   int
	Fields map[string]string
}

// ParseImport reads a CSV with a header row, keeping only mapped columns. The mapping goes from our
// field names to the file's column headings.
func ParseImport(data []byte, mapping map[string]string) ([]ImportRow, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true // Spreadsheet exports often have stray quotes inside fields.
	header, err := r.Read()
	if err != nil {
		return nil, errors.New("the file has no header row")
	}
	index := map[string]int{}
	for i, h := range header {
		index[strings.ToLower(strings.TrimSpace(h))] = i
	}
	cols := map[string]int{}
	for field, heading := range mapping {
		i, ok := index[strings.ToLower(strings.TrimSpace(heading))]
		if !ok {
			return nil, fmt.Errorf("the file has no column named %q", heading)
		}
		cols[field] = i
	}
	if _, ok := cols["domain"]; !ok {
		return nil, errors.New("a domain column must be mapped")
	}
	var out []ImportRow
	for line := 2; ; line++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d could not be read", line)
		}
		if len(out) >= MaxImportRows {
			return nil, fmt.Errorf("the file has more than %d rows", MaxImportRows)
		}
		row := ImportRow{Line: line, Fields: map[string]string{}}
		for field, i := range cols {
			if i < len(rec) {
				row.Fields[field] = strings.TrimSpace(rec[i])
			}
		}
		out = append(out, row)
	}
	return out, nil
}
