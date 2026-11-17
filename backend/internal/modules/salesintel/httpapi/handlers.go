// Package httpapi serves Sales Intelligence's contract. The route guard has already authenticated,
// checked the entitlement and permission, and validated the request.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/salesintel/app"
	"mspengine/internal/modules/salesintel/domain"
	"mspengine/internal/modules/salesintel/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func ok(w http.ResponseWriter, status int, v any) { httpx.JSON(w, status, v) }

func date(t time.Time) openapi_types.Date { return openapi_types.Date{Time: t} }

func span(from *From, to *To) app.Range {
	var r app.Range
	if from != nil {
		r.From = from.Time
	}
	if to != nil {
		r.To = to.Time
	}
	return r
}

func viewOut(v app.View) AnalyticsView {
	out := AnalyticsView{Section: v.Section, Available: v.Available, Currency: v.Currency, AsOf: v.AsOf, FactsAsOf: v.FactsAsOf,
		From: date(v.From), To: date(v.To), Series: []MetricSeries{}, Labels: v.Labels}
	if v.Reason != nil {
		reason := AnalyticsViewReason(*v.Reason)
		out.Reason = &reason
	}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	for _, s := range v.Series {
		ms := MetricSeries{Key: s.Def.Key, Name: s.Def.Name, ValueKind: ValueKind(s.Def.Kind), Unit: Unit(s.Def.Unit), Dimension: s.Dimension, Currency: s.Currency, Points: []MetricPoint{}}
		for _, p := range s.Points {
			mp := MetricPoint{Id: p.ID, Period: date(p.Period), Value: p.Value, ValueKind: ValueKind(s.Def.Kind), Flags: []MetricPointFlags{}}
			if p.DimValue != "" {
				dv := p.DimValue
				mp.DimensionValue = &dv
			}
			for _, f := range p.Flags {
				mp.Flags = append(mp.Flags, MetricPointFlags(f))
			}
			ms.Points = append(ms.Points, mp)
		}
		out.Series = append(out.Series, ms)
	}
	return out
}

func (h Handlers) section(w http.ResponseWriter, r *http.Request, name string, from *From, to *To) {
	v, err := h.S.Section(r.Context(), principal(r), name, span(from, to))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, viewOut(v))
}

// ListMetricDefinitions handles GET /sales-intelligence/definitions.
func (h Handlers) ListMetricDefinitions(w http.ResponseWriter, _ *http.Request) {
	out := MetricDefList{Data: []MetricDef{}, AttributionDays: domain.AttributionDays, RoiHorizonMonths: domain.ROIHorizonMonths}
	for _, d := range domain.Definitions {
		out.Data = append(out.Data, MetricDef{Key: d.Key, Name: d.Name, Section: d.Section, ValueKind: ValueKind(d.Kind), Unit: Unit(d.Unit), Dimensions: d.Dimensions, Description: d.Description})
	}
	ok(w, 200, out)
}

// GetDashboard handles GET /sales-intelligence/dashboard.
func (h Handlers) GetDashboard(w http.ResponseWriter, r *http.Request, params GetDashboardParams) {
	d, err := h.S.GetDashboard(r.Context(), principal(r), span(params.From, params.To))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := Dashboard{Currency: d.Currency, AsOf: d.AsOf, FactsAsOf: d.FactsAsOf, From: date(d.From), To: date(d.To), Sections: []DashboardSection{}}
	for _, s := range d.Sections {
		ds := DashboardSection{Key: DashboardSectionKey(s.Key), Available: s.Available, Cards: []Card{}}
		if s.Reason != nil {
			reason := DashboardSectionReason(*s.Reason)
			ds.Reason = &reason
		}
		for _, c := range s.Cards {
			flags := c.Flags
			if flags == nil {
				flags = []string{}
			}
			ds.Cards = append(ds.Cards, Card{Key: c.Def.Key, Name: c.Def.Name, Unit: Unit(c.Def.Unit), ValueKind: ValueKind(c.Def.Kind), Period: date(c.Period),
				Value: c.Value, Previous: c.Previous, Currency: c.Currency, Flags: flags})
		}
		out.Sections = append(out.Sections, ds)
	}
	ok(w, 200, out)
}

// GetRevenue handles GET /sales-intelligence/revenue.
func (h Handlers) GetRevenue(w http.ResponseWriter, r *http.Request, params GetRevenueParams) {
	h.section(w, r, "revenue", params.From, params.To)
}

// GetLeads handles GET /sales-intelligence/leads.
func (h Handlers) GetLeads(w http.ResponseWriter, r *http.Request, params GetLeadsParams) {
	h.section(w, r, "leads", params.From, params.To)
}

