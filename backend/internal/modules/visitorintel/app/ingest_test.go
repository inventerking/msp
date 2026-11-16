package app

import (
	"context"
	"net/netip"
	"testing"
)

func TestCleanURLKeepsOnlySiteAndUTM(t *testing.T) {
	got, utm, ok := cleanURL("https://www.acme.example/p?email=a%40b.example&utm_source=news&utm_campaign=x&t=1#frag", "acme.example", nil)
	if !ok || got != "https://www.acme.example/p?utm_campaign=x&utm_source=news" || string(utm) != `{"utm_campaign":"x","utm_source":"news"}` {
		t.Fatalf("%v %s %s", ok, got, utm)
	}
	for _, bad := range []string{"https://evil.example/", "https://acme.example.evil.example/", "javascript:alert(1)", "ftp://acme.example/"} {
		if _, _, ok := cleanURL(bad, "acme.example", []string{"http://localhost:5180"}); ok {
			t.Errorf("accepted %s", bad)
		}
	}
	if u, _, _ := cleanURL("https://user:pw@acme.example/", "acme.example", nil); u != "https://acme.example/" {
		t.Errorf("kept credentials: %s", u)
	}
}

func TestCleanURLAcceptsAllowedTestOrigins(t *testing.T) {
	if u, _, ok := cleanURL("http://localhost:5180/test-site/?x=1", "acme.example", []string{"http://localhost:5180"}); !ok || u != "http://localhost:5180/test-site/" {
		t.Fatalf("%v %s", ok, u)
	}
	if _, _, ok := cleanURL("http://localhost:9999/", "acme.example", []string{"http://localhost:5180"}); ok {
		t.Fatal("accepted another port")
	}
}

func TestReferrerAndOrigin(t *testing.T) {
	if referrerHost("https://www.acme.example/x", "acme.example") != nil {
		t.Error("internal referrer kept")
	}
	if h := referrerHost("https://Search.Example/?q=secret", "acme.example"); h == nil || *h != "search.example" {
		t.Errorf("referrer %v", h)
	}
	if !originAllowed("https://acme.example/", []string{"https://acme.example"}) || originAllowed("", []string{"https://acme.example"}) || originAllowed("https://evil.example", []string{"https://acme.example"}) {
		t.Error("origin check")
	}
}

func TestFakeIdentifierOnlyKnowsReservedRanges(t *testing.T) {
	f := FakeIdentifier{}
	for _, ip := range []string{"8.8.8.0", "81.2.69.0", "2001:4860::"} {
		if _, _, ok := f.Identify(context.Background(), netip.MustParseAddr(ip)); ok {
			t.Errorf("identified real address %s", ip)
		}
	}
	if d, _, ok := f.Identify(context.Background(), netip.MustParseAddr("203.0.113.0")); !ok || d != "riverside-accounts.example" {
		t.Errorf("reserved range: %v %s", ok, d)
	}
}
