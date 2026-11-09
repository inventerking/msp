package shared

import "testing"

func TestReservedDomainsOnlyWhenAllowed(t *testing.T) {
	AllowReservedTLDs = false
	if _, err := NormalizeDomain("acme.example"); err == nil {
		t.Fatal("a reserved domain was accepted in production mode")
	}
	AllowReservedTLDs = true
	defer func() { AllowReservedTLDs = false }()
	if got, err := NormalizeDomain("https://www.Acme-Legal.example/x"); err != nil || got != "acme-legal.example" {
		t.Fatalf("%q %v", got, err)
	}
	if got, _ := NormalizeDomain("a.b.acme.test"); got != "acme.test" {
		t.Fatalf("%q", got)
	}
	if _, err := NormalizeDomain("example"); err == nil {
		t.Fatal("a bare reserved label was accepted")
	}
}

func TestNormalizeDomain(t *testing.T) {
	good := map[string]string{
		"example.com": "example.com", "EXAMPLE.COM": "example.com", "www.example.com": "example.com", "https://www.Example.com/about?x=1": "example.com",
		"sub.deep.example.co.uk": "example.co.uk", "http://example.com:8080/": "example.com", "example.com.": "example.com", "someone@Example.org": "example.org",
		"  example.com  ": "example.com", "bücher.de": "xn--bcher-kva.de",
	}
	for in, want := range good {
		got, err := NormalizeDomain(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "localhost", "co.uk", "com", "127.0.0.1", "::1", "gmail.com", "someone@gmail.com", "outlook.com", "http://", "not a domain", "server.internal"} {
		if got, err := NormalizeDomain(in); err == nil {
			t.Errorf("%q should be rejected, got %q", in, got)
		}
	}
}
