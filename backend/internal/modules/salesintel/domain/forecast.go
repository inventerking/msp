package domain

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MethodVersion names the forecast method, stored with every run so old runs stay explainable.
const MethodVersion = "v1"

// MinHistoryMonths is the complete months of revenue history a revenue forecast needs.
const MinHistoryMonths = 3

// MinStageSample is how many closed deals a stage needs before its own win rate is used.
const MinStageSample = 10

// z80 gives an 80% band, assuming roughly normal variation.
const z80 = 1.2816

// Point is one forecast month. Low ≤ Expected ≤ High.
type Point struct {
	Period              time.Time
	Expected, Low, High int64
}

// ForecastResult is a forecast, or why there is none.
type ForecastResult struct {
	Points      []Point
	Assumptions map[string]any
	// Refusal is set when the forecast cannot be made, such as insufficient_history.
	Refusal string
}

// ForecastInput is what a forecast reads.
type ForecastInput struct {
	Revenue []RevenueFact
	Deals   []DealFact
	Convert Converter
	Now     time.Time
	Horizon int
}

// HistoryMonths counts complete months from the first revenue event to last month.
func HistoryMonths(revenue []RevenueFact, now time.Time) int {
	if len(revenue) == 0 {
		return 0
	}
	first := revenue[0].At
	for _, f := range revenue {
		if f.At.Before(first) {
			first = f.At
		}
	}
	return len(Months(first, MonthStart(now).AddDate(0, -1, 0)))
}

// stageRates are historical win rates by stage: of closed deals that reached a stage, the share won.
func stageRates(deals map[uuid.UUID]*DealState) map[string][2]int {
	out := map[string][2]int{}
	for _, d := range deals {
		if d.Status != "won" && d.Status != "lost" {
			continue
		}
		for st := range d.Stages {
			r := out[st]
			r[1]++
			if d.Status == "won" {
				r[0]++
			}
			out[st] = r
		}
	}
	return out
}

// winChance uses the stage's historical rate when enough deals closed after reaching it, and the
// deal's own probability otherwise.
func winChance(d *DealState, rates map[string][2]int) (float64, bool) {
	if r, ok := rates[d.Stage]; ok && r[1] >= MinStageSample {
		return float64(r[0]) / float64(r[1]), true
	}
	return float64(d.Probability) / 100, false
}

type pipeMonth struct{ expected, variance float64 }

// pipeline spreads open deals over their expected close months. Overdue deals count in the first
// month; deals with no close date are left out and counted in the assumptions.
func pipeline(in ForecastInput, deals map[uuid.UUID]*DealState, value func(*DealState) *int64) (map[time.Time]*pipeMonth, map[string]any) {
	rates := stageRates(deals)
	first := MonthStart(in.Now)
	out := map[time.Time]*pipeMonth{}
	noDate, overdue, fxMissing, stageRated := 0, 0, 0, 0
	for _, d := range deals {
		if d.Status != "open" {
			continue
		}
		if d.ExpectedClose == nil {
			noDate++
			continue
		}
		v := value(d)
		if v == nil {
			continue
		}
		amount, ok := in.Convert(*v, d.Currency, in.Now)
		if !ok {
			fxMissing++
			continue
		}
		m := MonthStart(*d.ExpectedClose)
		if m.Before(first) {
			m = first
			overdue++
		}
		p, byStage := winChance(d, rates)
		if byStage {
			stageRated++
		}
		pm := out[m]
		if pm == nil {
			pm = &pipeMonth{}
			out[m] = pm
		}
		x := float64(amount)
		pm.expected += x * p
		pm.variance += x * x * p * (1 - p)
	}
	used := map[string]any{}
	for st, r := range rates {
		if r[1] >= MinStageSample {
			used[st] = map[string]any{"win_rate": float64(r[0]) / float64(r[1]), "closed_deals": r[1]}
		}
	}
	return out, map[string]any{"deals_without_close_date": noDate, "overdue_deals_in_first_month": overdue, "deals_left_out_for_missing_rate": fxMissing,
		"deals_using_stage_rate": stageRated, "stage_win_rates": used, "min_stage_sample": MinStageSample}
}

// PipelineForecast is expected won value by close month. Each deal counts at its contract value, or
// 12 months of its MRR, times its chance of winning. The band comes from the spread of those chances.
func PipelineForecast(in ForecastInput) ForecastResult {
	deals := dealStates(in.Deals)
	byMonth, assumptions := pipeline(in, deals, dealValue)
	assumptions["method_version"] = MethodVersion
	assumptions["value_rule"] = "contract value, or 12 × MRR when only MRR is known"
	assumptions["band"] = "80% range from the uncertainty of each deal closing"
	out := ForecastResult{Assumptions: assumptions}
	for i := 0; i < in.Horizon; i++ {
		m := MonthStart(in.Now).AddDate(0, i, 0)
		pm := byMonth[m]
		if pm == nil {
			pm = &pipeMonth{}
		}
		sd := math.Sqrt(pm.variance)
		out.Points = append(out.Points, band(m, pm.expected, sd))
	}
	return out
}

func band(m time.Time, expected, sd float64) Point {
	e := math.Round(expected)
	low := math.Max(0, math.Round(expected-z80*sd))
	high := math.Round(expected + z80*sd)
	return Point{Period: m, Expected: int64(e), Low: int64(math.Min(low, e)), High: int64(math.Max(high, e))}
}

