package domain

import (
	"encoding/json"
	"testing"
)

// The worked example from the Proposals design.
func TestDesignExample(t *testing.T) {
	got, err := Calculate("GBP", 36, []Line{{Name: "Managed users", PricingModel: PerUser, Quantity: 50, UnitPriceMinor: 2000, SetupFeeMinor: 50000, DiscountBps: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines[0].RecurringTotalMinor != 90000 || got.SetupTotalMinor != 50000 || got.MRRTotalMinor != 90000 || got.TCVMinor != 3290000 {
		t.Fatalf("%+v", got)
	}
}

func TestFormulaTable(t *testing.T) {
	cases := []struct {
		name     string
		currency string
		term     int64
		lines    []Line
		mrr, tcv int64
		setup    int64
	}{
		{"fixed ignores quantity", "GBP", 12, []Line{{PricingModel: Fixed, Quantity: 7, UnitPriceMinor: 15000, SetupFeeMinor: 0}}, 15000, 180000, 0},
		{"per device", "USD", 24, []Line{{PricingModel: PerDevice, Quantity: 30, UnitPriceMinor: 499}}, 14970, 359280, 0},
		{"half up at line level", "GBP", 1, []Line{{PricingModel: PerUser, Quantity: 3, UnitPriceMinor: 333, DiscountBps: 1550}}, 844, 844, 0}, // 999 × 0.845 = 844.155
		{"exact half rounds up", "GBP", 1, []Line{{PricingModel: Fixed, Quantity: 1, UnitPriceMinor: 1, DiscountBps: 5000}}, 1, 1, 0},          // 0.5 → 1
		{"zero-decimal currency", "JPY", 12, []Line{{PricingModel: PerUser, Quantity: 10, UnitPriceMinor: 1500, SetupFeeMinor: 20000, DiscountBps: 250}}, 14625, 195500, 20000},
		{"full discount", "EUR", 6, []Line{{PricingModel: Fixed, Quantity: 1, UnitPriceMinor: 5000, SetupFeeMinor: 100, DiscountBps: 10000}}, 0, 100, 100},
		{"several lines", "GBP", 36, []Line{{PricingModel: PerUser, Quantity: 50, UnitPriceMinor: 2000, SetupFeeMinor: 50000, DiscountBps: 1000}, {PricingModel: Fixed, Quantity: 1, UnitPriceMinor: 25000, SetupFeeMinor: 10000}}, 115000, 4200000, 60000},
	}
	for _, c := range cases {
		got, err := Calculate(c.currency, c.term, c.lines)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.MRRTotalMinor != c.mrr || got.TCVMinor != c.tcv || got.SetupTotalMinor != c.setup {
			t.Errorf("%s: mrr %d tcv %d setup %d", c.name, got.MRRTotalMinor, got.TCVMinor, got.SetupTotalMinor)
		}
		if got.TCVMinor != got.SetupTotalMinor+got.MRRTotalMinor*got.TermMonths || got.TaxNote == "" {
			t.Errorf("%s: TCV identity or tax note broken", c.name)
		}
	}
}

func TestInvalidInputs(t *testing.T) {
	ok := Line{PricingModel: Fixed, Quantity: 1, UnitPriceMinor: 1}
	for name, c := range map[string]struct {
		term  int64
		lines []Line
	}{
		"no lines":       {12, nil},
		"zero term":      {0, []Line{ok}},
		"long term":      {121, []Line{ok}},
		"zero quantity":  {12, []Line{{PricingModel: PerUser, Quantity: 0, UnitPriceMinor: 1}}},
		"negative price": {12, []Line{{PricingModel: Fixed, Quantity: 1, UnitPriceMinor: -1}}},
		"discount > 100": {12, []Line{{PricingModel: Fixed, Quantity: 1, UnitPriceMinor: 1, DiscountBps: 10001}}},
		"unknown model":  {12, []Line{{PricingModel: "per_site", Quantity: 1, UnitPriceMinor: 1}}},
		"huge quantity":  {12, []Line{{PricingModel: PerUser, Quantity: MaxQuantity + 1, UnitPriceMinor: 1}}},
	} {
		if _, err := Calculate("GBP", c.term, c.lines); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The largest allowed values must not overflow.
func TestNoOverflowAtLimits(t *testing.T) {
	lines := make([]Line, MaxLines)
	for i := range lines {
		lines[i] = Line{PricingModel: PerUser, Quantity: MaxQuantity, UnitPriceMinor: MaxUnitMinor, SetupFeeMinor: MaxUnitMinor}
	}
	got, err := Calculate("GBP", MaxTermMonths, lines)
	if err != nil {
		t.Fatal(err)
	}
	if got.TCVMinor <= 0 || got.MRRTotalMinor <= 0 {
		t.Fatalf("overflow: %+v", got.TCVMinor)
	}
}

func TestSnapshotHashChangesWithContent(t *testing.T) {
	tot, _ := Calculate("GBP", 12, []Line{{PricingModel: Fixed, Quantity: 1, UnitPriceMinor: 100}})
	a := Snapshot{Title: "A", Content: json.RawMessage(`{"x":1}`), Totals: tot}
	b := a
	b.Totals.MRRTotalMinor++
	_, ha, _ := Hash(a)
	_, ha2, _ := Hash(a)
	_, hb, _ := Hash(b)
	if string(ha) != string(ha2) || string(ha) == string(hb) {
		t.Fatal("hash is not stable, or ignores a price change")
	}
}

func TestStateMachine(t *testing.T) {
	for _, ok := range [][2]string{{"draft", "pending_approval"}, {"pending_approval", "approved"}, {"approved", "issued"}, {"issued", "superseded"}, {"draft", "issued"}} {
		if !CanMove(ok[0], ok[1]) {
			t.Errorf("%v refused", ok)
		}
	}
	for _, bad := range [][2]string{{"issued", "draft"}, {"superseded", "issued"}, {"pending_approval", "issued"}, {"expired", "issued"}} {
		if CanMove(bad[0], bad[1]) {
			t.Errorf("%v allowed", bad)
		}
	}
}
