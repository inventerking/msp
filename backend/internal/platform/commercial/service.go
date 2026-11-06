// Package commercial owns the platform schema: modules, products, plans, subscriptions and
// entitlements. Products say what is sold. Entitlements say what a tenant can use right now.
package commercial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/commercial/repo"
)

// EntitlementsChanged is the realtime signal sent after any entitlement change.
const EntitlementsChanged = "tenant.entitlements_changed"

// Service implements commercial use cases.
type Service struct {
	pools *db.Pools
	hub   *realtime.Hub
}

// New creates the service.
func New(pools *db.Pools, hub *realtime.Hub) *Service { return &Service{pools: pools, hub: hub} }

// Entitlement is a module's status for a tenant.
type Entitlement struct {
	Key            string
	Status         string
	EffectiveUntil *time.Time
}

// ModuleStatus returns the effective status of one module, reading under tenant RLS. It fails
// closed: any database error is returned, and the caller must deny access.
func (s *Service) ModuleStatus(ctx context.Context, tenantID uuid.UUID, moduleKey string) (string, error) {
	status := "inactive"
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		row, err := repo.New(tx).GetEffectiveEntitlement(ctx, repo.GetEffectiveEntitlementParams{TenantID: tenantID, ModuleKey: moduleKey})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		status = row.Status
		if status == "active" && row.EffectiveUntil != nil && !row.EffectiveUntil.After(time.Now()) {
			status = "expired"
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read entitlement: %w", err)
	}
	return status, nil
}

// ListEntitlements returns every module with its status for the tenant.
func (s *Service) ListEntitlements(ctx context.Context, tenantID uuid.UUID) ([]Entitlement, error) {
	var out []Entitlement
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		out, err = listEntitlements(ctx, tx, tenantID)
		return err
	})
	return out, err
}

func listEntitlements(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) ([]Entitlement, error) {
	rows, err := repo.New(tx).ListEntitlements(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]Entitlement, 0, len(rows))
	for _, r := range rows {
		st := r.Status
		if st == "active" && r.EffectiveUntil != nil && !r.EffectiveUntil.After(time.Now()) {
			st = "expired"
		}
		if st == "inactive" && r.HasPending {
			st = "pending"
		}
		out = append(out, Entitlement{Key: r.Key, Status: st, EffectiveUntil: r.EffectiveUntil})
	}
	return out, nil
}

// ListModules returns the module catalog.
func (s *Service) ListModules(ctx context.Context) ([]repo.ListModulesRow, error) {
	var out []repo.ListModulesRow
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListModules(ctx)
		return err
	})
	return out, err
}

// ListPublishedProducts returns products a tenant may see.
func (s *Service) ListPublishedProducts(ctx context.Context) ([]repo.ListProductsRow, error) {
	var out []repo.ListProductsRow
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListProducts(ctx, true)
		return err
	})
	return out, err
}

// ListTenantSubscriptions returns the tenant's subscriptions.
func (s *Service) ListTenantSubscriptions(ctx context.Context, tenantID uuid.UUID) ([]repo.ListTenantSubscriptionsRow, error) {
	var out []repo.ListTenantSubscriptionsRow
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListTenantSubscriptions(ctx, tenantID)
		return err
	})
	return out, err
}

// --- Platform administration ---

func adminEvent(ctx context.Context, tenantID *uuid.UUID, action, resType string, resID *uuid.UUID, before, after any) audit.Event {
	a := authctx.AdminFrom(ctx)
	return audit.Event{TenantID: tenantID, ActorType: "platform_admin", ActorID: &a.AdminID, Action: action,
		ResourceType: resType, ResourceID: resID, Before: before, After: after}
}

// AdminListModules returns the module catalog through the platform role.
func (s *Service) AdminListModules(ctx context.Context) ([]repo.ListModulesRow, error) {
	var out []repo.ListModulesRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListModules(ctx)
		return err
	})
	return out, err
}

