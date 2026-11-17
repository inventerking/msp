package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr[T any](v T) *T { return &v }

func TestMovements(t *testing.T) {
	cases := []struct {
		name string
		got  Movement
		want Movement
	}{
		{"first contract is new", Activation(0, false, 1000), New},
		{"another contract is expansion", Activation(500, true, 1000), Expansion},
		{"returning customer is reactivation", Activation(0, true, 1000), Reactivation},
		{"zero MRR moves nothing", Activation(0, false, 0), None},
		{"increase is expansion", Change(1000, 1500), Expansion},
		{"decrease is contraction", Change(1500, 1000), Contraction},
		{"no change", Change(1000, 1000), None},
		{"last contract ending is churn", End(0, 1000), Churn},
		{"one of several ending is contraction", End(500, 1000), Contraction},
		{"ending a zero contract moves nothing", End(0, 0), None},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, c.got, c.want)
		}
	}
}

func TestEffectiveEndFromTerm(t *testing.T) {
	c := Contract{Start: day("2026-01-01"), TermMonths: ptr(int32(12))}
	if e := c.EffectiveEnd(); e == nil || e.Format(time.DateOnly) != "2026-12-31" {
		t.Fatalf("got %v", e)
	}
	c.End = ptr(day("2026-06-30"))
	if e := c.EffectiveEnd(); e.Format(time.DateOnly) != "2026-06-30" {
		t.Fatal("an explicit end date wins over the term")
	}
	if (Contract{Start: day("2026-01-01")}).EffectiveEnd() != nil {
		t.Fatal("no end and no term means open-ended")
	}
}

func TestDueAlertsPicksTightestWindowAndNoticeDeadline(t *testing.T) {
	c := Contract{Status: "active", Start: day("2025-01-01"), End: ptr(day("2026-12-31")), NoticeDays: ptr(int32(30))}
	// 100 days out: nothing yet for the end; the notice deadline (Dec 1) is 70 days out, in the 90 window.
	got := DueAlerts(c, day("2026-09-22"))
	if len(got) != 1 || got[0].Kind != "notice_deadline" || got[0].WindowDays != 90 || got[0].DueDate.Format(time.DateOnly) != "2026-12-01" {
		t.Fatalf("100 days out: %+v", got)
	}
	// 20 days out: the 30-day window only, not all three. The notice deadline has passed.
	got = DueAlerts(c, day("2026-12-11"))
	if len(got) != 1 || got[0].Kind != "contract_end" || got[0].WindowDays != 30 {
		t.Fatalf("20 days out: %+v", got)
	}
	// Ended contracts and past dates raise nothing.
	if len(DueAlerts(c, day("2027-01-05"))) != 0 {
		t.Fatal("past end date must raise nothing")
	}
	c.Status = "ended"
	if len(DueAlerts(c, day("2026-12-11"))) != 0 {
		t.Fatal("an ended contract must raise nothing")
	}
}

func TestValidateContract(t *testing.T) {
	start := day("2026-01-01")
	if f, _ := ValidateContract(start, ptr(day("2025-12-31")), nil, nil, "GBP", 100, 0); f != "end_date" {
		t.Errorf("end before start: %s", f)
	}
	if f, _ := ValidateContract(start, nil, nil, nil, "gbp", 100, 0); f != "currency" {
		t.Errorf("lower-case currency: %s", f)
	}
	if f, _ := ValidateContract(start, nil, nil, nil, "GBP", -1, 0); f != "mrr_minor" {
		t.Errorf("negative MRR: %s", f)
	}
	if f, _ := ValidateContract(start, nil, ptr(int32(0)), nil, "GBP", 1, 0); f != "term_months" {
		t.Errorf("zero term: %s", f)
	}
	if f, _ := ValidateContract(start, nil, nil, ptr(int32(-1)), "GBP", 1, 0); f != "notice_days" {
		t.Errorf("negative notice: %s", f)
	}
	if f, _ := ValidateContract(start, ptr(start), ptr(int32(12)), ptr(int32(30)), "GBP", 0, 0); f != "" {
		t.Errorf("valid contract rejected: %s", f)
	}
}

func TestAnalyzeNeverTreatsUnknownAsAbsent(t *testing.T) {
	backup, mail, voice, retired := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	in := Input{
		Customer: "Acme", Today: day("2026-11-01"),
		Services: []Service{
			{ID: backup, Name: "Backup", Status: "active", Category: "backup_dr"},
			{ID: mail, Name: "Email security", Status: "active", Category: "email_security"},
			{ID: voice, Name: "Voice", Status: "active", Category: "voip"},
			{ID: retired, Name: "Old thing", Status: "retired"},
		},
		Coverage:   map[uuid.UUID]Covered{backup: {Coverage: Absent}, voice: {Coverage: Declined}},
		PeerPrices: map[uuid.UUID]PeerPrice{backup: {Customers: 3, Median: 12000, Currency: "GBP"}},
	}
	recs, gaps := Analyze(in)
	if len(recs) != 2 || len(gaps) != 2 {
		t.Fatalf("want 2 recommendations and 2 gaps, got %+v %+v", recs, gaps)
	}
	var absent, unknown *Recommendation
	for i := range recs {
		switch recs[i].Kind {
		case "cross_sell":
			absent = &recs[i]
		case "confirm_coverage":
			unknown = &recs[i]
		}
	}
	if absent == nil || *absent.ServiceID != backup || absent.EstimatedMRR == nil || *absent.EstimatedMRR != 12000 || absent.EvidenceClass != "observed" {
		t.Fatalf("absent service: %+v", absent)
	}
	if !strings.Contains(absent.Rationale, "does not have Backup") || !strings.Contains(absent.Rationale, "estimate") {
		t.Errorf("absent rationale: %q", absent.Rationale)
	}
	if unknown == nil || *unknown.ServiceID != mail || unknown.EstimatedMRR != nil || unknown.EvidenceClass != "inferred" {
		t.Fatalf("unknown service: %+v", unknown)
	}
	if !strings.HasPrefix(unknown.Rationale, "Confirm whether Acme has Email security") || strings.Contains(unknown.Rationale, "does not have") {
		t.Errorf("unknown coverage must be worded as a question: %q", unknown.Rationale)
	}
	for _, r := range recs {
		if r.Rationale == "" || len(r.Evidence) == 0 {
			t.Errorf("every recommendation needs rationale and evidence: %+v", r)
		}
	}
}

