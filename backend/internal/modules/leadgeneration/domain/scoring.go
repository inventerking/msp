// Package domain holds Lead Generation rules with no database or HTTP dependencies.
package domain

import (
	"math"
	"slices"
	"strings"
)

// Factor states. An unknown factor means there is no data, which is not evidence of a poor fit.
const (
	Matched    = "matched"
	Partial    = "partial"
	NotMatched = "not_matched"
	Unknown    = "unknown"
)

// ICP is the tenant's target customer profile. An empty criterion means no preference, so that
// factor does not count towards the score at all.
type ICP struct {
	Industries   []string
	Countries    []string
	MinEmployees *int
	MaxEmployees *int
	Technologies []string
}

// Weights for each factor. They are relative: only their proportions matter.
type Weights struct {
	Industry   float64 `json:"industry"`
	Geography  float64 `json:"geography"`
	Employees  float64 `json:"employees"`
	Technology float64 `json:"technology"`
}

// DefaultWeights gives each factor equal importance.
var DefaultWeights = Weights{Industry: 25, Geography: 25, Employees: 25, Technology: 25}

// Facts are what is known about a company. Nil or empty means unknown.
type Facts struct {
	Industry     string
	Country      string
	Employees    *int
	Technologies []string
}

// Factor is one line of a score's explanation.
type Factor struct {
	Key      string         `json:"key"`
	State    string         `json:"state"`
	Weight   float64        `json:"weight"`
	Earned   *float64       `json:"earned"`
	Evidence map[string]any `json:"evidence"`
}

// Score is a 0-100 score with the share of evidence behind it.
type Score struct {
	Value    int
	Coverage float64
	Factors  []Factor
}

// ScoreProspect applies the method from the Lead Generation design:
//
//	known = points earned / weight of known factors
//	coverage = weight of known factors / total weight
//	score = 100 * (coverage * known + (1 - coverage) * 0.5)
//
// Unknown data pulls the score towards a neutral 50 instead of down, and thin data cannot give an
// extreme score. It returns ok=false when the ICP sets no criteria, as there is nothing to score.
func ScoreProspect(icp ICP, w Weights, f Facts) (Score, bool) {
	var factors []Factor
	add := func(key string, weight float64, state string, fraction float64, evidence map[string]any) {
		if weight <= 0 {
			return
		}
		fc := Factor{Key: key, State: state, Weight: weight, Evidence: evidence}
		if state != Unknown {
			e := round2(weight * fraction)
			fc.Earned = &e
		}
		factors = append(factors, fc)
	}
	if len(icp.Industries) > 0 {
		switch {
		case f.Industry == "":
			add("industry", w.Industry, Unknown, 0, map[string]any{"target": icp.Industries})
		case slices.Contains(icp.Industries, f.Industry):
			add("industry", w.Industry, Matched, 1, map[string]any{"value": f.Industry})
		default:
			add("industry", w.Industry, NotMatched, 0, map[string]any{"value": f.Industry, "target": icp.Industries})
		}
	}
	if len(icp.Countries) > 0 {
		switch {
		case f.Country == "":
			add("geography", w.Geography, Unknown, 0, map[string]any{"target": icp.Countries})
		case slices.Contains(icp.Countries, strings.ToUpper(f.Country)):
			add("geography", w.Geography, Matched, 1, map[string]any{"value": f.Country})
		default:
			add("geography", w.Geography, NotMatched, 0, map[string]any{"value": f.Country, "target": icp.Countries})
		}
	}
	if icp.MinEmployees != nil || icp.MaxEmployees != nil {
		target := map[string]any{"min": icp.MinEmployees, "max": icp.MaxEmployees}
		if f.Employees == nil {
			add("employees", w.Employees, Unknown, 0, target)
		} else {
			state, frac := employeeFit(*f.Employees, icp.MinEmployees, icp.MaxEmployees)
			target["value"] = *f.Employees
			add("employees", w.Employees, state, frac, target)
		}
	}
	if len(icp.Technologies) > 0 {
		if len(f.Technologies) == 0 {
			add("technology", w.Technology, Unknown, 0, map[string]any{"target": icp.Technologies})
		} else {
			hit := 0
			for _, t := range icp.Technologies {
				if slices.ContainsFunc(f.Technologies, func(x string) bool { return strings.EqualFold(x, t) }) {
					hit++
				}
			}
			frac := float64(hit) / float64(len(icp.Technologies))
			state := Partial
			switch hit {
			case 0:
				state = NotMatched
			case len(icp.Technologies):
				state = Matched
			}
			add("technology", w.Technology, state, frac, map[string]any{"value": f.Technologies, "target": icp.Technologies})
		}
	}
	if len(factors) == 0 {
		return Score{}, false
	}
	var total, known, earned float64
	for _, fc := range factors {
		total += fc.Weight
		if fc.Earned != nil {
			known += fc.Weight
			earned += *fc.Earned
		}
	}
	coverage := known / total
	knownScore := 0.0
	if known > 0 {
		knownScore = earned / known
	}
	value := 100 * (coverage*knownScore + (1-coverage)*0.5)
	return Score{Value: int(math.Round(value)), Coverage: round2(coverage), Factors: factors}, true
}

// employeeFit matches inside the range, gives half credit within 50% outside it, and none beyond.
func employeeFit(n int, lo, hi *int) (string, float64) {
	inside := (lo == nil || n >= *lo) && (hi == nil || n <= *hi)
	if inside {
		return Matched, 1
	}
	near := (lo == nil || float64(n) >= float64(*lo)*0.5) && (hi == nil || float64(n) <= float64(*hi)*1.5)
	if near {
		return Partial, 0.5
	}
	return NotMatched, 0
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// DataQuality labels how much is known about a prospect.
func DataQuality(coverage float64, scored bool) string {
	switch {
	case !scored:
		return "unknown"
	case coverage >= 0.75:
		return "high"
	case coverage >= 0.5:
		return "medium"
	case coverage > 0:
		return "low"
	}
	return "unknown"
}