// AdminListProducts returns all products with their newest version.
func (s *Service) AdminListProducts(ctx context.Context) ([]repo.ListProductsRow, error) {
	var out []repo.ListProductsRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListProducts(ctx, false)
		return err
	})
	return out, err
}

// CreateProduct creates a product with a draft first version.
func (s *Service) CreateProduct(ctx context.Context, key, kind, name, description string, moduleKeys []string) (repo.GetProductByVersionRow, error) {
	var out repo.GetProductByVersionRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		id, err := q.CreateProduct(ctx, repo.CreateProductParams{Key: key, Kind: kind, Name: name, Description: description})
		if isUnique(err) {
			return apperr.Conflict("product_key_taken", "A product with this key already exists")
		}
		if err != nil {
			return err
		}
		out, err = s.addVersion(ctx, tx, id, 1, moduleKeys)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "product.created", "product", &id, nil, map[string]any{"key": key, "modules": moduleKeys}))
	})
	return out, err
}

// CreateProductVersion adds a draft version. Published versions never change.
func (s *Service) CreateProductVersion(ctx context.Context, productID uuid.UUID, moduleKeys []string) (repo.GetProductByVersionRow, error) {
	var out repo.GetProductByVersionRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		next, err := repo.New(tx).NextProductVersion(ctx, productID)
		if err != nil {
			return err
		}
		if next == 1 {
			return apperr.NotFound()
		}
		out, err = s.addVersion(ctx, tx, productID, next, moduleKeys)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "product.version_created", "product", &productID, nil, map[string]any{"version": next, "modules": moduleKeys}))
	})
	return out, err
}

func (s *Service) addVersion(ctx context.Context, tx pgx.Tx, productID uuid.UUID, version int32, moduleKeys []string) (repo.GetProductByVersionRow, error) {
	q := repo.New(tx)
	vid, err := q.CreateProductVersion(ctx, repo.CreateProductVersionParams{ProductID: productID, Version: version})
	if err != nil {
		return repo.GetProductByVersionRow{}, err
	}
	for _, k := range moduleKeys {
		if _, err := q.GetModuleByKey(ctx, k); errors.Is(err, pgx.ErrNoRows) {
			return repo.GetProductByVersionRow{}, apperr.Validation(apperr.FieldError{Field: "module_keys", Code: "unknown_module"})
		} else if err != nil {
			return repo.GetProductByVersionRow{}, err
		}
		if err := q.AddVersionModule(ctx, repo.AddVersionModuleParams{VersionID: vid, ModuleKey: k}); err != nil {
			return repo.GetProductByVersionRow{}, err
		}
	}
	return q.GetProductByVersion(ctx, vid)
}

// PublishProductVersion makes a draft version available, after which it is immutable.
func (s *Service) PublishProductVersion(ctx context.Context, versionID uuid.UUID) (repo.GetProductByVersionRow, error) {
	var out repo.GetProductByVersionRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		n, err := q.PublishVersion(ctx, versionID)
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("invalid_transition", "Only a draft version can be published")
		}
		out, err = q.GetProductByVersion(ctx, versionID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "product.version_published", "product_version", &versionID, nil, nil))
	})
	return out, err
}

// PlanWithPrices is a plan and its price history.
type PlanWithPrices struct {
	Plan   repo.ListPlansRow
	Prices []repo.ListPricesRow
}

// ListPlans returns plans with their prices.
func (s *Service) ListPlans(ctx context.Context) ([]PlanWithPrices, error) {
	var out []PlanWithPrices
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		plans, err := q.ListPlans(ctx)
		if err != nil {
			return err
		}
		prices, err := q.ListPrices(ctx)
		if err != nil {
			return err
		}
		for _, p := range plans {
			pw := PlanWithPrices{Plan: p}
			for _, pr := range prices {
				if pr.PlanID == p.ID {
					pw.Prices = append(pw.Prices, pr)
				}
			}
			out = append(out, pw)
		}
		return nil
	})
	return out, err
}

