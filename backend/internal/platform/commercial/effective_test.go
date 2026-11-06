package commercial

import (
	"testing"
	"time"
)

func TestEffective(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	past, future, later := now.Add(-time.Hour), now.Add(time.Hour), now.Add(2*time.Hour)
	tests := []struct {
		name   string
		grants []Grant
		want   string
		until  *time.Time
	}{
		{"no grants", nil, "inactive", nil},
		{"pending does not grant access", []Grant{{Status: "pending"}}, "inactive", nil},
		{"active", []Grant{{Status: "active"}}, "active", nil},
		{"bundle ended but standalone remains", []Grant{{Status: "expired"}, {Status: "active"}}, "active", nil},
		{"active beats suspended", []Grant{{Status: "suspended"}, {Status: "active"}}, "active", nil},
		{"suspended beats ended", []Grant{{Status: "revoked"}, {Status: "suspended"}}, "suspended", nil},
		{"latest ended status wins", []Grant{{Status: "revoked"}, {Status: "expired"}}, "expired", nil},
		{"past end date is expired", []Grant{{Status: "active", EndsAt: &past}}, "expired", nil},
		{"latest end date applies", []Grant{{Status: "active", EndsAt: &future}, {Status: "active", EndsAt: &later}}, "active", &later},
		{"open-ended grant has no end", []Grant{{Status: "active", EndsAt: &future}, {Status: "active"}}, "active", nil},
		{"revoked alone", []Grant{{Status: "revoked"}}, "revoked", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, until := Effective(tc.grants, now)
			if got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
			if (until == nil) != (tc.until == nil) || (until != nil && !until.Equal(*tc.until)) {
				t.Fatalf("until = %v, want %v", until, tc.until)
			}
		})
	}
}
