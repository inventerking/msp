package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/salesintel/domain"
	"mspengine/internal/modules/salesintel/repo"
	"mspengine/internal/platform/audit"
)

// Point is one metric value.
type Point struct {
	ID       uuid.UUID
	Period   time.Time
	DimValue string
	Value    *float64
	Flags    []string
}

// Series is one metric over months, for one dimension. Every point has the metric's single kind.
type Series struct {
	Def       domain.Def
	Dimension string
	Currency  *string
	Points    []Point
}

// View is one analytics screen's data.
type View struct {
	Section   string
	AsOf      *time.Time
	FactsAsOf *time.Time
	Currency  string
	Available bool
	Reason    *string
	Series    []Series
	Labels    map[string]string
	From, To  time.Time
}

type section struct {
	keys     []string
	sources  []string
	presence func(repo.FactPresenceRow) bool
}

// Sections group metrics by screen, with the modules whose events feed them.
var sections = map[string]section{
	"revenue": {keys: []string{"mrr", "arr", "new_mrr", "expansion_mrr", "reactivation_mrr", "contraction_mrr", "churned_mrr", "net_new_mrr", "new_customers", "churned_customers"},
		sources: []string{"customer_expansion", "crm"}, presence: func(p repo.FactPresenceRow) bool { return p.Revenue }},
	"leads": {keys: []string{"leads_created", "leads_qualified", "leads_disqualified", "leads_converted", "qualification_rate"},
		sources: []string{"lead_generation", "chatbot", "visitor_intelligence"}, presence: func(p repo.FactPresenceRow) bool { return p.Leads }},
	"campaigns": {keys: []string{"messages_sent", "messages_bounced", "replies", "unsubscribes", "reply_rate", "bounce_rate", "campaign_roi"},
		sources: []string{"outreach"}, presence: func(p repo.FactPresenceRow) bool { return p.Outreach }},
	"funnel": {keys: []string{"deals_created", "deals_reached_stage", "open_deals", "weighted_pipeline"},
		sources: []string{"crm"}, presence: func(p repo.FactPresenceRow) bool { return p.Deals }},
	"win_loss": {keys: []string{"deals_won", "deals_lost", "win_rate", "won_deal_mrr", "meeting_conversion", "proposals_issued", "proposal_conversion"},
		sources: []string{"crm", "proposals"}, presence: func(p repo.FactPresenceRow) bool { return p.Deals }},
	"team": {keys: []string{"deals_won", "deals_lost", "win_rate", "won_deal_mrr"},
		sources: []string{"crm"}, presence: func(p repo.FactPresenceRow) bool { return p.Deals }},
	"costs": {keys: []string{"costs", "cac"}, presence: func(p repo.FactPresenceRow) bool { return p.Costs }},
}

// Range is a span of months. Zero values mean the last six months.
type Range struct{ From, To time.Time }

func (s *Service) months(r Range) (time.Time, time.Time) {
	to := domain.MonthStart(s.Now())
	if !r.To.IsZero() {
		to = domain.MonthStart(r.To)
	}
	from := to.AddDate(0, -5, 0)
	if !r.From.IsZero() {
		from = domain.MonthStart(r.From)
	}
	if from.After(to) {
		from = to
	}
	if limit := to.AddDate(0, -(HistoryMonths - 1), 0); from.Before(limit) {
		from = limit
	}
	return from, to
}

func (s *Service) availability(ctx context.Context, tenant uuid.UUID, name string, presence repo.FactPresenceRow) (bool, *string) {
	sec := sections[name]
	if name == "costs" {
		if !presence.Costs {
			return false, ptr("no_cost_data")
		}
		return true, nil
	}
	if sec.presence(presence) {
		return true, nil
	}
	for _, m := range sec.sources {
		if st, err := s.ModuleStatus(ctx, tenant, m); err == nil && st == "active" {
			return true, nil
		}
	}
	return false, ptr("module_not_active")
}

