package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/crypto"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/platform/audit"
	"mspengine/internal/platform/identity/repo"
)

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

func newSlug(name string) (string, error) {
	base := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(base) > 40 {
		base = strings.Trim(base[:40], "-")
	}
	if base == "" {
		base = "workspace"
	}
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base + "-" + hex.EncodeToString(b), nil
}

func userEvent(p authctx.Principal, action, resType string, resID *uuid.UUID, before, after any) audit.Event {
	t := p.TenantID
	return audit.Event{TenantID: &t, ActorType: "user", ActorID: &p.UserID, Action: action, ResourceType: resType, ResourceID: resID, Before: before, After: after}
}

// CreateTenant creates a workspace owned by the user and makes it the session's active tenant.
func (s *Service) CreateTenant(ctx context.Context, p authctx.Principal, name string) (repo.CreateTenantRow, error) {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		return repo.CreateTenantRow{}, apperr.Validation(apperr.FieldError{Field: "name", Code: "too_short"})
	}
	slug, err := newSlug(name)
	if err != nil {
		return repo.CreateTenantRow{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return repo.CreateTenantRow{}, err
	}
	var out repo.CreateTenantRow
	err = db.TenantTx(ctx, s.pools.App, id, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		out, err = q.CreateTenant(ctx, repo.CreateTenantParams{ID: id, Slug: slug, Name: name, DataRegion: dataRegion, OwnerUserID: &p.UserID})
		if err != nil {
			return err
		}
		if err := q.CreateTenantProfile(ctx, id); err != nil {
			return err
		}
		owner, err := q.GetRoleByKey(ctx, "owner")
		if err != nil {
			return err
		}
		if _, err := q.CreateMembership(ctx, repo.CreateMembershipParams{TenantID: id, UserID: p.UserID, RoleID: owner.ID}); err != nil {
			return err
		}
		if err := q.SetSessionTenant(ctx, repo.SetSessionTenantParams{ID: p.SessionID, TenantID: &id}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{TenantID: &id, ActorType: "user", ActorID: &p.UserID, Action: "tenant.created", ResourceType: "tenant", ResourceID: &id}); err != nil {
			return err
		}
		return events.Append(ctx, tx, events.Event{Type: "identity.tenant.created", TenantID: &id, SourceModule: "identity", AggregateType: "tenant", AggregateID: id}, map[string]any{"tenant_id": id})
	})
	return out, err
}

// SetActiveTenant switches workspace, but only to one the user actively belongs to.
func (s *Service) SetActiveTenant(ctx context.Context, p authctx.Principal, tenantID uuid.UUID) error {
	return db.TenantTx(ctx, s.pools.App, tenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if _, err := q.GetActiveMembership(ctx, repo.GetActiveMembershipParams{TenantID: tenantID, UserID: p.UserID}); errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		} else if err != nil {
			return err
		}
		if err := q.SetSessionTenant(ctx, repo.SetSessionTenantParams{ID: p.SessionID, TenantID: &tenantID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{TenantID: &tenantID, ActorType: "user", ActorID: &p.UserID, Action: "session.tenant_switched", ResourceType: "tenant", ResourceID: &tenantID})
	})
}

// ListMemberships returns the workspaces a user belongs to.
func (s *Service) ListMemberships(ctx context.Context, userID uuid.UUID) ([]repo.ListUserMembershipsRow, error) {
	var out []repo.ListUserMembershipsRow
	err := db.UserTx(ctx, s.pools.App, userID, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListUserMemberships(ctx, userID)
		return err
	})
	return out, err
}

// TenantDetail combines a tenant and its profile.
type TenantDetail struct {
	Tenant  repo.GetTenantRow
	Profile repo.GetTenantProfileRow
}

// GetTenant returns the caller's active tenant.
func (s *Service) GetTenant(ctx context.Context, p authctx.Principal) (TenantDetail, error) {
	var d TenantDetail
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		if d.Tenant, err = q.GetTenant(ctx, p.TenantID); err != nil {
			return err
		}
		d.Profile, err = q.GetTenantProfile(ctx, p.TenantID)
		return err
	})
	return d, err
}

// TenantUpdate holds optional changes.
type TenantUpdate struct {
	Name, Locale, Timezone, Currency, LegalName, WebsiteDomain, CountryCode *string
}

