package app

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/outreach/repo"
	"mspengine/internal/platform/audit"
)

// Messages lists sent and queued messages, optionally for one campaign.
func (s *Service) Messages(ctx context.Context, p authctx.Principal, campaign *uuid.UUID) ([]repo.ListMessagesRow, error) {
	var out []repo.ListMessagesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListMessages(ctx, repo.ListMessagesParams{TenantID: p.TenantID, CampaignID: campaign})
		return err
	})
	return out, err
}

// Message returns one message as it was sent.
func (s *Service) Message(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetMessageRow, error) {
	var out repo.GetMessageRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetMessage(ctx, repo.GetMessageParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// Replies lists replies.
func (s *Service) Replies(ctx context.Context, p authctx.Principal) ([]repo.ListRepliesRow, error) {
	var out []repo.ListRepliesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListReplies(ctx, p.TenantID)
		return err
	})
	return out, err
}

// ReplyClasses are the labels a person can give a reply.
var ReplyClasses = map[string]bool{"interested": true, "not_interested": true, "out_of_office": true, "unsubscribe_request": true, "other": true}

// ClassifyReply labels a reply. Labelling it "unsubscribe_request" opts the person out.
func (s *Service) ClassifyReply(ctx context.Context, p authctx.Principal, id uuid.UUID, class string) error {
	if !ReplyClasses[class] {
		return apperr.Validation(apperr.FieldError{Field: "classification", Code: "invalid"})
	}
	var contact *uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.ClassifyReply(ctx, repo.ClassifyReplyParams{TenantID: p.TenantID, ID: id, Classification: class})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		if class == "unsubscribe_request" {
			for _, r := range must(q.ListReplies(ctx, p.TenantID)) {
				if r.ID == id {
					contact = r.ContactID
				}
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.reply_classified", "reply", &id, map[string]any{"classification": class}))
	})
	if err != nil || contact == nil {
		return err
	}
	people, err := s.Shared.ContactsByID(ctx, p.TenantID, []uuid.UUID{*contact})
	if err != nil {
		return err
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		return s.optOut(ctx, q, tx, p.TenantID, *contact, people[*contact].Email, "unsubscribe", "reply", nil)
	})
}

func must[T any](v T, _ error) T { return v }

// Suppressions lists the tenant's suppression entries. Only hashes are kept, so addresses are not shown.
func (s *Service) Suppressions(ctx context.Context, p authctx.Principal) ([]repo.ListSuppressionsRow, error) {
	var out []repo.ListSuppressionsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListSuppressions(ctx, p.TenantID)
		return err
	})
	return out, err
}

// Suppress adds an address to the tenant's list by hand.
func (s *Service) Suppress(ctx context.Context, p authctx.Principal, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return apperr.Validation(apperr.FieldError{Field: "email", Code: "invalid_email"})
	}
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := q.Suppress(ctx, repo.SuppressParams{TenantID: p.TenantID, EmailHash: s.Shared.EmailKey(p.TenantID, email), Reason: "manual", Source: "user", CreatedBy: uid(p)}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.suppressed", "suppression", nil, nil))
	})
}

// Unsuppress removes an entry the tenant added by hand. Opt-outs, bounces and complaints stay.
func (s *Service) Unsuppress(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		for _, r := range must(q.ListSuppressions(ctx, p.TenantID)) {
			if r.ID == id && r.Reason != "manual" {
				return apperr.Conflict("not_removable", "Opt-outs, bounces and complaints cannot be removed")
			}
		}
		n, err := q.DeleteSuppression(ctx, repo.DeleteSuppressionParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "outreach.unsuppressed", "suppression", &id, nil))
	})
}