// CreatePlan creates a plan on a published product version.
func (s *Service) CreatePlan(ctx context.Context, p repo.CreatePlanParams) (repo.CreatePlanRow, error) {
	var out repo.CreatePlanRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).CreatePlan(ctx, p)
		if isUnique(err) {
			return apperr.Conflict("plan_key_taken", "A plan with this key already exists")
		}
		if isForeignKey(err) {
			return apperr.Validation(apperr.FieldError{Field: "product_version_id", Code: "unknown_version"})
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "plan.created", "plan", &out.ID, nil, map[string]any{"key": p.Key}))
	})
	return out, err
}

// CreatePrice ends any current price for the currency and adds the new one. Amounts are never edited.
func (s *Service) CreatePrice(ctx context.Context, planID uuid.UUID, currency string, amountMinor int64) (repo.CreatePriceRow, error) {
	var out repo.CreatePriceRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if _, err := q.GetPlan(ctx, planID); errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		} else if err != nil {
			return err
		}
		if err := q.EndCurrentPrice(ctx, repo.EndCurrentPriceParams{PlanID: planID, Currency: currency}); err != nil {
			return err
		}
		var err error
		out, err = q.CreatePrice(ctx, repo.CreatePriceParams{PlanID: planID, Currency: currency, AmountMinor: amountMinor})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, nil, "price.created", "plan", &planID, nil, map[string]any{"currency": currency, "amount_minor": amountMinor}))
	})
	return out, err
}

// CreateSubscription records a subscription awaiting admin activation, with one pending grant
// per module in the plan's product version. It grants no access until activated.
func (s *Service) CreateSubscription(ctx context.Context, tenantID, planID uuid.UUID, currency string) (repo.GetSubscriptionRow, error) {
	var out repo.GetSubscriptionRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		plan, err := q.GetPlan(ctx, planID)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Validation(apperr.FieldError{Field: "plan_id", Code: "unknown_plan"})
		}
		if err != nil {
			return err
		}
		if plan.Status != "active" {
			return apperr.Validation(apperr.FieldError{Field: "plan_id", Code: "plan_not_active"})
		}
		price, err := q.CurrentPrice(ctx, repo.CurrentPriceParams{PlanID: planID, Currency: currency})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.New(http.StatusUnprocessableEntity, "price_unavailable", "This plan has no price in that currency")
		}
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(map[string]any{"amount_minor": price.AmountMinor, "currency": currency, "billing_period": plan.BillingPeriod, "plan_key": plan.Key})
		if err != nil {
			return err
		}
		id, err := q.CreateSubscription(ctx, repo.CreateSubscriptionParams{TenantID: tenantID, PlanID: planID, PriceSnapshot: snapshot, Currency: currency})
		if isForeignKey(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		mods, err := q.PlanModuleIDs(ctx, planID)
		if err != nil {
			return err
		}
		for _, m := range mods {
			if _, err := q.CreateGrant(ctx, repo.CreateGrantParams{TenantID: tenantID, ModuleID: m, SubscriptionID: &id, Source: "subscription", Status: "pending"}); err != nil {
				return err
			}
		}
		if err := audit.Record(ctx, tx, adminEvent(ctx, &tenantID, "subscription.created", "subscription", &id, nil, map[string]any{"plan": plan.Key})); err != nil {
			return err
		}
		out, err = q.GetSubscription(ctx, id)
		return err
	})
	if err == nil {
		s.hub.Publish(ctx, tenantID, EntitlementsChanged)
	}
	return out, err
}

// ListAllSubscriptions returns recent subscriptions across tenants.
func (s *Service) ListAllSubscriptions(ctx context.Context) ([]repo.ListAllSubscriptionsRow, error) {
	var out []repo.ListAllSubscriptionsRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListAllSubscriptions(ctx)
		return err
	})
	return out, err
}