// UpdateTenant changes the active tenant's settings and profile.
func (s *Service) UpdateTenant(ctx context.Context, p authctx.Principal, u TenantUpdate) (TenantDetail, error) {
	if u.Timezone != nil {
		if _, err := time.LoadLocation(*u.Timezone); err != nil {
			return TenantDetail{}, apperr.Validation(apperr.FieldError{Field: "default_timezone", Code: "unknown_timezone"})
		}
	}
	if u.Currency != nil {
		c := strings.ToUpper(*u.Currency)
		u.Currency = &c
	}
	if u.CountryCode != nil {
		c := strings.ToUpper(*u.CountryCode)
		u.CountryCode = &c
	}
	var d TenantDetail
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.UpdateTenant(ctx, repo.UpdateTenantParams{ID: p.TenantID, Name: u.Name, DefaultLocale: u.Locale, DefaultTimezone: u.Timezone, ReportingCurrency: u.Currency}); err != nil {
			return err
		}
		if err := q.UpdateTenantProfile(ctx, repo.UpdateTenantProfileParams{TenantID: p.TenantID, LegalName: u.LegalName, WebsiteDomain: u.WebsiteDomain, CountryCode: u.CountryCode}); err != nil {
			return err
		}
		var err error
		if d.Tenant, err = q.GetTenant(ctx, p.TenantID); err != nil {
			return err
		}
		if d.Profile, err = q.GetTenantProfile(ctx, p.TenantID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userEvent(p, "tenant.updated", "tenant", &p.TenantID, nil, map[string]any{"name": d.Tenant.Name}))
	})
	return d, err
}

// ListRoles returns the roles available in the tenant.
func (s *Service) ListRoles(ctx context.Context, p authctx.Principal) ([]repo.ListRolesRow, error) {
	var out []repo.ListRolesRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListRoles(ctx)
		return err
	})
	return out, err
}

// ListMembers returns the tenant's members.
func (s *Service) ListMembers(ctx context.Context, p authctx.Principal) ([]repo.ListMembersRow, error) {
	var out []repo.ListMembersRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListMembers(ctx, p.TenantID)
		return err
	})
	return out, err
}

// canGrant checks that the actor holds every permission of the role, to stop privilege escalation.
func canGrant(ctx context.Context, q *repo.Queries, p authctx.Principal, roleKey string) (repo.GetRoleByKeyRow, error) {
	role, err := q.GetRoleByKey(ctx, roleKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return role, apperr.Validation(apperr.FieldError{Field: "role", Code: "unknown_role"})
	}
	if err != nil {
		return role, err
	}
	perms, err := q.ListRolePermissions(ctx, role.ID)
	if err != nil {
		return role, err
	}
	for _, perm := range perms {
		if !p.Permissions[perm] {
			return role, apperr.New(http.StatusForbidden, "role_exceeds_own_permissions", "You cannot grant a role with more access than your own")
		}
	}
	return role, nil
}

func lastOwner() *apperr.Error {
	return apperr.Conflict("last_owner", "A workspace must keep at least one owner")
}

// UpdateMember changes a member's role.
func (s *Service) UpdateMember(ctx context.Context, p authctx.Principal, id uuid.UUID, roleKey string) (repo.GetMemberRow, error) {
	var out repo.GetMemberRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.LockTenantOwners(ctx, p.TenantID); err != nil {
			return err
		}
		m, err := q.GetMember(ctx, repo.GetMemberParams{TenantID: p.TenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if m.RoleKey == "owner" && p.RoleKey != "owner" {
			return apperr.Forbidden()
		}
		role, err := canGrant(ctx, q, p, roleKey)
		if err != nil {
			return err
		}
		if m.RoleKey == "owner" && roleKey != "owner" {
			n, err := q.CountOwners(ctx, p.TenantID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return lastOwner()
			}
		}
		if err := q.UpdateMemberRole(ctx, repo.UpdateMemberRoleParams{TenantID: p.TenantID, ID: id, RoleID: role.ID}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userEvent(p, "member.role_changed", "membership", &id, map[string]any{"role": m.RoleKey}, map[string]any{"role": roleKey})); err != nil {
			return err
		}
		out, err = q.GetMember(ctx, repo.GetMemberParams{TenantID: p.TenantID, ID: id})
		return err
	})
	return out, err
}

// RemoveMember removes a member. Access ends at once, because each request re-checks membership.
func (s *Service) RemoveMember(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		if err := q.LockTenantOwners(ctx, p.TenantID); err != nil {
			return err
		}
		m, err := q.GetMember(ctx, repo.GetMemberParams{TenantID: p.TenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if m.RoleKey == "owner" {
			if p.RoleKey != "owner" {
				return apperr.Forbidden()
			}
			n, err := q.CountOwners(ctx, p.TenantID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return lastOwner()
			}
		}
		if err := q.RemoveMember(ctx, repo.RemoveMemberParams{TenantID: p.TenantID, ID: id}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userEvent(p, "member.removed", "membership", &id, map[string]any{"role": m.RoleKey}, nil))
	})
}

// Invitation is a created invitation.
type Invitation struct {
	ID        uuid.UUID
	Email     string
	RoleKey   string
	ExpiresAt time.Time
}

// inviteToken embeds the tenant ID, so acceptance can run under that tenant's RLS context
// instead of needing a role that can read every tenant's invitations.
func inviteToken(tenantID uuid.UUID) (string, []byte, error) {
	secret, _, err := crypto.NewToken()
	if err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(tenantID[:]) + "." + secret
	return token, crypto.HashToken(token), nil
}

func parseInviteTenant(token string) (uuid.UUID, bool) {
	head, _, ok := strings.Cut(token, ".")
	if !ok {
		return uuid.Nil, false
	}
	b, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return uuid.Nil, false
	}
	id, err := uuid.FromBytes(b)
	return id, err == nil
}

