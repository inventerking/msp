package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/modules/proposals/contract"
	"mspengine/internal/modules/proposals/repo"
	"mspengine/internal/platform/audit"
)

// Signer sends a document to an e-signature provider. Results come back by webhook.
type Signer interface {
	Key() string
	Send(ctx context.Context, requestID uuid.UUID, recipients []Recipient, pdf []byte) (envelopeID string, err error)
}

// FakeSigner sends nothing outside the system (D-05). Its envelope ID is derived from the request,
// so tests and demos can post the webhook a real provider would send.
type FakeSigner struct{}

// Key names the provider.
func (*FakeSigner) Key() string { return "fake_esign" }

// Send returns an envelope ID.
func (*FakeSigner) Send(_ context.Context, requestID uuid.UUID, _ []Recipient, _ []byte) (string, error) {
	return "env-" + requestID.String(), nil
}

// RequestSignature sends an issued version for signature, once per idempotency key.
func (s *Service) RequestSignature(ctx context.Context, p authctx.Principal, proposal uuid.UUID, version int32, recipients []Recipient, key string) (repo.GetSignatureRequestRow, error) {
	view, err := s.Version(ctx, p, proposal, version)
	if err != nil {
		return repo.GetSignatureRequestRow{}, err
	}
	if view.Status != "issued" {
		return repo.GetSignatureRequestRow{}, apperr.Conflict("not_issued", "Only the issued version can be sent for signature")
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = q.InsertSignatureRequest(ctx, repo.InsertSignatureRequestParams{TenantID: p.TenantID, VersionID: view.ID, ProviderKey: s.Signer.Key(), IdempotencyKey: key})
		if errors.Is(err, pgx.ErrNoRows) {
			id, err = q.FindSignatureRequestByKey(ctx, repo.FindSignatureRequestByKeyParams{TenantID: p.TenantID, IdempotencyKey: key})
			return err
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "proposals.signature_requested", "proposal", &proposal, map[string]any{"version": version, "recipients": len(recipients)}))
	})
	if err != nil {
		return repo.GetSignatureRequestRow{}, err
	}
	req, err := s.signatureRequest(ctx, p, id)
	if err != nil || req.ProviderEnvelopeID != nil {
		return req, err // Already sent.
	}
	var pdfBody []byte
	if view.PdfFileID != nil {
		if d, _, err := s.Files.ReadForTenant(ctx, p.TenantID, *view.PdfFileID); err == nil {
			pdfBody = d.Body
		}
	}
	envelope, err := s.Signer.Send(ctx, id, recipients, pdfBody)
	if err != nil {
		_ = s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
			return q.SetSignatureStatus(ctx, repo.SetSignatureStatusParams{TenantID: p.TenantID, ID: id, Status: "failed"})
		})
		return repo.GetSignatureRequestRow{}, apperr.New(502, "esign_failed", "The signature service did not accept the document. The proposal is still issued and viewable")
	}
	if err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		return q.SetEnvelope(ctx, repo.SetEnvelopeParams{TenantID: p.TenantID, ID: id, ProviderEnvelopeID: &envelope})
	}); err != nil {
		return repo.GetSignatureRequestRow{}, err
	}
	return s.signatureRequest(ctx, p, id)
}

