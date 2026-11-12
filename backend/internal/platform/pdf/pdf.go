// Package pdf renders documents. Templates escape every value, and rendering happens in a Gotenberg
// container with no network access, so a value in a document cannot reach internal systems.
package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// Renderer turns HTML into a PDF.
type Renderer interface {
	Render(ctx context.Context, html []byte) ([]byte, error)
}

// HTML fills a template with auto-escaping. Trusted markup cannot be injected through data.
func HTML(tmpl string, data any) ([]byte, error) {
	t, err := template.New("document").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	return b.Bytes(), nil
}

// HTMLFuncs is HTML with extra template functions, such as money formatting. Values stay escaped.
func HTMLFuncs(tmpl string, data any, funcs template.FuncMap) ([]byte, error) {
	t, err := template.New("document").Option("missingkey=error").Funcs(funcs).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	return b.Bytes(), nil
}

// Gotenberg calls a Gotenberg service.
type Gotenberg struct {
	URL    string
	Client *http.Client
}

// NewGotenberg creates a client.
func NewGotenberg(url string) *Gotenberg {
	return &Gotenberg{URL: strings.TrimRight(url, "/"), Client: &http.Client{Timeout: 60 * time.Second}}
}

// Render converts HTML to PDF.
func (g *Gotenberg) Render(ctx context.Context, html []byte) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="files"; filename="index.html"`)
	h.Set("Content-Type", "text/html")
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(html); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	// g.URL is operator configuration (GOTENBERG_URL), never tenant input.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.URL+"/forms/chromium/convert/html", &body) //nolint:gosec // See above.
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := g.Client.Do(req) //nolint:gosec // The URL is operator configuration.
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("renderer returned %d", resp.StatusCode)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		return nil, errors.New("renderer did not return a PDF")
	}
	return out, nil
}

// Fake returns a minimal PDF holding the HTML, for tests.
type Fake struct{ Last []byte }

// Render records its input.
func (f *Fake) Render(_ context.Context, html []byte) ([]byte, error) {
	f.Last = html
	return append([]byte("%PDF-1.7 fake\n"), html...), nil
}
