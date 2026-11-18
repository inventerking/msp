package notification

import "testing"

func TestPlatformNoticeHandlesEveryConsumedType(t *testing.T) {
	for _, tc := range []struct{ typ, key, field, want string }{
		{"platform.entitlement.activated", "entitlement.changed", "status", "activated"},
		{"platform.entitlement.revoked", "entitlement.changed", "status", "revoked"},
		{"platform.credits.low", "credits.low", "meter", "credit"},
	} {
		key, data := platformNotice(tc.typ, "crm")
		if key != tc.key || data[tc.field] != tc.want {
			t.Fatalf("%s: got %s %v", tc.typ, key, data)
		}
	}
}
