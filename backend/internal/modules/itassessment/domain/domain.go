// Package domain holds IT Assessment rules that need no network or database: which checks each
// authorization tier allows, how results are read, the findings shown, and scoring.
package domain

import (
	"slices"
	"strings"
	"time"
)

// Tiers (D-12). Passive reads only what public infrastructure already publishes. Exposure adds a few
// non-invasive requests to the domain's own web host, and needs verified control of the domain.
const (
	Passive  = "passive"
	Exposure = "exposure"
)

// Check keys. This list is the whole scope of the product: templates choose from it and cannot add
// to it. Exploitation, credential attempts, content discovery, fuzzing, load and port scanning are
// not here and cannot be configured.
const (
	CheckSPF     = "email.spf"
	CheckDMARC   = "email.dmarc"
	CheckDKIM    = "email.dkim_common_selectors"
	CheckMX      = "email.mx"
	CheckTLS     = "web.tls"
	CheckHeaders = "web.security_headers"
)

// Allowed lists the checks each tier permits.
var Allowed = map[string][]string{
	Passive:  {CheckSPF, CheckDMARC, CheckDKIM, CheckMX, CheckTLS},
	Exposure: {CheckSPF, CheckDMARC, CheckDKIM, CheckMX, CheckTLS, CheckHeaders},
}

// TierAllows reports whether an authorization tier covers a template tier.
func TierAllows(authorized, wanted string) bool {
	return authorized == Exposure || wanted == Passive
}

// Scope returns the checks a run may perform: the template's, limited to the tier's allowlist.
func Scope(tier string, requested []string) []string {
	var out []string
	for _, k := range requested {
		if slices.Contains(Allowed[tier], k) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// Statuses of a check.
const (
	Passed       = "passed"
	Failed       = "failed"
	NotDetected  = "not_detected"
	Inconclusive = "inconclusive"
	Skipped      = "skipped"
)

// Result is one check's outcome. Evidence holds public values only.
type Result struct {
	Key      string
	Status   string
	Target   string
	Evidence map[string]any
}

// Facts are what probes returned. A nil slice with an error flag means the lookup failed, which is
// inconclusive, never a failure.
type Facts struct {
	Domain     string
	RootTXT    []string
	RootTXTErr bool
	DMARC      []string
	DMARCErr   bool
	DKIMFound  []string // Selectors with a record.
	DKIMErr    bool
	MX         []string
	MXErr      bool
	TLS        *TLSInfo
	TLSErr     bool
	Headers    map[string]string
	HeadersErr bool
}

// TLSInfo describes the web host's certificate and protocol.
type TLSInfo struct {
	Version       string // "TLS1.2", "TLS1.3"...
	NotAfter      time.Time
	HostnameValid bool
}

// Evaluate turns probe facts into check results, for the checks in scope.
func Evaluate(scope []string, f Facts, now time.Time) []Result {
	var out []Result
	for _, k := range scope {
		r := Result{Key: k, Target: f.Domain, Evidence: map[string]any{}}
		switch k {
		case CheckSPF:
			spf := find(f.RootTXT, "v=spf1")
			switch {
			case f.RootTXTErr:
				r.Status = Inconclusive
			case spf == "":
				r.Status = NotDetected
			case strings.Contains(spf, "+all"):
				r.Status, r.Evidence["record"] = Failed, spf
			default:
				r.Status, r.Evidence["record"] = Passed, spf
			}
		case CheckDMARC:
			r.Target = "_dmarc." + f.Domain
			rec := find(f.DMARC, "v=DMARC1")
			switch {
			case f.DMARCErr:
				r.Status = Inconclusive
			case rec == "":
				r.Status = NotDetected
			case strings.Contains(strings.ReplaceAll(rec, " ", ""), "p=none"):
				r.Status, r.Evidence["record"] = Failed, rec
			default:
				r.Status, r.Evidence["record"] = Passed, rec
			}
		case CheckDKIM:
			switch {
			case f.DKIMErr:
				r.Status = Inconclusive
			case len(f.DKIMFound) == 0:
				r.Status = NotDetected // Only common selectors are checked; DKIM may still be set up.
			default:
				r.Status, r.Evidence["selectors"] = Passed, f.DKIMFound
			}
		case CheckMX:
			switch {
			case f.MXErr:
				r.Status = Inconclusive
			case len(f.MX) == 0:
				r.Status = NotDetected
			default:
				r.Status, r.Evidence["hosts"] = Passed, f.MX
			}
		case CheckTLS:
			switch {
			case f.TLSErr || f.TLS == nil:
				r.Status = Inconclusive
			default:
				r.Evidence["version"], r.Evidence["expires"], r.Evidence["hostname_valid"] = f.TLS.Version, f.TLS.NotAfter.Format(time.DateOnly), f.TLS.HostnameValid
				if f.TLS.Version == "TLS1.0" || f.TLS.Version == "TLS1.1" || !f.TLS.HostnameValid || f.TLS.NotAfter.Before(now.Add(14*24*time.Hour)) {
					r.Status = Failed
				} else {
					r.Status = Passed
				}
			}
		case CheckHeaders:
			switch {
			case f.HeadersErr || f.Headers == nil:
				r.Status = Inconclusive
			default:
				var missing []string
				for _, h := range []string{"strict-transport-security", "content-security-policy", "x-content-type-options"} {
					if f.Headers[h] == "" {
						missing = append(missing, h)
					}
				}
				r.Evidence["missing"] = missing
				r.Status = Passed
				if len(missing) > 0 {
					r.Status = Failed
				}
			}
		}
		out = append(out, r)
	}
	return out
}

func find(records []string, prefix string) string {
	for _, r := range records {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r)), strings.ToLower(prefix)) {
			return r
		}
	}
	return ""
}

