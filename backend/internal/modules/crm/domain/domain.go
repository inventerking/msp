// Package domain holds CRM rules that need no database: the default pipeline, stage movement and
// closing rules, and currency codes.
package domain

import (
	"errors"
	"regexp"
)

// Stage kinds.
const (
	Open = "open"
	Won  = "won"
	Lost = "lost"
)

// Stage is one pipeline column.
type Stage struct {
	Key         string
	Name        string
	Position    int
	Kind        string
	Probability int
}

// DefaultStages is the seeded pipeline from the CRM design. Tenants may change it.
var DefaultStages = []Stage{
	{"new_lead", "New lead", 1, Open, 5},
	{"qualified", "Qualified", 2, Open, 10},
	{"contacted", "Contacted", 3, Open, 20},
	{"meeting_scheduled", "Meeting scheduled", 4, Open, 30},
	{"assessment_completed", "Assessment completed", 5, Open, 40},
	{"proposal_sent", "Proposal sent", 6, Open, 60},
	{"negotiation", "Negotiation", 7, Open, 80},
	{"won", "Won", 8, Won, 100},
	{"lost", "Lost", 9, Lost, 0},
}

// Errors for stage changes.
var (
	ErrUseClose     = errors.New("a deal reaches a won or lost stage only by winning or losing it")
	ErrNoValue      = errors.New("a deal needs a contract value or monthly value before it can be won")
	ErrBadStageList = errors.New("a pipeline needs at least one open stage, exactly one won stage and exactly one lost stage")
)

// CanMoveTo checks a user's stage move. Users may move a deal to any open stage, back or forward.
func CanMoveTo(target Stage) error {
	if target.Kind != Open {
		return ErrUseClose
	}
	return nil
}

// AcceptsRequest decides whether a stage suggested by another module applies. Suggestions move a
// deal forward only, and never close it, so a late or repeated event cannot move a deal backward.
func AcceptsRequest(current, target Stage, dealOpen bool) bool {
	return dealOpen && target.Kind == Open && target.Position > current.Position
}

// CanWin checks the value rule for winning.
func CanWin(contractValue, mrr *int64) error {
	if (contractValue == nil || *contractValue <= 0) && (mrr == nil || *mrr <= 0) {
		return ErrNoValue
	}
	return nil
}

// ValidStages checks a pipeline's stage list.
func ValidStages(stages []Stage) error {
	open, won, lost := 0, 0, 0
	keys := map[string]bool{}
	for _, s := range stages {
		if keys[s.Key] || s.Key == "" || s.Name == "" || s.Probability < 0 || s.Probability > 100 {
			return ErrBadStageList
		}
		keys[s.Key] = true
		switch s.Kind {
		case Open:
			open++
		case Won:
			won++
		case Lost:
			lost++
		default:
			return ErrBadStageList
		}
	}
	if open == 0 || won != 1 || lost != 1 {
		return ErrBadStageList
	}
	return nil
}

var currency = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidCurrency checks the shape of an ISO 4217 code. The list of codes in use is reference data.
func ValidCurrency(c string) bool { return currency.MatchString(c) }