// RevenueForecast starts from actual MRR and rolls it forward month by month: the historical monthly
// rates of churn and expansion, minus contracts due to end without auto-renewal, plus the weighted
// MRR of open deals expected to close. The band widens with the historical spread of net new MRR.
func RevenueForecast(in ForecastInput) ForecastResult {
	history := HistoryMonths(in.Revenue, in.Now)
	if history < MinHistoryMonths {
		return ForecastResult{Refusal: "insufficient_history", Assumptions: map[string]any{"history_months": history, "min_history_months": MinHistoryMonths}}
	}
	facts := append([]RevenueFact(nil), in.Revenue...)
	sort.SliceStable(facts, func(i, j int) bool { return facts[i].At.Before(facts[j].At) })
	first := MonthStart(facts[0].At)
	months := Months(first, MonthStart(in.Now).AddDate(0, -1, 0))
	fxMissing := 0
	conv := make([]int64, len(facts))
	okAt := make([]bool, len(facts))
	for i, f := range facts {
		conv[i], okAt[i] = in.Convert(f.Delta, f.Currency, f.At)
		if !okAt[i] {
			fxMissing++
		}
	}
	mrrBefore := func(t time.Time) float64 {
		var s int64
		for i, f := range facts {
			if !f.At.Before(t) {
				break
			}
			if okAt[i] {
				s += conv[i]
			}
		}
		return float64(s)
	}
	var churnRates, expRates, nets []float64
	for _, m := range months {
		start := mrrBefore(m)
		next := m.AddDate(0, 1, 0)
		var lost, gained, net float64
		for i, f := range facts {
			if f.At.Before(m) || !f.At.Before(next) || !okAt[i] {
				continue
			}
			switch f.Movement {
			case "churn", "contraction":
				lost += -float64(conv[i])
			case "expansion", "reactivation":
				gained += float64(conv[i])
			}
			net += float64(conv[i])
		}
		nets = append(nets, net)
		if start > 0 {
			churnRates = append(churnRates, lost/start)
			expRates = append(expRates, gained/start)
		}
	}
	churn, expansion := mean(churnRates), mean(expRates)
	sd := stdev(nets)
	current := mrrBefore(in.Now.Add(time.Nanosecond))
	// Scheduled ends: each contract's latest state.
	latest := map[uuid.UUID]int{}
	for i, f := range facts {
		latest[f.Contract] = i
	}
	ends := map[time.Time]float64{}
	for _, i := range latest {
		f := facts[i]
		if f.ContractMRR <= 0 || f.End == nil || (f.AutoRenew != nil && *f.AutoRenew) {
			continue
		}
		v, ok := in.Convert(f.ContractMRR, f.Currency, in.Now)
		if !ok {
			continue
		}
		ends[MonthStart(*f.End)] += float64(v)
	}
	deals := dealStates(in.Deals)
	pipe, pipeNotes := pipeline(in, deals, func(d *DealState) *int64 { return d.MRR })
	out := ForecastResult{}
	expected := current
	scheduled := []map[string]any{}
	for i := 0; i < in.Horizon; i++ {
		m := MonthStart(in.Now).AddDate(0, i, 0)
		var p float64
		if pm := pipe[m]; pm != nil {
			p = pm.expected
		}
		expected = expected*(1+expansion-churn) - ends[m] + p
		if expected < 0 {
			expected = 0
		}
		if ends[m] > 0 {
			scheduled = append(scheduled, map[string]any{"period": m.Format(time.DateOnly), "mrr_minor": int64(ends[m])})
		}
		out.Points = append(out.Points, band(m, expected, sd*math.Sqrt(float64(i+1))))
	}
	out.Assumptions = map[string]any{
		"method_version": MethodVersion, "history_months": history, "starting_mrr_minor": int64(current),
		"monthly_churn_rate": churn, "monthly_expansion_rate": expansion, "net_new_mrr_stdev_minor": int64(math.Round(sd)),
		"scheduled_contract_ends": scheduled, "pipeline": pipeNotes, "revenue_events_left_out_for_missing_rate": fxMissing,
		"band":  "80% range from the month-to-month spread of net new MRR, widening with the square root of months ahead",
		"notes": "Historical churn can include contracts that ended on schedule, and scheduled ends are also subtracted, so the forecast leans low.",
	}
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stdev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := mean(xs)
	var s float64
	for _, x := range xs {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(xs)))
}

var numberRe = regexp.MustCompile(`\d[\d,.]*\d|\d`)

// CommentaryValid checks AI commentary: it must cite only metrics it was given, and every number in
// it must appear in the text of a cited metric. The AI never produces a number of its own.
func CommentaryValid(text string, cited []string, given map[string]string) bool {
	if strings.TrimSpace(text) == "" || len(cited) == 0 {
		return false
	}
	var allowed []string
	for _, id := range cited {
		g, ok := given[id]
		if !ok {
			return false
		}
		allowed = append(allowed, g)
	}
	pool := strings.Join(allowed, " ")
	for _, n := range numberRe.FindAllString(text, -1) {
		n = strings.TrimRight(n, ".,")
		if !containsNumber(pool, n) {
			return false
		}
	}
	return true
}

func containsNumber(pool, n string) bool {
	for _, p := range numberRe.FindAllString(pool, -1) {
		if strings.TrimRight(p, ".,") == n {
			return true
		}
	}
	return false
}
