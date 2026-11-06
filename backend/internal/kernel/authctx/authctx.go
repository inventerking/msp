// Package authctx carries the verified caller through a request.
package authctx

import (
	"context"

	"github.com/google/uuid"
)

type key int

const (
	metaKey key = iota
	principalKey
	adminKey
)

// Meta describes the request for logs and audit records.
type Meta struct {
	RequestID uuid.UUID
	IPHash    []byte
	IP        string
	UserAgent string
}

// Principal is a verified MSP user. TenantID always comes from the server-side session.
type Principal struct {
	UserID      uuid.UUID
	SessionID   uuid.UUID
	Email       string
	DisplayName string
	HasMFA      bool
	MFAVerified bool
	TenantID    uuid.UUID
	RoleKey     string
	TenantName  string
	TenantSlug  string
	TenantState string
	Permissions map[string]bool
}

// AdminPrincipal is a verified platform administrator.
type AdminPrincipal struct {
	AdminID     uuid.UUID
	SessionID   uuid.UUID
	Email       string
	DisplayName string
	Status      string
	Roles       []string
	HasMFA      bool
	MFAVerified bool
}

// HasRole reports whether the admin holds a role.
func (a AdminPrincipal) HasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func WithMeta(ctx context.Context, m Meta) context.Context { return context.WithValue(ctx, metaKey, m) }

func MetaFrom(ctx context.Context) Meta {
	m, _ := ctx.Value(metaKey).(Meta)
	return m
}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

func PrincipalFrom(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey).(Principal)
	return p
}

func WithAdmin(ctx context.Context, a AdminPrincipal) context.Context {
	return context.WithValue(ctx, adminKey, a)
}

func AdminFrom(ctx context.Context) AdminPrincipal {
	a, _ := ctx.Value(adminKey).(AdminPrincipal)
	return a
}