func TestAnalyzeAddsAssessmentEvidenceToSecurityGapsOnly(t *testing.T) {
	sec, voice, a := uuid.New(), uuid.New(), uuid.New()
	when := day("2026-10-01")
	recs, _ := Analyze(Input{Customer: "Acme", Today: day("2026-11-01"),
		Services:       []Service{{ID: sec, Name: "Security", Status: "active", Category: "cybersecurity"}, {ID: voice, Name: "Voice", Status: "active", Category: "voip"}},
		Coverage:       map[uuid.UUID]Covered{sec: {Coverage: Absent}, voice: {Coverage: Absent}},
		AssessmentRisk: ptr("high"), AssessmentID: &a, AssessmentAt: &when})
	for _, r := range recs {
		has := false
		for _, e := range r.Evidence {
			has = has || e.Kind == "assessment_risk"
		}
		if (*r.ServiceID == sec) != has {
			t.Errorf("service %v: assessment evidence %v", *r.ServiceID, has)
		}
	}
}

func TestAnalyzeRenewals(t *testing.T) {
	soon, later := uuid.New(), uuid.New()
	recs, _ := Analyze(Input{Customer: "Acme", Today: day("2026-11-01"), Contracts: []Contract{
		{ID: soon, Status: "active", Start: day("2025-12-01"), End: ptr(day("2026-11-30")), NoticeDays: ptr(int32(14)), Currency: "GBP", MRR: 50000, AutoRenew: true},
		{ID: later, Status: "active", Start: day("2025-12-01"), End: ptr(day("2027-06-30")), Currency: "GBP", MRR: 1000},
	}})
	if len(recs) != 1 || recs[0].Kind != "renewal" || *recs[0].ContractID != soon || len(recs[0].Evidence) != 2 {
		t.Fatalf("got %+v", recs)
	}
	if !strings.Contains(recs[0].Rationale, "GBP 500.00") || !strings.Contains(recs[0].Rationale, "in 29 days") || !strings.Contains(recs[0].Rationale, "2026-11-16") {
		t.Errorf("rationale: %q", recs[0].Rationale)
	}
}

func TestHealth(t *testing.T) {
	s := uuid.New()
	_, status := Health(Input{Today: day("2026-11-01")})
	if status != "unknown" {
		t.Errorf("no facts: %s", status)
	}
	healthy := Input{Today: day("2026-11-01"), Services: []Service{{ID: s, Status: "active"}}, Coverage: map[uuid.UUID]Covered{s: {Coverage: Active}},
		Contracts: []Contract{{Status: "active", MRR: 1000, End: ptr(day("2027-11-01"))}}}
	if _, st := Health(healthy); st != "healthy" {
		t.Errorf("healthy: %s", st)
	}
	watch := healthy
	watch.Contracts = []Contract{{Status: "active", MRR: 1000, End: ptr(day("2026-12-01"))}}
	if _, st := Health(watch); st != "watch" {
		t.Errorf("ending in 30 days: %s", st)
	}
	risk := watch
	risk.AssessmentRisk = ptr("critical")
	if _, st := Health(risk); st != "at_risk" {
		t.Errorf("ending soon and critical risk: %s", st)
	}
	auto := healthy
	auto.Contracts = []Contract{{Status: "active", MRR: 1000, End: ptr(day("2026-12-01")), AutoRenew: true}}
	if _, st := Health(auto); st != "healthy" {
		t.Errorf("auto-renewing contract is not a renewal risk: %s", st)
	}
}

func TestMoney(t *testing.T) {
	for in, want := range map[int64]string{0: "GBP 0.00", 5: "GBP 0.05", 123456789: "GBP 1,234,567.89", -1050: "GBP -10.50", 100000: "GBP 1,000.00"} {
		if got := Money(in, "GBP"); got != want {
			t.Errorf("Money(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestKeepsFacts(t *testing.T) {
	rule := "A contract worth GBP 500.00 a month ends on 2026-11-30, in 29 days."
	if !KeepsFacts("renewal", rule, "The GBP 500.00 monthly contract ends 2026-11-30, 29 days from now.") {
		t.Error("same facts reworded must pass")
	}
	if KeepsFacts("renewal", rule, "The GBP 600.00 monthly contract ends 2026-11-30, in 29 days.") {
		t.Error("a changed number must fail")
	}
	if KeepsFacts("renewal", rule, "A contract ends soon.") {
		t.Error("dropped facts must fail")
	}
	if KeepsFacts("renewal", rule, "") {
		t.Error("empty wording must fail")
	}
	if KeepsFacts("confirm_coverage", "Confirm whether Acme has Backup.", "Acme lacks Backup.") {
		t.Error("unknown coverage must not become a claim of absence")
	}
	if !KeepsFacts("confirm_coverage", "Confirm whether Acme has Backup.", "Check with Acme whether they use Backup.") {
		t.Error("a question must pass")
	}
}
