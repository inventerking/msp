package commercial

import "time"

// Grant is the part of an entitlement grant that decides effective access.
type Grant struct {
	Status string
	EndsAt *time.Time
}

// Effective calculates a module's status from all of a tenant's grants for it. Any active,
// unexpired grant wins, so ending one purchase never removes access held through another.
// The grants must be ordered oldest first, which makes the last ended status the most recent.
func Effective(grants []Grant, now time.Time) (status string, until *time.Time) {
	last := ""
	suspended := false
	active := false
	for _, g := range grants {
		switch g.Status {
		case "active":
			if g.EndsAt != nil && !g.EndsAt.After(now) {
				last = "expired"
				continue
			}
			if !active || (until != nil && (g.EndsAt == nil || g.EndsAt.After(*until))) {
				until = g.EndsAt
			}
			active = true
		case "suspended":
			suspended = true
		case "expired", "revoked":
			last = g.Status
		}
	}
	switch {
	case active:
		return "active", until
	case suspended:
		return "suspended", nil
	case last != "":
		return last, nil
	}
	return "inactive", nil
}
