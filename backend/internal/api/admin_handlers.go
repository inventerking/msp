package api

import (
	"net/http"

	"github.com/google/uuid"

	"mspengine/internal/api/adminapi"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/platform/commercial/repo"
)

type adminHandlers struct{ *Server }

const adminSessionMaxAge = 8 * 3600

var _ adminapi.ServerInterface = adminHandlers{}

func (h adminHandlers) AdminLogin(w http.ResponseWriter, r *http.Request) {
	var b adminapi.AdminLogin
	if !decode(w, r, &b) {
		return
	}
	token, enrolled, err := h.identity.AdminLogin(r.Context(), b.Email, b.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	h.setCookie(w, h.adminCookie, token, adminSessionMaxAge)
	writeJSON(w, http.StatusOK, adminapi.AdminLoginResult{MfaEnrolled: enrolled})
}

func (h adminHandlers) AdminLogout(w http.ResponseWriter, r *http.Request) {
	if err := h.identity.AdminLogout(r.Context(), authctx.AdminFrom(r.Context())); err != nil {
		writeError(w, r, err)
		return
	}
	h.setCookie(w, h.adminCookie, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) AdminEnrollTotp(w http.ResponseWriter, r *http.Request) {
	e, err := h.identity.AdminEnrollTOTP(r.Context(), authctx.AdminFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminapi.TotpEnrollment{Secret: e.Secret, OtpauthUrl: e.URL})
}

func (h adminHandlers) AdminConfirmTotp(w http.ResponseWriter, r *http.Request) {
	var b adminapi.MfaCode
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.AdminConfirmTOTP(r.Context(), authctx.AdminFrom(r.Context()), b.Code); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) AdminVerifyMfa(w http.ResponseWriter, r *http.Request) {
	var b adminapi.MfaCode
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.AdminVerifyMFA(r.Context(), authctx.AdminFrom(r.Context()), b.Code); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) GetAdminContext(w http.ResponseWriter, r *http.Request) {
	a := authctx.AdminFrom(r.Context())
	writeJSON(w, http.StatusOK, adminapi.AdminContext{
		Admin:       adminapi.Admin{Id: a.AdminID, Email: a.Email, DisplayName: a.DisplayName, Status: a.Status, Roles: a.Roles},
		CsrfToken:   h.identity.Keys().CSRFToken(a.SessionID.String()),
		MfaVerified: a.MFAVerified,
	})
}

func (h adminHandlers) ListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := h.identity.ListAdmins(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.AdminList{Data: []adminapi.Admin{}}
	for _, a := range rows {
		out.Data = append(out.Data, adminapi.Admin{Id: a.ID, Email: a.Email, DisplayName: a.DisplayName, Status: a.Status, Roles: a.Roles})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	var b adminapi.CreateAdmin
	if !decode(w, r, &b) {
		return
	}
	roles := make([]string, len(b.Roles))
	for i, x := range b.Roles {
		roles[i] = string(x)
	}
	creator := authctx.AdminFrom(r.Context())
	id, err := h.identity.CreateAdmin(r.Context(), &creator, string(b.Email), b.DisplayName, b.Password, roles)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminapi.Admin{Id: id, Email: string(b.Email), DisplayName: b.DisplayName, Status: "active", Roles: roles})
}

func (h adminHandlers) ListTenants(w http.ResponseWriter, r *http.Request, params adminapi.ListTenantsParams) {
	limit, before, err := pageParams(params.Limit, params.Cursor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := h.identity.AdminListTenants(r.Context(), before, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.TenantList{Data: []adminapi.Tenant{}}
	for _, t := range rows {
		out.Data = append(out.Data, adminapi.Tenant{Id: t.ID, Name: t.Name, Slug: t.Slug, Status: t.Status, SuspendedAt: t.SuspendedAt, CreatedAt: t.CreatedAt})
	}
	if len(rows) == int(limit) {
		c := rows[len(rows)-1].ID.String()
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) GetTenant(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	t, err := h.identity.AdminGetTenant(r.Context(), tenantId)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminapi.Tenant{Id: t.ID, Name: t.Name, Slug: t.Slug, Status: t.Status, SuspendedAt: t.SuspendedAt, CreatedAt: t.CreatedAt})
}

func (h adminHandlers) setTenantState(w http.ResponseWriter, r *http.Request, id uuid.UUID, suspend bool) {
	var b adminapi.Reason
	if !decode(w, r, &b) {
		return
	}
	t, err := h.identity.AdminSetTenantState(r.Context(), authctx.AdminFrom(r.Context()), id, suspend, b.Reason)
	if err != nil {
		writeError(w, r, err)
		return
	}
	h.Hub.Publish(r.Context(), id, "tenant.status_changed")
	writeJSON(w, http.StatusOK, adminapi.Tenant{Id: t.ID, Name: t.Name, Slug: t.Slug, Status: t.Status, SuspendedAt: t.SuspendedAt, CreatedAt: t.CreatedAt})
}

func (h adminHandlers) SuspendTenant(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	h.setTenantState(w, r, tenantId, true)
}

func (h adminHandlers) ReactivateTenant(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	h.setTenantState(w, r, tenantId, false)
}

func (h adminHandlers) ListTenantUsers(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	rows, err := h.identity.AdminListTenantUsers(r.Context(), tenantId)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.TenantUserList{Data: []adminapi.TenantUser{}}
	for _, u := range rows {
		out.Data = append(out.Data, adminapi.TenantUser{UserId: u.UserID, Email: u.Email, DisplayName: u.DisplayName, Role: u.RoleKey, Status: u.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) ListModules(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.AdminListModules(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.ModuleList{Data: []adminapi.Module{}}
	for _, m := range rows {
		out.Data = append(out.Data, adminapi.Module{Id: m.ID, Key: m.Key, Name: m.Name, Description: m.Description, Status: m.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func productJSON(p repo.GetProductByVersionRow) adminapi.Product {
	return adminapi.Product{Id: p.ID, Key: p.Key, Kind: adminapi.ProductKind(p.Kind), Name: p.Name, Description: p.Description, Status: p.Status, VersionId: p.VersionID, Version: int(p.Version), VersionStatus: p.VersionStatus, Modules: p.Modules}
}

func (h adminHandlers) ListProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.AdminListProducts(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.ProductList{Data: []adminapi.Product{}}
	for _, p := range rows {
		out.Data = append(out.Data, productJSON(repo.GetProductByVersionRow(p)))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var b adminapi.CreateProduct
	if !decode(w, r, &b) {
		return
	}
	p, err := h.commercial.CreateProduct(r.Context(), b.Key, string(b.Kind), b.Name, b.Description, b.ModuleKeys)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, productJSON(p))
}

func (h adminHandlers) CreateProductVersion(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.CreateProductVersion
	if !decode(w, r, &b) {
		return
	}
	p, err := h.commercial.CreateProductVersion(r.Context(), id, b.ModuleKeys)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, productJSON(p))
}

func (h adminHandlers) PublishProductVersion(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	p, err := h.commercial.PublishProductVersion(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, productJSON(p))
}

func (h adminHandlers) ListPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.ListPlans(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.PlanList{Data: []adminapi.Plan{}}
	for _, p := range rows {
		plan := adminapi.Plan{Id: p.Plan.ID, ProductVersionId: p.Plan.ProductVersionID, Key: p.Plan.Key, Name: p.Plan.Name, BillingPeriod: p.Plan.BillingPeriod, TrialDays: int(p.Plan.TrialDays), Status: p.Plan.Status, Prices: []adminapi.Price{}}
		for _, x := range p.Prices {
			plan.Prices = append(plan.Prices, adminapi.Price{Id: x.ID, Currency: x.Currency, AmountMinor: x.AmountMinor, ValidFrom: x.ValidFrom, ValidTo: x.ValidTo})
		}
		out.Data = append(out.Data, plan)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var b adminapi.CreatePlan
	if !decode(w, r, &b) {
		return
	}
	p, err := h.commercial.CreatePlan(r.Context(), repo.CreatePlanParams{ProductVersionID: b.ProductVersionId, Key: b.Key, Name: b.Name, BillingPeriod: string(b.BillingPeriod), TrialDays: int32(b.TrialDays)}) //nolint:gosec // The contract limits this to 0-365.
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminapi.Plan{Id: p.ID, ProductVersionId: p.ProductVersionID, Key: p.Key, Name: p.Name, BillingPeriod: p.BillingPeriod, TrialDays: int(p.TrialDays), Status: p.Status, Prices: []adminapi.Price{}})
}

func (h adminHandlers) CreatePrice(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.CreatePrice
	if !decode(w, r, &b) {
		return
	}
	x, err := h.commercial.CreatePrice(r.Context(), id, b.Currency, b.AmountMinor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, adminapi.Price{Id: x.ID, Currency: x.Currency, AmountMinor: x.AmountMinor, ValidFrom: x.ValidFrom, ValidTo: x.ValidTo})
}

func subJSON(s repo.GetSubscriptionRow) adminapi.Subscription {
	return adminapi.Subscription{Id: s.ID, TenantId: s.TenantID, PlanId: s.PlanID, PlanName: s.PlanName, Status: s.Status, Currency: s.Currency, CurrentPeriodEnd: s.CurrentPeriodEnd, CreatedAt: s.CreatedAt}
}

func (h adminHandlers) ListAdminSubscriptions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.ListAllSubscriptions(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.SubscriptionList{Data: []adminapi.Subscription{}}
	for _, s := range rows {
		out.Data = append(out.Data, subJSON(repo.GetSubscriptionRow(s)))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) CreateSubscription(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	var b adminapi.CreateSubscription
	if !decode(w, r, &b) {
		return
	}
	s, err := h.commercial.CreateSubscription(r.Context(), tenantId, b.PlanId, b.Currency)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, subJSON(s))
}

func (h adminHandlers) ActivateSubscription(w http.ResponseWriter, r *http.Request, id adminapi.Id) {
	var b adminapi.Reason
	if !decode(w, r, &b) {
		return
	}
	s, err := h.commercial.ActivateSubscription(r.Context(), id, b.Reason)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, subJSON(s))
}

func (h adminHandlers) ListTenantEntitlements(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID) {
	rows, err := h.commercial.AdminListEntitlements(r.Context(), tenantId)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.EntitlementList{Data: []adminapi.Entitlement{}}
	for _, e := range rows {
		out.Data = append(out.Data, adminapi.Entitlement{Key: e.Key, Status: e.Status, EffectiveUntil: e.EffectiveUntil})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) ChangeEntitlement(w http.ResponseWriter, r *http.Request, tenantId uuid.UUID, moduleKey string, action adminapi.ChangeEntitlementParamsAction) {
	var b adminapi.EntitlementChange
	if !decode(w, r, &b) {
		return
	}
	res, err := h.commercial.ChangeEntitlement(r.Context(), tenantId, moduleKey, string(action), b.Reason, b.EffectiveUntil)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminapi.EntitlementDetail{TenantId: tenantId, Module: moduleKey, Status: res.Status, EntitlementVersion: res.Version})
}

func (h adminHandlers) ListAuditEvents(w http.ResponseWriter, r *http.Request, params adminapi.ListAuditEventsParams) {
	limit, before, err := pageParams(params.Limit, params.Cursor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := h.identity.AdminListAudit(r.Context(), before, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.AuditEventList{Data: []adminapi.AuditEvent{}}
	for _, e := range rows {
		out.Data = append(out.Data, adminapi.AuditEvent{Id: e.ID, TenantId: e.TenantID, ActorType: e.ActorType, ActorId: e.ActorID, Action: e.Action, ResourceType: e.ResourceType, ResourceId: e.ResourceID, Outcome: e.Outcome, OccurredAt: e.OccurredAt})
	}
	if len(rows) == int(limit) {
		c := rows[len(rows)-1].ID.String()
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}
