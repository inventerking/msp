package api

import (
	"log/slog"
	"net/http"
	"time"

	"mspengine/internal/api/tenantapi"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/platform/identity"
)

type tenantHandlers struct{ *Server }

const sessionMaxAge = 7 * 24 * 3600

func (h tenantHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.Register
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.Register(r.Context(), string(b.Email), b.Password, b.DisplayName); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, tenantapi.Accepted{Status: "check_your_email"})
}

func (h tenantHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.Login
	if !decode(w, r, &b) {
		return
	}
	res, err := h.identity.Login(r.Context(), b.Email, b.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	h.setCookie(w, h.sessionCookie, res.Token, sessionMaxAge)
	writeJSON(w, http.StatusOK, tenantapi.LoginResult{MfaRequired: res.MFARequired})
}

func (h tenantHandlers) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.TokenRequest
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.VerifyEmail(r.Context(), b.Token); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.EmailRequest
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.ResendVerification(r.Context(), b.Email); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, tenantapi.Accepted{Status: "check_your_email"})
}

func (h tenantHandlers) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.EmailRequest
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.ForgotPassword(r.Context(), b.Email); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, tenantapi.Accepted{Status: "check_your_email"})
}

func (h tenantHandlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.PasswordReset
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.ResetPassword(r.Context(), b.Token, b.Password); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.identity.Logout(r.Context(), authctx.PrincipalFrom(r.Context())); err != nil {
		writeError(w, r, err)
		return
	}
	h.setCookie(w, h.sessionCookie, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) VerifyMfa(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.MfaCode
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.VerifyMFA(r.Context(), authctx.PrincipalFrom(r.Context()), b.Code); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) EnrollTotp(w http.ResponseWriter, r *http.Request) {
	e, err := h.identity.EnrollTOTP(r.Context(), authctx.PrincipalFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantapi.TotpEnrollment{Secret: e.Secret, OtpauthUrl: e.URL})
}

func (h tenantHandlers) ConfirmTotp(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.MfaCode
	if !decode(w, r, &b) {
		return
	}
	codes, err := h.identity.ConfirmTOTP(r.Context(), authctx.PrincipalFrom(r.Context()), b.Code)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantapi.RecoveryCodes{RecoveryCodes: codes})
}

