package migrations

import "testing"

func TestLatestVersionMatchesNewestFile(t *testing.T) {
	v, err := Latest()
	if err != nil || v < 11 {
		t.Fatalf("%d %v", v, err)
	}
}
