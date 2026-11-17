// Package domain holds Customer Expansion's rules: how contract changes move MRR, when renewals are
// due, which services are gaps, and how health is judged. It has no I/O, so every rule is tested
// directly.
package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Movement is how a contract change moves the customer's recurring revenue.
type Movement string

// Movements, as Sales Intelligence counts them in net new MRR.
const (
	New          Movement = "new"
	Expansion    Movement = "expansion"
	Contraction  Movement = "contraction"
	Churn        Movement = "churn"
	Reactivation Movement = "reactivation"
	None         Movement = "none"
)

// Activation classifies a contract becoming active. otherActive is the customer's MRR from their
// other active contracts; hadRevenue says whether they ever had an active contract before.
func Activation(otherActive int64, hadRevenue bool, mrr int64) Movement {
	switch {
	case mrr == 0:
		return None
	case otherActive > 0:
		return Expansion
	case hadRevenue:
		return Reactivation
	default:
		return New
	}
}

// Change classifies an active contract's MRR changing from before to after.
func Change(before, after int64) Movement {
	switch {
	case after > before:
		return Expansion
	case after < before:
		return Contraction
	default:
		return None
	}
}

// End classifies an active contract ending. It is churn only when the customer has no other
// active recurring revenue; otherwise the customer stays and their MRR contracts.
func End(otherActive, mrr int64) Movement {
	switch {
	case mrr == 0:
		return None
	case otherActive > 0:
		return Contraction
	default:
		return Churn
	}
}

// Statuses in which a contract counts towards MRR.
var countsAsMRR = map[string]bool{"active": true, "renewing": true}

// CountsAsMRR reports whether a contract in this status is recurring revenue now.
func CountsAsMRR(status string) bool { return countsAsMRR[status] }

// Contract is what the rules need to know about a contract.
type Contract struct {
	ID         uuid.UUID
	Status     string
	Start      time.Time
	End        *time.Time
	TermMonths *int32
	AutoRenew  bool
	NoticeDays *int32
	Currency   string
	MRR        int64
}

// EffectiveEnd is the end date, or the start plus the term when only a term is known.
func (c Contract) EffectiveEnd() *time.Time {
	if c.End != nil {
		return c.End
	}
	if c.TermMonths != nil {
		e := c.Start.AddDate(0, int(*c.TermMonths), 0).AddDate(0, 0, -1)
		return &e
	}
	return nil
}

// RenewalWindows are the days before a date at which an alert is raised.
var RenewalWindows = []int{90, 60, 30}

// Alert is a renewal alert due for a contract.
type Alert struct {
	Kind       string // contract_end or notice_deadline
	WindowDays int
	DueDate    time.Time
}

// DueAlerts returns the alerts a contract should have on a given day: for its end date and, when a
// notice period is set, for the last day to give notice. Only the tightest window that has been
// reached is returned for each date, so a contract first seen 20 days out gets the 30-day alert, not
// all three. Past dates raise nothing.
func DueAlerts(c Contract, today time.Time) []Alert {
	if !CountsAsMRR(c.Status) {
		return nil
	}
	end := c.EffectiveEnd()
	if end == nil {
		return nil
	}
	var out []Alert
	add := func(kind string, due time.Time) {
		left := days(today, due)
		if left < 0 {
			return
		}
		window := -1
		for _, w := range RenewalWindows {
			if left <= w {
				window = w
			}
		}
		if window >= 0 {
			out = append(out, Alert{Kind: kind, WindowDays: window, DueDate: due})
		}
	}
	add("contract_end", *end)
	if c.NoticeDays != nil && *c.NoticeDays > 0 {
		add("notice_deadline", end.AddDate(0, 0, -int(*c.NoticeDays)))
	}
	return out
}

func days(from, to time.Time) int {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(t.Sub(f).Hours() / 24)
}