func (s *Service) signatureRequest(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetSignatureRequestRow, error) {
	var out repo.GetSignatureRequestRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetSignatureRequest(ctx, repo.GetSignatureRequestParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// SignatureEvent is what the fake provider posts. A real adapter maps its own format to this.
type SignatureEvent struct {
	EventID    string `json:"event_id"`
	EnvelopeID string `json:"envelope_id"`
	Type       string `json:"type"` // sent, viewed, signed, declined, voided
}

var signatureStatus = map[string]string{"sent": "sent", "viewed": "viewed", "signed": "signed", "declined": "declined", "voided": "voided"}

// HandleSignatureWebhook applies a verified provider event. The envelope tells it which tenant's
// request this is. Each provider event applies once, so a replayed webhook signs nothing twice.
func (s *Service) HandleSignatureWebhook(ctx context.Context, _ *uuid.UUID, payload []byte) error {
	var ev SignatureEvent
	if err := json.Unmarshal(payload, &ev); err != nil || ev.EventID == "" || ev.EnvelopeID == "" {
		return jobs.Permanent(errors.New("unreadable signature event"))
	}
	status, ok := signatureStatus[ev.Type]
	if !ok {
		return nil // An event type we do not track.
	}
	var found repo.FindEnvelopeRow
	if err := pgx.BeginFunc(ctx, s.Pools.System, func(tx pgx.Tx) error {
		var err error
		found, err = repo.New(tx).FindEnvelope(ctx, repo.FindEnvelopeParams{ProviderKey: s.Signer.Key(), ProviderEnvelopeID: &ev.EnvelopeID})
		return err
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return jobs.Permanent(errors.New("unknown envelope"))
		}
		return err
	}
	tenant := found.TenantID
	return s.systemTx(ctx, tenant, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.InsertSignatureEvent(ctx, repo.InsertSignatureEventParams{TenantID: tenant, SignatureRequestID: found.ID, ProviderEventID: ev.EventID, EventType: ev.Type, Detail: payload})
		if err != nil || n == 0 {
			return err // n == 0: already applied.
		}
		req, err := q.GetSignatureRequest(ctx, repo.GetSignatureRequestParams{TenantID: tenant, ID: found.ID})
		if err != nil {
			return err
		}
		if req.Status == "signed" || req.Status == "declined" || req.Status == "voided" {
			return nil // Final already.
		}
		if err := q.SetSignatureStatus(ctx, repo.SetSignatureStatusParams{TenantID: tenant, ID: found.ID, Status: status}); err != nil {
			return err
		}
		if status != "signed" && status != "declined" {
			return nil
		}
		v, err := q.GetVersionByID(ctx, repo.GetVersionByIDParams{TenantID: tenant, ID: req.VersionID})
		if err != nil {
			return err
		}
		pr, err := q.GetProposal(ctx, repo.GetProposalParams{TenantID: tenant, ID: v.ProposalID})
		if err != nil {
			return err
		}
		if err := q.SetProposalStatus(ctx, repo.SetProposalStatusParams{TenantID: tenant, ID: pr.ID, Status: status}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{TenantID: &tenant, ActorType: "system", Action: "proposals." + status, ResourceType: "proposal", ResourceID: &pr.ID, After: map[string]any{"version": v.Version, "provider_event_id": ev.EventID}}); err != nil {
			return err
		}
		return emit(ctx, tx, tenant, "proposal."+status, "proposal", pr.ID, map[string]any{"proposal_id": pr.ID, "version_id": v.ID, "crm_company_id": pr.CrmCompanyID, "deal_id": pr.DealID, "title": pr.Title,
			"currency": v.Currency, "mrr_total_minor": v.MrrTotalMinor, "setup_total_minor": v.SetupTotalMinor, "term_months": v.TermMonths})
	})
}

// GetIssuedSummary implements the Proposals contract.
func (s *Service) GetIssuedSummary(ctx context.Context, tenantID, versionID uuid.UUID) (contract.IssuedSummary, error) {
	var out contract.IssuedSummary
	err := s.systemTx(ctx, tenantID, func(q *repo.Queries, _ pgx.Tx) error {
		v, err := q.GetVersionByID(ctx, repo.GetVersionByIDParams{TenantID: tenantID, ID: versionID})
		if err != nil {
			return notFound(err)
		}
		if v.CommercialSnapshot == nil {
			return apperr.NotFound()
		}
		pr, err := q.GetProposal(ctx, repo.GetProposalParams{TenantID: tenantID, ID: v.ProposalID})
		if err != nil {
			return err
		}
		out = contract.IssuedSummary{VersionID: v.ID, ProposalID: pr.ID, CRMCompanyID: pr.CrmCompanyID, DealID: pr.DealID, Currency: v.Currency, SetupTotalMinor: v.SetupTotalMinor,
			MRRTotalMinor: v.MrrTotalMinor, TermMonths: v.TermMonths, TCVMinor: v.TcvMinor, Signed: pr.Status == "signed"}
		return nil
	})
	return out, err
}

// Jobs are the module's job handlers.
func (s *Service) Jobs() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobDeliver: s.HandleDeliver}
}

var _ contract.Issued = (*Service)(nil)
