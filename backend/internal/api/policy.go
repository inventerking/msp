package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
)

// policy is the access rule an operation declares in the OpenAPI contract. The guard enforces it,
// so authorization cannot drift from the contract and no route can be missing a rule.
type policy struct {
	Access     string // public, session, tenant or admin
	Permission string
	Module     string
	MFAExempt  bool
	RecentAuth bool
	AdminRoles []string
	route      *routers.Route
}

// loadPolicies reads every operation's rules. A missing or unknown rule is a startup failure.
func loadPolicies(spec *openapi3.T, base string) (map[string]*policy, error) {
	out := map[string]*policy{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			key := method + " " + base + path
			p := &policy{route: &routers.Route{Spec: spec, Path: path, PathItem: item, Method: method, Operation: op}}
			if err := ext(op, "x-access", &p.Access); err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			switch p.Access {
			case "public", "session", "tenant", "admin":
			default:
				return nil, fmt.Errorf("%s: declares no valid x-access", key)
			}
			for name, target := range map[string]any{"x-permission": &p.Permission, "x-module": &p.Module, "x-mfa-exempt": &p.MFAExempt, "x-recent-auth": &p.RecentAuth, "x-admin-roles": &p.AdminRoles} {
				if err := ext(op, name, target); err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
			}
			if p.Access == "admin" && len(p.AdminRoles) == 0 {
				return nil, fmt.Errorf("%s: admin route declares no x-admin-roles", key)
			}
			if p.Access == "public" && (p.Permission != "" || p.Module != "") {
				return nil, fmt.Errorf("%s: public route declares a permission", key)
			}
			if p.Module != "" && p.Permission == "" {
				return nil, fmt.Errorf("%s: module route declares no x-permission", key)
			}
			out[key] = p
		}
	}
	return out, nil
}

func ext(op *openapi3.Operation, name string, target any) error {
	v, ok := op.Extensions[name]
	if !ok {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

func routeKey(method, pattern string) string {
	return strings.ToUpper(method) + " " + pattern
}
