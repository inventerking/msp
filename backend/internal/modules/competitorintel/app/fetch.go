package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"mspengine/internal/kernel/egress"
	"mspengine/internal/modules/competitorintel/domain"
	"mspengine/internal/platform/integration/fakes"
)

// Page is a fetched page.
type Page struct {
	Status      int
	ContentType string
	Body        []byte
}

// Fetcher gets public pages. The live one goes through the Egress Guard; the fake one reads canned
// pages, so nothing is fetched from real websites until legal review allows it.
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) (Page, error)
	Live() bool
}

// LiveFetcher fetches through the SSRF-guarded egress client.
type LiveFetcher struct{ Client *egress.Client }

// NewLiveFetcher creates a fetcher that identifies itself honestly.
func NewLiveFetcher() *LiveFetcher {
	c := egress.New()
	c.UserAgent = domain.UserAgent + " (+research for a managed service provider)"
	return &LiveFetcher{Client: c}
}

// Fetch gets one page.
func (f *LiveFetcher) Fetch(ctx context.Context, rawURL string) (Page, error) {
	r, err := f.Client.Get(ctx, rawURL)
	if err != nil {
		return Page{}, err
	}
	return Page{Status: r.Status, ContentType: r.ContentType, Body: r.Body}, nil
}

// Live reports that this fetcher reaches real websites.
func (*LiveFetcher) Live() bool { return true }

// FakeFetcher serves invented pages for the fake companies, on reserved .example domains.
type FakeFetcher struct {
	mu    sync.Mutex
	pages map[string]Page
}

// NewFakeFetcher builds pages from the sample companies. A site named no-robots.example disallows
// everything, so robots handling can be seen working.
func NewFakeFetcher() *FakeFetcher {
	f := &FakeFetcher{pages: map[string]Page{}}
	for i, r := range fakes.SampleCompanies().Records {
		html := fmt.Sprintf(`<html><head><title>%s</title><script>var note = "Pricing: free";</script></head><body>
<h1>%s</h1>
<p>Services: Managed IT, Cybersecurity, Cloud backup</p>
<p>Pricing: From £%d per user per month</p>
<p>Regions: %s</p>
<p>Industries: %s</p>
</body></html>`, r.Name, r.Name, 40+i*5, r.Country, strings.ReplaceAll(r.Industry, "_", " "))
		f.pages["https://"+r.Domain+"/"] = Page{Status: 200, ContentType: "text/html", Body: []byte(html)}
	}
	f.pages["https://no-robots.example/robots.txt"] = Page{Status: 200, ContentType: "text/plain", Body: []byte("User-agent: *\nDisallow: /\n")}
	f.pages["https://no-robots.example/"] = Page{Status: 200, ContentType: "text/html", Body: []byte("<p>Pricing: secret</p>")}
	return f
}

// Set replaces a page, so tests and demos can show a competitor changing its site.
func (f *FakeFetcher) Set(rawURL string, p Page) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[rawURL] = p
}

// Fetch returns a canned page, or 404.
func (f *FakeFetcher) Fetch(_ context.Context, rawURL string) (Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Page{}, err
	}
	u.RawQuery, u.Fragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.pages[u.String()]; ok {
		return p, nil
	}
	return Page{Status: 404, ContentType: "text/plain"}, nil
}

// Live reports that this fetcher reaches no real website.
func (*FakeFetcher) Live() bool { return false }
