package egress

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestIsPublic(t *testing.T) {
	denied := []string{
		"127.0.0.1", "127.1.2.3", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "0.1.2.3",
		"192.0.2.1", "198.51.100.1", "203.0.113.1", "198.18.0.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "fe80::1", "fc00::1", "fd12:3456::1", "ff02::1", "2001:db8::1", "fec0::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "64:ff9b::7f00:1", "2002:7f00:1::", "2001:0:4136:e378::1",
	}
	for _, s := range denied {
		if IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s should be denied", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "172.32.0.1", "192.169.0.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		if !IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
	if IsPublic(netip.Addr{}) {
		t.Error("the zero address was allowed")
	}
}

func TestCheckURL(t *testing.T) {
	c := New()
	for _, u := range []string{
		"file:///etc/passwd", "gopher://example.com", "ftp://example.com", "http://", "not a url\x7f",
		"http://localhost/", "http://LOCALHOST/", "http://foo.localhost/", "http://localhost./", "http://db.internal/", "http://printer.local/", "http://intranet/",
		"http://127.0.0.1/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/",
		"http://2130706433/", "http://0x7f000001/", "http://0x7f.0.0.1/", "http://017700000001/", "http://127.1/",
		"http://user:pass@example.com/", "http://example.com@127.0.0.1/",
		"http://example.com:22/", "https://example.com:6379/", "http://example.com:8080/",
	} {
		if _, err := c.CheckURL(u); !errors.Is(err, ErrDenied) {
			t.Errorf("%q should be denied, got %v", u, err)
		}
	}
	for _, u := range []string{"https://example.com/", "http://example.com/path?q=1", "https://example.com:443/", "http://8.8.8.8/", "https://sub.example.co.uk/"} {
		if _, err := c.CheckURL(u); err != nil {
			t.Errorf("%q should be allowed: %v", u, err)
		}
	}
}

// fakeDNS answers from a table, and can change its answer between lookups as a rebinding attack does.
type fakeDNS struct {
	answers map[string][][]string
	calls   map[string]int
}

func (f *fakeDNS) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	seq, ok := f.answers[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	i := f.calls[host]
	f.calls[host]++
	if i >= len(seq) {
		i = len(seq) - 1
	}
	var out []netip.Addr
	for _, s := range seq[i] {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

// client routes "public" addresses to a local test server, standing in for the internet.
func client(t *testing.T, srv *httptest.Server, dns *fakeDNS) (*Client, *[]string, *[]Decision) {
	t.Helper()
	dialed := &[]string{}
	decisions := &[]Decision{}
	c := New()
	c.Resolver = dns
	c.OnDecision = func(d Decision) { *decisions = append(*decisions, d) }
	c.DialAddr = func(ctx context.Context, network, addr string) (net.Conn, error) {
		*dialed = append(*dialed, addr)
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	return c, dialed, decisions
}

func TestAllowsPublicHostAndPinsAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hello")) }))
	defer srv.Close()
	dns := &fakeDNS{answers: map[string][][]string{"public.example": {{"8.8.8.8"}}}, calls: map[string]int{}}
	c, dialed, decisions := client(t, srv, dns)
	r, err := c.Get(context.Background(), "http://public.example/")
	if err != nil || string(r.Body) != "hello" {
		t.Fatalf("%v %q", err, r.Body)
	}
	if (*dialed)[0] != "8.8.8.8:80" {
		t.Fatalf("connected by %q, not by the validated address", (*dialed)[0])
	}
	if !(*decisions)[0].Allowed {
		t.Fatalf("decision not recorded: %+v", decisions)
	}
}

func TestDeniesHostResolvingToPrivateAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("request reached the target") }))
	defer srv.Close()
	for name, answer := range map[string][]string{
		"rfc1918.example":   {"10.1.2.3"},
		"meta.example":      {"169.254.169.254"},
		"loop.example":      {"127.0.0.1"},
		"mapped.example":    {"::ffff:192.168.0.1"},
		"v6loop.example":    {"::1"},
		"cgnat.example":     {"100.64.1.1"},
		"mixed.example":     {"8.8.8.8", "10.0.0.1"},
		"mixed-six.example": {"2606:4700::1", "fc00::1"},
	} {
		t.Run(name, func(t *testing.T) {
			dns := &fakeDNS{answers: map[string][][]string{name: {answer}}, calls: map[string]int{}}
			c, dialed, decisions := client(t, srv, dns)
			_, err := c.Get(context.Background(), "http://"+name+"/")
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("got %v", err)
			}
			if len(*dialed) != 0 {
				t.Fatalf("connected to %v", *dialed)
			}
			if len(*decisions) == 0 || (*decisions)[0].Allowed {
				t.Fatalf("denial not recorded: %+v", decisions)
			}
		})
	}
}