// Section returns one screen's metrics. Team performance is audited, because it shows individuals.
func (s *Service) Section(ctx context.Context, p authctx.Principal, name string, r Range) (View, error) {
	sec, ok := sections[name]
	if !ok {
		return View{}, invalid("section", "unknown")
	}
	from, to := s.months(r)
	cur, err := s.Currency(ctx, p.TenantID)
	if err != nil {
		return View{}, err
	}
	v := View{Section: name, Currency: cur, From: from, To: to, Labels: map[string]string{}}
	var presence repo.FactPresenceRow
	var rows []repo.ListSnapshotsRow
	var dim *string
	if name == "team" {
		dim = ptr(domain.ByOwner)
	}
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if presence, err = q.FactPresence(ctx, p.TenantID); err != nil {
			return err
		}
		st, err := q.GetRollupState(ctx, p.TenantID)
		if err == nil {
			v.AsOf, v.FactsAsOf = st.LastRolledAt, st.FactsAsOf
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if rows, err = q.ListSnapshots(ctx, repo.ListSnapshotsParams{TenantID: p.TenantID, Keys: sec.keys, FromMonth: from, ToMonth: to, Dimension: dim}); err != nil {
			return err
		}
		labels, err := q.ListLabels(ctx, p.TenantID)
		for _, l := range labels {
			v.Labels[l.Ref.String()] = l.Label
		}
		if err != nil {
			return err
		}
		if name == "team" {
			return audit.Record(ctx, tx, userAudit(p, "sales_intel.team_viewed", "report", nil, map[string]any{"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly)}))
		}
		return nil
	})
	if err != nil {
		return View{}, err
	}
	v.Available, v.Reason = s.availability(ctx, p.TenantID, name, presence)
	v.Series = toSeries(rows, name != "team")
	return v, nil
}

// toSeries groups snapshot rows by metric and dimension. Owner breakdowns stay out of general views.
func toSeries(rows []repo.ListSnapshotsRow, hideOwners bool) []Series {
	var out []Series
	index := map[string]int{}
	for _, r := range rows {
		if hideOwners && r.Dimension == domain.ByOwner {
			continue
		}
		def, ok := domain.Lookup(r.MetricKey)
		if !ok {
			continue
		}
		key := r.MetricKey + "|" + r.Dimension
		i, seen := index[key]
		if !seen {
			out = append(out, Series{Def: def, Dimension: r.Dimension, Currency: r.Currency})
			i = len(out) - 1
			index[key] = i
		}
		out[i].Points = append(out[i].Points, Point{ID: r.ID, Period: r.PeriodStart, DimValue: r.DimensionValue, Value: r.Value, Flags: r.Flags})
	}
	return out
}

// Card is the latest value of one metric, with the month before for comparison.
type Card struct {
	Def      domain.Def
	Currency *string
	Period   time.Time
	Value    *float64
	Previous *float64
	Flags    []string
}

// DashboardSection is one dashboard area.
type DashboardSection struct {
	Key       string
	Available bool
	Reason    *string
	Cards     []Card
}

// Dashboard is the overview.
type Dashboard struct {
	AsOf, FactsAsOf *time.Time
	Currency        string
	From, To        time.Time
	Sections        []DashboardSection
}

var dashboardCards = []struct {
	section string
	keys    []string
}{
	{"revenue", []string{"mrr", "arr", "net_new_mrr", "new_customers"}},
	{"leads", []string{"leads_created", "qualification_rate"}},
	{"campaigns", []string{"messages_sent", "reply_rate"}},
	{"funnel", []string{"open_deals", "weighted_pipeline"}},
	{"win_loss", []string{"deals_won", "win_rate"}},
	{"costs", []string{"costs", "cac"}},
}

// GetDashboard returns the overview for the range's last month.
func (s *Service) GetDashboard(ctx context.Context, p authctx.Principal, r Range) (Dashboard, error) {
	from, to := s.months(r)
	cur, err := s.Currency(ctx, p.TenantID)
	if err != nil {
		return Dashboard{}, err
	}
	d := Dashboard{Currency: cur, From: from, To: to}
	var presence repo.FactPresenceRow
	var rows []repo.ListSnapshotsRow
	var keys []string
	for _, c := range dashboardCards {
		keys = append(keys, c.keys...)
	}
	err = s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if presence, err = q.FactPresence(ctx, p.TenantID); err != nil {
			return err
		}
		if st, err := q.GetRollupState(ctx, p.TenantID); err == nil {
			d.AsOf, d.FactsAsOf = st.LastRolledAt, st.FactsAsOf
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		rows, err = q.ListSnapshots(ctx, repo.ListSnapshotsParams{TenantID: p.TenantID, Keys: keys, FromMonth: to.AddDate(0, -1, 0), ToMonth: to, Dimension: ptr(domain.All)})
		return err
	})
	if err != nil {
		return Dashboard{}, err
	}
	byKey := map[string]map[time.Time]repo.ListSnapshotsRow{}
	for _, row := range rows {
		if byKey[row.MetricKey] == nil {
			byKey[row.MetricKey] = map[time.Time]repo.ListSnapshotsRow{}
		}
		byKey[row.MetricKey][row.PeriodStart] = row
	}
	for _, c := range dashboardCards {
		ok, reason := s.availability(ctx, p.TenantID, c.section, presence)
		ds := DashboardSection{Key: c.section, Available: ok, Reason: reason}
		if ok {
			for _, k := range c.keys {
				def, _ := domain.Lookup(k)
				card := Card{Def: def, Period: to}
				// The current month's pipeline metrics exist only for now; others use the last month.
				if now, ok := byKey[k][to]; ok {
					card.Value, card.Currency, card.Flags = now.Value, now.Currency, now.Flags
				}
				if prev, ok := byKey[k][to.AddDate(0, -1, 0)]; ok {
					card.Previous = prev.Value
					if card.Currency == nil {
						card.Currency = prev.Currency
					}
				}
				ds.Cards = append(ds.Cards, card)
			}
		}
		d.Sections = append(d.Sections, ds)
	}
	return d, nil
}

// Metric returns one metric's series. Owner breakdowns are only on the team screen.
func (s *Service) Metric(ctx context.Context, p authctx.Principal, key string, r Range, dimension string) (View, error) {
	def, ok := domain.Lookup(key)
	if !ok {
		return View{}, invalid("key", "unknown_metric")
	}
	if dimension == "" {
		dimension = domain.All
	}
	allowed := false
	for _, d := range def.Dimensions {
		allowed = allowed || d == dimension
	}
	if !allowed || dimension == domain.ByOwner {
		return View{}, invalid("dimension", "not_available")
	}
	from, to := s.months(r)
	cur, err := s.Currency(ctx, p.TenantID)
	if err != nil {
		return View{}, err
	}
	v := View{Section: def.Section, Currency: cur, From: from, To: to, Available: true, Labels: map[string]string{}}
	var rows []repo.ListSnapshotsRow
	err = s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if st, err := q.GetRollupState(ctx, p.TenantID); err == nil {
			v.AsOf, v.FactsAsOf = st.LastRolledAt, st.FactsAsOf
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		labels, err := q.ListLabels(ctx, p.TenantID)
		if err != nil {
			return err
		}
		for _, l := range labels {
			v.Labels[l.Ref.String()] = l.Label
		}
		rows, err = q.ListSnapshots(ctx, repo.ListSnapshotsParams{TenantID: p.TenantID, Keys: []string{key}, FromMonth: from, ToMonth: to, Dimension: &dimension})
		return err
	})
	v.Series = toSeries(rows, true)
	return v, err
}
