package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/modules/proposals/domain"
	"mspengine/internal/modules/proposals/repo"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/pdf"
)

type deliverPayload struct {
	VersionID  uuid.UUID   `json:"version_id"`
	Recipients []Recipient `json:"recipients"`
	Message    string      `json:"message"`
}

// HandleDeliver renders the PDF once and emails each recipient their link. It works from the frozen
// snapshot, so a retry sends exactly what was issued.
func (s *Service) HandleDeliver(ctx context.Context, j jobs.Job) error {
	var pl deliverPayload
	if err := j.Decode(&pl); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("delivery without tenant"))
	}
	tenant := *j.TenantID
	var v repo.GetVersionByIDRow
	var links []repo.LinksForVersionRow
	err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if v, err = q.GetVersionByID(ctx, repo.GetVersionByIDParams{TenantID: tenant, ID: pl.VersionID}); err != nil {
			return err
		}
		links, err = q.LinksForVersion(ctx, repo.LinksForVersionParams{TenantID: tenant, VersionID: pl.VersionID})
		return err
	})
	if err != nil {
		return err
	}
	if v.CommercialSnapshot == nil {
		return jobs.Permanent(errors.New("version was never issued"))
	}
	if v.PdfFileID == nil && s.PDF != nil {
		body, err := s.renderPDF(ctx, v.CommercialSnapshot)
		if err != nil {
			return err // Retried. The proposal stays issued and viewable meanwhile.
		}
		fileID, err := s.Files.SaveGenerated(ctx, tenant, Module, "proposal.pdf", "application/pdf", body)
		if err != nil {
			return err
		}
		if err := s.systemTx(ctx, tenant, func(q *repo.Queries, _ pgx.Tx) error {
			return q.SetVersionPDF(ctx, repo.SetVersionPDFParams{TenantID: tenant, ID: v.ID, PdfFileID: &fileID})
		}); err != nil {
			return err
		}
	}
	names := map[string]string{}
	for _, r := range pl.Recipients {
		names[strings.ToLower(strings.TrimSpace(r.Email))] = r.Name
	}
	for _, l := range links {
		link := fmt.Sprintf("%s/p/%s", strings.TrimRight(s.BaseURL, "/"), s.linkToken(l.ID))
		body := fmt.Sprintf("Hello %s,\n\nA proposal has been prepared for you. View it here:\n%s\n", names[l.RecipientEmail], link)
		if pl.Message != "" {
			body += "\n" + pl.Message + "\n"
		}
		if err := s.Mailer.Send(ctx, mailer.Message{To: l.RecipientEmail, Subject: "Your proposal", Body: body}); err != nil {
			return fmt.Errorf("send proposal email: %w", err)
		}
	}
	return nil
}

const proposalHTML = `<!doctype html><html><head><meta charset="utf-8"><style>
body{font-family:sans-serif;font-size:11pt;color:#111} h1{font-size:18pt} table{border-collapse:collapse;width:100%} td,th{border:1px solid #ccc;padding:4px;text-align:left}
.num{text-align:right} footer{margin-top:24px;font-size:9pt;color:#444}
</style></head><body>
<h1>{{.Title}}</h1><p>Prepared for {{.Customer}}. Version {{.Version}}{{if .Valid}}, valid until {{.Valid}}{{end}}.</p>
{{range .Sections}}<h2>{{.Heading}}</h2><p>{{.Body}}</p>{{end}}
<h2>Pricing ({{.Totals.Currency}})</h2>
<table><tr><th>Service</th><th>Model</th><th class="num">Qty</th><th class="num">Monthly</th><th class="num">Setup</th></tr>
{{range .Totals.Lines}}<tr><td>{{.Name}}</td><td>{{.PricingModel}}</td><td class="num">{{.Quantity}}</td><td class="num">{{money .RecurringTotalMinor}}</td><td class="num">{{money .SetupFeeMinor}}</td></tr>{{end}}
</table>
<p>Setup total: {{money .Totals.SetupTotalMinor}}. Monthly total: {{money .Totals.MRRTotalMinor}}. Term: {{.Totals.TermMonths}} months. Total contract value: {{money .Totals.TCVMinor}}.</p>
<p>{{.Totals.TaxNote}}</p>
{{if .SLA}}<h2>Service levels</h2><p>{{.SLA}}</p>{{end}}{{if .Terms}}<h2>Terms</h2><p>{{.Terms}}</p>{{end}}
</body></html>`