// Finding is what a person reads about a result.
type Finding struct {
	Severity, Classification, Title, Description, Recommendation string
}

// FindingFor explains a result that needs attention. Wording stays honest: public signals can show
// a likely gap, never confirm a vulnerability.
func FindingFor(r Result) (Finding, bool) {
	key := r.Key + ":" + r.Status
	f, ok := findings[key]
	return f, ok
}

var findings = map[string]Finding{
	CheckSPF + ":" + NotDetected:   {"medium", "potential_issue", "No SPF record found", "No SPF record was published for this domain, so receiving mail servers cannot check which servers may send its email.", "Publish an SPF record that lists the services allowed to send email for the domain."},
	CheckSPF + ":" + Failed:        {"high", "confirmed_misconfiguration", "SPF allows any sender", "The published SPF record ends in +all, which tells receivers that any server may send email for this domain.", "Change the record to end in -all or ~all after listing the real sending services."},
	CheckDMARC + ":" + NotDetected: {"medium", "potential_issue", "No DMARC policy found", "No DMARC record was published, so receivers are not told how to treat email that fails SPF or DKIM checks.", "Publish a DMARC record, starting with reporting, then move to quarantine or reject."},
	CheckDMARC + ":" + Failed:      {"low", "potential_issue", "DMARC is in monitoring mode only", "The DMARC policy is p=none, which reports problems but does not ask receivers to act on spoofed email.", "Once reports look clean, move the policy to quarantine, then reject."},
	CheckDKIM + ":" + NotDetected:  {"info", "informational", "DKIM not detected on common selectors", "No DKIM key was found on the common selector names checked. DKIM selectors cannot be listed publicly, so DKIM may still be set up under another name.", "Confirm with the email provider that DKIM signing is enabled."},
	CheckMX + ":" + NotDetected:    {"info", "informational", "No mail servers published", "The domain publishes no MX records, so it does not appear to receive email.", "If the domain should receive email, publish MX records for the mail provider."},
	CheckTLS + ":" + Failed:        {"high", "potential_issue", "Website encryption needs attention", "The website's certificate is close to expiry, does not match the domain, or the server offers an outdated TLS version.", "Renew or replace the certificate and disable TLS 1.0 and 1.1."},
	CheckHeaders + ":" + Failed:    {"low", "potential_issue", "Browser security headers missing", "The website does not send some headers that tell browsers to apply extra protections.", "Add Strict-Transport-Security, Content-Security-Policy and X-Content-Type-Options headers."},
}

// Weights for scoring. A missing control matters more than a missing best practice.
var weights = map[string]int{CheckSPF: 25, CheckDMARC: 25, CheckDKIM: 10, CheckMX: 5, CheckTLS: 25, CheckHeaders: 10}

// Score rates the conclusive checks only, and reports what share of the weight they cover. Like lead
// scoring, a check that could not run counts neither for nor against.
func Score(results []Result) (score int, coverage float64, risk string, ok bool) {
	total, known, earned := 0, 0, 0
	for _, r := range results {
		w := weights[r.Key]
		total += w
		switch r.Status {
		case Passed:
			known += w
			earned += w
		case Failed, NotDetected:
			known += w
			if r.Key == CheckDKIM && r.Status == NotDetected {
				earned += w / 2 // Not detectable from outside with certainty.
			}
		}
	}
	if known == 0 || total == 0 {
		return 0, 0, "", false
	}
	score = (earned*100 + known/2) / known
	coverage = float64(known) / float64(total)
	switch {
	case score >= 80:
		risk = "low"
	case score >= 60:
		risk = "moderate"
	case score >= 40:
		risk = "elevated"
	default:
		risk = "high"
	}
	return score, coverage, risk, true
}

// ReportNotice is printed on every report.
const ReportNotice = "This assessment read only public DNS records and, where authorized, the public website. It does not test for or confirm vulnerabilities, and a passed check does not mean a system is secure."
