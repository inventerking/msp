package app

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"mspengine/internal/kernel/egress"
	"mspengine/internal/modules/itassessment/domain"
)

// Prober reads public facts about a domain. The live prober reaches real DNS and the domain's own
// web host; the fake one reaches nothing, so no domain the team does not control is ever touched.
type Prober interface {
	Live() bool
	TXT(ctx context.Context, name string) ([]string, error)
	MX(ctx context.Context, domain string) ([]string, error)
	TLS(ctx context.Context, host string) (*domain.TLSInfo, error)
	Headers(ctx context.Context, rawURL string) (map[string]string, error)
}

// ErrNoRecord means a lookup succeeded and found nothing, which is a result, not an error.
var ErrNoRecord = errors.New("no record")

// LiveProber uses public DNS and the Egress Guard. Every address it connects to must be public.
type LiveProber struct {
	Resolver *net.Resolver
	Client   *egress.Client
	// OnDecision records each connection decision for the assessment's evidence.
	OnDecision func(host string, ip netip.Addr, allowed bool, reason string)
}

// NewLiveProber creates a live prober with short timeouts.
func NewLiveProber() *LiveProber {
	c := egress.New()
	c.UserAgent = "MSPGrowthEngineAssessment (+security posture check authorized by the domain owner)"
	c.Timeout = 10 * time.Second
	c.MaxBytes = 64 << 10
	return &LiveProber{Resolver: net.DefaultResolver, Client: c}
}

// Live reports that this prober reaches real systems.
func (*LiveProber) Live() bool { return true }

func notFound(err error) error {
	var de *net.DNSError
	if errors.As(err, &de) && de.IsNotFound {
		return ErrNoRecord
	}
	return err
}

// TXT looks up text records.
func (p *LiveProber) TXT(ctx context.Context, name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := p.Resolver.LookupTXT(ctx, name)
	return r, notFound(err)
}

// MX looks up mail servers.
func (p *LiveProber) MX(ctx context.Context, d string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := p.Resolver.LookupMX(ctx, d)
	out := make([]string, len(r))
	for i, m := range r {
		out[i] = strings.TrimSuffix(m.Host, ".")
	}
	return out, notFound(err)
}

// TLS performs one handshake to the host's first public address, refusing any private address.
func (p *LiveProber) TLS(ctx context.Context, host string) (*domain.TLSInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := p.Resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, notFound(err)
	}
	for _, ip := range ips {
		if !egress.IsPublic(ip) {
			p.note(host, ip, false, "address is not public")
			return nil, egress.ErrDenied
		}
	}
	p.note(host, ips[0], true, "")
	d := tls.Dialer{Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS10}} //nolint:gosec // Reading which versions a server offers is the check.
	conn, err := d.DialContext(ctx, "tcp", netip.AddrPortFrom(ips[0].Unmap(), 443).String())
	if err != nil {
		var ce *tls.CertificateVerificationError
		if errors.As(err, &ce) && len(ce.UnverifiedCertificates) > 0 {
			return &domain.TLSInfo{Version: "unknown", NotAfter: ce.UnverifiedCertificates[0].NotAfter, HostnameValid: false}, nil
		}
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	st := conn.(*tls.Conn).ConnectionState()
	info := &domain.TLSInfo{Version: tlsName(st.Version), HostnameValid: true}
	if len(st.PeerCertificates) > 0 {
		info.NotAfter = st.PeerCertificates[0].NotAfter
	}
	return info, nil
}

func tlsName(v uint16) string {
	return map[uint16]string{tls.VersionTLS10: "TLS1.0", tls.VersionTLS11: "TLS1.1", tls.VersionTLS12: "TLS1.2", tls.VersionTLS13: "TLS1.3"}[v]
}

// Headers fetches the home page through the Egress Guard and returns its response headers.
func (p *LiveProber) Headers(ctx context.Context, rawURL string) (map[string]string, error) {
	r, err := p.Client.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range r.Header {
		out[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return out, nil
}

func (p *LiveProber) note(host string, ip netip.Addr, allowed bool, reason string) {
	if p.OnDecision != nil {
		p.OnDecision(host, ip, allowed, reason)
	}
}

// FakeProber answers from invented data. Domains are read by their first label: strong-… passes
// everything, weak-… fails most checks, and anything else looks like a typical small business.
type FakeProber struct {
	mu sync.Mutex
	// AutoVerify treats reserved .example and .test domains as verified, since nothing real is
	// reached. Tests turn it off to see verification fail.
	AutoVerify bool
	txt        map[string][]string
}

// NewFakeProber creates a fake prober that verifies reserved domains automatically.
func NewFakeProber() *FakeProber { return &FakeProber{AutoVerify: true, txt: map[string][]string{}} }

// Live reports that this prober reaches nothing.
func (*FakeProber) Live() bool { return false }

// SetTXT publishes a text record in the fake DNS.
func (f *FakeProber) SetTXT(name string, values []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txt[name] = values
}

func profile(name string) string {
	if i := strings.Index(name, "._domainkey."); i >= 0 {
		name = name[i+len("._domainkey."):]
	}
	label := strings.Split(strings.TrimPrefix(name, "_dmarc."), ".")[0]
	switch {
	case strings.HasPrefix(label, "strong-"):
		return "strong"
	case strings.HasPrefix(label, "weak-"):
		return "weak"
	}
	return "typical"
}

// TXT answers from the fake records, then from the domain's profile.
func (f *FakeProber) TXT(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	v, ok := f.txt[name]
	f.mu.Unlock()
	if ok {
		return v, nil
	}
	p := profile(name)
	switch {
	case strings.HasPrefix(name, "_dmarc."):
		return map[string][]string{"strong": {"v=DMARC1; p=reject"}, "typical": {"v=DMARC1; p=none"}}[p], nilIfAny(p != "weak")
	case strings.Contains(name, "._domainkey."):
		if p == "strong" && strings.HasPrefix(name, "selector1.") {
			return []string{"v=DKIM1; k=rsa; p=MIIB"}, nil
		}
		return nil, ErrNoRecord
	case strings.HasPrefix(name, "_"):
		return nil, ErrNoRecord
	}
	return map[string][]string{"strong": {"v=spf1 include:mail.example -all"}, "weak": {"v=spf1 +all"}, "typical": {"v=spf1 include:mail.example ~all"}}[p], nil
}

func nilIfAny(found bool) error {
	if found {
		return nil
	}
	return ErrNoRecord
}

// MX answers from the profile.
func (f *FakeProber) MX(_ context.Context, d string) ([]string, error) {
	if profile(d) == "weak" {
		return nil, ErrNoRecord
	}
	return []string{"mx1.mail.example"}, nil
}

// TLS answers from the profile.
func (f *FakeProber) TLS(_ context.Context, host string) (*domain.TLSInfo, error) {
	if profile(host) == "weak" {
		return &domain.TLSInfo{Version: "TLS1.0", NotAfter: time.Now().AddDate(0, 0, 5), HostnameValid: true}, nil
	}
	return &domain.TLSInfo{Version: "TLS1.3", NotAfter: time.Now().AddDate(0, 3, 0), HostnameValid: true}, nil
}

// Headers answers from the profile.
func (f *FakeProber) Headers(_ context.Context, rawURL string) (map[string]string, error) {
	host := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	if profile(host) == "strong" {
		return map[string]string{"strict-transport-security": "max-age=31536000", "content-security-policy": "default-src 'self'", "x-content-type-options": "nosniff"}, nil
	}
	return map[string]string{"x-content-type-options": "nosniff"}, nil
}