func (s *Service) renderPDF(ctx context.Context, raw []byte) ([]byte, error) {
	view, err := snapshotView(raw)
	if err != nil {
		return nil, err
	}
	html, err := pdf.HTMLFuncs(proposalHTML, view, map[string]any{"money": money})
	if err != nil {
		return nil, err
	}
	return s.PDF.Render(ctx, html)
}

// money shows minor units with two decimals. Zero-decimal currencies are shown the same way until
// currency metadata is added, which the status document lists.
func money(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

// PublicView is what a share link shows: only the frozen snapshot.
type PublicView struct {
	domain.Snapshot
	Sections   []Section
	Superseded bool
	Expired    bool
	HasPDF     bool
}

func snapshotView(raw []byte) (PublicView, error) {
	var out PublicView
	if err := jsonUnmarshal(raw, &out.Snapshot); err != nil {
		return out, err
	}
	var c Content
	if err := jsonUnmarshal(out.Content, &c); err != nil {
		return out, err
	}
	out.Sections = c.Sections
	return out, nil
}

var errLinkGone = apperr.New(http.StatusNotFound, "link_unavailable", "This link has expired or been withdrawn")

// resolveLink finds a share link's tenant as the system role, then checks it under that tenant.
func (s *Service) resolveLink(ctx context.Context, token string) (repo.FindShareLinkRow, error) {
	var link repo.FindShareLinkRow
	err := pgx.BeginFunc(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		link, err = repo.New(tx).FindShareLink(ctx, tokenHash(token))
		return err
	})
	if err != nil || link.RevokedAt != nil || link.ExpiresAt.Before(time.Now()) {
		return link, errLinkGone
	}
	return link, nil
}

// Public returns the issued version behind a link, and records the view, at most once in ten
// minutes per link. The first view publishes an event.
func (s *Service) Public(ctx context.Context, token string) (PublicView, error) {
	link, err := s.resolveLink(ctx, token)
	if err != nil {
		return PublicView{}, err
	}
	var out PublicView
	err = s.systemTx(ctx, link.TenantID, func(q *repo.Queries, tx pgx.Tx) error {
		v, err := q.GetVersionByID(ctx, repo.GetVersionByIDParams{TenantID: link.TenantID, ID: link.VersionID})
		if err != nil || v.CommercialSnapshot == nil {
			return errLinkGone
		}
		if out, err = snapshotView(v.CommercialSnapshot); err != nil {
			return err
		}
		out.Superseded, out.Expired, out.HasPDF = v.Status == "superseded", v.Status == "expired", v.PdfFileID != nil
		recent, err := q.RecentView(ctx, repo.RecentViewParams{TenantID: link.TenantID, ShareLinkID: link.ID})
		if err != nil || recent {
			return err
		}
		if err := q.InsertView(ctx, repo.InsertViewParams{TenantID: link.TenantID, ShareLinkID: link.ID, IpHash: authctx.MetaFrom(ctx).IPHash}); err != nil {
			return err
		}
		pr, err := q.GetProposal(ctx, repo.GetProposalParams{TenantID: link.TenantID, ID: v.ProposalID})
		if err != nil {
			return err
		}
		if pr.Status != "issued" {
			return nil
		}
		if err := q.SetProposalStatus(ctx, repo.SetProposalStatusParams{TenantID: link.TenantID, ID: pr.ID, Status: "viewed"}); err != nil {
			return err
		}
		return emit(ctx, tx, link.TenantID, "proposal.viewed", "proposal", pr.ID, map[string]any{"proposal_id": pr.ID, "version_id": v.ID, "crm_company_id": pr.CrmCompanyID, "deal_id": pr.DealID, "title": pr.Title})
	})
	return out, err
}

// PublicPDF returns the PDF behind a link.
func (s *Service) PublicPDF(ctx context.Context, token string) (files.Download, error) {
	link, err := s.resolveLink(ctx, token)
	if err != nil {
		return files.Download{}, err
	}
	var fileID *uuid.UUID
	if err := s.systemTx(ctx, link.TenantID, func(q *repo.Queries, _ pgx.Tx) error {
		v, err := q.GetVersionByID(ctx, repo.GetVersionByIDParams{TenantID: link.TenantID, ID: link.VersionID})
		fileID = v.PdfFileID
		return err
	}); err != nil {
		return files.Download{}, errLinkGone
	}
	if fileID == nil {
		return files.Download{}, apperr.Conflict("pdf_not_ready", "The PDF is not ready yet")
	}
	d, _, err := s.Files.ReadForTenant(ctx, link.TenantID, *fileID)
	return d, err
}
