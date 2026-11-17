package app

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/salesintel/domain"
	"mspengine/internal/modules/salesintel/repo"
)

// HistoryMonths is how far back metrics are computed.
const HistoryMonths = 24

// facts loads a tenant's facts for the domain functions.
type facts struct {
	in        domain.Input
	reporting string
}

// converter returns a cached currency converter for one run, at each amount's own date (D-11).
func (s *Service) converter(ctx context.Context, reporting string) domain.Converter {
	type key struct {
		cur string
		day string
	}
	cache := map[key]*float64{}
	return func(amount int64, currency string, on time.Time) (int64, bool) {
		if currency == reporting {
			return amount, true
		}
		k := key{currency, on.UTC().Format(time.DateOnly)}
		r, seen := cache[k]
		if !seen {
			rate, found, err := s.Rates(ctx, currency, reporting, on)
			if err != nil {
				slog.WarnContext(ctx, "exchange rate lookup failed", "error", err)
			}
			if found && err == nil {
				r = &rate
			}
			cache[k] = r
		}
		if r == nil {
			return 0, false
		}
		return int64(math.Round(float64(amount) * *r)), true
	}
}

func (s *Service) loadFacts(ctx context.Context, tenant uuid.UUID) (facts, error) {
	reporting, err := s.Currency(ctx, tenant)
	if err != nil {
		return facts{}, err
	}
	now := s.Now().UTC()
	out := facts{reporting: reporting, in: domain.Input{Convert: s.converter(ctx, reporting), Now: now, To: domain.MonthStart(now)}}
	err = s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		first, err := q.FirstFactAt(ctx, tenant)
		if err != nil {
			return err
		}
		from := domain.MonthStart(first)
		if earliest := domain.MonthStart(now).AddDate(0, -(HistoryMonths - 1), 0); from.Before(earliest) {
			from = earliest
		}
		out.in.From = from
		leads, err := q.ListLeadFacts(ctx, tenant)
		if err != nil {
			return err
		}
		for _, f := range leads {
			out.in.Leads = append(out.in.Leads, domain.LeadFact{EventType: f.EventType, Prospect: f.ProspectRef, Source: f.SourceKey, At: f.OccurredAt})
		}
		outreach, err := q.ListOutreachFacts(ctx, tenant)
		if err != nil {
			return err
		}
		for _, f := range outreach {
			out.in.Outreach = append(out.in.Outreach, domain.OutreachFact{EventType: f.EventType, Campaign: f.CampaignRef, Company: f.CompanyRef, At: f.OccurredAt})
		}
		deals, err := q.ListDealFacts(ctx, tenant)
		if err != nil {
			return err
		}
		for i, f := range deals {
			out.in.Deals = append(out.in.Deals, domain.DealFact{EventType: f.EventType, Deal: f.DealRef, Proposal: f.ProposalRef, Owner: f.OwnerUserRef, Company: f.CompanyRef,
				Stage: f.StageKey, Status: f.Status, Source: f.SourceModule, Amount: f.AmountMinor, MRR: f.MrrMinor, Currency: f.Currency, Probability: f.Probability,
				ExpectedClose: f.ExpectedClose, At: f.OccurredAt, Seq: i})
		}
		revenue, err := q.ListRevenueFacts(ctx, tenant)
		if err != nil {
			return err
		}
		for _, f := range revenue {
			out.in.Revenue = append(out.in.Revenue, domain.RevenueFact{Movement: f.Movement, Contract: f.ContractRef, Delta: f.MrrDeltaMinor, ContractMRR: f.ContractMrrMinor,
				Currency: f.Currency, End: f.ContractEndDate, AutoRenew: f.AutoRenew, At: f.OccurredAt})
		}
		costs, err := q.ListCosts(ctx, tenant)
		if err != nil {
			return err
		}
		for _, c := range costs {
			out.in.Costs = append(out.in.Costs, domain.Cost{Start: c.PeriodStart, End: c.PeriodEnd, Campaign: c.CampaignRef, Amount: c.AmountMinor, Currency: c.Currency})
		}
		return nil
	})
	return out, err
}

// HandleRollup recomputes one tenant's metrics and replaces them in one transaction, so readers see
// the old set or the new one, never a mix. A failure leaves the old values with their timestamp.
func (s *Service) HandleRollup(ctx context.Context, j jobs.Job) error {
	if j.TenantID == nil {
		return nil
	}
	return s.Rollup(ctx, *j.TenantID)
}

// Rollup recomputes a tenant's metrics now.
func (s *Service) Rollup(ctx context.Context, tenant uuid.UUID) error {
	started := s.Now()
	f, err := s.loadFacts(ctx, tenant)
	if err != nil {
		return err
	}
	snaps := domain.Compute(f.in)
	p := repo.InsertSnapshotsParams{TenantID: tenant, ComputedAt: s.Now()}
	for _, sn := range snaps {
		def, _ := domain.Lookup(sn.Key)
		p.Keys = append(p.Keys, sn.Key)
		p.Periods = append(p.Periods, sn.Period)
		p.Dims = append(p.Dims, sn.Dimension)
		p.DimValues = append(p.DimValues, sn.DimValue)
		v := ""
		if sn.Value != nil {
			v = strconv.FormatFloat(*sn.Value, 'f', -1, 64)
		}
		p.Vals = append(p.Vals, v)
		p.Kinds = append(p.Kinds, string(sn.Kind))
		cur := ""
		if def.Unit == "money" {
			cur = f.reporting
		}
		p.Currencies = append(p.Currencies, cur)
		p.Flags = append(p.Flags, strings.Join(sn.Flags, ","))
	}
	return s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		if err := q.DeleteSnapshots(ctx, tenant); err != nil {
			return err
		}
		if len(p.Keys) > 0 {
			if err := q.InsertSnapshots(ctx, p); err != nil {
				return err
			}
		}
		return q.FinishRollup(ctx, repo.FinishRollupParams{TenantID: tenant, ComputedAt: &p.ComputedAt, StartedAt: &started})
	})
}

// HandleSweep queues rollups for tenants with new facts, or whose metrics predate this month.
func (s *Service) HandleSweep(ctx context.Context, _ jobs.Job) error {
	var due []uuid.UUID
	if err := db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		due, err = repo.New(tx).DueRollups(ctx)
		return err
	}); err != nil {
		return err
	}
	hour := s.Now().UTC().Truncate(time.Hour).Format(time.RFC3339)
	for _, tenant := range due {
		t := tenant
		if err := s.systemTx(ctx, t, func(_ *repo.Queries, tx pgx.Tx) error {
			_, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &t, Kind: JobRollup, Queue: jobs.QueueAnalytics, MaxAttempts: 5, IdempotencyKey: "si-rollup-sweep:" + t.String() + ":" + hour})
			return err
		}); err != nil {
			slog.WarnContext(ctx, "queue rollup failed", "error", err)
		}
	}
	return nil
}
