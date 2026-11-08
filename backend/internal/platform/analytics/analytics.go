// Package analytics is common plumbing for read-oriented metrics. Sales Intelligence builds on it in
// Phase 6. Its key rule: actual, estimated and forecast values must never be added together.
package analytics

import (
	"errors"
	"fmt"
)

// ValueKind says how much a number can be trusted.
type ValueKind string

const (
	Actual    ValueKind = "actual"
	Estimated ValueKind = "estimated"
	Forecast  ValueKind = "forecast"
)

// Value is a labelled number. The label travels with it.
type Value struct {
	Amount float64
	Kind   ValueKind
}

// ErrMixedKinds means someone tried to combine numbers of different certainty.
var ErrMixedKinds = errors.New("cannot combine values of different kinds")

// Sum adds values of one kind, and refuses a mix.
func Sum(values ...Value) (Value, error) {
	if len(values) == 0 {
		return Value{}, errors.New("nothing to sum")
	}
	out := Value{Kind: values[0].Kind}
	for _, v := range values {
		if v.Kind != out.Kind {
			return Value{}, ErrMixedKinds
		}
		out.Amount += v.Amount
	}
	return out, nil
}

// Metric defines one metric once, so every report means the same thing by it.
type Metric struct {
	Key  string
	Kind ValueKind
}

// Registry holds metric definitions.
type Registry struct{ metrics map[string]Metric }

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{metrics: map[string]Metric{}} }

// Define adds a metric. Defining one twice is a programming error.
func (r *Registry) Define(m Metric) error {
	if _, dup := r.metrics[m.Key]; dup {
		return fmt.Errorf("metric %s is already defined", m.Key)
	}
	switch m.Kind {
	case Actual, Estimated, Forecast:
	default:
		return fmt.Errorf("metric %s has an unknown kind %q", m.Key, m.Kind)
	}
	r.metrics[m.Key] = m
	return nil
}

// Lookup returns a metric definition.
func (r *Registry) Lookup(key string) (Metric, bool) {
	m, ok := r.metrics[key]
	return m, ok
}

// Convert changes currency for reporting, in minor units. A missing rate gives no result rather
// than a guess, so the caller can flag the amount and leave it out of totals.
func Convert(minor int64, rate float64, rateFound bool) (int64, bool) {
	if !rateFound || rate <= 0 {
		return 0, false
	}
	v := float64(minor) * rate
	if v >= 0 {
		return int64(v + 0.5), true
	}
	return int64(v - 0.5), true
}
