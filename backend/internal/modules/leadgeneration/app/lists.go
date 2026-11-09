package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/leadgeneration/repo"
	"mspengine/internal/platform/audit"
)

// Lists returns the tenant's prospect lists.
func (s *Service) Lists(ctx context.Context, p authctx.Principal) ([]repo.ListListsRow, error) {
	var out []repo.ListListsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListLists(ctx, p.TenantID)
		return err
	})
	return out, err
}

func uniqueViolation(err error) bool {
	return err != nil && (containsCode(err, "23505"))
}

// CreateList adds a list.
func (s *Service) CreateList(ctx context.Context, p authctx.Principal, name string, description *string) (repo.InsertListRow, error) {
	var out repo.InsertListRow
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		out, err = q.InsertList(ctx, repo.InsertListParams{TenantID: p.TenantID, Name: name, Description: description, CreatedBy: uid(p)})
		if uniqueViolation(err) {
			return apperr.Conflict("list_name_taken", "A list with this name already exists")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.list_created", "prospect_list", &out.ID, map[string]any{"name": name}))
	})
	return out, err
}

// UpdateList renames a list or changes its description.
func (s *Service) UpdateList(ctx context.Context, p authctx.Principal, id uuid.UUID, name, description *string) (repo.UpdateListRow, error) {
	var out repo.UpdateListRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.UpdateList(ctx, repo.UpdateListParams{TenantID: p.TenantID, ID: id, Name: name, Description: description})
		if uniqueViolation(err) {
			return apperr.Conflict("list_name_taken", "A list with this name already exists")
		}
		return notFound(err)
	})
	return out, err
}

// DeleteList removes a list. Its prospects stay.
func (s *Service) DeleteList(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteList(ctx, repo.DeleteListParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userAudit(p, "lead.list_deleted", "prospect_list", &id, nil))
	})
}

// AddMembers adds prospects to a list. Every prospect must belong to the tenant.
func (s *Service) AddMembers(ctx context.Context, p authctx.Principal, listID uuid.UUID, ids []uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		return addMembers(ctx, q, p.TenantID, uid(p), listID, ids)
	})
}

func addMembers(ctx context.Context, q *repo.Queries, tenant uuid.UUID, by *uuid.UUID, listID uuid.UUID, ids []uuid.UUID) error {
	if _, err := q.GetList(ctx, repo.GetListParams{TenantID: tenant, ID: listID}); err != nil {
		return notFound(err)
	}
	n, err := q.CountLiveProspects(ctx, repo.CountLiveProspectsParams{TenantID: tenant, Ids: ids})
	if err != nil {
		return err
	}
	if int(n) != len(unique(ids)) {
		return apperr.Validation(apperr.FieldError{Field: "prospect_ids", Code: "unknown_prospect"})
	}
	for _, id := range ids {
		if err := q.AddListMember(ctx, repo.AddListMemberParams{TenantID: tenant, ListID: listID, ProspectID: id, AddedBy: by}); err != nil {
			return err
		}
	}
	return nil
}

func unique(ids []uuid.UUID) map[uuid.UUID]bool {
	m := map[uuid.UUID]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// RemoveMembers removes prospects from a list.
func (s *Service) RemoveMembers(ctx context.Context, p authctx.Principal, listID uuid.UUID, ids []uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if _, err := q.GetList(ctx, repo.GetListParams{TenantID: p.TenantID, ID: listID}); err != nil {
			return notFound(err)
		}
		for _, id := range ids {
			if _, err := q.RemoveListMember(ctx, repo.RemoveListMemberParams{TenantID: p.TenantID, ListID: listID, ProspectID: id}); err != nil {
				return err
			}
		}
		return nil
	})
}
