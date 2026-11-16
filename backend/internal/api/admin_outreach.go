package api

import (
	"context"
	"net/http"

	"mspengine/internal/api/adminapi"
	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	outrepo "mspengine/internal/modules/outreach/repo"
)

// OutreachPolicy is Outreach's platform-level administration: jurisdiction rules and the global
// suppression list (D-09, D-10).
type OutreachPolicy interface {
	JurisdictionRules(ctx context.Context) ([]outrepo.AdminJurisdictionRulesRow, error)
	SetJurisdictionRule(ctx context.Context, a authctx.AdminPrincipal, country, rule string, note *string) error
	DeleteJurisdictionRule(ctx context.Context, a authctx.AdminPrincipal, country string) error
	AddGlobalSuppression(ctx context.Context, a authctx.AdminPrincipal, email, reason string) error
	GlobalSuppressionCount(ctx context.Context) (int, error)
}

func (h adminHandlers) outreach(w http.ResponseWriter, r *http.Request) OutreachPolicy {
	if h.OutreachPolicy == nil {
		writeError(w, r, apperr.New(http.StatusServiceUnavailable, "module_unavailable", "Outreach is not available"))
	}
	return h.OutreachPolicy
}

func (h adminHandlers) ListJurisdictionRules(w http.ResponseWriter, r *http.Request) {
	o := h.outreach(w, r)
	if o == nil {
		return
	}
	rows, err := o.JurisdictionRules(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := adminapi.JurisdictionRuleList{Data: []adminapi.JurisdictionRule{}}
	for _, x := range rows {
		out.Data = append(out.Data, adminapi.JurisdictionRule{CountryCode: x.CountryCode, Rule: adminapi.JurisdictionRuleRule(x.Rule), Note: x.Note, UpdatedBy: x.UpdatedBy, UpdatedAt: x.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h adminHandlers) SetJurisdictionRule(w http.ResponseWriter, r *http.Request, countryCode adminapi.CountryCode) {
	o := h.outreach(w, r)
	if o == nil {
		return
	}
	var b adminapi.JurisdictionRuleInput
	if !decode(w, r, &b) {
		return
	}
	if err := o.SetJurisdictionRule(r.Context(), authctx.AdminFrom(r.Context()), countryCode, string(b.Rule), b.Note); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) DeleteJurisdictionRule(w http.ResponseWriter, r *http.Request, countryCode adminapi.CountryCode) {
	o := h.outreach(w, r)
	if o == nil {
		return
	}
	if err := o.DeleteJurisdictionRule(r.Context(), authctx.AdminFrom(r.Context()), countryCode); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h adminHandlers) GetGlobalSuppressionCount(w http.ResponseWriter, r *http.Request) {
	o := h.outreach(w, r)
	if o == nil {
		return
	}
	n, err := o.GlobalSuppressionCount(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminapi.GlobalSuppressionCount{Count: n})
}

func (h adminHandlers) AddGlobalSuppression(w http.ResponseWriter, r *http.Request) {
	o := h.outreach(w, r)
	if o == nil {
		return
	}
	var b adminapi.GlobalSuppressionInput
	if !decode(w, r, &b) {
		return
	}
	if err := o.AddGlobalSuppression(r.Context(), authctx.AdminFrom(r.Context()), string(b.Email), string(b.Reason)); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
