// Package archtest enforces module boundaries, so they do not depend on discipline alone.
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const modulePrefix = "mspengine/internal/"

// imports maps each package directory under internal to its internal imports.
func imports(t *testing.T) map[string][]string {
	t.Helper()
	root := filepath.Join("..")
	out := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(filepath.Dir(path))
		pkg = strings.TrimPrefix(pkg, "../")
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, modulePrefix) {
				out[pkg] = append(out[pkg], strings.TrimPrefix(p, modulePrefix))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// violations applies the import rules from the backend architecture document.
func violations(g map[string][]string) []string {
	var bad []string
	for pkg, deps := range g {
		for _, dep := range deps {
			switch {
			case strings.HasPrefix(pkg, "kernel/") && (strings.HasPrefix(dep, "platform/") || strings.HasPrefix(dep, "modules/") || strings.HasPrefix(dep, "api")):
				bad = append(bad, pkg+" -> "+dep+": kernel must not depend on higher layers")
			case strings.HasPrefix(pkg, "platform/") && (strings.HasPrefix(dep, "modules/") || strings.HasPrefix(dep, "api")):
				bad = append(bad, pkg+" -> "+dep+": platform must not depend on modules")
			case strings.HasPrefix(pkg, "platform/") && strings.HasPrefix(dep, "platform/"):
				// A service may use another's package, but never its generated repository,
				// except its own. Audit is the shared recording service.
				if strings.Contains(dep, "/repo") && strings.Split(pkg, "/")[1] != strings.Split(dep, "/")[1] && dep != "platform/audit/repo" {
					bad = append(bad, pkg+" -> "+dep+": repositories are private to their owner")
				}
			case strings.HasPrefix(pkg, "modules/") && strings.HasPrefix(dep, "modules/"):
				if strings.Split(pkg, "/")[1] != strings.Split(dep, "/")[1] && !strings.HasSuffix(dep, "/contract") {
					bad = append(bad, pkg+" -> "+dep+": modules may only import another module's contract")
				}
			case strings.HasPrefix(pkg, "modules/") && strings.Contains(dep, "/repo"):
				bad = append(bad, pkg+" -> "+dep+": repositories are private to their owner")
			}
			// Sales Intelligence is read-only towards the rest of the system: it takes facts from
			// events and may not reach any module, not even a contract, nor billing.
			if strings.HasPrefix(pkg, "modules/salesintel") && (strings.HasPrefix(dep, "modules/") && !strings.HasPrefix(dep, "modules/salesintel") || dep == "platform/commercial") {
				bad = append(bad, pkg+" -> "+dep+": sales intelligence has no write path into other modules or billing")
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// moduleCycles reports cycles in the module dependency graph.
func moduleCycles(g map[string][]string) []string {
	mod := func(p string) string {
		if !strings.HasPrefix(p, "modules/") {
			return ""
		}
		return strings.Split(p, "/")[1]
	}
	edges := map[string]map[string]bool{}
	for pkg, deps := range g {
		from := mod(pkg)
		if from == "" {
			continue
		}
		for _, d := range deps {
			if to := mod(d); to != "" && to != from {
				if edges[from] == nil {
					edges[from] = map[string]bool{}
				}
				edges[from][to] = true
			}
		}
	}
	var cycles []string
	state := map[string]int{}
	var visit func(n string, path []string)
	visit = func(n string, path []string) {
		state[n] = 1
		for next := range edges[n] {
			switch state[next] {
			case 1:
				cycles = append(cycles, strings.Join(append(path, n, next), " -> "))
			case 0:
				visit(next, append(path, n))
			}
		}
		state[n] = 2
	}
	for n := range edges {
		if state[n] == 0 {
			visit(n, nil)
		}
	}
	return cycles
}

func TestImportRules(t *testing.T) {
	g := imports(t)
	if len(g) == 0 {
		t.Fatal("found no packages; the test is not looking in the right place")
	}
	for _, v := range violations(g) {
		t.Error(v)
	}
	for _, c := range moduleCycles(g) {
		t.Error("module cycle: " + c)
	}
}

// The rules must actually catch faults, or a passing test proves nothing.
func TestRulesCatchPlantedFaults(t *testing.T) {
	cases := map[string]map[string][]string{
		"module imports another module's internals": {"modules/crm": {"modules/outreach"}},
		"module reaches a repository":               {"modules/crm": {"platform/identity/repo"}},
		"kernel imports platform":                   {"kernel/db": {"platform/audit"}},
		"platform imports a module":                 {"platform/identity": {"modules/crm"}},
		"platform reaches another repository":       {"platform/identity": {"platform/commercial/repo"}},
	}
	for name, g := range cases {
		if len(violations(g)) == 0 {
			t.Errorf("not caught: %s", name)
		}
	}
	if len(violations(map[string][]string{"modules/salesintel/app": {"modules/crm/contract"}})) == 0 {
		t.Error("not caught: sales intelligence importing a CRM contract")
	}
	if len(violations(map[string][]string{"modules/salesintel/app": {"platform/commercial"}})) == 0 {
		t.Error("not caught: sales intelligence importing billing")
	}
	if moduleCycles(map[string][]string{"modules/a": {"modules/b/contract"}, "modules/b": {"modules/a/contract"}}) == nil {
		t.Error("not caught: contract-level module cycle")
	}
	if len(violations(map[string][]string{"modules/a": {"modules/b/contract", "kernel/db"}})) != 0 {
		t.Error("false positive on an allowed import")
	}
}

var schemaRef = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+([a-z_]+)\.[a-z_]+`)

func wrongSchemas(sql string, owned ...string) []string {
	var bad []string
	for _, m := range schemaRef.FindAllStringSubmatch(sql, -1) {
		if strings.EqualFold(m[1], "sqlc") {
			continue // sqlc.arg(...) is a query parameter, not a schema.
		}
		ok := false
		for _, o := range owned {
			ok = ok || strings.EqualFold(m[1], o)
		}
		if !ok {
			bad = append(bad, m[1])
		}
	}
	return bad
}

// A repository's SQL may touch only the schema its owner owns.
func TestSQLStaysInOwnSchema(t *testing.T) {
	owners := map[string]string{
		"../platform/identity/queries.sql":         "identity",
		"../platform/commercial/queries.sql":       "platform",
		"../platform/audit/queries.sql":            "audit",
		"../kernel/jobs/queries.sql":               "jobs",
		"../platform/shared/queries.sql":           "shared",
		"../platform/integration/queries.sql":      "integration",
		"../platform/notification/queries.sql":     "notification",
		"../platform/files/queries.sql":            "files",
		"../platform/trust/queries.sql":            "platform",
		"../modules/leadgeneration/queries.sql":    "lead_generation",
		"../modules/competitorintel/queries.sql":   "competitor_intelligence",
		"../modules/crm/queries.sql":               "crm",
		"../modules/proposals/queries.sql":         "proposals",
		"../modules/itassessment/queries.sql":      "it_assessment",
		"../modules/customerexpansion/queries.sql": "customer_expansion",
		"../modules/outreach/queries.sql":          "outreach",
		"../modules/visitorintel/queries.sql":      "visitor_intelligence",
		"../modules/chatbot/queries.sql":           "chatbot",
		"../modules/salesintel/queries.sql":        "sales_intelligence",
	}
	for path, schema := range owners {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bad := wrongSchemas(string(b), schema); len(bad) > 0 {
			t.Errorf("%s touches schemas %v, but owns only %s", path, bad, schema)
		}
	}
	if len(wrongSchemas("SELECT 1 FROM crm.deals", "identity")) == 0 {
		t.Error("planted wrong-schema SQL was not caught")
	}
}

// Plain SET would survive on a pooled connection and leak tenant context to the next request.
func TestNoPlainSetInApplicationCode(t *testing.T) {
	plainSet := regexp.MustCompile(`(?i)["` + "`" + `]\s*SET\s+(SESSION\s+)?app\.`)
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if plainSet.Match(b) {
			t.Errorf("%s uses a plain SET for tenant context", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
