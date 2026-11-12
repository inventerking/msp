package api_test

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"mspengine/internal/kernel/jobs"
	itaapp "mspengine/internal/modules/itassessment/app"
)

func assessTenant(t *testing.T, name string) (*Client, string) {
	t.Helper()
	c, tenant := crmTenant(t, name)
	adminClient(t, "super_admin").must(200, http.MethodPost, "/api/v1/admin/tenants/"+tenant+"/entitlements/it_assessment/activate", map[string]any{"reason": "phase 4 test"})
	return c, tenant
}

func templateID(t *testing.T, c *Client, tier string) string {
	t.Helper()
	for _, x := range c.must(200, http.MethodGet, "/api/v1/it-assessments/templates", nil)["data"].([]any) {
		if x.(map[string]any)["tier"] == tier {
			return x.(map[string]any)["id"].(string)
		}
	}
	t.Fatalf("no %s template", tier)
	return ""
}

func authorize(t *testing.T, c *Client, domainName, tier string, verification any) map[string]any {
	t.Helper()
	body := map[string]any{"domain": domainName, "tier": tier, "authorizer_name": "Dana Owner", "authorizer_email": "dana@" + domainName, "authorizer_role": "Director", "attested": true}
	if verification != nil {
		body["domain_verification_id"] = verification
	}
	return c.must(201, http.MethodPost, "/api/v1/it-assessments/authorizations", body)
}

func runAssessment(t *testing.T, c *Client, auth, template string) map[string]any {
	t.Helper()
	a := c.must(202, http.MethodPost, "/api/v1/it-assessments/assessments", map[string]any{"authorization_id": auth, "template_id": template})
	work(t, jobs.QueueAssessment)
	return c.must(200, http.MethodGet, "/api/v1/it-assessments/assessments/"+a["id"].(string), nil)
}

