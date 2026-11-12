package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/proposals/domain"
	"mspengine/internal/modules/proposals/repo"
	"mspengine/internal/platform/audit"
)

// Catalog lists the tenant's services.
func (s *Service) Catalog(ctx context.Context, p authctx.Principal) ([]repo.ListCatalogRow, error) {
	var out []repo.ListCatalogRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListCatalog(ctx, p.TenantID)
		return err
	})
	return out, err
}

// CatalogInput describes a service.
type CatalogInput struct {
	Name, PricingModel, Currency string
	Description                  *string
	UnitPrice, SetupFee          int64
}

// AddCatalogItem adds a service. Its price is copied into each proposal, so a later change here
// never alters a proposal already written.
func (s *Service) AddCatalogItem(ctx context.Context, p authctx.Principal, in CatalogInput) (repo.GetCatalogItemRow, error) {
	if _, err := domain.Calculate(in.Currency, 1, []domain.Line{{PricingModel: in.PricingModel, Quantity: 1, UnitPriceMinor: in.UnitPrice, SetupFeeMinor: in.SetupFee}}); err != nil {
		return repo.GetCatalogItemRow{}, apperr.Validation(apperr.FieldError{Field: "unit_price_minor", Code: "invalid"})
	}
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		if id, err = q.InsertCatalogItem(ctx, repo.InsertCatalogItemParams{TenantID: p.TenantID, Name: in.Name, Description: in.Description, PricingModel: in.PricingModel, Currency: in.Currency, UnitPriceMinor: in.UnitPrice, SetupFeeMinor: in.SetupFee}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.catalog_item_added", "catalog_item", &id, map[string]any{"name": in.Name, "unit_price_minor": in.UnitPrice, "setup_fee_minor": in.SetupFee, "currency": in.Currency}))
	})
	if err != nil {
		return repo.GetCatalogItemRow{}, err
	}
	return s.catalogItem(ctx, p, id)
}

func (s *Service) catalogItem(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetCatalogItemRow, error) {
	var out repo.GetCatalogItemRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetCatalogItem(ctx, repo.GetCatalogItemParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// CatalogPatch changes a service.
type CatalogPatch struct {
	Name, Description, Status *string
	UnitPrice, SetupFee       *int64
}

// UpdateCatalogItem changes a service. Price changes are audited.
func (s *Service) UpdateCatalogItem(ctx context.Context, p authctx.Principal, id uuid.UUID, in CatalogPatch) (repo.GetCatalogItemRow, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		before, err := q.GetCatalogItem(ctx, repo.GetCatalogItemParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if _, err := q.UpdateCatalogItem(ctx, repo.UpdateCatalogItemParams{TenantID: p.TenantID, ID: id, Name: in.Name, Description: in.Description, Status: in.Status, UnitPriceMinor: in.UnitPrice, SetupFeeMinor: in.SetupFee}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.catalog_item_changed", "catalog_item", &id, map[string]any{
			"before": map[string]any{"unit_price_minor": before.UnitPriceMinor, "setup_fee_minor": before.SetupFeeMinor}, "unit_price_minor": in.UnitPrice, "setup_fee_minor": in.SetupFee, "status": in.Status}))
	})
	if err != nil {
		return repo.GetCatalogItemRow{}, err
	}
	return s.catalogItem(ctx, p, id)
}

// TextTemplates lists SLA and terms texts.
func (s *Service) TextTemplates(ctx context.Context, p authctx.Principal) ([]repo.ListTextTemplatesRow, error) {
	var out []repo.ListTextTemplatesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListTextTemplates(ctx, p.TenantID)
		return err
	})
	return out, err
}

// AddTextTemplate adds an SLA or terms text. The text belongs to the tenant; the product does not
// warrant it.
func (s *Service) AddTextTemplate(ctx context.Context, p authctx.Principal, kind, name, body string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		switch kind {
		case "sla":
			id, err = q.InsertSLA(ctx, repo.InsertSLAParams{TenantID: p.TenantID, Name: name, Body: body})
		case "terms":
			id, err = q.InsertTerms(ctx, repo.InsertTermsParams{TenantID: p.TenantID, Name: name, Body: body})
		default:
			return apperr.Validation(apperr.FieldError{Field: "kind", Code: "invalid"})
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.text_template_added", kind+"_template", &id, map[string]any{"name": name}))
	})
	return id, err
}

// UpdateTextTemplate changes an SLA or terms text, raising its version.
func (s *Service) UpdateTextTemplate(ctx context.Context, p authctx.Principal, kind string, id uuid.UUID, name, body *string) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var n int64
		var err error
		switch kind {
		case "sla":
			n, err = q.UpdateSLA(ctx, repo.UpdateSLAParams{TenantID: p.TenantID, ID: id, Name: name, Body: body})
		case "terms":
			n, err = q.UpdateTerms(ctx, repo.UpdateTermsParams{TenantID: p.TenantID, ID: id, Name: name, Body: body})
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.text_template_changed", kind+"_template", &id, nil))
	})
}

// Section is one heading and body of proposal text.
type Section struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// Templates lists proposal templates.
func (s *Service) Templates(ctx context.Context, p authctx.Principal) ([]repo.ListProposalTemplatesRow, error) {
	var out []repo.ListProposalTemplatesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListProposalTemplates(ctx, p.TenantID)
		return err
	})
	return out, err
}

// AddTemplate adds a proposal template.
func (s *Service) AddTemplate(ctx context.Context, p authctx.Principal, name string, sections []Section, sla, terms *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := checkTexts(ctx, q, p.TenantID, sla, terms); err != nil {
			return err
		}
		var err error
		if id, err = q.InsertProposalTemplate(ctx, repo.InsertProposalTemplateParams{TenantID: p.TenantID, Name: name, Sections: mustJSON(sections), DefaultSlaID: sla, DefaultTermsID: terms}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.template_added", "proposal_template", &id, map[string]any{"name": name}))
	})
	return id, err
}

func checkTexts(ctx context.Context, q *repo.Queries, tenant uuid.UUID, sla, terms *uuid.UUID) error {
	if sla != nil {
		if _, err := q.GetSLA(ctx, repo.GetSLAParams{TenantID: tenant, ID: *sla}); err != nil {
			return apperr.Validation(apperr.FieldError{Field: "default_sla_id", Code: "unknown_template"})
		}
	}
	if terms != nil {
		if _, err := q.GetTerms(ctx, repo.GetTermsParams{TenantID: tenant, ID: *terms}); err != nil {
			return apperr.Validation(apperr.FieldError{Field: "default_terms_id", Code: "unknown_template"})
		}
	}
	return nil
}

// UpdateTemplate changes a proposal template.
func (s *Service) UpdateTemplate(ctx context.Context, p authctx.Principal, id uuid.UUID, name *string, sections []Section, setSLA bool, sla *uuid.UUID, setTerms bool, terms *uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if err := checkTexts(ctx, q, p.TenantID, sla, terms); err != nil {
			return err
		}
		var secs []byte
		if sections != nil {
			secs = mustJSON(sections)
		}
		n, err := q.UpdateProposalTemplate(ctx, repo.UpdateProposalTemplateParams{TenantID: p.TenantID, ID: id, Name: name, Sections: secs, SetSla: setSLA, DefaultSlaID: sla, SetTerms: setTerms, DefaultTermsID: terms})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		return err
	})
}