// ActivateSubscription is the admin decision that turns a subscription's pending grants into access.
func (s *Service) ActivateSubscription(ctx context.Context, id uuid.UUID, reason string) (repo.GetSubscriptionRow, error) {
	admin := authctx.AdminFrom(ctx)
	var out repo.GetSubscriptionRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		out, err = q.GetSubscription(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		n, err := q.SetSubscriptionStatus(ctx, repo.SetSubscriptionStatusParams{ID: id, Status: "active", FromStatus: "pending_activation"})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("invalid_transition", "Only a subscription awaiting activation can be activated")
		}
		grants, err := q.ListSubscriptionGrants(ctx, repo.ListSubscriptionGrantsParams{TenantID: out.TenantID, SubscriptionID: &id})
		if err != nil {
			return err
		}
		now := time.Now()
		for _, g := range grants {
			if g.Status != "pending" {
				continue
			}
			if err := q.SetGrantStatus(ctx, repo.SetGrantStatusParams{ID: g.ID, Status: "active", Reason: &reason, ActivatedBy: &admin.AdminID, StartsAt: &now}); err != nil {
				return err
			}
			if _, err := s.recompute(ctx, q, out.TenantID, g.ModuleID, &g.ID, reason); err != nil {
				return err
			}
		}
		if err := audit.Record(ctx, tx, adminEvent(ctx, &out.TenantID, "subscription.activated", "subscription", &id, nil, map[string]any{"reason": reason})); err != nil {
			return err
		}
		out, err = q.GetSubscription(ctx, id)
		return err
	})
	if err == nil {
		s.hub.Publish(ctx, out.TenantID, EntitlementsChanged)
	}
	return out, err
}

// ChangeResult reports an entitlement after a change.
type ChangeResult struct {
	Status  string
	Version int64
}

// ChangeEntitlement activates, suspends, expires or revokes a module for a tenant.
func (s *Service) ChangeEntitlement(ctx context.Context, tenantID uuid.UUID, moduleKey, action, reason string, until *time.Time) (ChangeResult, error) {
	admin := authctx.AdminFrom(ctx)
	if action == "revoke" && !admin.HasRole("super_admin") {
		return ChangeResult{}, apperr.New(http.StatusForbidden, "insufficient_role", "Only a Super Admin can revoke access")
	}
	var res ChangeResult
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		mod, err := q.GetModuleByKey(ctx, moduleKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		grants, err := q.ListGrantsForModule(ctx, repo.ListGrantsForModuleParams{TenantID: tenantID, ModuleID: mod.ID})
		if err != nil {
			return err
		}
		var changed []uuid.UUID
		set := func(id uuid.UUID, status string, starts, ends *time.Time) error {
			changed = append(changed, id)
			return q.SetGrantStatus(ctx, repo.SetGrantStatusParams{ID: id, Status: status, Reason: &reason, ActivatedBy: &admin.AdminID, StartsAt: starts, EndsAt: ends})
		}
		now := time.Now()
		switch action {
		case "activate":
			for _, g := range grants {
				switch g.Status {
				case "active":
				case "revoked":
					if !admin.HasRole("super_admin") {
						return apperr.New(http.StatusForbidden, "insufficient_role", "Only a Super Admin can restore revoked access")
					}
					if err := set(g.ID, "active", &now, until); err != nil {
						return err
					}
				default:
					if err := set(g.ID, "active", &now, until); err != nil {
						return err
					}
				}
			}
			if len(grants) == 0 {
				if !admin.HasRole("super_admin") {
					return apperr.New(http.StatusForbidden, "insufficient_role", "Only a Super Admin can grant access without a subscription")
				}
				id, err := q.CreateGrant(ctx, repo.CreateGrantParams{TenantID: tenantID, ModuleID: mod.ID, Source: "admin_override", Status: "active", StartsAt: &now, EndsAt: until, ActivatedBy: &admin.AdminID, StatusReason: &reason})
				if isForeignKey(err) {
					return apperr.NotFound()
				}
				if err != nil {
					return err
				}
				changed = append(changed, id)
			}
		case "suspend":
			for _, g := range grants {
				if g.Status == "active" {
					if err := set(g.ID, "suspended", nil, g.EndsAt); err != nil {
						return err
					}
				}
			}
		case "expire":
			for _, g := range grants {
				if g.Status == "active" || g.Status == "suspended" {
					if err := set(g.ID, "expired", nil, &now); err != nil {
						return err
					}
				}
			}
		case "revoke":
			for _, g := range grants {
				if g.Status != "revoked" {
					if err := set(g.ID, "revoked", nil, g.EndsAt); err != nil {
						return err
					}
				}
			}
		default:
			return apperr.Validation(apperr.FieldError{Field: "action", Code: "unknown_action"})
		}
		if len(changed) == 0 && action != "activate" {
			return apperr.Conflict("invalid_transition", "This module has nothing to "+action)
		}
		var grantRef *uuid.UUID
		if len(changed) > 0 {
			grantRef = &changed[0]
		}
		res, err = s.recompute(ctx, q, tenantID, mod.ID, grantRef, reason)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(ctx, &tenantID, "entitlement."+action, "module", &mod.ID, nil, map[string]any{"module": moduleKey, "reason": reason, "status": res.Status}))
	})
	if err == nil {
		s.hub.Publish(ctx, tenantID, EntitlementsChanged)
	}
	return res, err
}