// CreateInvitation invites an email address and sends the link.
func (s *Service) CreateInvitation(ctx context.Context, p authctx.Principal, email, roleKey string) (Invitation, error) {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return Invitation{}, apperr.Validation(apperr.FieldError{Field: "email", Code: "invalid_email"})
	}
	token, hash, err := inviteToken(p.TenantID)
	if err != nil {
		return Invitation{}, err
	}
	var inv Invitation
	err = db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		role, err := canGrant(ctx, q, p, roleKey)
		if err != nil {
			return err
		}
		r, err := q.CreateInvitation(ctx, repo.CreateInvitationParams{TenantID: p.TenantID, Email: email, RoleID: role.ID, TokenHash: hash, InvitedBy: p.UserID, ExpiresAt: time.Now().Add(inviteTTL)})
		if isUnique(err) {
			return apperr.Conflict("invitation_exists", "This person already has a pending invitation")
		}
		if err != nil {
			return err
		}
		inv = Invitation{ID: r.ID, Email: email, RoleKey: roleKey, ExpiresAt: r.ExpiresAt}
		return audit.Record(ctx, tx, userEvent(p, "invitation.created", "invitation", &r.ID, nil, map[string]any{"role": roleKey}))
	})
	if err != nil {
		return Invitation{}, err
	}
	s.sendAsync(ctx, mailer.Message{To: email, Subject: "You have been invited to " + p.TenantName,
		Body: "You have been invited to join a workspace on MSP Growth Engine:\n\n" + s.baseURL + "/invite/" + token + "\n\nThis link expires in 7 days.\n"})
	return inv, nil
}

// ListInvitations returns pending invitations.
func (s *Service) ListInvitations(ctx context.Context, p authctx.Principal) ([]repo.ListInvitationsRow, error) {
	var out []repo.ListInvitationsRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListInvitations(ctx, p.TenantID)
		return err
	})
	return out, err
}

// RevokeInvitation cancels a pending invitation.
func (s *Service) RevokeInvitation(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		n, err := repo.New(tx).RevokeInvitation(ctx, repo.RevokeInvitationParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return audit.Record(ctx, tx, userEvent(p, "invitation.revoked", "invitation", &id, nil, nil))
	})
}

// AcceptInvitation joins the workspace. The signed-in email must match the invited email.
func (s *Service) AcceptInvitation(ctx context.Context, p authctx.Principal, token string) (repo.GetTenantRow, error) {
	var t repo.GetTenantRow
	tenantID, ok := parseInviteTenant(token)
	if !ok {
		return t, apperr.NotFound()
	}
	err := db.TenantTx(ctx, s.pools.App, tenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		inv, err := q.GetInvitationByHash(ctx, repo.GetInvitationByHashParams{TenantID: tenantID, TokenHash: crypto.HashToken(token)})
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if !strings.EqualFold(inv.Email, p.Email) {
			return apperr.NotFound()
		}
		if _, err := q.CreateMembership(ctx, repo.CreateMembershipParams{TenantID: tenantID, UserID: p.UserID, RoleID: inv.RoleID}); err != nil {
			return err
		}
		if err := q.AcceptInvitation(ctx, repo.AcceptInvitationParams{TenantID: tenantID, ID: inv.ID}); err != nil {
			return err
		}
		if err := q.SetSessionTenant(ctx, repo.SetSessionTenantParams{ID: p.SessionID, TenantID: &tenantID}); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{TenantID: &tenantID, ActorType: "user", ActorID: &p.UserID, Action: "member.joined", ResourceType: "invitation", ResourceID: &inv.ID}); err != nil {
			return err
		}
		t, err = q.GetTenant(ctx, tenantID)
		return err
	})
	return t, err
}

// ListAudit returns the tenant's audit events under tenant RLS.
func (s *Service) ListAudit(ctx context.Context, p authctx.Principal, before *uuid.UUID, limit int32) ([]AuditRow, error) {
	var out []AuditRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		rows, err := audit.List(ctx, tx, before, limit)
		if err != nil {
			return err
		}
		out = rows
		return nil
	})
	return out, err
}

// MembersWithPermission lists active members of the current tenant who hold a permission. It runs in
// the caller's transaction, under that tenant's context.
func MembersWithPermission(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, permission string) ([]uuid.UUID, error) {
	return repo.New(tx).MembersWithPermission(ctx, repo.MembersWithPermissionParams{TenantID: tenantID, Permission: permission})
}

// UserEmail returns a user's address, for delivering a notification.
func UserEmail(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (string, error) {
	u, err := repo.New(tx).GetUserByID(ctx, userID)
	return u.Email, err
}

// ReportingCurrency is the currency a tenant reports in. Modules use it to convert amounts and
// to label totals; they never read the tenants table themselves.
func (s *Service) ReportingCurrency(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var cur string
	err := db.TenantTx(ctx, s.pools.App, tenantID, uuid.Nil, func(tx pgx.Tx) error {
		t, err := repo.New(tx).GetTenant(ctx, tenantID)
		cur = t.ReportingCurrency
		return err
	})
	return cur, err
}