// ValidateContract checks a contract's own fields. It returns the field and code of the first fault.
func ValidateContract(start time.Time, end *time.Time, term, notice *int32, currency string, mrr, setup int64) (field, code string) {
	switch {
	case !ValidCurrency(currency):
		return "currency", "invalid_currency"
	case mrr < 0:
		return "mrr_minor", "negative"
	case setup < 0:
		return "setup_minor", "negative"
	case end != nil && end.Before(start):
		return "end_date", "before_start"
	case term != nil && *term <= 0:
		return "term_months", "not_positive"
	case notice != nil && *notice < 0:
		return "notice_days", "negative"
	}
	return "", ""
}

// ValidCurrency accepts three upper-case letters. The reference list is checked by the database.
func ValidCurrency(c string) bool {
	if len(c) != 3 {
		return false
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// Coverage states.
const (
	Active   = "active"
	Absent   = "absent"
	Declined = "declined"
	Unknown  = "unknown"
)

// Service is one of the tenant's services.
type Service struct {
	ID       uuid.UUID
	Name     string
	Status   string
	Category string
}

// Covered is one customer's recorded coverage of a service.
type Covered struct {
	Coverage string
	MRR      *int64
	Currency *string
}

// PeerPrice is what the tenant's other customers pay for a service.
type PeerPrice struct {
	Customers int
	Median    int64
	Currency  string
}

// Input is everything one analysis looks at. Nothing in it describes a person.
type Input struct {
	Customer   string
	Today      time.Time
	Services   []Service
	Coverage   map[uuid.UUID]Covered
	Contracts  []Contract
	PeerPrices map[uuid.UUID]PeerPrice
	// AssessmentRisk is the latest IT assessment's risk level, if there is one.
	AssessmentRisk *string
	AssessmentID   *uuid.UUID
	AssessmentAt   *time.Time
}

// Evidence supports a recommendation.
type Evidence struct {
	Kind          string
	Detail        map[string]any
	ReferenceType string
	ReferenceID   *uuid.UUID
}

// Recommendation is a rule-based suggestion. Rationale is plain text built from the evidence.
type Recommendation struct {
	Kind          string
	ServiceID     *uuid.UUID
	ContractID    *uuid.UUID
	Rationale     string
	Evidence      []Evidence
	EstimatedMRR  *int64
	Currency      *string
	EvidenceClass string
	DedupeKey     string
}

// Gap is a service the customer may be missing.
type Gap struct {
	ServiceID uuid.UUID
	State     string // confirmed_absent or unknown_coverage
}

// securityCategories are where a risky assessment is relevant evidence.
var securityCategories = map[string]bool{"cybersecurity": true, "email_security": true, "backup_dr": true, "endpoint": true, "compliance": true}

// RenewalHorizonDays is how far ahead a contract end produces a renewal recommendation.
const RenewalHorizonDays = 90

// Analyze applies the rules. Unknown coverage is never treated as absent: it produces a request to
// confirm, with no revenue estimate. Declined services produce nothing.
func Analyze(in Input) ([]Recommendation, []Gap) {
	var recs []Recommendation
	var gaps []Gap
	risky := in.AssessmentRisk != nil && (*in.AssessmentRisk == "high" || *in.AssessmentRisk == "critical")
	services := append([]Service(nil), in.Services...)
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	for _, s := range services {
		if s.Status != "active" {
			continue
		}
		cov, ok := in.Coverage[s.ID]
		state := Unknown
		if ok {
			state = cov.Coverage
		}
		id := s.ID
		var ev []Evidence
		if risky && securityCategories[s.Category] && (state == Absent || state == Unknown) {
			ev = append(ev, Evidence{Kind: "assessment_risk", ReferenceType: "it_assessment", ReferenceID: in.AssessmentID,
				Detail: map[string]any{"risk_level": *in.AssessmentRisk, "assessed_at": dateOf(in.AssessmentAt)}})
		}
		switch state {
		case Absent:
			gaps = append(gaps, Gap{ServiceID: id, State: "confirmed_absent"})
			r := Recommendation{Kind: "cross_sell", ServiceID: &id, EvidenceClass: "observed", DedupeKey: "gap:" + id.String(),
				Evidence: append([]Evidence{{Kind: "service_absent", ReferenceType: "tenant_service", ReferenceID: &id, Detail: map[string]any{"service": s.Name}}}, ev...)}
			text := fmt.Sprintf("%s does not have %s, according to your coverage records.", in.Customer, s.Name)
			if p, ok := in.PeerPrices[s.ID]; ok && p.Customers > 0 {
				m, c := p.Median, p.Currency
				r.EstimatedMRR, r.Currency = &m, &c
				r.Evidence = append(r.Evidence, Evidence{Kind: "peer_price", Detail: map[string]any{"customers": p.Customers, "median_mrr_minor": p.Median, "currency": p.Currency}})
				text += fmt.Sprintf(" %d of your customers pay a median of %s a month for it; this is an estimate.", p.Customers, Money(p.Median, p.Currency))
			}
			r.Rationale = text + riskNote(ev)
			recs = append(recs, r)
		case Unknown:
			gaps = append(gaps, Gap{ServiceID: id, State: "unknown_coverage"})
			recs = append(recs, Recommendation{Kind: "confirm_coverage", ServiceID: &id, EvidenceClass: "inferred", DedupeKey: "confirm:" + id.String(),
				Rationale: fmt.Sprintf("Confirm whether %s has %s. Your records do not say.", in.Customer, s.Name) + riskNote(ev),
				Evidence:  append([]Evidence{{Kind: "coverage_unknown", ReferenceType: "tenant_service", ReferenceID: &id, Detail: map[string]any{"service": s.Name}}}, ev...)})
		}
	}
	for _, c := range in.Contracts {
		end := c.EffectiveEnd()
		if !CountsAsMRR(c.Status) || end == nil {
			continue
		}
		left := days(in.Today, *end)
		if left < 0 || left > RenewalHorizonDays {
			continue
		}
		id, mrr, cur := c.ID, c.MRR, c.Currency
		r := Recommendation{Kind: "renewal", ContractID: &id, EvidenceClass: "observed", DedupeKey: "renewal:" + id.String() + ":" + end.Format(time.DateOnly),
			EstimatedMRR: &mrr, Currency: &cur,
			Evidence: []Evidence{{Kind: "contract_ending", ReferenceType: "contract", ReferenceID: &id, Detail: map[string]any{"end_date": end.Format(time.DateOnly), "days_left": left, "auto_renew": c.AutoRenew}}}}
		text := fmt.Sprintf("A contract worth %s a month ends on %s, in %d days.", Money(c.MRR, c.Currency), end.Format(time.DateOnly), left)
		if c.AutoRenew {
			text += " It renews automatically unless notice is given."
		}
		if c.NoticeDays != nil && *c.NoticeDays > 0 {
			deadline := end.AddDate(0, 0, -int(*c.NoticeDays))
			r.Evidence = append(r.Evidence, Evidence{Kind: "notice_deadline", ReferenceType: "contract", ReferenceID: &id, Detail: map[string]any{"deadline": deadline.Format(time.DateOnly), "notice_days": *c.NoticeDays}})
			text += fmt.Sprintf(" Notice is due by %s.", deadline.Format(time.DateOnly))
		}
		r.Rationale = text
		recs = append(recs, r)
	}
	return recs, gaps
}

func riskNote(ev []Evidence) string {
	for _, e := range ev {
		if e.Kind == "assessment_risk" {
			return fmt.Sprintf(" Their latest IT assessment found %s risk.", e.Detail["risk_level"])
		}
	}
	return ""
}

func dateOf(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.DateOnly)
}

// Money formats minor units with their currency, such as "GBP 1,250.00". Every currency is shown
// with two decimals; currencies without minor units are rare among MSP customers.
func Money(minor int64, currency string) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole, frac := minor/100, minor%100
	s := fmt.Sprintf("%d", whole)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s %s%s.%02d", currency, sign, b.String(), frac)
}