// GetCampaigns handles GET /sales-intelligence/campaigns.
func (h Handlers) GetCampaigns(w http.ResponseWriter, r *http.Request, params GetCampaignsParams) {
	h.section(w, r, "campaigns", params.From, params.To)
}

// GetFunnel handles GET /sales-intelligence/funnel.
func (h Handlers) GetFunnel(w http.ResponseWriter, r *http.Request, params GetFunnelParams) {
	h.section(w, r, "funnel", params.From, params.To)
}

// GetWinLoss handles GET /sales-intelligence/win-loss.
func (h Handlers) GetWinLoss(w http.ResponseWriter, r *http.Request, params GetWinLossParams) {
	h.section(w, r, "win_loss", params.From, params.To)
}

// GetTeam handles GET /sales-intelligence/team.
func (h Handlers) GetTeam(w http.ResponseWriter, r *http.Request, params GetTeamParams) {
	h.section(w, r, "team", params.From, params.To)
}

// GetCostsView handles GET /sales-intelligence/costs-summary.
func (h Handlers) GetCostsView(w http.ResponseWriter, r *http.Request, params GetCostsViewParams) {
	h.section(w, r, "costs", params.From, params.To)
}

// GetMetric handles GET /sales-intelligence/metrics/{key}.
func (h Handlers) GetMetric(w http.ResponseWriter, r *http.Request, key string, params GetMetricParams) {
	dim := ""
	if params.Dimension != nil {
		dim = string(*params.Dimension)
	}
	v, err := h.S.Metric(r.Context(), principal(r), key, span(params.From, params.To), dim)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, viewOut(v))
}

func forecastOut(v app.RunView) Forecast {
	out := Forecast{Id: v.ID, Kind: ForecastKind(v.Kind), Status: ForecastStatus(v.Status), ErrorCode: v.ErrorCode, AsOf: date(v.AsOf), MethodVersion: v.MethodVersion,
		HorizonMonths: int(v.HorizonMonths), Currency: v.Currency, ValueKind: ForecastValueKindForecast, Previous: v.Previous, Points: []ForecastPoint{},
		CreatedAt: v.CreatedAt, CompletedAt: v.CompletedAt, Assumptions: map[string]any{}}
	_ = json.Unmarshal(v.Assumptions, &out.Assumptions)
	for _, p := range v.Points {
		out.Points = append(out.Points, ForecastPoint{Period: date(p.PeriodStart), Expected: p.ExpectedMinor, Low: p.LowMinor, High: p.HighMinor, ValueKind: ForecastPointValueKindForecast})
	}
	return out
}

// ListForecasts handles GET /sales-intelligence/forecasts.
func (h Handlers) ListForecasts(w http.ResponseWriter, r *http.Request, params ListForecastsParams) {
	var kind *string
	if params.Kind != nil {
		k := string(*params.Kind)
		kind = &k
	}
	runs, err := h.S.Forecasts(r.Context(), principal(r), kind)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ForecastList{Data: []Forecast{}}
	for _, run := range runs {
		out.Data = append(out.Data, forecastOut(run))
	}
	ok(w, 200, out)
}

// RequestForecast handles POST /sales-intelligence/forecasts.
func (h Handlers) RequestForecast(w http.ResponseWriter, r *http.Request) {
	var b ForecastInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	horizon := 0
	if b.HorizonMonths != nil {
		horizon = *b.HorizonMonths
	}
	run, err := h.S.RequestForecast(r.Context(), principal(r), string(b.Kind), horizon)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 202, forecastOut(run))
}

// GetForecast handles GET /sales-intelligence/forecasts/{id}.
func (h Handlers) GetForecast(w http.ResponseWriter, r *http.Request, id Id) {
	run, err := h.S.Forecast(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, forecastOut(run))
}

