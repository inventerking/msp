package commercial

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial/repo"
)

// Meter keys.
const (
	MeterAI    = "ai_credits"
	MeterLeads = "lead_credits"
)

// Credit use follows reserve, call the provider, then commit what was used or release the rest.
// All of it goes through the platform role, so a tenant request can never change its own balance.

// JobSweepReservations releases reservations left behind by crashed work.
const JobSweepReservations = "credits.sweep_reservations"

const lowCreditThreshold = 100

func creditsExhausted() *apperr.Error {
	return apperr.New(http.StatusPaymentRequired, "credits_exhausted", "Your workspace has run out of credits")
}

func reservationKey(id uuid.UUID) string { return "reservation:" + id.String() }

// Grant adds credits. The key makes a repeated request grant only once.
func (s *Service) Grant(ctx context.Context, tenantID uuid.UUID, meterKey string, amount int64, idempotencyKey string, actorID *uuid.UUID, reason string) error {
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		meter, err := q.GetMeterByKey(ctx, meterKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Validation(apperr.FieldError{Field: "meter_key", Code: "unknown_meter"})
		}
		if err != nil {
			return err
		}
		account, err := q.EnsureCreditAccount(ctx, repo.EnsureCreditAccountParams{TenantID: tenantID, MeterID: meter.ID})
		if isForeignKey(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		actor := "system"
		if actorID != nil {
			actor = "platform_admin"
		}
		if _, err := q.InsertLedger(ctx, repo.InsertLedgerParams{TenantID: tenantID, AccountID: account, EntryType: "grant", Amount: amount, IdempotencyKey: "grant:" + idempotencyKey, ActorType: actor, ActorID: actorID}); errors.Is(err, pgx.ErrNoRows) {
			return nil // Already granted.
		} else if err != nil {
			return err
		}
		if _, err := q.AddBalance(ctx, repo.AddBalanceParams{ID: account, Amount: amount}); errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict("invalid_transition", "The balance cannot go below what is currently reserved")
		} else if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &tenantID, ActorType: actor, ActorID: actorID, Action: "credits.granted", ResourceType: "credit_account", ResourceID: &account, After: map[string]any{"meter": meterKey, "amount": amount, "reason": reason}})
	})
}

// Reserve holds credits before a costly call. It returns credits_exhausted if too few remain.
func (s *Service) Reserve(ctx context.Context, tenantID uuid.UUID, meterKey string, amount int64, module string, ttl time.Duration) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	err = db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		meter, err := q.GetMeterByKey(ctx, meterKey)
		if err != nil {
			return err
		}
		account, err := q.EnsureCreditAccount(ctx, repo.EnsureCreditAccountParams{TenantID: tenantID, MeterID: meter.ID})
		if err != nil {
			return err
		}
		n, err := q.ReserveCredits(ctx, repo.ReserveCreditsParams{ID: account, Amount: amount})
		if err != nil {
			return err
		}
		if n == 0 {
			return creditsExhausted()
		}
		exp := time.Now().Add(ttl)
		kind := "credit_reservation"
		_, err = q.InsertLedger(ctx, repo.InsertLedgerParams{TenantID: tenantID, AccountID: account, EntryType: "reservation", Amount: amount, SourceModule: &module, ReferenceType: &kind, ReferenceID: &id, IdempotencyKey: reservationKey(id), ExpiresAt: &exp, ActorType: "system"})
		return err
	})
	return id, err
}

// Commit spends what was used, up to the reservation, and returns the rest. Repeating does nothing.
func (s *Service) Commit(ctx context.Context, tenantID, reservation uuid.UUID, used int64) error {
	return s.settle(ctx, tenantID, reservation, used, "commit")
}

// Release returns a whole reservation. Repeating does nothing.
func (s *Service) Release(ctx context.Context, tenantID, reservation uuid.UUID) error {
	return s.settle(ctx, tenantID, reservation, 0, "release")
}

func (s *Service) settle(ctx context.Context, tenantID, reservation uuid.UUID, used int64, kind string) error {
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		res, err := q.GetReservation(ctx, repo.GetReservationParams{TenantID: tenantID, IdempotencyKey: reservationKey(reservation)})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if used > res.Amount {
			used = res.Amount // A reservation is the ceiling, so a large response cannot overspend.
		}
		ref := "credit_reservation"
		// One settlement key per reservation: a commit after a release, or the reverse, does nothing.
		if _, err := q.InsertLedger(ctx, repo.InsertLedgerParams{TenantID: tenantID, AccountID: res.AccountID, EntryType: kind, Amount: used, SourceModule: res.SourceModule, ReferenceType: &ref, ReferenceID: &reservation, IdempotencyKey: "settled:" + reservation.String(), ActorType: "system"}); errors.Is(err, pgx.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		balance, err := q.SettleReservation(ctx, repo.SettleReservationParams{ID: res.AccountID, ReservedAmount: res.Amount, Spent: used})
		if err != nil {
			return err
		}
		if used > 0 && balance < lowCreditThreshold && balance+used >= lowCreditThreshold {
			return events.Append(ctx, tx, events.Event{Type: "platform.credits.low", TenantID: &tenantID, SourceModule: "platform", AggregateType: "credit_account", AggregateID: res.AccountID}, map[string]any{"balance": balance})
		}
		return nil
	})
}

// HandleSweepReservations releases reservations whose work crashed before settling.
func (s *Service) HandleSweepReservations(ctx context.Context, _ jobs.Job) error {
	rows, err := repo.New(s.pools.Platform).ListExpiredReservations(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.ReferenceID == nil {
			continue
		}
		if err := s.Release(ctx, r.TenantID, *r.ReferenceID); err != nil {
			return err
		}
	}
	return nil
}

// RequireEntitled is for jobs and side effects, which must check again before acting, because a
// module may have been revoked since the work was queued. It returns a permanent job error.
func (s *Service) RequireEntitled(ctx context.Context, tenantID uuid.UUID, module string) error {
	status, err := s.ModuleStatus(ctx, tenantID, module)
	if err != nil {
		return err
	}
	if status != "active" {
		return jobs.Permanent(errors.New("module " + module + " is " + status))
	}
	return nil
}

// Balances returns a tenant's credit accounts, read under tenant RLS.
func (s *Service) Balances(ctx context.Context, tenantID uuid.UUID) ([]repo.ListCreditAccountsRow, error) {
	var out []repo.ListCreditAccountsRow
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListCreditAccounts(ctx, tenantID)
		return err
	})
	return out, err
}

// AdminBalances returns a tenant's credit accounts and recent ledger entries.
func (s *Service) AdminBalances(ctx context.Context, tenantID uuid.UUID) ([]repo.ListCreditAccountsRow, []repo.ListLedgerRow, error) {
	var accounts []repo.ListCreditAccountsRow
	var ledger []repo.ListLedgerRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		if accounts, err = q.ListCreditAccounts(ctx, tenantID); err != nil {
			return err
		}
		ledger, err = q.ListLedger(ctx, tenantID)
		return err
	})
	return accounts, ledger, err
}

// AdminGrant grants credits on behalf of an admin.
func (s *Service) AdminGrant(ctx context.Context, a authctx.AdminPrincipal, tenantID uuid.UUID, meterKey string, amount int64, key, reason string) error {
	return s.Grant(ctx, tenantID, meterKey, amount, key, &a.AdminID, reason)
}
