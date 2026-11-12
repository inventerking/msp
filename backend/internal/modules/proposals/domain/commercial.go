// Package domain holds the Proposals rules that need no database: the commercial calculation, the
// version state machine, and snapshot hashing. Every price shown to a customer comes from here.
package domain

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
)

// Pricing models.
const (
	PerUser   = "per_user"
	PerDevice = "per_device"
	Fixed     = "fixed"
)

// Line is one priced service. Amounts are in a currency's minor unit.
type Line struct {
	CatalogItemID  *string `json:"catalog_item_id,omitempty"`
	Name           string  `json:"name"`
	Description    string  `json:"description,omitempty"`
	PricingModel   string  `json:"pricing_model"`
	Quantity       int64   `json:"quantity"`
	UnitPriceMinor int64   `json:"unit_price_minor"`
	SetupFeeMinor  int64   `json:"setup_fee_minor"`
	DiscountBps    int64   `json:"discount_bps"`
}

// LineTotal is a line with its computed recurring total.
type LineTotal struct {
	Line
	RecurringTotalMinor int64 `json:"recurring_total_minor"`
}

// Totals is the result of a calculation.
type Totals struct {
	Currency        string      `json:"currency"`
	Lines           []LineTotal `json:"lines"`
	SetupTotalMinor int64       `json:"setup_total_minor"`
	MRRTotalMinor   int64       `json:"mrr_total_minor"`
	TermMonths      int64       `json:"term_months"`
	TCVMinor        int64       `json:"tcv_minor"`
	TaxNote         string      `json:"tax_note"`
}

// TaxNote is shown on every proposal until D-11 adds tax lines.
const TaxNote = "All prices exclude tax."

// Limits keep every total far inside int64, so no calculation can overflow.
const (
	MaxQuantity   = 1_000_000
	MaxUnitMinor  = 1_000_000_000_00 // One billion in major units.
	MaxTermMonths = 120
	MaxLines      = 200
)

// Calculation errors.
var (
	ErrNoLines      = errors.New("a proposal needs at least one line")
	ErrBadLine      = errors.New("a line has an invalid quantity, price, discount or pricing model")
	ErrBadTerm      = errors.New("the term must be between 1 and 120 months")
	ErrTooManyLines = errors.New("a proposal can have at most 200 lines")
)

// Calculate prices every line and the totals. The server always calculates: values sent by a
// client are never trusted. Rounding is half up, at line level, in the minor unit.
//
//	line recurring = quantity × unit price, less discount   (per user, per device)
//	line recurring = unit price, less discount              (fixed)
//	setup total    = sum of line setup fees
//	MRR total      = sum of line recurring totals
//	TCV            = setup total + MRR total × term months
func Calculate(currency string, termMonths int64, lines []Line) (Totals, error) {
	if len(lines) == 0 {
		return Totals{}, ErrNoLines
	}
	if len(lines) > MaxLines {
		return Totals{}, ErrTooManyLines
	}
	if termMonths < 1 || termMonths > MaxTermMonths {
		return Totals{}, ErrBadTerm
	}
	t := Totals{Currency: currency, TermMonths: termMonths, TaxNote: TaxNote}
	for _, l := range lines {
		if l.Quantity < 1 || l.Quantity > MaxQuantity || l.UnitPriceMinor < 0 || l.UnitPriceMinor > MaxUnitMinor ||
			l.SetupFeeMinor < 0 || l.SetupFeeMinor > MaxUnitMinor || l.DiscountBps < 0 || l.DiscountBps > 10_000 {
			return Totals{}, ErrBadLine
		}
		gross := l.UnitPriceMinor
		switch l.PricingModel {
		case PerUser, PerDevice:
			gross = l.Quantity * l.UnitPriceMinor
		case Fixed:
		default:
			return Totals{}, ErrBadLine
		}
		net := applyDiscount(gross, l.DiscountBps)
		t.Lines = append(t.Lines, LineTotal{Line: l, RecurringTotalMinor: net})
		t.SetupTotalMinor += l.SetupFeeMinor
		t.MRRTotalMinor += net
	}
	t.TCVMinor = t.SetupTotalMinor + t.MRRTotalMinor*termMonths
	return t, nil
}

// applyDiscount takes basis points off an amount, rounding half up, in exact integer arithmetic.
func applyDiscount(gross, bps int64) int64 {
	n := new(big.Int).Mul(big.NewInt(gross), big.NewInt(10_000-bps))
	n.Add(n, big.NewInt(5_000)) // Half up: add half the divisor before dividing.
	n.Quo(n, big.NewInt(10_000))
	return n.Int64()
}

// Snapshot is everything a customer is shown when a version is issued. Once issued, the database
// refuses to change it.
type Snapshot struct {
	Title    string          `json:"title"`
	Customer string          `json:"customer"`
	Content  json.RawMessage `json:"content"`
	Totals   Totals          `json:"totals"`
	SLA      string          `json:"sla,omitempty"`
	Terms    string          `json:"terms,omitempty"`
	Version  int32           `json:"version"`
	Valid    string          `json:"valid_until"`
}

// Hash returns the SHA-256 of a snapshot's canonical JSON. Approvals and issue requests carry the
// hash they saw, so nobody approves or sends something other than what they looked at.
func Hash(s Snapshot) ([]byte, []byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(b)
	return b, sum[:], nil
}

// Version statuses, and the moves allowed between them.
var transitions = map[string][]string{
	"draft":            {"pending_approval", "issued"},
	"pending_approval": {"approved", "draft"},
	"approved":         {"issued", "draft"},
	"issued":           {"superseded", "expired"},
}

// CanMove reports whether a version may go from one status to another.
func CanMove(from, to string) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}
