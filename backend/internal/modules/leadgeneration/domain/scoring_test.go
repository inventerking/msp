package domain

import "testing"

func ptr(n int) *int { return &n }

var icp = ICP{Industries: []string{"legal_services"}, Countries: []string{"GB"}, MinEmployees: ptr(20), MaxEmployees: ptr(200), Technologies: []string{"m365"}}

func TestEveryFactorUnknownGivesNeutral50(t *testing.T) {
	s, ok := ScoreProspect(icp, DefaultWeights, Facts{})
	if !ok || s.Value != 50 || s.Coverage != 0 {
		t.Fatalf("%+v %v", s, ok)
	}
	for _, f := range s.Factors {
		if f.State != Unknown || f.Earned != nil {
			t.Fatalf("factor %+v", f)
		}
	}
}

func TestPerfectFit(t *testing.T) {
	s, _ := ScoreProspect(icp, DefaultWeights, Facts{Industry: "legal_services", Country: "gb", Employees: ptr(50), Technologies: []string{"M365", "other"}})
	if s.Value != 100 || s.Coverage != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestNoFit(t *testing.T) {
	s, _ := ScoreProspect(icp, DefaultWeights, Facts{Industry: "retail", Country: "US", Employees: ptr(5000), Technologies: []string{"gws"}})
	if s.Value != 0 || s.Coverage != 1 {
		t.Fatalf("%+v", s)
	}
}

// The worked example from the design: three of four equal factors known, two of them matched.
func TestDesignExample(t *testing.T) {
	s, _ := ScoreProspect(icp, DefaultWeights, Facts{Industry: "legal_services", Country: "GB", Employees: ptr(5000)})
	if s.Value != 63 || s.Coverage != 0.75 {
		t.Fatalf("%+v", s) // 100 * (0.75 * 2/3 + 0.25 * 0.5) = 62.5, rounded.
	}
}

// Missing data must never count against a prospect.
func TestUnknownNeverScoresBelowNeutralForAnOtherwisePerfectFit(t *testing.T) {
	full, _ := ScoreProspect(icp, DefaultWeights, Facts{Industry: "legal_services", Country: "GB", Employees: ptr(50), Technologies: []string{"m365"}})
	partial, _ := ScoreProspect(icp, DefaultWeights, Facts{Industry: "legal_services"})
	if partial.Value <= 50 || partial.Value >= full.Value {
		t.Fatalf("partial %d, full %d", partial.Value, full.Value)
	}
	// One matched factor out of four cannot produce a top score.
	if partial.Value > 70 {
		t.Fatalf("thin data gave an extreme score: %d", partial.Value)
	}
}

func TestEmptyCriterionIsIgnored(t *testing.T) {
	s, ok := ScoreProspect(ICP{Industries: []string{"healthcare"}}, DefaultWeights, Facts{Industry: "healthcare", Country: "ZZ"})
	if !ok || s.Value != 100 || len(s.Factors) != 1 {
		t.Fatalf("%+v %v", s, ok)
	}
	if _, ok := ScoreProspect(ICP{}, DefaultWeights, Facts{Industry: "x"}); ok {
		t.Fatal("an empty profile should not produce a score")
	}
}

func TestEmployeeBands(t *testing.T) {
	for n, want := range map[int]string{20: Matched, 200: Matched, 12: Partial, 250: Partial, 5: NotMatched, 400: NotMatched} {
		if got, _ := employeeFit(n, ptr(20), ptr(200)); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
}

func TestTechnologyPartialCredit(t *testing.T) {
	s, _ := ScoreProspect(ICP{Technologies: []string{"m365", "azure"}}, DefaultWeights, Facts{Technologies: []string{"azure"}})
	if s.Value != 50 || s.Factors[0].State != Partial {
		t.Fatalf("%+v", s)
	}
}

func TestWeightsChangeTheBalance(t *testing.T) {
	f := Facts{Industry: "legal_services", Country: "US"}
	heavyIndustry, _ := ScoreProspect(icp, Weights{Industry: 90, Geography: 10}, f)
	heavyGeo, _ := ScoreProspect(icp, Weights{Industry: 10, Geography: 90}, f)
	if heavyIndustry.Value <= heavyGeo.Value {
		t.Fatalf("%d vs %d", heavyIndustry.Value, heavyGeo.Value)
	}
	zero, ok := ScoreProspect(icp, Weights{Industry: 100}, f)
	if !ok || len(zero.Factors) != 1 {
		t.Fatalf("a zero-weight factor should not count: %+v", zero)
	}
}

func TestDataQuality(t *testing.T) {
	for c, want := range map[float64]string{1: "high", 0.75: "high", 0.5: "medium", 0.25: "low", 0: "unknown"} {
		if got := DataQuality(c, true); got != want {
			t.Errorf("%v: %s", c, got)
		}
	}
	if DataQuality(1, false) != "unknown" {
		t.Error("unscored should be unknown")
	}
}
