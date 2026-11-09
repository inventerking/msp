// Package shared owns the shared schema: canonical companies, tenant-scoped contacts, reference data.
package shared

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// AllowReservedTLDs accepts the reserved .example and .test domains. It is on outside production, so
// fake data never creates canonical records for real companies' domains, and off in production.
var AllowReservedTLDs = false

// ErrBadDomain means an input cannot identify a company.
var ErrBadDomain = errors.New("not a company domain")

// freeMail domains identify a mailbox provider, not the company behind an address.
var freeMail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true, "msn.com": true,
	"yahoo.com": true, "ymail.com": true, "icloud.com": true, "me.com": true, "mac.com": true, "aol.com": true, "proton.me": true,
	"protonmail.com": true, "gmx.com": true, "gmx.de": true, "web.de": true, "mail.com": true, "zoho.com": true, "yandex.com": true, "qq.com": true, "163.com": true,
}

// NormalizeDomain reduces a website or email domain to its registrable domain, in ASCII form:
// "https://WWW.Example.co.uk/about" becomes "example.co.uk". Free-mail domains are rejected.
func NormalizeDomain(input string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(input))
	if at := strings.LastIndex(s, "@"); at >= 0 && !strings.Contains(s, "://") {
		s = s[at+1:]
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", ErrBadDomain
		}
		s = u.Hostname()
	} else if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if host, _, ok := strings.Cut(s, ":"); ok {
		s = host
	}
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return "", ErrBadDomain
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return "", ErrBadDomain
	}
	ascii, err := idna.Lookup.ToASCII(s)
	if err != nil {
		return "", ErrBadDomain
	}
	if AllowReservedTLDs {
		if labels := strings.Split(ascii, "."); len(labels) >= 2 && (labels[len(labels)-1] == "example" || labels[len(labels)-1] == "test") {
			return strings.Join(labels[len(labels)-2:], "."), nil
		}
	}
	reg, err := publicsuffix.EffectiveTLDPlusOne(ascii)
	if err != nil {
		return "", ErrBadDomain // A bare public suffix, such as "co.uk", or no dot at all.
	}
	if suffix, icann := publicsuffix.PublicSuffix(ascii); !icann && !strings.Contains(suffix, ".") {
		return "", ErrBadDomain // An unknown top-level domain, such as "internal".
	}
	if freeMail[reg] {
		return "", ErrBadDomain
	}
	return reg, nil
}

// NormalizeEmail lower-cases and trims an address.
func NormalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }
