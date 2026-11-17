// Package domain holds Sales Intelligence's metric definitions and forecast methods. Every number
// the module shows is computed here, from facts, with no I/O, so each definition is tested directly.
// The definitions are what the Phase 6 gate reviews: change them here, with their tests.
package domain

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kind says what a number is. It is part of every metric's key, and nothing sums across kinds.
type Kind string

// Value kinds.
const (
	Actual    Kind = "actual"
	Estimated Kind = "estimated"
	Forecast  Kind = "forecast"
)

// Def defines one metric.
type Def struct {
	Key         string
	Name        string
	Section     string // revenue, deals, funnel, leads, campaigns, costs, team
	Kind        Kind
	Unit        string // money (reporting currency, minor units), count, ratio (0 to 1)
	Dimensions  []string
	Description string
}

// Dimensions.
const (
	All      = "all"
	BySource = "source"
	ByOwner  = "owner"
	ByStage  = "stage"
	ByCamp   = "campaign"
)

// AttributionDays is how long before a win a campaign reply counts towards it.
const AttributionDays = 90

// ROIHorizonMonths is how many months of won MRR count as a campaign's return.
const ROIHorizonMonths = 12

// Definitions are every metric, defined once. Months are calendar months in UTC.
var Definitions = []Def{
	{"mrr", "MRR", "revenue", Actual, "money", []string{All}, "Sum of active contracts' monthly recurring revenue at the end of the month, from contract events. Each change is converted at its own date's exchange rate."},
	{"arr", "ARR", "revenue", Actual, "money", []string{All}, "MRR × 12."},
	{"new_mrr", "New MRR", "revenue", Actual, "money", []string{All}, "MRR from customers' first contracts that started in the month."},
	{"expansion_mrr", "Expansion MRR", "revenue", Actual, "money", []string{All}, "MRR added in the month by existing customers: increases and further contracts."},
	{"reactivation_mrr", "Reactivation MRR", "revenue", Actual, "money", []string{All}, "MRR from former customers who started a contract again in the month."},
	{"contraction_mrr", "Contraction MRR", "revenue", Actual, "money", []string{All}, "MRR lost in the month by customers who stay: decreases, and contracts ending while others continue. Shown as a positive amount."},
	{"churned_mrr", "Churned MRR", "revenue", Actual, "money", []string{All}, "MRR lost in the month when a customer's last active contract ended. Shown as a positive amount."},
	{"net_new_mrr", "Net new MRR", "revenue", Actual, "money", []string{All}, "New + expansion + reactivation − contraction − churn, for the month."},
	{"new_customers", "New customers", "revenue", Actual, "count", []string{All}, "Customers whose first contract started in the month."},
	{"churned_customers", "Churned customers", "revenue", Actual, "count", []string{All}, "Customers whose last active contract ended in the month."},
	{"deals_won", "Deals won", "deals", Actual, "count", []string{All, ByOwner, BySource}, "Deals whose current status is won, counted in the month they were won."},
	{"deals_lost", "Deals lost", "deals", Actual, "count", []string{All, ByOwner, BySource}, "Deals whose current status is lost, counted in the month they were lost."},
	{"win_rate", "Win rate", "deals", Actual, "ratio", []string{All, ByOwner, BySource}, "Won ÷ (won + lost), for deals closed in the month. Empty when no deal closed."},
	{"won_deal_mrr", "Won deal MRR", "deals", Actual, "money", []string{All, ByOwner, BySource}, "Sum of the expected MRR recorded on deals won in the month, converted at the win date."},
	{"meeting_conversion", "Meeting conversion", "deals", Actual, "ratio", []string{All}, "Of deals whose first meeting was in the month, the share now won."},
	{"proposals_issued", "Proposals issued", "deals", Actual, "count", []string{All}, "Proposals first issued in the month."},
	{"proposal_conversion", "Proposal conversion", "deals", Actual, "ratio", []string{All}, "Of proposals first issued in the month, the share signed."},
	{"deals_created", "Deals created", "funnel", Actual, "count", []string{All}, "Deals created in the month."},
	{"deals_reached_stage", "Deals reaching each stage", "funnel", Actual, "count", []string{ByStage}, "Of deals created in the month, how many reached each stage."},
	{"open_deals", "Open deals", "funnel", Actual, "count", []string{All, ByStage}, "Deals open now. Shown for the current month only."},
	{"weighted_pipeline", "Weighted pipeline", "funnel", Estimated, "money", []string{All, ByStage}, "Sum of contract value × probability on open deals, now. A deal with only an MRR is valued at 12 months of it. Converted at today's rate."},
	{"leads_created", "Leads", "leads", Actual, "count", []string{All, BySource}, "Prospects added, chatbot leads and website conversions in the month."},
	{"leads_qualified", "Leads qualified", "leads", Actual, "count", []string{All, BySource}, "Prospects marked qualified in the month."},
	{"leads_disqualified", "Leads disqualified", "leads", Actual, "count", []string{All, BySource}, "Prospects marked disqualified in the month."},
	{"leads_converted", "Leads converted", "leads", Actual, "count", []string{All, BySource}, "Prospects that became CRM companies in the month."},
	{"qualification_rate", "Qualification rate", "leads", Actual, "ratio", []string{All, BySource}, "Of prospects added in the month, the share since qualified."},
	{"messages_sent", "Emails sent", "campaigns", Actual, "count", []string{All, ByCamp}, "Outreach emails sent in the month."},
	{"messages_bounced", "Bounces", "campaigns", Actual, "count", []string{All, ByCamp}, "Outreach emails that bounced in the month."},
	{"replies", "Replies", "campaigns", Actual, "count", []string{All, ByCamp}, "Replies received in the month."},
	{"unsubscribes", "Unsubscribes", "campaigns", Actual, "count", []string{All, ByCamp}, "People who unsubscribed in the month, counted once each."},
	{"reply_rate", "Reply rate", "campaigns", Actual, "ratio", []string{All, ByCamp}, "Replies ÷ emails sent, in the month."},
	{"bounce_rate", "Bounce rate", "campaigns", Actual, "ratio", []string{All, ByCamp}, "Bounces ÷ emails sent, in the month."},
	{"costs", "Costs", "costs", Actual, "money", []string{All}, "Costs you entered, spread evenly over each entry's days and converted at its start date."},
	{"cac", "Customer acquisition cost", "costs", Estimated, "money", []string{All}, "Costs in the month ÷ new customers in the month. Only for months with costs entered; estimated, because costs are entered by hand."},
	{"campaign_roi", "Campaign ROI", "campaigns", Estimated, "ratio", []string{ByCamp}, "For campaigns with costs: (attributed won MRR × 12 − cost) ÷ cost. A won deal is attributed to a campaign when its company replied to that campaign in the 90 days before the win. Estimated, because attribution is a model."},
}

var defs = func() map[string]Def {
	m := map[string]Def{}
	for _, d := range Definitions {
		m[d.Key] = d
	}
	return m
}()

// Lookup returns a metric's definition.
func Lookup(key string) (Def, bool) {
	d, ok := defs[key]
	return d, ok
}

// LeadFact is a lead event.
type LeadFact struct {
	EventType string
	Prospect  *uuid.UUID
	Source    string
	At        time.Time
}

// OutreachFact is an outreach event.
type OutreachFact struct {
	EventType string
	Campaign  uuid.UUID
	Company   *uuid.UUID
	At        time.Time
}

// DealFact is a deal, meeting or proposal event. Status and the deal fields are the deal's state
// after the event, when the event carries them.
type DealFact struct {
	EventType     string
	Deal          *uuid.UUID
	Proposal      *uuid.UUID
	Owner         *uuid.UUID
	Company       *uuid.UUID
	Stage         *string
	Status        *string
	Source        *string
	Amount        *int64
	MRR           *int64
	Currency      *string
	Probability   *int32
	ExpectedClose *time.Time
	At            time.Time
	Seq           int
}

// RevenueFact is a contract movement.
type RevenueFact struct {
	Movement    string
	Contract    uuid.UUID
	Delta       int64
	ContractMRR int64
	Currency    string
	End         *time.Time
	AutoRenew   *bool
	At          time.Time
}

// Cost is a cost entry.
type Cost struct {
	Start, End time.Time
	Campaign   *uuid.UUID
	Amount     int64
	Currency   string
}

// Converter turns an amount into the reporting currency at a date. ok is false when no rate exists;
// the amount is then left out of totals and flagged, never guessed (D-11).
type Converter func(amount int64, currency string, on time.Time) (converted int64, ok bool)

// Input is one tenant's facts and the months to compute.
type Input struct {
	Leads    []LeadFact
	Outreach []OutreachFact
	Deals    []DealFact
	Revenue  []RevenueFact
	Costs    []Cost
	Convert  Converter
	From, To time.Time // First days of months, inclusive.
	Now      time.Time
}

// Snapshot is one metric value. A nil Value means it cannot be computed, such as a rate with no
// denominator; Flags say why or what was left out.
type Snapshot struct {
	Key       string
	Period    time.Time
	Dimension string
	DimValue  string
	Value     *float64
	Kind      Kind
	Flags     []string
}

// Flags.
const (
	FlagFXMissing = "fx_missing"
	FlagNoData    = "no_denominator"
)

// MonthStart is the first day of t's month, in UTC.
func MonthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Months lists the first days of each month from from to to, inclusive.
func Months(from, to time.Time) []time.Time {
	var out []time.Time
	for m := MonthStart(from); !m.After(MonthStart(to)); m = m.AddDate(0, 1, 0) {
		out = append(out, m)
	}
	return out
}

type acc struct {
	values map[string]float64
	flags  map[string]map[string]bool
}

func newAcc() *acc { return &acc{values: map[string]float64{}, flags: map[string]map[string]bool{}} }

func k(key string, m time.Time, dim, val string) string {
	return key + "|" + m.Format(time.DateOnly) + "|" + dim + "|" + val
}

func (a *acc) add(key string, m time.Time, dim, val string, v float64) {
	a.values[k(key, m, dim, val)] += v
}

func (a *acc) flag(key string, m time.Time, dim, val, f string) {
	id := k(key, m, dim, val)
	if a.flags[id] == nil {
		a.flags[id] = map[string]bool{}
	}
	a.flags[id][f] = true
	if _, ok := a.values[id]; !ok {
		a.values[id] = 0
	}
}

func (a *acc) get(key string, m time.Time, dim, val string) (float64, bool) {
	v, ok := a.values[k(key, m, dim, val)]
	return v, ok
}

// both adds to the total and to one dimension value.
func (a *acc) both(key string, m time.Time, dim, val string, v float64) {
	a.add(key, m, All, "", v)
	if dim != All {
		a.add(key, m, dim, val, v)
	}
}

func str(p *string, empty string) string {
	if p == nil || *p == "" {
		return empty
	}
	return *p
}

func uid(p *uuid.UUID, empty string) string {
	if p == nil {
		return empty
	}
	return p.String()
}

// Compute calculates every metric for the months asked. Months outside the range are still read,
// because MRR at the end of a month includes everything before it.
func Compute(in Input) []Snapshot {
	months := Months(in.From, in.To)
	inRange := map[time.Time]bool{}
	for _, m := range months {
		inRange[m] = true
	}
	a := newAcc()
	computeRevenue(in, months, a)
	deals := dealStates(in.Deals)
	computeDeals(in, deals, inRange, a)
	computeLeads(in, inRange, a)
	computeOutreach(in, inRange, a)
	computeCosts(in, months, a)
	computeROI(in, deals, a)
	// Rates from counts.
	for _, m := range months {
		for _, dim := range []string{All, ByOwner, BySource} {
			for _, val := range dimValues(a, "deals_won", "deals_lost", m, dim) {
				w, _ := a.get("deals_won", m, dim, val)
				l, _ := a.get("deals_lost", m, dim, val)
				ratio(a, "win_rate", m, dim, val, w, w+l)
			}
		}
		for _, dim := range []string{All, ByCamp} {
			for _, val := range dimValues(a, "messages_sent", "", m, dim) {
				sent, _ := a.get("messages_sent", m, dim, val)
				rep, _ := a.get("replies", m, dim, val)
				b, _ := a.get("messages_bounced", m, dim, val)
				ratio(a, "reply_rate", m, dim, val, rep, sent)
				ratio(a, "bounce_rate", m, dim, val, b, sent)
			}
		}
		if mrr, ok := a.get("mrr", m, All, ""); ok {
			a.add("arr", m, All, "", mrr*12)
			if a.flags[k("mrr", m, All, "")][FlagFXMissing] {
				a.flag("arr", m, All, "", FlagFXMissing)
			}
		}
		if cost, ok := a.get("costs", m, All, ""); ok {
			n, _ := a.get("new_customers", m, All, "")
			ratio(a, "cac", m, All, "", cost, n)
			if a.flags[k("costs", m, All, "")][FlagFXMissing] {
				a.flag("cac", m, All, "", FlagFXMissing)
			}
		}
	}
	return snapshots(a)
}

// ratio stores num ÷ den, or an empty value flagged as having no denominator.
func ratio(a *acc, key string, m time.Time, dim, val string, num, den float64) {
	if den == 0 {
		a.flag(key, m, dim, val, FlagNoData)
		a.values[k(key, m, dim, val)] = math.NaN()
		return
	}
	a.values[k(key, m, dim, val)] = num / den
}

// dimValues lists the values of a dimension that have data for either key in a month.
func dimValues(a *acc, key1, key2 string, m time.Time, dim string) []string {
	if dim == All {
		_, ok1 := a.get(key1, m, All, "")
		_, ok2 := a.get(key2, m, All, "")
		if ok1 || ok2 {
			return []string{""}
		}
		return nil
	}
	seen := map[string]bool{}
	prefix := m.Format(time.DateOnly) + "|" + dim + "|"
	for id := range a.values {
		for _, key := range []string{key1, key2} {
			if key == "" {
				continue
			}
			p := key + "|" + prefix
			if len(id) > len(p) && id[:len(p)] == p {
				seen[id[len(p):]] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func computeRevenue(in Input, months []time.Time, a *acc) {
	facts := append([]RevenueFact(nil), in.Revenue...)
	sort.SliceStable(facts, func(i, j int) bool { return facts[i].At.Before(facts[j].At) })
	type conv struct {
		f     RevenueFact
		delta int64
		ok    bool
	}
	cs := make([]conv, len(facts))
	for i, f := range facts {
		d, ok := in.Convert(f.Delta, f.Currency, f.At)
		cs[i] = conv{f, d, ok}
	}
	for _, m := range months {
		next := m.AddDate(0, 1, 0)
		var mrr int64
		missing := false
		for _, c := range cs {
			if !c.f.At.Before(next) {
				break
			}
			if !c.ok {
				missing = true
				continue
			}
			mrr += c.delta
		}
		a.add("mrr", m, All, "", float64(mrr))
		if missing {
			a.flag("mrr", m, All, "", FlagFXMissing)
		}
	}
	inRange := map[time.Time]bool{}
	for _, m := range months {
		inRange[m] = true
	}
	for _, m := range months {
		for _, key := range []string{"new_mrr", "expansion_mrr", "reactivation_mrr", "contraction_mrr", "churned_mrr", "net_new_mrr", "new_customers", "churned_customers"} {
			a.add(key, m, All, "", 0)
		}
	}
	for _, c := range cs {
		m := MonthStart(c.f.At)
		if !inRange[m] {
			continue
		}
		switch c.f.Movement {
		case "new":
			a.add("new_customers", m, All, "", 1)
		case "churn":
			a.add("churned_customers", m, All, "", 1)
		}
		key := map[string]string{"new": "new_mrr", "expansion": "expansion_mrr", "reactivation": "reactivation_mrr", "contraction": "contraction_mrr", "churn": "churned_mrr"}[c.f.Movement]
		if !c.ok {
			if key != "" {
				a.flag(key, m, All, "", FlagFXMissing)
			}
			a.flag("net_new_mrr", m, All, "", FlagFXMissing)
			continue
		}
		if key == "" {
			continue // A date change with no MRR movement.
		}
		v := float64(c.delta)
		if c.f.Movement == "contraction" || c.f.Movement == "churn" {
			v = -v
		}
		a.add(key, m, All, "", v)
		a.add("net_new_mrr", m, All, "", float64(c.delta))
	}
}

// DealState is a deal's current state, rebuilt from its latest events whatever order they arrived in.
type DealState struct {
	ID            uuid.UUID
	Status        string
	Owner         *uuid.UUID
	Company       *uuid.UUID
	Source        string
	Stage         string
	Amount, MRR   *int64
	Currency      string
	Probability   int32
	ExpectedClose *time.Time
	Created       *time.Time
	ClosedAt      *time.Time
	Stages        map[string]bool
	FirstMeeting  *time.Time
}

func dealStates(facts []DealFact) map[uuid.UUID]*DealState {
	sorted := append([]DealFact(nil), facts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].At.Equal(sorted[j].At) {
			return sorted[i].Seq < sorted[j].Seq
		}
		return sorted[i].At.Before(sorted[j].At)
	})
	out := map[uuid.UUID]*DealState{}
	for _, f := range sorted {
		if f.Deal == nil {
			continue
		}
		d := out[*f.Deal]
		if d == nil {
			d = &DealState{ID: *f.Deal, Stages: map[string]bool{}, Status: "open"}
			out[*f.Deal] = d
		}
		at := f.At
		switch f.EventType {
		case "crm.deal.created":
			d.Created = &at
		case "crm.meeting.scheduled", "crm.meeting.completed":
			if d.FirstMeeting == nil || at.Before(*d.FirstMeeting) {
				d.FirstMeeting = &at
			}
		case "crm.deal.won", "crm.deal.lost":
			d.ClosedAt = &at
		}
		if !isDealEvent(f.EventType) {
			continue
		}
		if f.Stage != nil {
			d.Stage = *f.Stage
			d.Stages[*f.Stage] = true
		}
		if f.Status != nil {
			d.Status = *f.Status
		}
		d.Owner, d.Company, d.Amount, d.MRR, d.ExpectedClose = f.Owner, f.Company, f.Amount, f.MRR, f.ExpectedClose
		d.Source = str(f.Source, "direct")
		d.Currency = str(f.Currency, d.Currency)
		if f.Probability != nil {
			d.Probability = *f.Probability
		}
		if d.Status == "open" {
			d.ClosedAt = nil
		}
	}
	return out
}

func isDealEvent(t string) bool {
	switch t {
	case "crm.deal.created", "crm.deal.updated", "crm.deal.stage_changed", "crm.deal.won", "crm.deal.lost", "crm.deal.reopened":
		return true
	}
	return false
}

func computeDeals(in Input, deals map[uuid.UUID]*DealState, inRange map[time.Time]bool, a *acc) {
	now := MonthStart(in.Now)
	for _, d := range deals {
		if (d.Status == "won" || d.Status == "lost") && d.ClosedAt != nil {
			m := MonthStart(*d.ClosedAt)
			if inRange[m] {
				owner := uid(d.Owner, "unassigned")
				key := "deals_" + d.Status
				a.both(key, m, ByOwner, owner, 1)
				a.add(key, m, BySource, d.Source, 1)
				if d.Status == "won" {
					a.add("won_deal_mrr", m, All, "", 0)
					a.add("won_deal_mrr", m, ByOwner, owner, 0)
					a.add("won_deal_mrr", m, BySource, d.Source, 0)
					if d.MRR != nil {
						if v, ok := in.Convert(*d.MRR, d.Currency, *d.ClosedAt); ok {
							a.both("won_deal_mrr", m, ByOwner, owner, float64(v))
							a.add("won_deal_mrr", m, BySource, d.Source, float64(v))
						} else {
							a.flag("won_deal_mrr", m, All, "", FlagFXMissing)
						}
					}
				}
			}
		}
		if d.Created != nil && inRange[MonthStart(*d.Created)] {
			m := MonthStart(*d.Created)
			a.add("deals_created", m, All, "", 1)
			for st := range d.Stages {
				a.add("deals_reached_stage", m, ByStage, st, 1)
			}
		}
		if d.FirstMeeting != nil && inRange[MonthStart(*d.FirstMeeting)] {
			m := MonthStart(*d.FirstMeeting)
			a.add("meeting_deals", m, All, "", 1)
			if d.Status == "won" {
				a.add("meeting_won", m, All, "", 1)
			}
		}
		if d.Status == "open" && inRange[now] {
			a.both("open_deals", now, ByStage, str(&d.Stage, "unknown"), 1)
			a.add("weighted_pipeline", now, All, "", 0)
			value := dealValue(d)
			if value == nil {
				continue
			}
			v, ok := in.Convert(*value, d.Currency, in.Now)
			if !ok {
				a.flag("weighted_pipeline", now, All, "", FlagFXMissing)
				continue
			}
			a.both("weighted_pipeline", now, ByStage, str(&d.Stage, "unknown"), float64(v)*float64(d.Probability)/100)
		}
	}
	for m := range inRange {
		if n, ok := a.get("meeting_deals", m, All, ""); ok {
			w, _ := a.get("meeting_won", m, All, "")
			ratio(a, "meeting_conversion", m, All, "", w, n)
		}
	}
	// Proposals.
	type prop struct {
		issued *time.Time
		signed bool
	}
	props := map[uuid.UUID]*prop{}
	for _, f := range in.Deals {
		if f.Proposal == nil {
			continue
		}
		p := props[*f.Proposal]
		if p == nil {
			p = &prop{}
			props[*f.Proposal] = p
		}
		at := f.At
		switch f.EventType {
		case "proposals.version.issued":
			if p.issued == nil || at.Before(*p.issued) {
				p.issued = &at
			}
		case "proposals.proposal.signed":
			p.signed = true
		}
	}
	for _, p := range props {
		if p.issued == nil || !inRange[MonthStart(*p.issued)] {
			continue
		}
		m := MonthStart(*p.issued)
		a.add("proposals_issued", m, All, "", 1)
		if p.signed {
			a.add("proposals_signed_cohort", m, All, "", 1)
		} else {
			a.add("proposals_signed_cohort", m, All, "", 0)
		}
	}
	for m := range inRange {
		if n, ok := a.get("proposals_issued", m, All, ""); ok {
			s, _ := a.get("proposals_signed_cohort", m, All, "")
			ratio(a, "proposal_conversion", m, All, "", s, n)
		}
	}
}

// dealValue is a deal's contract value, or 12 months of its MRR when only that is known.
func dealValue(d *DealState) *int64 {
	if d.Amount != nil {
		return d.Amount
	}
	if d.MRR != nil {
		v := *d.MRR * 12
		return &v
	}
	return nil
}

var leadCreated = map[string]string{"lead_generation.prospect.created": "", "chatbot.lead.captured": "chatbot", "visitor_intelligence.conversion.recorded": "website"}

func computeLeads(in Input, inRange map[time.Time]bool, a *acc) {
	source := map[uuid.UUID]string{}
	created := map[uuid.UUID]time.Time{}
	qualified := map[uuid.UUID]bool{}
	for _, f := range in.Leads {
		if f.EventType == "lead_generation.prospect.created" && f.Prospect != nil {
			source[*f.Prospect] = f.Source
			created[*f.Prospect] = f.At
		}
		if f.EventType == "lead_generation.prospect.qualified" && f.Prospect != nil {
			qualified[*f.Prospect] = true
		}
	}
	for _, f := range in.Leads {
		m := MonthStart(f.At)
		if !inRange[m] {
			continue
		}
		src := f.Source
		if f.Prospect != nil {
			if s, ok := source[*f.Prospect]; ok {
				src = s
			}
		}
		if fixed, ok := leadCreated[f.EventType]; ok && fixed != "" {
			src = fixed
		}
		src = str(&src, "unknown")
		switch f.EventType {
		case "lead_generation.prospect.created", "chatbot.lead.captured", "visitor_intelligence.conversion.recorded":
			a.both("leads_created", m, BySource, src, 1)
		case "lead_generation.prospect.qualified":
			a.both("leads_qualified", m, BySource, src, 1)
		case "lead_generation.prospect.disqualified":
			a.both("leads_disqualified", m, BySource, src, 1)
		case "lead_generation.prospect.converted":
			a.both("leads_converted", m, BySource, src, 1)
		}
	}
	cohort := newAcc()
	for p, at := range created {
		m := MonthStart(at)
		if !inRange[m] {
			continue
		}
		src := str(ptr(source[p]), "unknown")
		cohort.both("n", m, BySource, src, 1)
		if qualified[p] {
			cohort.both("q", m, BySource, src, 1)
		} else {
			cohort.both("q", m, BySource, src, 0)
		}
	}
	for id, n := range cohort.values {
		if id[:2] != "n|" {
			continue
		}
		rest := id[2:]
		q := cohort.values["q|"+rest]
		parts := splitKey(rest)
		m, _ := time.Parse(time.DateOnly, parts[0])
		ratio(a, "qualification_rate", m, parts[1], parts[2], q, n)
	}
}

func splitKey(s string) [3]string {
	var out [3]string
	i := 0
	start := 0
	for j := 0; j < len(s) && i < 2; j++ {
		if s[j] == '|' {
			out[i] = s[start:j]
			i++
			start = j + 1
		}
	}
	out[2] = s[start:]
	return out
}

func ptr[T any](v T) *T { return &v }

func computeOutreach(in Input, inRange map[time.Time]bool, a *acc) {
	key := map[string]string{"outreach.message.sent": "messages_sent", "outreach.message.bounced": "messages_bounced", "outreach.reply.received": "replies", "outreach.unsubscribe.recorded": "unsubscribes"}
	for _, f := range in.Outreach {
		m := MonthStart(f.At)
		if !inRange[m] || key[f.EventType] == "" {
			continue
		}
		c := f.Campaign.String()
		for _, kk := range []string{"messages_sent", "messages_bounced", "replies", "unsubscribes"} {
			a.add(kk, m, All, "", 0)
			a.add(kk, m, ByCamp, c, 0)
		}
		a.both(key[f.EventType], m, ByCamp, c, 1)
	}
}

func computeCosts(in Input, months []time.Time, a *acc) {
	for _, c := range in.Costs {
		// Costs are whole days: compare dates, not times of day.
		c.Start, c.End = dayOf(c.Start), dayOf(c.End)
		v, ok := in.Convert(c.Amount, c.Currency, c.Start)
		days := int(c.End.Sub(c.Start).Hours()/24) + 1
		if days <= 0 {
			continue
		}
		for _, m := range months {
			next := m.AddDate(0, 1, 0)
			from, to := maxTime(c.Start, m), minTime(c.End, next.AddDate(0, 0, -1))
			overlap := int(to.Sub(from).Hours()/24) + 1
			if overlap <= 0 {
				continue
			}
			if !ok {
				a.flag("costs", m, All, "", FlagFXMissing)
				continue
			}
			a.add("costs", m, All, "", float64(v)*float64(overlap)/float64(days))
		}
	}
}

func computeROI(in Input, deals map[uuid.UUID]*DealState, a *acc) {
	now := MonthStart(in.Now)
	cost := map[uuid.UUID]float64{}
	missing := map[uuid.UUID]bool{}
	for _, c := range in.Costs {
		if c.Campaign == nil {
			continue
		}
		v, ok := in.Convert(c.Amount, c.Currency, c.Start)
		if !ok {
			missing[*c.Campaign] = true
			continue
		}
		cost[*c.Campaign] += float64(v)
	}
	if len(cost) == 0 && len(missing) == 0 {
		return
	}
	replies := map[uuid.UUID][]OutreachFact{}
	for _, f := range in.Outreach {
		if f.EventType == "outreach.reply.received" && f.Company != nil {
			replies[f.Campaign] = append(replies[f.Campaign], f)
		}
	}
	for camp := range unionKeys(cost, missing) {
		var attributed float64
		for _, d := range deals {
			if d.Status != "won" || d.ClosedAt == nil || d.Company == nil || d.MRR == nil {
				continue
			}
			for _, r := range replies[camp] {
				if *r.Company == *d.Company && !r.At.After(*d.ClosedAt) && d.ClosedAt.Sub(r.At) <= AttributionDays*24*time.Hour {
					if v, ok := in.Convert(*d.MRR, d.Currency, *d.ClosedAt); ok {
						attributed += float64(v) * ROIHorizonMonths
					} else {
						missing[camp] = true
					}
					break
				}
			}
		}
		c := cost[camp]
		ratio(a, "campaign_roi", now, ByCamp, camp.String(), attributed-c, c)
		if missing[camp] {
			a.flag("campaign_roi", now, ByCamp, camp.String(), FlagFXMissing)
		}
	}
}

func unionKeys(a map[uuid.UUID]float64, b map[uuid.UUID]bool) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

func dayOf(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func snapshots(a *acc) []Snapshot {
	out := make([]Snapshot, 0, len(a.values))
	for id, v := range a.values {
		p := splitKey4(id)
		def, ok := defs[p[0]]
		if !ok {
			continue // Helper keys.
		}
		m, _ := time.Parse(time.DateOnly, p[1])
		dim, val := p[2], p[3]
		if dim == All {
			val = ""
		}
		s := Snapshot{Key: def.Key, Period: m, Dimension: dim, DimValue: val, Kind: def.Kind}
		if !math.IsNaN(v) {
			x := v
			if def.Unit == "money" || def.Unit == "count" {
				x = math.Round(x)
			}
			s.Value = &x
		}
		for f := range a.flags[id] {
			s.Flags = append(s.Flags, f)
		}
		sort.Strings(s.Flags)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		if !out[i].Period.Equal(out[j].Period) {
			return out[i].Period.Before(out[j].Period)
		}
		if out[i].Dimension != out[j].Dimension {
			return out[i].Dimension < out[j].Dimension
		}
		return out[i].DimValue < out[j].DimValue
	})
	return out
}

func splitKey4(s string) [4]string {
	var out [4]string
	i, start := 0, 0
	for j := 0; j < len(s) && i < 3; j++ {
		if s[j] == '|' {
			out[i] = s[start:j]
			i++
			start = j + 1
		}
	}
	out[3] = s[start:]
	return out
}

// Display renders a value as people read it: money with its currency and two decimals, ratios as
// percentages, counts as whole numbers. Commentary is checked against exactly this text.
func Display(def Def, value *float64, currency string) string {
	if value == nil {
		return "not available"
	}
	v := *value
	switch def.Unit {
	case "money":
		return currency + " " + groupDecimal(math.Round(v)/100)
	case "ratio":
		pct := math.Round(v*1000) / 10
		if pct == math.Trunc(pct) {
			return strconv.FormatFloat(pct, 'f', 0, 64) + "%"
		}
		return strconv.FormatFloat(pct, 'f', 1, 64) + "%"
	default:
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
	}
}

func groupDecimal(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	whole, frac := s[:len(s)-3], s[len(s)-3:]
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String() + frac
	}
	return b.String() + frac
}