// recompute projects grants into the effective entitlement and records the change.
func (s *Service) recompute(ctx context.Context, q *repo.Queries, tenantID, moduleID uuid.UUID, grantID *uuid.UUID, reason string) (ChangeResult, error) {
	admin := authctx.AdminFrom(ctx)
	rows, err := q.ListGrantsForModule(ctx, repo.ListGrantsForModuleParams{TenantID: tenantID, ModuleID: moduleID})
	if err != nil {
		return ChangeResult{}, err
	}
	gs := make([]Grant, 0, len(rows))
	for _, r := range rows {
		gs = append(gs, Grant{Status: r.Status, EndsAt: r.EndsAt})
	}
	status, until := Effective(gs, time.Now())
	var from *string
	if prev, err := q.GetEffectiveStatus(ctx, repo.GetEffectiveStatusParams{TenantID: tenantID, ModuleID: moduleID}); err == nil {
		from = &prev
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ChangeResult{}, err
	}
	var effFrom *time.Time
	if status == "active" {
		now := time.Now()
		effFrom = &now
	}
	row, err := q.UpsertEffective(ctx, repo.UpsertEffectiveParams{TenantID: tenantID, ModuleID: moduleID, Status: status, EffectiveFrom: effFrom, EffectiveUntil: until})
	if err != nil {
		return ChangeResult{}, err
	}
	if err := q.InsertHistory(ctx, repo.InsertHistoryParams{TenantID: tenantID, ModuleID: moduleID, GrantID: grantID, FromStatus: from, ToStatus: status, ActorType: "platform_admin", ActorID: &admin.AdminID, Reason: &reason}); err != nil {
		return ChangeResult{}, err
	}
	return ChangeResult{Status: row.Status, Version: row.EntitlementVersion}, nil
}

// AdminListEntitlements returns a tenant's module statuses through the platform role.
func (s *Service) AdminListEntitlements(ctx context.Context, tenantID uuid.UUID) ([]Entitlement, error) {
	var out []Entitlement
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = listEntitlements(ctx, tx, tenantID)
		return err
	})
	return out, err
}

func isUnique(err error) bool { return pgCode(err) == "23505" }

func isForeignKey(err error) bool { return pgCode(err) == "23503" }

func pgCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}