func costOut(c repo.GetCostRow) Cost {
	return Cost{Id: c.ID, PeriodStart: date(c.PeriodStart), PeriodEnd: date(c.PeriodEnd), Category: c.Category, CampaignRef: c.CampaignRef, AmountMinor: c.AmountMinor,
		Currency: c.Currency, Note: c.Note, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func costIn(b CostInput) app.CostInput {
	return app.CostInput{Start: b.PeriodStart.Time, End: b.PeriodEnd.Time, Category: string(b.Category), Campaign: b.CampaignRef, Amount: b.AmountMinor, Currency: b.Currency, Note: b.Note}
}

// ListCosts handles GET /sales-intelligence/costs.
func (h Handlers) ListCosts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Costs(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CostList{Data: []Cost{}}
	for _, c := range rows {
		out.Data = append(out.Data, costOut(repo.GetCostRow(c)))
	}
	ok(w, 200, out)
}

// CreateCost handles POST /sales-intelligence/costs.
func (h Handlers) CreateCost(w http.ResponseWriter, r *http.Request) {
	var b CostInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	c, err := h.S.CreateCost(r.Context(), principal(r), costIn(b))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, costOut(c))
}

// UpdateCost handles PATCH /sales-intelligence/costs/{id}. The body is the whole entry.
func (h Handlers) UpdateCost(w http.ResponseWriter, r *http.Request, id Id) {
	var b CostInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	c, err := h.S.UpdateCost(r.Context(), principal(r), id, costIn(b))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, costOut(c))
}

// DeleteCost handles DELETE /sales-intelligence/costs/{id}.
func (h Handlers) DeleteCost(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteCost(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func reportOut(id uuid.UUID, name string, raw []byte, created time.Time) Report {
	var d app.Definition
	_ = json.Unmarshal(raw, &d)
	from, _ := time.Parse(time.DateOnly, d.From)
	to, _ := time.Parse(time.DateOnly, d.To)
	out := Report{Id: id, Name: name, CreatedAt: created, Definition: ReportDefinition{Metrics: d.Metrics, From: date(from), To: date(to)}}
	if d.Dimension != "" {
		dim := ReportDefinitionDimension(d.Dimension)
		out.Definition.Dimension = &dim
	}
	return out
}

// ListReports handles GET /sales-intelligence/reports.
func (h Handlers) ListReports(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Reports(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ReportList{Data: []Report{}}
	for _, d := range rows {
		out.Data = append(out.Data, reportOut(d.ID, d.Name, d.Definition, d.CreatedAt))
	}
	ok(w, 200, out)
}

// CreateReport handles POST /sales-intelligence/reports.
func (h Handlers) CreateReport(w http.ResponseWriter, r *http.Request) {
	var b ReportInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	def := app.Definition{Metrics: b.Definition.Metrics, From: b.Definition.From.Format(time.DateOnly), To: b.Definition.To.Format(time.DateOnly)}
	if b.Definition.Dimension != nil {
		def.Dimension = string(*b.Definition.Dimension)
	}
	d, err := h.S.CreateReport(r.Context(), principal(r), b.Name, def)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, reportOut(d.ID, d.Name, d.Definition, d.CreatedAt))
}

func exportOut(v app.ExportView) Export {
	return Export{Id: v.ID, Status: ExportStatus(v.Status), ErrorCode: v.ErrorCode, DownloadUrl: v.DownloadURL, ExpiresAt: v.ExpiresAt, CreatedAt: v.CreatedAt}
}

// ExportReport handles POST /sales-intelligence/reports/{id}/export.
func (h Handlers) ExportReport(w http.ResponseWriter, r *http.Request, id Id) {
	eid, err := h.S.RequestExport(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := h.S.Export(r.Context(), principal(r), eid)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 202, exportOut(v))
}

// GetExport handles GET /sales-intelligence/exports/{id}.
func (h Handlers) GetExport(w http.ResponseWriter, r *http.Request, id Id) {
	v, err := h.S.Export(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 200, exportOut(v))
}

func insightOut(i app.Insight) Insight {
	out := Insight{Id: i.ID, Scope: i.Scope, Commentary: i.Commentary, CitedMetricIds: i.Cited, CreatedAt: i.CreatedAt, Metrics: []InsightMetric{}}
	if out.CitedMetricIds == nil {
		out.CitedMetricIds = []uuid.UUID{}
	}
	for _, m := range i.Metrics {
		out.Metrics = append(out.Metrics, InsightMetric{Id: m.ID, Key: m.Key, Period: m.Period, Display: m.Display, ValueKind: ValueKind(m.ValueKind)})
	}
	return out
}

// ListInsights handles GET /sales-intelligence/insights.
func (h Handlers) ListInsights(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.Insights(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := InsightList{Data: []Insight{}}
	for _, i := range rows {
		out.Data = append(out.Data, insightOut(i))
	}
	ok(w, 200, out)
}

// RequestInsight handles POST /sales-intelligence/insights.
func (h Handlers) RequestInsight(w http.ResponseWriter, r *http.Request) {
	var b InsightInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	i, err := h.S.RequestInsight(r.Context(), principal(r), string(b.Scope))
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, 201, insightOut(i))
}
