package domain

import (
	"testing"
	"time"
)

func TestScopeIsLimitedByTier(t *testing.T) {
	got := Scope(Passive, []string{CheckSPF, CheckHeaders, "network.port_scan", CheckSPF})
	if len(got) != 1 || got[0] != CheckSPF {
		t.Fatalf("passive scope: %v", got)
	}
	if got := Scope(Exposure, []string{CheckHeaders, "exploit.anything"}); len(got) != 1 {
		t.Fatalf("exposure scope: %v", got)
	}
	if !TierAllows(Exposure, Passive) || TierAllows(Passive, Exposure) || !TierAllows(Passive, Passive) {
		t.Fatal("tier rule broken")
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	all := Allowed[Exposure]
	good := Facts{Domain: "a.example", RootTXT: []string{"v=spf1 include:mail.example -all"}, DMARC: []string{"v=DMARC1; p=reject"}, DKIMFound: []string{"selector1"},
		MX: []string{"mx.a.example"}, TLS: &TLSInfo{Version: "TLS1.3", NotAfter: now.AddDate(0, 3, 0), HostnameValid: true},
		Headers: map[string]string{"strict-transport-security": "max-age=1", "content-security-policy": "default-src 'self'", "x-content-type-options": "nosniff"}}
	for _, r := range Evaluate(all, good, now) {
		if r.Status != Passed {
			t.Errorf("%s: %s", r.Key, r.Status)
		}
	}
	if s, c, risk, ok := Score(Evaluate(all, good, now)); !ok || s != 100 || c != 1 || risk != "low" {
		t.Fatalf("good score %d %v %s", s, c, risk)
	}
	bad := Facts{Domain: "b.example", RootTXT: []string{"v=spf1 +all"}, DMARC: []string{"v=DMARC1; p=none"}, TLS: &TLSInfo{Version: "TLS1.0", NotAfter: now.AddDate(0, 3, 0), HostnameValid: true}, Headers: map[string]string{}}
	res := Evaluate(all, bad, now)
	want := map[string]string{CheckSPF: Failed, CheckDMARC: Failed, CheckDKIM: NotDetected, CheckMX: NotDetected, CheckTLS: Failed, CheckHeaders: Failed}
	for _, r := range res {
		if r.Status != want[r.Key] {
			t.Errorf("%s: %s, want %s", r.Key, r.Status, want[r.Key])
		}
		if _, ok := FindingFor(r); !ok {
			t.Errorf("%s:%s has no finding", r.Key, r.Status)
		}
	}
	if s, _, risk, _ := Score(res); s >= 40 || risk != "high" {
		t.Fatalf("bad score %d %s", s, risk)
	}
}

// A lookup that failed is inconclusive: it neither passes nor fails, and lowers coverage.
func TestFailedLookupsAreInconclusive(t *testing.T) {
	f := Facts{Domain: "c.example", RootTXT: []string{"v=spf1 -all"}, DMARCErr: true, DKIMErr: true, MXErr: true, TLSErr: true}
	res := Evaluate(Allowed[Passive], f, time.Now())
	for _, r := range res {
		if r.Key != CheckSPF && r.Status != Inconclusive {
			t.Errorf("%s: %s", r.Key, r.Status)
		}
	}
	s, cov, _, ok := Score(res)
	if !ok || s != 100 || cov >= 0.5 {
		t.Fatalf("score %d coverage %v", s, cov)
	}
	if _, _, _, ok := Score(Evaluate(Allowed[Passive], Facts{RootTXTErr: true, DMARCErr: true, DKIMErr: true, MXErr: true, TLSErr: true}, time.Now())); ok {
		t.Fatal("a run with nothing conclusive must have no score")
	}
}

func TestDKIMWordingIsHonest(t *testing.T) {
	f, _ := FindingFor(Result{Key: CheckDKIM, Status: NotDetected})
	if f.Classification != "informational" || f.Severity != "info" {
		t.Fatalf("DKIM absence must not be presented as a problem: %+v", f)
	}
}
