// Package trust publishes legal documents and takes abuse reports. The documents' text is written
// by counsel (D-19), not by this platform: it versions and shows the text, records acceptance, and
// never claims a document was reviewed unless an admin marks it so.
package trust

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/trust/repo"
)

// Document kinds.
var kinds = map[string]bool{"terms": true, "privacy": true, "dpa": true, "acceptable_use": true, "subprocessors": true}

// Service implements legal documents and abuse reports.
type Service struct{ pools *db.Pools }

// New creates the service.
func New(pools *db.Pools) *Service { return &Service{pools: pools} }

func adminEvent(a authctx.AdminPrincipal, tenant *uuid.UUID, action, resType string, id *uuid.UUID, after any) audit.Event {
	return audit.Event{TenantID: tenant, ActorType: "platform_admin", ActorID: &a.AdminID, Action: action, ResourceType: resType, ResourceID: id, After: after}
}

// Documents lists every version, for admins.
func (s *Service) Documents(ctx context.Context) ([]repo.ListDocumentsRow, error) {
	var out []repo.ListDocumentsRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListDocuments(ctx)
		return err
	})
	return out, err
}

// Document returns one version, for admins.
func (s *Service) Document(ctx context.Context, id uuid.UUID) (repo.GetDocumentRow, error) {
	var out repo.GetDocumentRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).GetDocument(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	})
	return out, err
}

// DocumentInput is a draft's content.
type DocumentInput struct {
	Kind, Title, Body  string
	RequiresAcceptance bool
}

func (in DocumentInput) check() error {
	switch {
	case !kinds[in.Kind]:
		return apperr.Validation(apperr.FieldError{Field: "kind", Code: "invalid"})
	case strings.TrimSpace(in.Title) == "" || len(in.Title) > 200:
		return apperr.Validation(apperr.FieldError{Field: "title", Code: "invalid_length"})
	case strings.TrimSpace(in.Body) == "" || len(in.Body) > 200000:
		return apperr.Validation(apperr.FieldError{Field: "body", Code: "invalid_length"})
	}
	return nil
}

// CreateDraft starts a new version of a document.
func (s *Service) CreateDraft(ctx context.Context, in DocumentInput) (uuid.UUID, error) {
	if err := in.check(); err != nil {
		return uuid.Nil, err
	}
	a := authctx.AdminFrom(ctx)
	var id uuid.UUID
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		v, err := q.NextDocumentVersion(ctx, in.Kind)
		if err != nil {
			return err
		}
		if id, err = q.InsertDocument(ctx, repo.InsertDocumentParams{Kind: in.Kind, Version: v, Title: strings.TrimSpace(in.Title), Body: in.Body,
			RequiresAcceptance: in.RequiresAcceptance, CreatedBy: &a.AdminID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(a, nil, "legal.document_drafted", "legal_document", &id, map[string]any{"kind": in.Kind, "version": v}))
	})
	return id, err
}

// UpdateDraft changes a draft. Published versions never change; a new version replaces them.
func (s *Service) UpdateDraft(ctx context.Context, id uuid.UUID, in DocumentInput) error {
	if err := in.check(); err != nil {
		return err
	}
	a := authctx.AdminFrom(ctx)
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		n, err := repo.New(tx).UpdateDraft(ctx, repo.UpdateDraftParams{ID: id, Title: strings.TrimSpace(in.Title), Body: in.Body, RequiresAcceptance: in.RequiresAcceptance})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.Conflict("not_a_draft", "Only a draft can be changed. Create a new version instead")
		}
		return audit.Record(ctx, tx, adminEvent(a, nil, "legal.document_edited", "legal_document", &id, nil))
	})
}

// Publish makes a draft the current version of its kind and retires the previous one. Whether
// counsel reviewed it is recorded as the admin states it; the platform cannot know.
func (s *Service) Publish(ctx context.Context, id uuid.UUID, counselReviewed bool) error {
	a := authctx.AdminFrom(ctx)
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		q := repo.New(tx)
		d, err := q.GetDocument(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if d.Status != "draft" {
			return apperr.Conflict("not_a_draft", "Only a draft can be published")
		}
		if err := q.RetirePublished(ctx, d.Kind); err != nil {
			return err
		}
		if _, err := q.PublishDocument(ctx, repo.PublishDocumentParams{ID: id, PublishedBy: &a.AdminID, CounselReviewed: counselReviewed}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminEvent(a, nil, "legal.document_published", "legal_document", &id, map[string]any{"kind": d.Kind, "version": d.Version, "counsel_reviewed": counselReviewed}))
	})
}