func (h tenantHandlers) DisableMfa(w http.ResponseWriter, r *http.Request) {
	if err := h.identity.DisableMFA(r.Context(), authctx.PrincipalFrom(r.Context())); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) Reauthenticate(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.PasswordRequest
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.Reauthenticate(r.Context(), authctx.PrincipalFrom(r.Context()), b.Password); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	p := authctx.PrincipalFrom(r.Context())
	rows, err := h.identity.ListSessions(r.Context(), p.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.SessionList{Data: []tenantapi.SessionInfo{}}
	for _, s := range rows {
		out.Data = append(out.Data, tenantapi.SessionInfo{Id: s.ID, Current: s.ID == p.SessionID, UserAgent: s.UserAgent, CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) RevokeSession(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	if err := h.identity.RevokeSession(r.Context(), authctx.PrincipalFrom(r.Context()), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) GetMeContext(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := authctx.PrincipalFrom(ctx)
	out := tenantapi.MeContext{
		User:        tenantapi.UserSummary{Id: p.UserID, Email: p.Email, DisplayName: p.DisplayName, MfaEnabled: p.HasMFA},
		MfaRequired: p.HasMFA && !p.MFAVerified,
		Memberships: []tenantapi.MembershipSummary{},
		Permissions: []string{},
		Modules:     []tenantapi.Entitlement{},
		CsrfToken:   h.identity.Keys().CSRFToken(p.SessionID.String()),
	}
	if out.MfaRequired {
		writeJSON(w, http.StatusOK, out) // Nothing about the workspace until verification completes.
		return
	}
	ms, err := h.identity.ListMemberships(ctx, p.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for _, m := range ms {
		out.Memberships = append(out.Memberships, tenantapi.MembershipSummary{TenantId: m.TenantID, TenantName: m.TenantName, Role: m.RoleKey})
	}
	active := false
	for _, m := range ms {
		active = active || m.TenantID == p.TenantID
	}
	if !active {
		writeJSON(w, http.StatusOK, out)
		return
	}
	access, err := h.identity.Access(ctx, p.UserID, p.TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out.Tenant = &tenantapi.TenantSummary{Id: p.TenantID, Name: access.TenantName, Slug: access.TenantSlug, Status: access.TenantState}
	out.Role = &access.RoleKey
	for perm := range access.Permissions {
		out.Permissions = append(out.Permissions, perm)
	}
	ents, err := h.commercial.ListEntitlements(ctx, p.TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	for _, e := range ents {
		out.Modules = append(out.Modules, tenantapi.Entitlement{Key: e.Key, Status: e.Status, EffectiveUntil: e.EffectiveUntil})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) SetActiveTenant(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.ActiveTenant
	if !decode(w, r, &b) {
		return
	}
	if err := h.identity.SetActiveTenant(r.Context(), authctx.PrincipalFrom(r.Context()), b.TenantId); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.CreateTenant
	if !decode(w, r, &b) {
		return
	}
	t, err := h.identity.CreateTenant(r.Context(), authctx.PrincipalFrom(r.Context()), b.Name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, tenantapi.TenantSummary{Id: t.ID, Name: t.Name, Slug: t.Slug, Status: t.Status})
}

func (h tenantHandlers) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.TokenRequest
	if !decode(w, r, &b) {
		return
	}
	t, err := h.identity.AcceptInvitation(r.Context(), authctx.PrincipalFrom(r.Context()), b.Token)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantapi.TenantSummary{Id: t.ID, Name: t.Name, Slug: t.Slug, Status: t.Status})
}

func tenantDetail(d identity.TenantDetail) tenantapi.TenantDetail {
	return tenantapi.TenantDetail{
		Id: d.Tenant.ID, Name: d.Tenant.Name, Slug: d.Tenant.Slug, Status: d.Tenant.Status,
		DefaultLocale: d.Tenant.DefaultLocale, DefaultTimezone: d.Tenant.DefaultTimezone, ReportingCurrency: d.Tenant.ReportingCurrency,
		LegalName: d.Profile.LegalName, WebsiteDomain: d.Profile.WebsiteDomain, CountryCode: d.Profile.CountryCode, OnboardingStatus: d.Profile.OnboardingStatus,
	}
}

func (h tenantHandlers) GetTenant(w http.ResponseWriter, r *http.Request) {
	d, err := h.identity.GetTenant(r.Context(), authctx.PrincipalFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantDetail(d))
}

func (h tenantHandlers) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.UpdateTenant
	if !decode(w, r, &b) {
		return
	}
	d, err := h.identity.UpdateTenant(r.Context(), authctx.PrincipalFrom(r.Context()), identity.TenantUpdate{
		Name: b.Name, Locale: b.DefaultLocale, Timezone: b.DefaultTimezone, Currency: b.ReportingCurrency,
		LegalName: b.LegalName, WebsiteDomain: b.WebsiteDomain, CountryCode: b.CountryCode,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantDetail(d))
}

func (h tenantHandlers) ListRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := h.identity.ListRoles(r.Context(), authctx.PrincipalFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.RoleList{Data: []tenantapi.Role{}}
	for _, x := range rows {
		out.Data = append(out.Data, tenantapi.Role{Id: x.ID, Key: x.Key, Name: x.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) ListMembers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.identity.ListMembers(r.Context(), authctx.PrincipalFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.MemberList{Data: []tenantapi.Member{}}
	for _, m := range rows {
		out.Data = append(out.Data, tenantapi.Member{Id: m.ID, UserId: m.UserID, Email: m.Email, DisplayName: m.DisplayName, Role: m.RoleKey, Status: m.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) UpdateMember(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	var b tenantapi.UpdateMember
	if !decode(w, r, &b) {
		return
	}
	m, err := h.identity.UpdateMember(r.Context(), authctx.PrincipalFrom(r.Context()), id, b.Role)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantapi.Member{Id: m.ID, UserId: m.UserID, Email: m.Email, DisplayName: m.DisplayName, Role: m.RoleKey, Status: m.Status})
}

func (h tenantHandlers) RemoveMember(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	if err := h.identity.RemoveMember(r.Context(), authctx.PrincipalFrom(r.Context()), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) ListInvitations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.identity.ListInvitations(r.Context(), authctx.PrincipalFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.InvitationList{Data: []tenantapi.Invitation{}}
	for _, i := range rows {
		out.Data = append(out.Data, tenantapi.Invitation{Id: i.ID, Email: i.Email, Role: i.RoleKey, ExpiresAt: i.ExpiresAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.CreateInvitation
	if !decode(w, r, &b) {
		return
	}
	i, err := h.identity.CreateInvitation(r.Context(), authctx.PrincipalFrom(r.Context()), string(b.Email), b.Role)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, tenantapi.Invitation{Id: i.ID, Email: i.Email, Role: i.RoleKey, ExpiresAt: i.ExpiresAt})
}

func (h tenantHandlers) RevokeInvitation(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	if err := h.identity.RevokeInvitation(r.Context(), authctx.PrincipalFrom(r.Context()), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) ListTenantAuditEvents(w http.ResponseWriter, r *http.Request, params tenantapi.ListTenantAuditEventsParams) {
	limit, before, err := pageParams(params.Limit, params.Cursor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := h.identity.ListAudit(r.Context(), authctx.PrincipalFrom(r.Context()), before, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.AuditEventList{Data: []tenantapi.AuditEvent{}}
	for _, e := range rows {
		out.Data = append(out.Data, tenantapi.AuditEvent{Id: e.ID, TenantId: e.TenantID, ActorType: e.ActorType, ActorId: e.ActorID, Action: e.Action, ResourceType: e.ResourceType, ResourceId: e.ResourceID, Outcome: e.Outcome, OccurredAt: e.OccurredAt})
	}
	if len(rows) == int(limit) {
		c := rows[len(rows)-1].ID.String()
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) ListCatalogModules(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.ListModules(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.ModuleList{Data: []tenantapi.Module{}}
	for _, m := range rows {
		out.Data = append(out.Data, tenantapi.Module{Id: m.ID, Key: m.Key, Name: m.Name, Description: m.Description, Status: m.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) ListCatalogProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.ListPublishedProducts(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.ProductList{Data: []tenantapi.Product{}}
	for _, p := range rows {
		out.Data = append(out.Data, tenantapi.Product{Id: p.ID, Key: p.Key, Kind: tenantapi.ProductKind(p.Kind), Name: p.Name, Description: p.Description, Status: p.Status, VersionId: p.VersionID, Version: int(p.Version), VersionStatus: p.VersionStatus, Modules: p.Modules})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) ListEntitlements(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.ListEntitlements(r.Context(), authctx.PrincipalFrom(r.Context()).TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.EntitlementList{Data: []tenantapi.Entitlement{}}
	for _, e := range rows {
		out.Data = append(out.Data, tenantapi.Entitlement{Key: e.Key, Status: e.Status, EffectiveUntil: e.EffectiveUntil})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.commercial.ListTenantSubscriptions(r.Context(), authctx.PrincipalFrom(r.Context()).TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.SubscriptionList{Data: []tenantapi.Subscription{}}
	for _, s := range rows {
		out.Data = append(out.Data, tenantapi.Subscription{Id: s.ID, TenantId: s.TenantID, PlanId: s.PlanID, PlanName: s.PlanName, Status: s.Status, Currency: s.Currency, CurrentPeriodEnd: s.CurrentPeriodEnd, CreatedAt: s.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// StreamEvents sends small refetch signals over SSE, with heartbeats to keep the connection open.
func (h tenantHandlers) StreamEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.DebugContext(r.Context(), "stream write deadline unavailable", "error", err)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()
	events, closeSub := h.Hub.Subscribe(r.Context(), authctx.PrincipalFrom(r.Context()).TenantID)
	defer closeSub()
	beat := time.NewTicker(25 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			if _, err := w.Write([]byte("event: " + e + "\ndata: {}\n\n")); err != nil {
				return
			}
		case <-beat.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
		}
		_ = rc.Flush()
	}
}

var _ tenantapi.ServerInterface = tenantHandlers{}
