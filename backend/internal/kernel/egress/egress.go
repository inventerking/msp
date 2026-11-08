// Package egress is the guarded HTTP client for every request whose target a tenant can influence:
// assessments, research fetches, knowledge URLs. It defends against server-side request forgery.
// Every resolved address must be public, the connection is pinned to the validated address so DNS
// rebinding cannot swap it, and each redirect is checked again. Container network rules are the
// second layer, so one bug here is not enough to reach internal systems.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrDenied means the target is not allowed. Callers record it as evidence and do not retry.
var ErrDenied = errors.New("egress denied")

// Decision is one allow or deny, for the caller's records.
type Decision struct {
	Host    string
	IP      netip.Addr
	Allowed bool
	Reason  string
}

// Resolver finds addresses. Tests replace it to simulate hostile DNS.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Client makes guarded requests.
type Client struct {
	Resolver     Resolver
	Timeout      time.Duration
	MaxRedirects int
	MaxBytes     int64
	AllowedPorts []string
	UserAgent    string
	// OnDecision records each decision, for evidence such as it_assessment.egress_decisions.
	OnDecision func(Decision)
	// DialAddr connects to an already validated address. Tests can replace it.
	DialAddr func(ctx context.Context, network, addr string) (net.Conn, error)
}

// New creates a client with safe defaults.
func New() *Client {
	d := &net.Dialer{Timeout: 5 * time.Second}
	return &Client{Resolver: net.DefaultResolver, Timeout: 15 * time.Second, MaxRedirects: 5, MaxBytes: 5 << 20, AllowedPorts: []string{"80", "443"}, UserAgent: "MSPGrowthEngine/1.0", DialAddr: d.DialContext}
}

var blocked = func() []netip.Prefix {
	var out []netip.Prefix
	for _, c := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24",
		"192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "::ffff:0:0/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32", "2002::/16", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}()

// IsPublic reports whether an address is safe to contact. IPv4-mapped IPv6 addresses are unwrapped
// first, so ::ffff:127.0.0.1 is treated as the loopback address it is. Ranges that embed an IPv4
// address, such as NAT64 and 6to4, are blocked, because they could reach a private one.
func IsPublic(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, p := range blocked {
		if p.Contains(a) {
			return false
		}
	}
	return a.IsGlobalUnicast()
}

// Hostnames made only of digits, hex or dots can be read as IP addresses by some software.
var numericHost = regexp.MustCompile(`(?i)^(0x[0-9a-f]+|[0-9]+)(\.(0x[0-9a-f]+|[0-9]+))*$`)

// CheckURL validates a URL's shape before any network activity.
func (c *Client) CheckURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: unparseable URL", ErrDenied)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: scheme %q", ErrDenied, u.Scheme)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: URL contains credentials", ErrDenied)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: no host", ErrDenied)
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") || !strings.Contains(lower, ".") && !strings.Contains(lower, ":") {
		return nil, fmt.Errorf("%w: internal hostname", ErrDenied)
	}
	if a, err := netip.ParseAddr(host); err == nil {
		if !IsPublic(a) {
			return nil, fmt.Errorf("%w: address not public", ErrDenied)
		}
	} else if numericHost.MatchString(lower) {
		return nil, fmt.Errorf("%w: numeric hostname", ErrDenied)
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	if !slices.Contains(c.AllowedPorts, port) {
		return nil, fmt.Errorf("%w: port %s", ErrDenied, port)
	}
	return u, nil
}

func (c *Client) note(d Decision) {
	if c.OnDecision != nil {
		c.OnDecision(d)
	}
}

// dial resolves the host, requires every address to be public, and connects to a validated address
// by number, so a second DNS answer cannot change where the connection goes.
func (c *Client) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: bad address", ErrDenied)
	}
	if !slices.Contains(c.AllowedPorts, port) {
		c.note(Decision{Host: host, Reason: "port " + port})
		return nil, fmt.Errorf("%w: port %s", ErrDenied, port)
	}
	var addrs []netip.Addr
	if a, perr := netip.ParseAddr(host); perr == nil {
		addrs = []netip.Addr{a}
	} else {
		addrs, err = c.Resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", host, err)
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	// One bad answer denies the whole name, as an attacker can mix public and private answers.
	for _, a := range addrs {
		if !IsPublic(a) {
			c.note(Decision{Host: host, IP: a, Reason: "address not public"})
			return nil, fmt.Errorf("%w: %s resolves to a non-public address", ErrDenied, host)
		}
	}
	chosen := addrs[0].Unmap()
	c.note(Decision{Host: host, IP: chosen, Allowed: true})
	return c.DialAddr(ctx, network, net.JoinHostPort(chosen.String(), port))
}

func (c *Client) http() *http.Client {
	t := &http.Transport{
		Proxy:                 nil, // Proxy settings from the environment must not route around the guard.
		DialContext:           c.dial,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableKeepAlives:     true,
	}
	return &http.Client{Transport: t, Timeout: c.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= c.MaxRedirects {
			return fmt.Errorf("%w: too many redirects", ErrDenied)
		}
		if _, err := c.CheckURL(req.URL.String()); err != nil {
			c.note(Decision{Host: req.URL.Hostname(), Reason: "redirect target denied"})
			return err
		}
		return nil
	}}
}

// Response is a size-limited response.
type Response struct {
	Status      int
	ContentType string
	Body        []byte
	Truncated   bool
	FinalURL    string
}

// Get fetches a URL through the guard.
func (c *Client) Get(ctx context.Context, raw string) (Response, error) {
	u, err := c.CheckURL(raw)
	if err != nil {
		c.note(Decision{Host: hostOf(raw), Reason: err.Error()})
		return Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.http().Do(req)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBytes+1))
	if err != nil {
		return Response{}, err
	}
	out := Response{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), FinalURL: resp.Request.URL.String()}
	if int64(len(body)) > c.MaxBytes {
		out.Truncated, body = true, body[:c.MaxBytes]
	}
	out.Body = body
	return out, nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ParsePort is a small helper for callers that build allow lists.
func ParsePort(p int) string { return strconv.Itoa(p) }
