package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/identity/repo"
	"mspengine/internal/platform/shared"
)

// ICP is a tenant's target customer profile, used by lead scoring.
type ICP struct {
	Name         string
	Industries   []string
	Countries    []string
	MinEmployees *int32
	MaxEmployees *int32
	Technologies []string
	Version      int32
}

// DefaultICP returns the tenant's profile, or found=false if none has been set.
func (s *Service) DefaultICP(ctx context.Context, tenantID uuid.UUID) (ICP, bool, error) {
	var out ICP
	found := false
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		r, err := repo.New(tx).GetDefaultICP(ctx, tenantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		out = ICP{Name: r.Name, Industries: r.Industries, Countries: r.Countries, MinEmployees: r.MinEmployees, MaxEmployees: r.MaxEmployees, Technologies: r.Technologies, Version: r.Version}
		return nil
	})
	return out, found, err
}

// SaveDefaultICP sets the tenant's profile. Codes must come from reference data.
func (s *Service) SaveDefaultICP(ctx context.Context, p authctx.Principal, in ICP) (ICP, error) {
	for i, c := range in.Countries {
		in.Countries[i] = strings.ToUpper(strings.TrimSpace(c))
	}
	for i, t := range in.Technologies {
		in.Technologies[i] = strings.ToLower(strings.TrimSpace(t))
	}
	if in.MinEmployees != nil && in.MaxEmployees != nil && *in.MinEmployees > *in.MaxEmployees {
		return ICP{}, apperr.Validation(apperr.FieldError{Field: "min_employees", Code: "min_above_max"})
	}
	if in.Name == "" {
		in.Name = "Default"
	}
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := shared.CheckCodes(ctx, tx, in.Countries, in.Industries); err != nil {
			return err
		}
		r, err := q.UpsertDefaultICP(ctx, repo.UpsertDefaultICPParams{TenantID: p.TenantID, Name: in.Name, Industries: nonNil(in.Industries), Countries: nonNil(in.Countries), MinEmployees: in.MinEmployees, MaxEmployees: in.MaxEmployees, Technologies: nonNil(in.Technologies), CreatedBy: &p.UserID})
		if err != nil {
			return err
		}
		in.Version = r.Version
		return audit.Record(ctx, tx, userEvent(p, "icp.updated", "icp", &r.ID, nil, map[string]any{"version": r.Version}))
	})
	return in, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
