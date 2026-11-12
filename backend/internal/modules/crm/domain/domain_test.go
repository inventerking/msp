package domain

import "testing"

func TestDefaultPipelineIsValid(t *testing.T) {
	if err := ValidStages(DefaultStages); err != nil {
		t.Fatal(err)
	}
}

func TestValidStages(t *testing.T) {
	open := Stage{"a", "A", 1, Open, 10}
	won := Stage{"w", "W", 2, Won, 100}
	lost := Stage{"l", "L", 3, Lost, 0}
	for name, s := range map[string][]Stage{
		"no open":      {won, lost},
		"two won":      {open, won, {"w2", "W2", 4, Won, 100}, lost},
		"no lost":      {open, won},
		"duplicate":    {open, open, won, lost},
		"bad kind":     {{"x", "X", 1, "maybe", 1}, won, lost},
		"bad percent":  {{"x", "X", 1, Open, 120}, won, lost},
		"missing name": {{"x", "", 1, Open, 1}, won, lost},
	} {
		if ValidStages(s) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestUsersCannotMoveIntoClosedStages(t *testing.T) {
	if CanMoveTo(Stage{Kind: Won}) == nil || CanMoveTo(Stage{Kind: Lost}) == nil || CanMoveTo(Stage{Kind: Open}) != nil {
		t.Fatal("close stages must go through win or lose")
	}
}

func TestRequestsMoveForwardOnly(t *testing.T) {
	cur := Stage{Position: 5, Kind: Open}
	cases := []struct {
		target Stage
		open   bool
		want   bool
	}{
		{Stage{Position: 6, Kind: Open}, true, true},
		{Stage{Position: 4, Kind: Open}, true, false},  // A late event cannot move a deal back.
		{Stage{Position: 5, Kind: Open}, true, false},  // A repeat changes nothing.
		{Stage{Position: 8, Kind: Won}, true, false},   // Only a person can close a deal.
		{Stage{Position: 6, Kind: Open}, false, false}, // A closed deal stays closed.
	}
	for i, c := range cases {
		if got := AcceptsRequest(cur, c.target, c.open); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
}

func TestWinNeedsAValue(t *testing.T) {
	zero, some := int64(0), int64(100)
	if CanWin(nil, nil) == nil || CanWin(&zero, &zero) == nil || CanWin(&some, nil) != nil || CanWin(nil, &some) != nil {
		t.Fatal("win value rule broken")
	}
}

func TestCurrency(t *testing.T) {
	for c, want := range map[string]bool{"GBP": true, "usd": false, "EURO": false, "": false} {
		if ValidCurrency(c) != want {
			t.Errorf("%q", c)
		}
	}
}
