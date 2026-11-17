// Package httpapi serves Customer Expansion's contract. The route guard has already authenticated,
// checked the entitlement and permission, and validated the request.
package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/customerexpansion/app"
	"mspengine/internal/modules/customerexpansion/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func ok(w http.ResponseWriter, status int, v any) { httpx.JSON(w, status, v) }

func done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func date(t time.Time) openapi_types.Date { return openapi_types.Date{Time: t} }

func optDate(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	d := date(*t)
	return &d
}

func fromDate(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}

func optInt(v *int32) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

func toInt32(v *int) *int32 {
	if v == nil {
		return nil
	}
	i := int32(*v) //nolint:gosec // Bounded by the contract.
	return &i
}

func customerOut(c app.Customer) Customer {
	out := Customer{Id: c.ID, CrmCompany: CompanyRef{Id: c.Company.ID, Name: c.Company.Name, Domain: c.Company.Domain, Lifecycle: c.Company.Lifecycle},
		HealthStatus: CustomerHealthStatus(c.HealthStatus), HealthSource: CustomerHealthSource(c.HealthSource), HealthAsOf: c.HealthAsOf,
		LastAnalyzedAt: c.LastAnalyzedAt, MixedCurrency: c.MixedCurrency, RenewalNextDate: optDate(c.RenewalNext), CreatedAt: c.CreatedAt,
		CoverageSummary: CoverageSummary{Active: int(c.CoveredActive), Absent: int(c.CoveredAbsent), Declined: int(c.CoveredDeclined), Unknown: c.Unknown}}
	if !c.MixedCurrency {
		out.ActiveMrrMinor = c.ActiveMrrMinor
		if c.MrrCurrency != "" {
			cur := c.MrrCurrency
			out.Currency = &cur
		}
	}
	return out
}

// ListCustomers handles GET /customer-expansion/customers.
func (h Handlers) ListCustomers(w http.ResponseWriter, r *http.Request, params ListCustomersParams) {
	var health *string
	if params.Health != nil {
		v := string(*params.Health)
		health = &v
	}
	rows, err := h.S.Customers(r.Context(), principal(r), health, nil)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CustomerList{Data: []Customer{}}
	for _, c := range rows {
		out.Data = append(out.Data, customerOut(c))
	}
	ok(w, 200, out)
}

// AddCustomer handles POST /customer-expansion/customers.
func (h Handlers) AddCustomer(w http.ResponseWriter, r *http.Request) {
	var b CustomerInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	in := app.NewCustomer{CRMCompanyID: b.CrmCompanyId, Domain: b.Domain}
	if b.Name != nil {
		in.Name = *b.Name
	}
	id, err := h.S.AddCustomer(r.Context(), principal(r), in)
	if err != nil {
		fail(w, r, err)
		return
	}
	h.customer(w, r, id, 201)
}

func (h Handlers) customer(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	c, err := h.S.Customer(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, status, customerOut(c))
}

// ImportCustomers handles POST /customer-expansion/customers/import.
func (h Handlers) ImportCustomers(w http.ResponseWriter, r *http.Request) {
	var b ImportInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	res, err := h.S.ImportCustomers(r.Context(), principal(r), b.FileId)
	if err != nil {
		fail(w, r, err)
		return
	}
	if res.Errors == nil {
		res.Errors = []string{}
	}
	ok(w, 200, ImportResult{Created: res.Created, Contracts: res.Contracts, Errors: res.Errors})
}

// GetCustomer handles GET /customer-expansion/customers/{id}.
func (h Handlers) GetCustomer(w http.ResponseWriter, r *http.Request, id Id) { h.customer(w, r, id, 200) }