// Indicator is one health signal. Polarity is positive, neutral, negative or unknown.
type Indicator struct {
	Key      string
	Value    map[string]any
	Polarity string
}

// Health derives indicators and a status from business facts only. With no known indicator the
// status is unknown; one negative makes it watch, two or more at risk.
func Health(in Input) ([]Indicator, string) {
	var out []Indicator
	var active int64
	hasContracts := len(in.Contracts) > 0
	nearest := -1
	for _, c := range in.Contracts {
		if !CountsAsMRR(c.Status) {
			continue
		}
		active += c.MRR
		if end := c.EffectiveEnd(); end != nil && !c.AutoRenew {
			if d := days(in.Today, *end); d >= 0 && (nearest < 0 || d < nearest) {
				nearest = d
			}
		}
	}
	switch {
	case !hasContracts:
		out = append(out, Indicator{Key: "active_mrr", Value: map[string]any{"mrr_minor": 0}, Polarity: "unknown"})
	case active > 0:
		out = append(out, Indicator{Key: "active_mrr", Value: map[string]any{"mrr_minor": active}, Polarity: "positive"})
	default:
		out = append(out, Indicator{Key: "active_mrr", Value: map[string]any{"mrr_minor": 0}, Polarity: "negative"})
	}
	switch {
	case nearest < 0:
		// Nothing ends soon: reassuring only if there is revenue to keep.
		polarity := "unknown"
		if active > 0 {
			polarity = "positive"
		}
		out = append(out, Indicator{Key: "renewal_proximity", Value: map[string]any{"days_left": nil}, Polarity: polarity})
	case nearest <= 60:
		out = append(out, Indicator{Key: "renewal_proximity", Value: map[string]any{"days_left": nearest}, Polarity: "negative"})
	case nearest <= 90:
		out = append(out, Indicator{Key: "renewal_proximity", Value: map[string]any{"days_left": nearest}, Polarity: "neutral"})
	default:
		out = append(out, Indicator{Key: "renewal_proximity", Value: map[string]any{"days_left": nearest}, Polarity: "positive"})
	}
	var known, have int
	for _, s := range in.Services {
		if s.Status != "active" {
			continue
		}
		if c, ok := in.Coverage[s.ID]; ok && c.Coverage != Unknown {
			known++
			if c.Coverage == Active {
				have++
			}
		}
	}
	breadth := Indicator{Key: "coverage_breadth", Value: map[string]any{"active": have, "known": known}, Polarity: "unknown"}
	switch {
	case known == 0:
	case have*2 >= known:
		breadth.Polarity = "positive"
	case have > 0:
		breadth.Polarity = "neutral"
	default:
		breadth.Polarity = "negative"
	}
	out = append(out, breadth)
	risk := Indicator{Key: "assessment_risk", Value: map[string]any{"risk_level": nil}, Polarity: "unknown"}
	if in.AssessmentRisk != nil {
		risk.Value["risk_level"] = *in.AssessmentRisk
		switch *in.AssessmentRisk {
		case "high", "critical":
			risk.Polarity = "negative"
		case "medium":
			risk.Polarity = "neutral"
		case "low":
			risk.Polarity = "positive"
		}
	}
	out = append(out, risk)
	negatives, knownCount := 0, 0
	for _, i := range out {
		if i.Polarity != "unknown" {
			knownCount++
		}
		if i.Polarity == "negative" {
			negatives++
		}
	}
	switch {
	case knownCount == 0:
		return out, "unknown"
	case negatives >= 2:
		return out, "at_risk"
	case negatives == 1:
		return out, "watch"
	default:
		return out, "healthy"
	}
}

var numberPattern = regexp.MustCompile(`\d[\d,.]*\d|\d`)

// KeepsFacts reports whether AI wording keeps the rule wording's facts: exactly the same numbers
// and dates, none added or dropped, and for unknown coverage no claim that the customer lacks it.
func KeepsFacts(kind, rule, worded string) bool {
	if strings.TrimSpace(worded) == "" {
		return false
	}
	count := func(s string) map[string]int {
		m := map[string]int{}
		for _, n := range numberPattern.FindAllString(s, -1) {
			m[strings.TrimRight(n, ".,")]++
		}
		return m
	}
	a, b := count(rule), count(worded)
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if b[k] == 0 {
			return false
		}
	}
	if kind == "confirm_coverage" {
		l := strings.ToLower(worded)
		for _, claim := range []string{"does not have", "doesn't have", "lacks", "is missing", "has no ", "without "} {
			if strings.Contains(l, claim) {
				return false
			}
		}
	}
	return true
}
