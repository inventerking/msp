package domain

import (
	"strings"
	"testing"
)

func TestSafeCell(t *testing.T) {
	for in, want := range map[string]string{"=HYPERLINK(\"x\")": "'=HYPERLINK(\"x\")", "+1": "'+1", "-2": "'-2", "@SUM(A1)": "'@SUM(A1)", "\tx": "'\tx", "plain": "plain", "": ""} {
		if got := SafeCell(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	out, err := WriteCSV([]string{"a"}, [][]string{{"=cmd|' /C calc'!A0"}})
	if err != nil || !strings.Contains(string(out), "'=cmd") {
		t.Fatalf("%v %s", err, out)
	}
}

func TestParseImport(t *testing.T) {
	data := []byte("\xef\xbb\xbfWebsite,Company Name,Extra\nacme.example,Acme,x\n,Blank,y\n\"quoted.example\",\"Q, Inc\",z\n")
	rows, err := ParseImport(data, map[string]string{"domain": "website", "name": "Company Name"})
	if err != nil || len(rows) != 3 {
		t.Fatalf("%v %d", err, len(rows))
	}
	if rows[0].Fields["domain"] != "acme.example" || rows[2].Fields["name"] != "Q, Inc" || rows[2].Line != 4 {
		t.Fatalf("%+v", rows)
	}
	if _, ok := rows[0].Fields["Extra"]; ok {
		t.Fatal("an unmapped column was kept")
	}
	if _, err := ParseImport(data, map[string]string{"name": "Company Name"}); err == nil {
		t.Fatal("import without a domain column was accepted")
	}
	if _, err := ParseImport(data, map[string]string{"domain": "nope"}); err == nil {
		t.Fatal("a missing column was accepted")
	}
	big := "domain\n" + strings.Repeat("a.example\n", MaxImportRows+1)
	if _, err := ParseImport([]byte(big), map[string]string{"domain": "domain"}); err == nil {
		t.Fatal("an oversized import was accepted")
	}
}
