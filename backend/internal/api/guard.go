package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
)

const recentAuthWindow = 15 * time.Minute

func (s *Server) policyFor(r *http.Request) *policy {
	return s.policies[routeKey(r.Method, chi.RouteContext(r.Context()).RoutePattern())]
}

// guard authenticates and authorizes tenant-domain requests, in the documented order: session,
// membership, entitlement, then permission. Tenant ID only ever comes from the session.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pol := s.policyFor(r)
		if pol == nil {
			writeError(w, r, apperr.NotFound()) // Default deny: an undeclared route serves nothing.
			return
		}
		ctx, err := s.authorizeTenant(r, pol)
		if err != nil {
			writeError(w, r, err)
			return
		}
		r = r.WithContext(ctx)
		if err := validateRequest(r, pol); err != nil {
			writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorizeTenant(r *http.Request, pol *policy) (context.Context, error) {
	ctx := r.Context()
	if pol.Access == "public" {
		return ctx, nil
	}
	c, err := r.Cookie(s.sessionCookie)
	if err != nil || c.Value == "" {
		return nil, apperr.Unauthenticated()
	}
	sess, err := s.identity.ResolveSession(ctx, c.Value)
	if err != nil {
		return nil, err
	}
	if unsafeMethod(r.Method) && !s.identity.Keys().VerifyCSRF(sess.ID.String(), r.Header.Get("X-CSRF-Token")) {
		return nil, apperr.New(http.StatusForbidden, "csrf_failed", "Please refresh the page and try again")
	}
	if sess.HasMFA && !sess.MFAVerified && !pol.MFAExempt {
		return nil, apperr.New(http.StatusUnauthorized, "mfa_required", "Two-step verification is required")
	}
	if pol.RecentAuth && time.Since(sess.LastAuthAt) > recentAuthWindow {
		return nil, apperr.New(http.StatusUnauthorized, "recent_auth_required", "Please confirm your password to continue")
	}
	p := authctx.Principal{UserID: sess.UserID, SessionID: sess.ID, Email: sess.Email, DisplayName: sess.DisplayName, HasMFA: sess.HasMFA, MFAVerified: sess.MFAVerified}
	if sess.ActiveTenant != nil {
		p.TenantID = *sess.ActiveTenant
	}
	if pol.Access == "session" {
		return authctx.WithPrincipal(ctx, p), nil
	}
	noTenant := apperr.New(http.StatusForbidden, "no_active_tenant", "Choose a workspace to continue")
	if sess.ActiveTenant == nil {
		return nil, noTenant
	}
	access, err := s.identity.Access(ctx, sess.UserID, *sess.ActiveTenant)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return nil, noTenant // Removed from the workspace; access ended on this request.
	}
	if err != nil {
		return nil, err
	}
	if access.TenantState != "active" {
		return nil, apperr.New(http.StatusForbidden, "tenant_suspended", "This workspace is suspended")
	}
	p.RoleKey, p.TenantName, p.TenantSlug, p.TenantState, p.Permissions = access.RoleKey, access.TenantName, access.TenantSlug, access.TenantState, access.Permissions
	if pol.Module != "" {
		status, err := s.commercial.ModuleStatus(ctx, p.TenantID, pol.Module)
		if err != nil {
			return nil, err // Fail closed.
		}
		if status != "active" {
			e := apperr.New(http.StatusForbidden, "module_not_entitled", "This module is not active for your workspace")
			e.ModuleKey, e.ModuleStatus = pol.Module, status
			return nil, e
		}
	}
	if pol.Permission != "" && !p.Permissions[pol.Permission] {
		return nil, apperr.Forbidden()
	}
	return authctx.WithPrincipal(ctx, p), nil
}

// adminGuard authenticates platform administrators. It reads a different cookie and different
// tables from the tenant guard, so a tenant session can never pass it.
func (s *Server) adminGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pol := s.policyFor(r)
		if pol == nil {
			writeError(w, r, apperr.NotFound())
			return
		}
		ctx, err := s.authorizeAdmin(r, pol)
		if err != nil {
			writeError(w, r, err)
			return
		}
		r = r.WithContext(ctx)
		if err := validateRequest(r, pol); err != nil {
			writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorizeAdmin(r *http.Request, pol *policy) (context.Context, error) {
	ctx := r.Context()
	if pol.Access == "public" {
		return ctx, nil
	}
	c, err := r.Cookie(s.adminCookie)
	if err != nil || c.Value == "" {
		return nil, apperr.Unauthenticated()
	}
	a, err := s.identity.ResolveAdminSession(ctx, c.Value)
	if err != nil {
		return nil, err
	}
	if unsafeMethod(r.Method) && !s.identity.Keys().VerifyCSRF(a.SessionID.String(), r.Header.Get("X-CSRF-Token")) {
		return nil, apperr.New(http.StatusForbidden, "csrf_failed", "Please refresh the page and try again")
	}
	if !a.MFAVerified && !pol.MFAExempt {
		return nil, apperr.New(http.StatusUnauthorized, "mfa_required", "Two-step verification is required")
	}
	allowed := false
	for _, role := range pol.AdminRoles {
		allowed = allowed || a.HasRole(role)
	}
	if !allowed {
		return nil, apperr.New(http.StatusForbidden, "insufficient_role", "Your role does not allow this")
	}
	return authctx.WithAdmin(ctx, a), nil
}

func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// validateRequest checks the request against the contract, so handlers receive only valid shapes.
func validateRequest(r *http.Request, pol *policy) error {
	if pol.RawBody {
		return nil // File and webhook bodies are bytes. Their handlers apply their own limits.
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	params := map[string]string{}
	rc := chi.RouteContext(r.Context())
	for i, k := range rc.URLParams.Keys {
		if k != "*" && i < len(rc.URLParams.Values) {
			params[k] = rc.URLParams.Values[i]
		}
	}
	err := openapi3filter.ValidateRequest(r.Context(), &openapi3filter.RequestValidationInput{
		Request: r, PathParams: params, Route: pol.route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	})
	if err == nil {
		return nil
	}
	var re *openapi3filter.RequestError
	if !errors.As(err, &re) {
		return apperr.New(http.StatusBadRequest, "malformed_request", "The request could not be read")
	}
	field := "body"
	if re.Parameter != nil {
		field = re.Parameter.Name
	}
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		if ptr := se.JSONPointer(); len(ptr) > 0 {
			field = strings.Join(ptr, ".")
		}
	}
	return apperr.Validation(apperr.FieldError{Field: field, Code: "invalid"})
}
