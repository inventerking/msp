package domain

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func at(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

func month(s string) time.Time { return MonthStart(at(s + "-01")) }

// gbpOnly converts GBP at 1 and USD at 0.8, and has no rate for anything else.
func gbpOnly(amount int64, currency string, _ time.Time) (int64, bool) {
	switch currency {
	case "GBP":
		return amount, true
	case "USD":
		return int64(math.Round(float64(amount) * 0.8)), true
	}
	return 0, false
}

type index map[string]Snapshot

func byKey(ss []Snapshot) index {
	out := index{}
	for _, s := range ss {
		out[s.Key+"|"+s.Period.Format("2006-01")+"|"+s.Dimension+"|"+s.DimValue] = s
	}
	return out
}

func (ix index) val(t *testing.T, key, period, dim, val string) *float64 {
	t.Helper()
	s, ok := ix[key+"|"+period+"|"+dim+"|"+val]
	if !ok {
		return nil
	}
	return s.Value
}

func (ix index) want(t *testing.T, key, period, dim, val string, want float64) {
	t.Helper()
	got := ix.val(t, key, period, dim, val)
	if got == nil {
		t.Errorf("%s %s %s=%s: no value, want %v", key, period, dim, val, want)
		return
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s %s %s=%s: got %v, want %v", key, period, dim, val, *got, want)
	}
}

func TestRevenueMovementsAndMRR(t *testing.T) {
	c1, c2 := uuid.New(), uuid.New()
	in := Input{Convert: gbpOnly, From: month("2026-01"), To: month("2026-05"), Now: at("2026-05-20"), Revenue: []RevenueFact{
		{Movement: "new", Contract: c1, Delta: 100000, ContractMRR: 100000, Currency: "GBP", At: at("2026-01-10")},
		{Movement: "expansion", Contract: c2, Delta: 50000, ContractMRR: 50000, Currency: "GBP", At: at("2026-02-03")},
		{Movement: "contraction", Contract: c2, Delta: -20000, ContractMRR: 30000, Currency: "GBP", At: at("2026-03-15")},
		{Movement: "contraction", Contract: c2, Delta: -30000, ContractMRR: 0, Currency: "GBP", At: at("2026-04-01")},
		{Movement: "churn", Contract: c1, Delta: -100000, ContractMRR: 0, Currency: "GBP", At: at("2026-04-20")},
		{Movement: "reactivation", Contract: c1, Delta: 80000, ContractMRR: 80000, Currency: "GBP", At: at("2026-05-02")},
	}}
	ix := byKey(Compute(in))
	for m, want := range map[string]float64{"2026-01": 100000, "2026-02": 150000, "2026-03": 130000, "2026-04": 0, "2026-05": 80000} {
		ix.want(t, "mrr", m, All, "", want)
		ix.want(t, "arr", m, All, "", want*12)
	}
	ix.want(t, "new_mrr", "2026-01", All, "", 100000)
	ix.want(t, "new_customers", "2026-01", All, "", 1)
	ix.want(t, "expansion_mrr", "2026-02", All, "", 50000)
	ix.want(t, "contraction_mrr", "2026-03", All, "", 20000)
	ix.want(t, "contraction_mrr", "2026-04", All, "", 30000)
	ix.want(t, "churned_mrr", "2026-04", All, "", 100000)
	ix.want(t, "churned_customers", "2026-04", All, "", 1)
	ix.want(t, "net_new_mrr", "2026-04", All, "", -130000)
	ix.want(t, "reactivation_mrr", "2026-05", All, "", 80000)
	ix.want(t, "net_new_mrr", "2026-05", All, "", 80000)
	// Net new MRR over the period equals the change in MRR: the movements reconcile.
	var net float64
	for _, m := range []string{"2026-01", "2026-02", "2026-03", "2026-04", "2026-05"} {
		net += *ix.val(t, "net_new_mrr", m, All, "")
	}
	if net != 80000 {
		t.Errorf("net new MRR %v does not reconcile with MRR 80000", net)
	}
}

func TestMissingExchangeRateIsExcludedAndFlagged(t *testing.T) {
	in := Input{Convert: gbpOnly, From: month("2026-01"), To: month("2026-01"), Now: at("2026-01-20"), Revenue: []RevenueFact{
		{Movement: "new", Contract: uuid.New(), Delta: 1000, Currency: "GBP", At: at("2026-01-02")},
		{Movement: "new", Contract: uuid.New(), Delta: 1000, Currency: "USD", At: at("2026-01-03")},
		{Movement: "new", Contract: uuid.New(), Delta: 5000, Currency: "JPY", At: at("2026-01-04")},
	}}
	ix := byKey(Compute(in))
	ix.want(t, "mrr", "2026-01", All, "", 1800) // 1000 + 800, and the JPY contract left out.
	s := ix["mrr|2026-01|all|"]
	if len(s.Flags) != 1 || s.Flags[0] != FlagFXMissing {
		t.Errorf("flags: %v", s.Flags)
	}
	if f := ix["arr|2026-01|all|"].Flags; len(f) != 1 {
		t.Errorf("ARR must carry the flag too: %v", f)
	}
}

func deal(id uuid.UUID, typ, status, stage string, when time.Time, owner *uuid.UUID, mrr int64) DealFact {
	return DealFact{EventType: typ, Deal: &id, Status: &status, Stage: &stage, Owner: owner, MRR: &mrr, Currency: ptr("GBP"), At: when, Probability: ptr(int32(50))}
}

func TestWinRateUsesCurrentStateWhateverTheOrder(t *testing.T) {
	won, lost, reopened, openD := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	rep := uuid.New()
	facts := []DealFact{
		// Delivered out of order: the win arrives before the creation, but occurred later.
		deal(won, "crm.deal.won", "won", "won", at("2026-03-10"), &rep, 20000),
		deal(won, "crm.deal.created", "open", "qualify", at("2026-02-01"), &rep, 20000),
		deal(lost, "crm.deal.created", "open", "qualify", at("2026-02-02"), nil, 0),
		deal(lost, "crm.deal.lost", "lost", "lost", at("2026-03-12"), nil, 0),
		// Won and then reopened: no longer counts as won.
		deal(reopened, "crm.deal.won", "won", "won", at("2026-03-05"), &rep, 9000),
		deal(reopened, "crm.deal.reopened", "open", "proposal", at("2026-03-20"), &rep, 9000),
		deal(openD, "crm.deal.created", "open", "qualify", at("2026-03-01"), &rep, 1000),
	}
	ix := byKey(Compute(Input{Convert: gbpOnly, Deals: facts, From: month("2026-02"), To: month("2026-03"), Now: at("2026-03-25")}))
	ix.want(t, "deals_won", "2026-03", All, "", 1)
	ix.want(t, "deals_lost", "2026-03", All, "", 1)
	ix.want(t, "win_rate", "2026-03", All, "", 0.5)
	ix.want(t, "won_deal_mrr", "2026-03", All, "", 20000)
	ix.want(t, "win_rate", "2026-03", ByOwner, rep.String(), 1)
	ix.want(t, "win_rate", "2026-03", ByOwner, "unassigned", 0)
	ix.want(t, "deals_created", "2026-02", All, "", 2)
	ix.want(t, "deals_reached_stage", "2026-02", ByStage, "qualify", 2)
	ix.want(t, "deals_reached_stage", "2026-02", ByStage, "won", 1)
	// Open now: the reopened deal and the open one, weighted at 50% of 12 months of MRR.
	ix.want(t, "open_deals", "2026-03", All, "", 2)
	ix.want(t, "weighted_pipeline", "2026-03", All, "", (9000*12+1000*12)*0.5)
	if ix["weighted_pipeline|2026-03|all|"].Kind != Estimated || ix["win_rate|2026-03|all|"].Kind != Actual {
		t.Error("value kinds must follow the definitions")
	}
	// A month with no closed deals has no win rate, not zero.
	if v := ix.val(t, "win_rate", "2026-02", All, ""); v != nil {
		t.Errorf("win rate with nothing closed: %v", *v)
	}
}

func TestZeroDenominatorsGiveNoValue(t *testing.T) {
	ix := byKey(Compute(Input{Convert: gbpOnly, From: month("2026-01"), To: month("2026-01"), Now: at("2026-01-20"),
		Costs: []Cost{{Start: at("2026-01-01"), End: at("2026-01-31"), Amount: 31000, Currency: "GBP"}}}))
	s, ok := ix["cac|2026-01|all|"]
	if !ok || s.Value != nil || len(s.Flags) != 1 || s.Flags[0] != FlagNoData {
		t.Fatalf("CAC with no new customers: %+v", s)
	}
}

func TestMeetingAndProposalConversion(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	p1, p2 := uuid.New(), uuid.New()
	facts := []DealFact{
		{EventType: "crm.meeting.scheduled", Deal: &a, At: at("2026-02-03")},
		{EventType: "crm.meeting.scheduled", Deal: &b, At: at("2026-02-04")},
		{EventType: "crm.meeting.completed", Deal: &a, At: at("2026-01-20")}, // Earlier meeting: the cohort is January.
		deal(a, "crm.deal.won", "won", "won", at("2026-02-20"), nil, 100),
		{EventType: "proposals.version.issued", Proposal: &p1, At: at("2026-02-01")},
		{EventType: "proposals.version.issued", Proposal: &p1, At: at("2026-02-05")},
		{EventType: "proposals.proposal.signed", Proposal: &p1, At: at("2026-02-10")},
		{EventType: "proposals.version.issued", Proposal: &p2, At: at("2026-02-08")},
		{EventType: "proposals.proposal.declined", Proposal: &p2, At: at("2026-02-12")},
	}
	ix := byKey(Compute(Input{Convert: gbpOnly, Deals: facts, From: month("2026-01"), To: month("2026-02"), Now: at("2026-02-25")}))
	ix.want(t, "meeting_conversion", "2026-01", All, "", 1)
	ix.want(t, "meeting_conversion", "2026-02", All, "", 0)
	ix.want(t, "proposals_issued", "2026-02", All, "", 2)
	ix.want(t, "proposal_conversion", "2026-02", All, "", 0.5)
}

func TestLeadsBySourceAndQualification(t *testing.T) {
	p1, p2, p3 := uuid.New(), uuid.New(), uuid.New()
	facts := []LeadFact{
		{EventType: "lead_generation.prospect.created", Prospect: &p1, Source: "search", At: at("2026-03-01")},
		{EventType: "lead_generation.prospect.created", Prospect: &p2, Source: "search", At: at("2026-03-02")},
		{EventType: "lead_generation.prospect.created", Prospect: &p3, Source: "csv_import", At: at("2026-03-03")},
		{EventType: "lead_generation.prospect.qualified", Prospect: &p1, At: at("2026-04-01")},
		{EventType: "chatbot.lead.captured", At: at("2026-03-05")},
		{EventType: "visitor_intelligence.conversion.recorded", At: at("2026-03-06")},
	}
	ix := byKey(Compute(Input{Convert: gbpOnly, Leads: facts, From: month("2026-03"), To: month("2026-04"), Now: at("2026-04-10")}))
	ix.want(t, "leads_created", "2026-03", All, "", 5)
	ix.want(t, "leads_created", "2026-03", BySource, "search", 2)
	ix.want(t, "leads_created", "2026-03", BySource, "chatbot", 1)
	ix.want(t, "leads_created", "2026-03", BySource, "website", 1)
	ix.want(t, "leads_qualified", "2026-04", BySource, "search", 1)
	// Of three prospects added in March, one was later qualified.
	ix.want(t, "qualification_rate", "2026-03", All, "", 1.0/3)
	ix.want(t, "qualification_rate", "2026-03", BySource, "search", 0.5)
	ix.want(t, "qualification_rate", "2026-03", BySource, "csv_import", 0)
}

func TestCampaignRatesROIAndCostSpread(t *testing.T) {
	camp, other := uuid.New(), uuid.New()
	company := uuid.New()
	winner := uuid.New()
	out := []OutreachFact{
		{EventType: "outreach.message.sent", Campaign: camp, At: at("2026-03-01")},
		{EventType: "outreach.message.sent", Campaign: camp, At: at("2026-03-01")},
		{EventType: "outreach.message.sent", Campaign: camp, At: at("2026-03-02")},
		{EventType: "outreach.message.sent", Campaign: camp, At: at("2026-03-02")},
		{EventType: "outreach.message.bounced", Campaign: camp, At: at("2026-03-02")},
		{EventType: "outreach.reply.received", Campaign: camp, Company: &company, At: at("2026-03-05")},
		{EventType: "outreach.message.sent", Campaign: other, At: at("2026-03-03")},
	}
	won := deal(winner, "crm.deal.won", "won", "won", at("2026-04-10"), nil, 10000)
	won.Company = &company
	costs := []Cost{
		// 30 days over March and April: 17 days in March, 13 in April.
		{Start: at("2026-03-15"), End: at("2026-04-13"), Campaign: &camp, Amount: 30000, Currency: "GBP"},
		{Start: at("2026-03-01"), End: at("2026-03-31"), Amount: 10000, Currency: "GBP"},
	}
	ix := byKey(Compute(Input{Convert: gbpOnly, Outreach: out, Deals: []DealFact{won}, Costs: costs, From: month("2026-03"), To: month("2026-04"), Now: at("2026-04-20")}))
	ix.want(t, "messages_sent", "2026-03", All, "", 5)
	ix.want(t, "reply_rate", "2026-03", ByCamp, camp.String(), 0.25)
	ix.want(t, "bounce_rate", "2026-03", ByCamp, camp.String(), 0.25)
	ix.want(t, "reply_rate", "2026-03", ByCamp, other.String(), 0)
	ix.want(t, "costs", "2026-03", All, "", 17000+10000)
	ix.want(t, "costs", "2026-04", All, "", 13000)
	// The reply came 36 days before the win, so 12 months of its MRR count: (120000 − 30000) ÷ 30000.
	ix.want(t, "campaign_roi", "2026-04", ByCamp, camp.String(), 3)
	if _, ok := ix["campaign_roi|2026-04|campaign|"+other.String()]; ok {
		t.Error("a campaign with no costs has no ROI")
	}
}

func TestEveryDefinitionHasOneKind(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Definitions {
		if seen[d.Key] {
			t.Errorf("%s is defined twice", d.Key)
		}
		seen[d.Key] = true
		if d.Kind != Actual && d.Kind != Estimated {
			t.Errorf("%s: snapshots are actual or estimated; forecasts are stored apart", d.Key)
		}
		if d.Description == "" || len(d.Dimensions) == 0 {
			t.Errorf("%s needs a description and dimensions", d.Key)
		}
	}
	for _, key := range []string{"cac", "campaign_roi", "weighted_pipeline"} {
		if d, _ := Lookup(key); d.Kind != Estimated {
			t.Errorf("%s must be estimated", key)
		}
	}
	for _, key := range []string{"mrr", "arr", "net_new_mrr", "win_rate", "meeting_conversion", "proposal_conversion"} {
		if d, _ := Lookup(key); d.Kind != Actual {
			t.Errorf("%s must be actual", key)
		}
	}
}

func TestRevenueForecastRefusesShortHistory(t *testing.T) {
	r := RevenueForecast(ForecastInput{Convert: gbpOnly, Now: at("2026-03-15"), Horizon: 6, Revenue: []RevenueFact{
		{Movement: "new", Contract: uuid.New(), Delta: 1000, ContractMRR: 1000, Currency: "GBP", At: at("2026-01-10")},
	}})
	if r.Refusal != "insufficient_history" || len(r.Points) != 0 {
		t.Fatalf("two complete months must be refused: %+v", r)
	}
}

func TestRevenueForecastRollsForward(t *testing.T) {
	c1, c2 := uuid.New(), uuid.New()
	end := at("2026-06-30")
	no := false
	open := uuid.New()
	closeOn := at("2026-07-15")
	mrr := int64(4000)
	r := RevenueForecast(ForecastInput{Convert: gbpOnly, Now: at("2026-05-10"), Horizon: 3,
		Revenue: []RevenueFact{
			{Movement: "new", Contract: c1, Delta: 10000, ContractMRR: 10000, Currency: "GBP", At: at("2026-01-05")},
			{Movement: "expansion", Contract: c2, Delta: 2000, ContractMRR: 2000, Currency: "GBP", End: &end, AutoRenew: &no, At: at("2026-02-05")},
		},
		Deals: []DealFact{{EventType: "crm.deal.created", Deal: &open, Status: ptr("open"), Stage: ptr("proposal"), MRR: &mrr, Currency: ptr("GBP"), Probability: ptr(int32(50)), ExpectedClose: &closeOn, At: at("2026-04-01")}},
	})
	if r.Refusal != "" || len(r.Points) != 3 {
		t.Fatalf("got %+v", r)
	}
	// January starts from nothing, so three months set the rates: 2000 on a 10000 base in February,
	// and nothing in March or April. That is 0.2 ÷ 3 a month, with no churn.
	rate := 0.2 / 3
	if got := r.Assumptions["monthly_expansion_rate"].(float64); math.Abs(got-rate) > 1e-9 {
		t.Errorf("expansion rate %v", got)
	}
	may := 12000 * (1 + rate)
	june := may*(1+rate) - 2000  // The 2000 contract ends in June without auto-renewal.
	july := june*(1+rate) + 2000 // Half of a 4000 MRR deal expected to close in July.
	for i, want := range []float64{may, june, july} {
		p := r.Points[i]
		if math.Abs(float64(p.Expected)-math.Round(want)) > 1 {
			t.Errorf("month %d: expected %d, want %.0f", i, p.Expected, want)
		}
		if p.Low > p.Expected || p.High < p.Expected {
			t.Errorf("month %d: band %d..%d does not contain %d", i, p.Low, p.High, p.Expected)
		}
	}
	if r.Points[2].High-r.Points[2].Low < r.Points[0].High-r.Points[0].Low {
		t.Error("the band must widen with time")
	}
}

func TestPipelineForecastUsesStageRatesWhenTheSampleIsLarge(t *testing.T) {
	var facts []DealFact
	for i := 0; i < MinStageSample; i++ {
		id := uuid.New()
		status := "lost"
		if i < 3 {
			status = "won"
		}
		facts = append(facts, deal(id, "crm.deal.created", "open", "demo", at("2026-01-01"), nil, 0), deal(id, "crm.deal."+status, status, status, at("2026-02-01"), nil, 0))
	}
	open := uuid.New()
	closeOn := at("2026-06-10")
	amount := int64(100000)
	facts = append(facts, DealFact{EventType: "crm.deal.created", Deal: &open, Status: ptr("open"), Stage: ptr("demo"), Amount: &amount, Currency: ptr("GBP"),
		Probability: ptr(int32(90)), ExpectedClose: &closeOn, At: at("2026-05-01")})
	r := PipelineForecast(ForecastInput{Convert: gbpOnly, Deals: facts, Now: at("2026-05-05"), Horizon: 2})
	if len(r.Points) != 2 || r.Points[0].Expected != 0 {
		t.Fatalf("got %+v", r.Points)
	}
	// Three of ten closed demo-stage deals were won, so 30%, not the deal's own 90%.
	if r.Points[1].Expected != 30000 {
		t.Errorf("June expected %d, want 30000", r.Points[1].Expected)
	}
	if r.Points[1].Low != 0 || r.Points[1].High <= 30000 {
		t.Errorf("band %d..%d", r.Points[1].Low, r.Points[1].High)
	}
}

func TestCommentaryValid(t *testing.T) {
	given := map[string]string{"m1": "mrr 2026-04: GBP 1,250.00 (actual)", "m2": "win_rate 2026-04: 40% (actual)"}
	if !CommentaryValid("MRR reached GBP 1,250.00 in 2026-04, with a 40% win rate.", []string{"m1", "m2"}, given) {
		t.Error("numbers from cited metrics must pass")
	}
	if CommentaryValid("MRR should reach GBP 2,000.00 next month.", []string{"m1"}, given) {
		t.Error("an invented number must fail")
	}
	if CommentaryValid("Win rate was 40%.", []string{"m1"}, given) {
		t.Error("a number from a metric that is not cited must fail")
	}
	if CommentaryValid("Fine.", []string{"m9"}, given) {
		t.Error("citing an unknown metric must fail")
	}
}

func TestDisplay(t *testing.T) {
	mrr, _ := Lookup("mrr")
	rate, _ := Lookup("win_rate")
	count, _ := Lookup("deals_won")
	for _, c := range []struct {
		def  Def
		v    *float64
		want string
	}{
		{mrr, ptr(125000.0), "GBP 1,250.00"}, {mrr, ptr(-5050.0), "GBP -50.50"}, {mrr, nil, "not available"},
		{rate, ptr(0.4), "40%"}, {rate, ptr(1.0 / 3), "33.3%"}, {count, ptr(7.0), "7"},
	} {
		if got := Display(c.def, c.v, "GBP"); got != c.want {
			t.Errorf("Display(%s, %v) = %q, want %q", c.def.Key, c.v, got, c.want)
		}
	}
}