// UpdateCustomer handles PATCH /customer-expansion/customers/{id}.
func (h Handlers) UpdateCustomer(w http.ResponseWriter, r *http.Request, id Id) {
	var b CustomerPatch
	keys, good := httpx.DecodePatch(w, r, &b)
	if !good {
		return
	}
	if keys["health_status"] {
		var status *string
		if b.HealthStatus != nil {
			v := string(*b.HealthStatus)
			status = &v
		}
		if err := h.S.SetHealth(r.Context(), principal(r), id, status); err != nil {
			fail(w, r, err)
			return
		}
	}
	h.customer(w, r, id, 200)
}

func contractOut(c repo.ListContractsRow) Contract {
	out := Contract{Id: c.ID, DealId: c.DealID, ProposalVersionId: c.ProposalVersionID, Status: ContractStatus(c.Status), StartDate: date(c.StartDate),
		EndDate: optDate(c.EndDate), TermMonths: optInt(c.TermMonths), AutoRenew: c.AutoRenew, NoticeDays: optInt(c.NoticeDays), Currency: c.Currency,
		MrrMinor: c.MrrMinor, SetupMinor: c.SetupMinor, Source: ContractSource(c.Source), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if c.ReviewReason != nil {
		rr := ContractReviewReason(*c.ReviewReason)
		out.ReviewReason = &rr
	}
	return out
}

// ListContracts handles GET /customer-expansion/customers/{id}/contracts.
func (h Handlers) ListContracts(w http.ResponseWriter, r *http.Request, id Id) {
	rows, err := h.S.Contracts(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ContractList{Data: []Contract{}}
	for _, c := range rows {
		out.Data = append(out.Data, contractOut(c))
	}
	ok(w, 200, out)
}

// CreateContract handles POST /customer-expansion/customers/{id}/contracts.
func (h Handlers) CreateContract(w http.ResponseWriter, r *http.Request, id Id) {
	var b ContractInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	start := b.StartDate.Time
	in := app.ContractInput{Start: &start, End: fromDate(b.EndDate), TermMonths: toInt32(b.TermMonths), AutoRenew: b.AutoRenew, NoticeDays: toInt32(b.NoticeDays),
		Currency: &b.Currency, MRR: &b.MrrMinor, Setup: b.SetupMinor, ProposalVersionID: b.ProposalVersionId, DealID: b.DealId}
	if b.Status != nil {
		st := string(*b.Status)
		in.Status = &st
	}
	cid, err := h.S.CreateContract(r.Context(), principal(r), id, in)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, Ref{Id: cid})
}

// UpdateContract handles PATCH /customer-expansion/contracts/{id}.
func (h Handlers) UpdateContract(w http.ResponseWriter, r *http.Request, id Id) {
	var b ContractPatch
	keys, good := httpx.DecodePatch(w, r, &b)
	if !good {
		return
	}
	in := app.ContractInput{Start: fromDate(b.StartDate), End: fromDate(b.EndDate), SetEnd: keys["end_date"], TermMonths: toInt32(b.TermMonths), SetTerm: keys["term_months"],
		AutoRenew: b.AutoRenew, NoticeDays: toInt32(b.NoticeDays), SetNotice: keys["notice_days"], Currency: b.Currency, MRR: b.MrrMinor, Setup: b.SetupMinor,
		ClearReview: b.ClearReview != nil && *b.ClearReview}
	if b.Status != nil {
		st := string(*b.Status)
		in.Status = &st
	}
	done(w, r, h.S.UpdateContract(r.Context(), principal(r), id, in))
}

func coverageOut(lines []app.CoverageLine) CoverageList {
	out := CoverageList{Data: []CoverageLine{}}
	for _, l := range lines {
		out.Data = append(out.Data, CoverageLine{TenantServiceId: l.Service.ID, ServiceName: l.Service.Name, ServiceStatus: CoverageLineServiceStatus(l.Service.Status),
			CategoryKey: l.Service.CategoryKey, Coverage: CoverageLineCoverage(l.Coverage), Quantity: optInt(l.Quantity), MrrMinor: l.MRR, Currency: l.Currency, UpdatedAt: l.Updated})
	}
	return out
}

// GetCoverage handles GET /customer-expansion/customers/{id}/services.
func (h Handlers) GetCoverage(w http.ResponseWriter, r *http.Request, id Id) {
	lines, err := h.S.Coverage(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, coverageOut(lines))
}

// SetCoverage handles PUT /customer-expansion/customers/{id}/services.
func (h Handlers) SetCoverage(w http.ResponseWriter, r *http.Request, id Id) {
	var b CoverageInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	items := make([]app.CoverageInput, 0, len(b.Items))
	for _, it := range b.Items {
		items = append(items, app.CoverageInput{ServiceID: it.TenantServiceId, Coverage: string(it.Coverage), Quantity: toInt32(it.Quantity), MRR: it.MrrMinor, Currency: it.Currency})
	}
	if err := h.S.SetCoverage(r.Context(), principal(r), id, items); err != nil {
		fail(w, r, err)
		return
	}
	h.GetCoverage(w, r, id)
}

// AnalyzeCustomer handles POST /customer-expansion/customers/{id}/analyze.
func (h Handlers) AnalyzeCustomer(w http.ResponseWriter, r *http.Request, id Id) {
	a, err := h.S.Analyze(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := Analysis{HealthStatus: AnalysisHealthStatus(a.Health), Created: a.Created, Refreshed: a.Refreshed, Expired: int(a.Expired), Indicators: []Indicator{}}
	for _, i := range a.Indicators {
		out.Indicators = append(out.Indicators, Indicator{Key: IndicatorKey(i.Key), Polarity: IndicatorPolarity(i.Polarity), Value: i.Value})
	}
	ok(w, 200, out)
}

// ListRenewals handles GET /customer-expansion/renewals.
func (h Handlers) ListRenewals(w http.ResponseWriter, r *http.Request, params ListRenewalsParams) {
	var status *string
	if params.Status != nil {
		v := string(*params.Status)
		status = &v
	}
	within := int32(120)
	if params.WithinDays != nil {
		within = int32(*params.WithinDays) //nolint:gosec // Bounded by the contract.
	}
	rows, err := h.S.Renewals(r.Context(), principal(r), status, within)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := RenewalList{Data: []Renewal{}}
	for _, a := range rows {
		out.Data = append(out.Data, Renewal{Id: a.ID, ContractId: a.ContractID, CustomerId: a.CustomerProfileID, CustomerName: a.CustomerName, Kind: RenewalKind(a.Kind),
			WindowDays: int(a.WindowDays), DueDate: date(a.DueDate), Status: RenewalStatus(a.Status), MrrMinor: a.MrrMinor, Currency: a.Currency, AutoRenew: a.AutoRenew,
			AcknowledgedAt: a.AcknowledgedAt})
	}
	ok(w, 200, out)
}

// AcknowledgeRenewal handles POST /customer-expansion/renewal-alerts/{id}/acknowledge.
func (h Handlers) AcknowledgeRenewal(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.AcknowledgeAlert(r.Context(), principal(r), id))
}

func recommendationOut(v app.RecommendationView) Recommendation {
	out := Recommendation{Id: v.ID, Customer: NamedRef{Id: v.CustomerProfileID, Name: v.CustomerName}, Kind: RecommendationKind(v.Kind), ContractId: v.ContractID,
		Rationale: v.Rationale, AiWorded: v.AIWorded, EstimatedMrrMinor: v.EstimatedMrrMinor, Currency: v.Currency, EvidenceClass: RecommendationEvidenceClass(v.EvidenceClass),
		Status: RecommendationStatus(v.Status), CreatedAt: v.CreatedAt, DecidedAt: v.DecidedAt, Evidence: []Evidence{}}
	if v.TenantServiceID != nil && v.ServiceName != nil {
		out.Service = &NamedRef{Id: *v.TenantServiceID, Name: *v.ServiceName}
	}
	for _, e := range v.Evidence {
		ev := Evidence{Kind: EvidenceKind(e.Kind), Detail: e.Detail}
		if ev.Detail == nil {
			ev.Detail = map[string]any{}
		}
		if e.ReferenceType != nil && e.ReferenceID != nil {
			ev.Reference = &EvidenceRef{Type: *e.ReferenceType, Id: *e.ReferenceID}
		}
		out.Evidence = append(out.Evidence, ev)
	}
	return out
}

// ListRecommendations handles GET /customer-expansion/recommendations.
func (h Handlers) ListRecommendations(w http.ResponseWriter, r *http.Request, params ListRecommendationsParams) {
	f := app.RecommendationFilter{Profile: params.CustomerId}
	if params.Status != nil {
		v := string(*params.Status)
		f.Status = &v
	}
	rows, err := h.S.Recommendations(r.Context(), principal(r), f)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := RecommendationList{Data: []Recommendation{}}
	for _, v := range rows {
		out.Data = append(out.Data, recommendationOut(v))
	}
	ok(w, 200, out)
}

// GetRecommendation handles GET /customer-expansion/recommendations/{id}.
func (h Handlers) GetRecommendation(w http.ResponseWriter, r *http.Request, id Id) {
	v, err := h.S.Recommendation(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, recommendationOut(v))
}

// AcceptRecommendation handles POST /customer-expansion/recommendations/{id}/accept. Repeating it
// with any key returns the same opportunity, because there is one per recommendation.
func (h Handlers) AcceptRecommendation(w http.ResponseWriter, r *http.Request, id Id, _ AcceptRecommendationParams) {
	var b AcceptInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	res, err := h.S.Accept(r.Context(), principal(r), id, b.CreateDeal, b.DealTitle)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := AcceptResult{OpportunityId: res.OpportunityID, DealId: res.DealID, DealPending: res.DealPending}
	if res.DealSkipped != nil {
		s := AcceptResultDealSkipped(*res.DealSkipped)
		out.DealSkipped = &s
	}
	ok(w, 200, out)
}

// DismissRecommendation handles POST /customer-expansion/recommendations/{id}/dismiss.
func (h Handlers) DismissRecommendation(w http.ResponseWriter, r *http.Request, id Id) {
	done(w, r, h.S.Dismiss(r.Context(), principal(r), id))
}

// ListOpportunities handles GET /customer-expansion/opportunities.
func (h Handlers) ListOpportunities(w http.ResponseWriter, r *http.Request, params ListOpportunitiesParams) {
	var status *string
	if params.Status != nil {
		v := string(*params.Status)
		status = &v
	}
	rows, err := h.S.Opportunities(r.Context(), principal(r), status)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := OpportunityList{Data: []Opportunity{}}
	for _, o := range rows {
		out.Data = append(out.Data, Opportunity{Id: o.ID, RecommendationId: o.RecommendationID, Customer: NamedRef{Id: o.CustomerProfileID, Name: o.CustomerName}, Kind: o.Kind,
			DealId: o.DealID, EstimatedMrrMinor: o.EstimatedMrrMinor, RealisedMrrMinor: o.RealisedMrrMinor, Currency: o.Currency, Status: OpportunityStatus(o.Status),
			CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt})
	}
	ok(w, 200, out)
}

// OpportunitySummary handles GET /customer-expansion/opportunities/summary.
func (h Handlers) OpportunitySummary(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.OpportunitySummary(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := OpportunitySummary{Data: []SummaryRow{}}
	for _, s := range rows {
		row := SummaryRow{Status: SummaryRowStatus(s.Status), Count: int(s.Count), EstimatedMrrMinor: s.EstimatedMrrMinor, RealisedMrrMinor: s.RealisedMrrMinor}
		if s.Currency != "" {
			c := s.Currency
			row.Currency = &c
		}
		out.Data = append(out.Data, row)
	}
	ok(w, 200, out)
}
