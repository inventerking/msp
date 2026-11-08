package analytics

import (
	"errors"
	"testing"
)

func TestSumRefusesMixedKinds(t *testing.T) {
	v, err := Sum(Value{1, Actual}, Value{2, Actual})
	if err != nil || v.Amount != 3 || v.Kind != Actual {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := Sum(Value{1, Actual}, Value{2, Forecast}); !errors.Is(err, ErrMixedKinds) {
		t.Fatalf("got %v", err)
	}
	if _, err := Sum(); err == nil {
		t.Fatal("an empty sum should fail")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if err := r.Define(Metric{"mrr", Actual}); err != nil {
		t.Fatal(err)
	}
	if err := r.Define(Metric{"mrr", Actual}); err == nil {
		t.Fatal("a duplicate was accepted")
	}
	if err := r.Define(Metric{"x", "guess"}); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if m, ok := r.Lookup("mrr"); !ok || m.Kind != Actual {
		t.Fatalf("%+v %v", m, ok)
	}
}

func TestConvert(t *testing.T) {
	if v, ok := Convert(1000, 1.2, true); !ok || v != 1200 {
		t.Fatalf("%d %v", v, ok)
	}
	if v, ok := Convert(-1000, 1.25, true); !ok || v != -1250 {
		t.Fatalf("%d %v", v, ok)
	}
	if _, ok := Convert(1000, 0, false); ok {
		t.Fatal("converted without a rate")
	}
	if _, ok := Convert(1000, -1, true); ok {
		t.Fatal("converted with a bad rate")
	}
}
