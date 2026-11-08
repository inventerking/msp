package pdf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTemplatesEscapeValues(t *testing.T) {
	out, err := HTML(`<h1>{{.Name}}</h1><a href="{{.URL}}">x</a>`, map[string]string{"Name": `<script>alert(1)</script><img src="http://169.254.169.254/">`, "URL": "javascript:alert(1)"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<script>") || strings.Contains(s, "<img") || strings.Contains(s, "javascript:") {
		t.Fatalf("not escaped: %s", s)
	}
}

func TestMissingValuesFail(t *testing.T) {
	if _, err := HTML(`{{.Absent}}`, map[string]string{}); err == nil {
		t.Fatal("a document rendered with a missing value")
	}
}

func TestGotenbergClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/chromium/convert/html" {
			t.Errorf("path %s", r.URL.Path)
		}
		f, h, err := r.FormFile("files")
		if err != nil || h.Filename != "index.html" {
			t.Errorf("%v", err)
			return
		}
		defer func() { _ = f.Close() }()
		_, _ = w.Write([]byte("%PDF-1.7 rendered"))
	}))
	defer srv.Close()
	out, err := NewGotenberg(srv.URL).Render(context.Background(), []byte("<p>x</p>"))
	if err != nil || !strings.HasPrefix(string(out), "%PDF-") {
		t.Fatalf("%v %s", err, out)
	}
}

func TestRendererFailuresAreErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		"not a pdf":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			if _, err := NewGotenberg(srv.URL).Render(context.Background(), []byte("x")); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
