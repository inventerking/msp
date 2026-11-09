package domain

import (
	"strings"
	"testing"
)

func TestRobots(t *testing.T) {
	r := ParseRobots([]byte("User-agent: *\nDisallow: /private\nAllow: /private/ok\n\nUser-agent: OtherBot\nDisallow: /\n"))
	for path, want := range map[string]bool{"/": true, "/pricing": true, "/private": false, "/private/x": false, "/private/ok/page": true} {
		if got := r.Allowed(path); got != want {
			t.Errorf("%s: got %v", path, got)
		}
	}
	ours := ParseRobots([]byte("User-agent: *\nAllow: /\n\nUser-agent: MSPGrowthEngineResearch\nDisallow: /\n"))
	if ours.Allowed("/") {
		t.Error("a group naming our agent was ignored")
	}
	if !ParseRobots([]byte("garbage")).Allowed("/x") || !ParseRobots([]byte("User-agent: *\nDisallow:\n")).Allowed("/x") {
		t.Error("an empty or unreadable file must allow")
	}
	wild := ParseRobots([]byte("User-agent: *\nDisallow: /*.pdf$\nDisallow: /tmp*\n"))
	if wild.Allowed("/a/b.pdf") || !wild.Allowed("/a/b.pdf.html") || wild.Allowed("/tmpfiles") {
		t.Error("wildcards are not applied")
	}
}

func TestPageText(t *testing.T) {
	page := `<html><head><style>.x{}</style><script>alert("Pricing: free")</script></head><body><h1>Acme IT</h1><p>Pricing:  From &pound;45 per user</p><!-- Services: hidden --><div>Services: <b>Backup</b>, Security</div></body></html>`
	got := PageText([]byte(page), "text/html")
	if strings.Contains(got, "alert") || strings.Contains(got, "hidden") || strings.Contains(got, "<") {
		t.Fatalf("markup or hidden text kept: %q", got)
	}
	if !strings.Contains(got, "Pricing: From £45 per user") || !strings.Contains(got, "Services: Backup , Security") {
		t.Fatalf("text lost: %q", got)
	}
	if len(PageText([]byte(strings.Repeat("a", MaxPageText*2)), "text/plain")) != MaxPageText {
		t.Fatal("text not bounded")
	}
}

func TestLabel(t *testing.T) {
	page := "Our Services: Managed IT, Cloud backup\nPricing: From £45 per user per month"
	cases := []struct{ quote, value, want string }{
		{"Pricing: From £45 per user per month", "From £45 per user per month", Observed},
		{"pricing:   from £45 PER user per month", "£45 per user", Observed}, // Spacing and case do not matter.
		{"Pricing: From £30 per user per month", "£30 per user", AIEstimate}, // Not on the page.
		{"Pricing: From £45 per user per month", "£99", AIEstimate},          // Value not in the quote.
		{"", "Managed IT", AIEstimate},
	}
	for _, c := range cases {
		if got := Label(page, c.quote, c.value); got != c.want {
			t.Errorf("%q / %q: got %s", c.quote, c.value, got)
		}
	}
}

func TestChanges(t *testing.T) {
	before := map[string][]string{"pricing": {"From £45"}, "service": {"Backup", "Security"}, "geography": {"GB"}}
	after := map[string][]string{"pricing": {"from £50"}, "service": {"security", "backup"}, "industry": {"Healthcare"}}
	got := Changes(before, after)
	if len(got) != 2 || got[0].Attribute != "industry" || got[1].Attribute != "pricing" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Summary() != "industry first seen: healthcare" || got[1].Summary() != "pricing changed from from £45 to from £50" {
		t.Fatalf("summaries: %q %q", got[0].Summary(), got[1].Summary())
	}
}
