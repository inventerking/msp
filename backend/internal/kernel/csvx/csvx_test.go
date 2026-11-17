package csvx

import (
	"strings"
	"testing"
)

func TestSafeCellNeutralisesFormulas(t *testing.T) {
	for _, in := range []string{"=1+1", "+SUM(A1)", "-2", "@cmd", "\tx", "|x", "%x"} {
		if got := SafeCell(in); got != "'"+in {
			t.Errorf("SafeCell(%q) = %q", in, got)
		}
	}
	if SafeCell("Acme") != "Acme" || SafeCell("") != "" {
		t.Error("plain text must be unchanged")
	}
}

func TestWriteEscapesEveryCell(t *testing.T) {
	b, err := Write([]string{"name"}, [][]string{{"=HYPERLINK(\"x\")"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "'=HYPERLINK") {
		t.Fatalf("not escaped: %s", b)
	}
}

func TestReadRequiresColumnsAndBoundsRows(t *testing.T) {
	if _, err := Read([]byte("a,b\n1,2\n"), []string{"name"}, 10); err == nil {
		t.Fatal("a missing required column must fail")
	}
	if _, err := Read([]byte("name\nx\ny\nz\n"), []string{"name"}, 2); err == nil {
		t.Fatal("too many rows must fail")
	}
	rows, err := Read([]byte("\xef\xbb\xbfName , Domain\n Acme , acme.test \n"), []string{"name", "domain"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Fields["name"] != "Acme" || rows[0].Fields["domain"] != "acme.test" || rows[0].Line != 2 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}
