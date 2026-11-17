package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/customerexpansion/domain"
	"mspengine/internal/modules/customerexpansion/repo"
	"mspengine/internal/platform/audit"
)

// HandleRenewalScan raises renewal alerts across tenants. The system role finds candidate contracts;
// each is then handled under its own tenant. Alerts are unique by contract, date and window, so a
// repeated scan raises nothing new. It runs for every tenant, because contracts are kept for all
// (D-03); only the screens need the module.
func (s *Service) HandleRenewalScan(ctx context.Context, _ jobs.Job) error {
	var due []repo.ContractsEndingSoonRow
	if err := db.Tx(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		due, err = repo.New(tx).ContractsEndingSoon(ctx)
		return err
	}); err != nil {
		return err
	}
	today := s.today()
	for _, d := range due {
		if err := s.scanContract(ctx, d.TenantID, d.ID, today); err != nil {
			slog.WarnContext(ctx, "renewal scan failed for a contract", "error", err)
		}
	}
	return nil
}

func (s *Service) scanContract(ctx context.Context, tenant, id uuid.UUID, today time.Time) error {
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		c, err := q.GetContractForUpdate(ctx, repo.GetContractForUpdateParams{TenantID: tenant, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		alerts := domain.DueAlerts(domain.Contract{ID: c.ID, Status: c.Status, Start: c.StartDate, End: c.EndDate, TermMonths: c.TermMonths,
			AutoRenew: boolOf(c.AutoRenew), NoticeDays: c.NoticeDays, Currency: c.Currency, MRR: c.MrrMinor}, today)
		for _, a := range alerts {
			n, err := q.InsertAlert(ctx, repo.InsertAlertParams{TenantID: tenant, ContractID: c.ID, WindowDays: int32(a.WindowDays), DueDate: a.DueDate, Kind: a.Kind}) //nolint:gosec // 30 to 90.
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			if err := emit(ctx, tx, tenant, "renewal.due", "contract", c.ID, map[string]any{"contract_id": c.ID, "due_date": a.DueDate.Format(time.DateOnly),
				"window_days": a.WindowDays, "kind": a.Kind}); err != nil {
				return err
			}
			if c.OwnerUserID != nil && s.Notify != nil {
				name := "a customer"
				if summary, err := s.Registry.GetCompanySummary(ctx, tenant, c.CrmCompanyID); err == nil {
					name = summary.Name
				}
				when := "ends on " + a.DueDate.Format(time.DateOnly)
				if a.Kind == "notice_deadline" {
					when = "needs notice by " + a.DueDate.Format(time.DateOnly)
				}
				key := "renewal:" + c.ID.String() + ":" + a.DueDate.Format(time.DateOnly) + ":" + a.Kind
				if err := s.Notify.Notify(ctx, tx, tenant, *c.OwnerUserID, "expansion.renewal_due", map[string]string{"customer": name, "when": when}, key); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// RenewalView is an alert with its customer's name.
type RenewalView struct {
	repo.ListAlertsRow
	CustomerName string
}

// Renewals lists alerts due within a number of days, optionally by status.
func (s *Service) Renewals(ctx context.Context, p authctx.Principal, status *string, within int32) ([]RenewalView, error) {
	var rows []repo.ListAlertsRow
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		rows, err = q.ListAlerts(ctx, repo.ListAlertsParams{TenantID: p.TenantID, Status: status, WithinDays: within})
		return err
	}); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.CrmCompanyID)
	}
	names, err := s.Registry.CompanySummaries(ctx, p.TenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]RenewalView, 0, len(rows))
	for _, r := range rows {
		out = append(out, RenewalView{ListAlertsRow: r, CustomerName: names[r.CrmCompanyID].Name})
	}
	return out, nil
}

// AcknowledgeAlert marks an alert as seen. Repeating it is harmless.
func (s *Service) AcknowledgeAlert(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		a, err := q.GetAlert(ctx, repo.GetAlertParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if a.Status != "open" {
			return nil
		}
		n, err := q.AcknowledgeAlert(ctx, repo.AcknowledgeAlertParams{TenantID: p.TenantID, ID: id, UserID: uid(p)})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("alert_changed", "This alert changed meanwhile")
		}
		return audit.Record(ctx, tx, userAudit(p, "expansion.renewal_acknowledged", "renewal_alert", &id, map[string]any{"contract_id": a.ContractID}))
	})
}
