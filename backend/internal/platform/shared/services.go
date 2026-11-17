package shared

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/shared/repo"
)

// TenantService is one of the services a tenant sells. Proposals price them and Customer Expansion
// records which customers have them.
type TenantService struct {
	ID          uuid.UUID
	Name        string
	Description *string
	Status      string
	CategoryKey *string
	CreatedAt   time.Time
}

// ServiceInput creates or changes a tenant service. Nil fields are left unchanged on update.
type ServiceInput struct {
	Name        *string
	Description *string
	SetDesc     bool
	CategoryKey *string
	SetCategory bool
	Status      *string
}

// ServiceCategories returns the platform's service categories.
func (s *Service) ServiceCategories(ctx context.Context) ([]repo.ListServiceCategoriesRow, error) {
	var out []repo.ListServiceCategoriesRow
	err := pgx.BeginFunc(ctx, s.pools.App, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListServiceCategories(ctx)
		return err
	})
	return out, err
}

// TenantServices lists the tenant's services, retired ones included so history still has names.
func (s *Service) TenantServices(ctx context.Context, tenantID uuid.UUID) ([]TenantService, error) {
	var out []TenantService
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		rows, err := repo.New(tx).ListTenantServices(ctx, tenantID)
		for _, r := range rows {
			out = append(out, TenantService(r))
		}
		return err
	})
	return out, err
}

func (s *Service) checkService(ctx context.Context, q *repo.Queries, in ServiceInput) error {
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 120 {
			return apperr.Validation(apperr.FieldError{Field: "name", Code: "invalid_length"})
		}
		in.Name = &n
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "retired" {
		return apperr.Validation(apperr.FieldError{Field: "status", Code: "invalid_status"})
	}
	if in.CategoryKey != nil {
		ok, err := q.ServiceCategoryExists(ctx, *in.CategoryKey)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Validation(apperr.FieldError{Field: "category_key", Code: "unknown_category"})
		}
	}
	return nil
}

// CreateTenantService adds a service. Names are unique within the tenant.
func (s *Service) CreateTenantService(ctx context.Context, p authctx.Principal, in ServiceInput) (TenantService, error) {
	if in.Name == nil {
		return TenantService{}, apperr.Validation(apperr.FieldError{Field: "name", Code: "required"})
	}
	name := strings.TrimSpace(*in.Name)
	in.Name = &name
	var out TenantService
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := s.checkService(ctx, q, in); err != nil {
			return err
		}
		id, err := q.InsertTenantService(ctx, repo.InsertTenantServiceParams{TenantID: p.TenantID, CategoryKey: in.CategoryKey, Name: name, Description: in.Description})
		if isUnique(err) {
			return apperr.Conflict("service_exists", "A service with this name already exists")
		}
		if err != nil {
			return err
		}
		r, err := q.GetTenantService(ctx, repo.GetTenantServiceParams{TenantID: p.TenantID, ID: id})
		out = TenantService(r)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "shared.service_created", ResourceType: "tenant_service", ResourceID: &id, After: map[string]any{"name": name}})
	})
	return out, err
}

// UpdateTenantService renames, recategorises or retires a service. Retiring keeps it, so contracts
// and coverage that name it stay readable.
func (s *Service) UpdateTenantService(ctx context.Context, p authctx.Principal, id uuid.UUID, in ServiceInput) (TenantService, error) {
	var out TenantService
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := s.checkService(ctx, q, in); err != nil {
			return err
		}
		if in.Name != nil {
			n := strings.TrimSpace(*in.Name)
			in.Name = &n
		}
		n, err := q.UpdateTenantService(ctx, repo.UpdateTenantServiceParams{TenantID: p.TenantID, ID: id, Name: in.Name, SetDescription: in.SetDesc, Description: in.Description,
			SetCategory: in.SetCategory, CategoryKey: in.CategoryKey, Status: in.Status})
		if isUnique(err) {
			return apperr.Conflict("service_exists", "A service with this name already exists")
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		r, err := q.GetTenantService(ctx, repo.GetTenantServiceParams{TenantID: p.TenantID, ID: id})
		out = TenantService(r)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &p.TenantID, ActorType: "user", ActorID: &p.UserID, Action: "shared.service_updated", ResourceType: "tenant_service", ResourceID: &id,
			After: map[string]any{"name": in.Name, "status": in.Status, "category_key": in.CategoryKey}})
	})
	return out, err
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