func TestPassiveAssessmentFindsAndScores(t *testing.T) {
	c, _ := assessTenant(t, "Assess Tenant")
	auth := authorize(t, c, "weak-dental.example", "passive", nil)
	if auth["method"] != "attestation" || auth["terms_version"] == "" {
		t.Fatalf("authorization: %v", auth)
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/it-assessments/assessments", map[string]any{"authorization_id": auth["id"], "template_id": templateID(t, c, "exposure")})
	got := runAssessment(t, c, auth["id"].(string), templateID(t, c, "passive"))
	if got["status"] != "completed" || got["risk_level"] != "high" || got["score"].(float64) >= 40 || got["notice"] == "" {
		t.Fatalf("assessment: %v", asJSON(got))
	}
	text := asJSON(got)
	if !strings.Contains(text, "SPF allows any sender") || strings.Contains(text, "web.security_headers") {
		t.Fatalf("findings or scope wrong: %s", text)
	}
	for _, f := range got["findings"].([]any) {
		m := f.(map[string]any)
		if m["check_key"] == "email.dkim_common_selectors" && m["classification"] != "informational" {
			t.Fatalf("DKIM absence presented as a problem: %v", m)
		}
	}
	c.wantCode(429, "assessment_rate_limited", http.MethodPost, "/api/v1/it-assessments/assessments", map[string]any{"authorization_id": auth["id"], "template_id": templateID(t, c, "passive")})

	// The result reaches the CRM timeline, on the company the assessment created.
	dispatch(t)
	if !strings.Contains(asJSON(c.must(200, http.MethodGet, "/api/v1/crm/activities?crm_company_id="+got["crm_company_id"].(string), nil)), "IT assessment completed (score") {
		t.Fatal("assessment not on the CRM timeline")
	}

	rep := c.must(202, http.MethodPost, "/api/v1/it-assessments/assessments/"+got["id"].(string)+"/reports", nil)
	work(t, jobs.QueueDefault)
	s, _, pdf := c.raw(http.MethodGet, "/api/v1/it-assessments/reports/"+rep["id"].(string)+"/pdf", nil)
	if s != 200 || !bytes.Contains(pdf, []byte("does not test for or confirm vulnerabilities")) {
		t.Fatalf("report %d: %.200s", s, pdf)
	}
}

func TestExposureNeedsVerifiedControl(t *testing.T) {
	c, _ := assessTenant(t, "Verify Tenant")
	fake := graph.Assessments.Prober.(*itaapp.FakeProber)
	fake.AutoVerify = false
	defer func() { fake.AutoVerify = true }()
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/it-assessments/authorizations", map[string]any{"domain": "strong-legal.example", "tier": "exposure", "authorizer_name": "Dana", "authorizer_email": "d@strong-legal.example", "attested": true})
	v := c.must(201, http.MethodPost, "/api/v1/it-assessments/domain-verifications", map[string]any{"domain": "strong-legal.example", "method": "dns_txt"})
	token := regexp.MustCompile(`msp-verify=([0-9a-f]+)`).FindStringSubmatch(v["instructions"].(string))[1]
	if got := c.must(200, http.MethodPost, "/api/v1/it-assessments/domain-verifications/"+v["id"].(string)+"/check", nil); got["status"] != "pending" {
		t.Fatalf("verified without the record: %v", got)
	}
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/it-assessments/authorizations", map[string]any{"domain": "strong-legal.example", "tier": "exposure", "authorizer_name": "Dana", "authorizer_email": "d@strong-legal.example", "attested": true, "domain_verification_id": v["id"]})
	fake.SetTXT("_msp-verify.strong-legal.example", []string{"msp-verify=" + token})
	if got := c.must(200, http.MethodPost, "/api/v1/it-assessments/domain-verifications/"+v["id"].(string)+"/check", nil); got["status"] != "verified" {
		t.Fatalf("not verified: %v", got)
	}
	// A verification proves control of its own domain only.
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/it-assessments/authorizations", map[string]any{"domain": "other-legal.example", "tier": "exposure", "authorizer_name": "Dana", "authorizer_email": "d@other-legal.example", "attested": true, "domain_verification_id": v["id"]})
	auth := authorize(t, c, "strong-legal.example", "exposure", v["id"])
	if auth["method"] != "verified_domain_control" {
		t.Fatalf("auth: %v", auth)
	}
	got := runAssessment(t, c, auth["id"].(string), templateID(t, c, "exposure"))
	if got["score"] != float64(100) || got["risk_level"] != "low" || !strings.Contains(asJSON(got["checks"]), "web.security_headers") {
		t.Fatalf("exposure run: %v", asJSON(got))
	}
	c.must(204, http.MethodPost, "/api/v1/it-assessments/authorizations/"+auth["id"].(string)+"/revoke", nil)
	c.wantCode(409, "authorization_inactive", http.MethodPost, "/api/v1/it-assessments/assessments", map[string]any{"authorization_id": auth["id"], "template_id": templateID(t, c, "exposure")})
}

func TestTemplatesCannotWidenScope(t *testing.T) {
	c, _ := assessTenant(t, "Scope Tenant")
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/it-assessments/templates", map[string]any{"name": "Sneaky", "tier": "passive", "check_keys": []string{"email.spf", "web.security_headers"}})
	c.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/it-assessments/templates", map[string]any{"name": "Scanner", "tier": "exposure", "check_keys": []string{"network.port_scan"}})
	c.must(201, http.MethodPost, "/api/v1/it-assessments/templates", map[string]any{"name": "Email only", "tier": "passive", "check_keys": []string{"email.spf", "email.dmarc"}})
}

func TestAssessmentsAreIsolated(t *testing.T) {
	a, _ := assessTenant(t, "Assess Alpha")
	b, _ := assessTenant(t, "Assess Beta")
	auth := authorize(t, a, "typical-firm.example", "passive", nil)
	got := runAssessment(t, a, auth["id"].(string), templateID(t, a, "passive"))
	var bLog []string
	b.log = &bLog
	b.wantCode(404, "not_found", http.MethodGet, "/api/v1/it-assessments/assessments/"+got["id"].(string), nil)
	b.wantCode(422, "validation_failed", http.MethodPost, "/api/v1/it-assessments/assessments", map[string]any{"authorization_id": auth["id"], "template_id": templateID(t, b, "passive")})
	b.wantCode(409, "not_active", http.MethodPost, "/api/v1/it-assessments/authorizations/"+auth["id"].(string)+"/revoke", nil)
	for _, path := range []string{"/assessments", "/authorizations", "/domain-verifications"} {
		b.must(200, http.MethodGet, "/api/v1/it-assessments"+path, nil)
	}
	for _, line := range bLog {
		if strings.Contains(line, "typical-firm.example") || strings.Contains(line, got["id"].(string)) {
			t.Errorf("tenant B saw tenant A's assessment: %s", line)
		}
	}
}