// Published returns the current version of each document, for anyone.
func (s *Service) Published(ctx context.Context) ([]repo.PublishedDocumentsRow, error) {
	var out []repo.PublishedDocumentsRow
	err := db.Tx(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).PublishedDocuments(ctx)
		return err
	})
	return out, err
}

// ReportInput is an abuse report from anyone.
type ReportInput struct {
	Category, Details, SubjectURL, ReporterEmail string
}

var categories = map[string]bool{"spam": true, "phishing": true, "malware": true, "privacy": true, "harassment": true, "unauthorized_scanning": true, "other": true}

// Report records an abuse report. It reveals nothing about any tenant to the reporter.
func (s *Service) Report(ctx context.Context, in ReportInput) (uuid.UUID, error) {
	details := strings.TrimSpace(in.Details)
	switch {
	case !categories[in.Category]:
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "category", Code: "invalid"})
	case details == "" || len(details) > 5000:
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "details", Code: "invalid_length"})
	}
	var subject, email *string
	if u := strings.TrimSpace(in.SubjectURL); u != "" {
		parsed, err := url.Parse(u)
		if err != nil || len(u) > 2000 || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "subject_url", Code: "invalid_url"})
		}
		subject = &u
	}
	if e := strings.TrimSpace(in.ReporterEmail); e != "" {
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e || len(e) > 254 {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "reporter_email", Code: "invalid_email"})
		}
		email = &e
	}
	var id uuid.UUID
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		if id, err = repo.New(tx).InsertAbuseReport(ctx, repo.InsertAbuseReportParams{Category: in.Category, SubjectUrl: subject, Details: details, ReporterEmail: email}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{ActorType: "public", Action: "abuse.reported", ResourceType: "abuse_report", ResourceID: &id, After: map[string]any{"category": in.Category}})
	})
	return id, err
}

// Reports lists abuse reports for admins.
func (s *Service) Reports(ctx context.Context, status *string) ([]repo.ListAbuseReportsRow, error) {
	var out []repo.ListAbuseReportsRow
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListAbuseReports(ctx, status)
		return err
	})
	return out, err
}

// OpenReports counts reports waiting for review, for monitoring.
func (s *Service) OpenReports(ctx context.Context) (int, error) {
	var n int32
	err := db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		var err error
		n, err = repo.New(tx).OpenAbuseReports(ctx)
		return err
	})
	return int(n), err
}

// ReviewInput is an admin's decision on a report. Suspending the tenant is a separate admin action.
type ReviewInput struct {
	Status   string
	Decision *string
	Tenant   *uuid.UUID
}

// Review records a decision on a report.
func (s *Service) Review(ctx context.Context, id uuid.UUID, in ReviewInput) error {
	if in.Status != "reviewing" && in.Status != "actioned" && in.Status != "dismissed" {
		return apperr.Validation(apperr.FieldError{Field: "status", Code: "invalid"})
	}
	if (in.Status == "actioned" || in.Status == "dismissed") && (in.Decision == nil || strings.TrimSpace(*in.Decision) == "") {
		return apperr.Validation(apperr.FieldError{Field: "decision", Code: "required"})
	}
	if in.Decision != nil && len(*in.Decision) > 2000 {
		return apperr.Validation(apperr.FieldError{Field: "decision", Code: "too_long"})
	}
	a := authctx.AdminFrom(ctx)
	return db.Tx(ctx, s.pools.Platform, func(tx pgx.Tx) error {
		n, err := repo.New(tx).ReviewAbuseReport(ctx, repo.ReviewAbuseReportParams{ID: id, Status: in.Status, Decision: in.Decision, TargetTenantID: in.Tenant, ReviewedBy: &a.AdminID})
		if isFK(err) {
			return apperr.Validation(apperr.FieldError{Field: "target_tenant_id", Code: "unknown_tenant"})
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, adminEvent(a, in.Tenant, "abuse.reviewed", "abuse_report", &id, map[string]any{"status": in.Status}))
	})
}

func isFK(err error) bool {
	var pe interface{ SQLState() string }
	return errors.As(err, &pe) && pe.SQLState() == "23503"
}