// The first lookup says public and a later one says private. Connecting by the validated number
// means the later answer is never used for this connection.
func TestDNSRebindingCannotSwapTheConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	dns := &fakeDNS{answers: map[string][][]string{"rebind.example": {{"8.8.8.8"}, {"127.0.0.1"}}}, calls: map[string]int{}}
	c, dialed, _ := client(t, srv, dns)
	if _, err := c.Get(context.Background(), "http://rebind.example/"); err != nil {
		t.Fatal(err)
	}
	if dns.calls["rebind.example"] != 1 || (*dialed)[0] != "8.8.8.8:80" {
		t.Fatalf("lookups=%d dialed=%v", dns.calls["rebind.example"], *dialed)
	}
	// If the name resolves privately on a later request, that request is denied.
	if _, err := c.Get(context.Background(), "http://rebind.example/"); !errors.Is(err, ErrDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestRedirectsAreCheckedAgain(t *testing.T) {
	for name, target := range map[string]string{
		"metadata address": "http://169.254.169.254/latest/meta-data/",
		"loopback":         "http://127.0.0.1/admin",
		"localhost":        "http://localhost/",
		"bad scheme":       "file:///etc/passwd",
		"bad port":         "http://example.com:6379/",
		"resolves private": "http://evil.example/",
	} {
		t.Run(name, func(t *testing.T) {
			reached := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host == "start.example" {
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				reached = true
			}))
			defer srv.Close()
			dns := &fakeDNS{answers: map[string][][]string{"start.example": {{"8.8.8.8"}}, "evil.example": {{"10.0.0.1"}}}, calls: map[string]int{}}
			c, _, _ := client(t, srv, dns)
			_, err := c.Get(context.Background(), "http://start.example/")
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("got %v", err)
			}
			if reached {
				t.Fatal("the redirect target was contacted")
			}
		})
	}
}

func TestRedirectLoopStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://start.example/", http.StatusFound) }))
	defer srv.Close()
	dns := &fakeDNS{answers: map[string][][]string{"start.example": {{"8.8.8.8"}}}, calls: map[string]int{}}
	c, _, _ := client(t, srv, dns)
	if _, err := c.Get(context.Background(), "http://start.example/"); !errors.Is(err, ErrDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestLargeResponsesAreTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 100))) }))
	defer srv.Close()
	dns := &fakeDNS{answers: map[string][][]string{"big.example": {{"8.8.8.8"}}}, calls: map[string]int{}}
	c, _, _ := client(t, srv, dns)
	c.MaxBytes = 10
	r, err := c.Get(context.Background(), "http://big.example/")
	if err != nil || !r.Truncated || len(r.Body) != 10 {
		t.Fatalf("%v %v %d", err, r.Truncated, len(r.Body))
	}
}

func TestEnvironmentProxyIsIgnored(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://10.0.0.1:3128")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("direct")) }))
	defer srv.Close()
	dns := &fakeDNS{answers: map[string][][]string{"p.example": {{"8.8.8.8"}}}, calls: map[string]int{}}
	c, dialed, _ := client(t, srv, dns)
	if _, err := c.Get(context.Background(), "http://p.example/"); err != nil {
		t.Fatal(err)
	}
	if (*dialed)[0] != "8.8.8.8:80" {
		t.Fatalf("went via %v", *dialed)
	}
}
