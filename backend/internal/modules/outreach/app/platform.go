package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/modules/outreach/domain"
	"mspengine/internal/modules/outreach/repo"
	"mspengine/internal/platform/audit"
)

// Jurisdiction rules and the global suppression list are platform data, kept as data so that
// legal review can change them without a release (D-09, D-10). A country with no rule is blocked.

func (s *Service) platformTx(ctx context.Context, fn func(q *repo.Queries, tx pgx.Tx) error) error {
	return db.Tx(ctx, s.Pools.Platform, func(tx pgx.Tx) error { return fn(repo.New(tx), tx) })
}

func adminAudit(a authctx.AdminPrincipal, action string, after any) audit.Event {
	return audit.Event{ActorType: "platform_admin", ActorID: &a.AdminID, Action: action, ResourceType: "outreach_policy", After: after}
}

// JurisdictionRules lists the rules.
func (s *Service) JurisdictionRules(ctx context.Context) ([]repo.AdminJurisdictionRulesRow, error) {
	var out []repo.AdminJurisdictionRulesRow
	err := s.platformTx(ctx, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.AdminJurisdictionRules(ctx)
		return err
	})
	return out, err
}

// SetJurisdictionRule sets a country's rule.
func (s *Service) SetJurisdictionRule(ctx context.Context, a authctx.AdminPrincipal, country, rule string, note *string) error {
	country = strings.ToUpper(country)
	if rule != domain.OptOutAllowed && rule != domain.ConsentRequired && rule != domain.Blocked {
		return apperr.Validation(apperr.FieldError{Field: "rule", Code: "invalid"})
	}
	return s.platformTx(ctx, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.UpsertJurisdictionRule(ctx, repo.UpsertJurisdictionRuleParams{CountryCode: country, Rule: rule, Note: note, UpdatedBy: &a.AdminID}); err != nil {
			if strings.Contains(err.Error(), "23503") {
				return apperr.Validation(apperr.FieldError{Field: "country_code", Code: "unknown_country"})
			}
			return err
		}
		return audit.Record(ctx, tx, adminAudit(a, "outreach.jurisdiction_rule_set", map[string]any{"country_code": country, "rule": rule, "note": note}))
	})
}

// DeleteJurisdictionRule removes a rule, which blocks the country.
func (s *Service) DeleteJurisdictionRule(ctx context.Context, a authctx.AdminPrincipal, country string) error {
	country = strings.ToUpper(country)
	return s.platformTx(ctx, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteJurisdictionRule(ctx, country)
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, adminAudit(a, "outreach.jurisdiction_rule_removed", map[string]any{"country_code": country}))
	})
}

// AddGlobalSuppression stops every tenant from emailing an address. Only a keyed hash is stored,
// and the audit record holds no address.
func (s *Service) AddGlobalSuppression(ctx context.Context, a authctx.AdminPrincipal, email, reason string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if reason != "legal_request" && reason != "abuse" && reason != "complaint" {
		return apperr.Validation(apperr.FieldError{Field: "reason", Code: "invalid"})
	}
	return s.platformTx(ctx, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.InsertGlobalSuppression(ctx, repo.InsertGlobalSuppressionParams{EmailHmac: s.Shared.GlobalEmailKey(email), Reason: reason}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminAudit(a, "outreach.global_suppression_added", map[string]any{"reason": reason}))
	})
}

// GlobalSuppressionCount is how many addresses are on the global list.
func (s *Service) GlobalSuppressionCount(ctx context.Context) (int, error) {
	var n int32
	err := s.platformTx(ctx, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		n, err = q.CountGlobalSuppressions(ctx)
		return err
	})
	return int(n), err
}
