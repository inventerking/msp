// Package domain holds Outreach rules that need no database: rendering, the sending window,
// jurisdiction rules and the send gates. Every message passes every gate immediately before it is
// sent, not only when its campaign starts.
package domain

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// Window says when a campaign may send, in its own time zone.
type Window struct {
	Days  []int  `json:"days"`  // 0 = Sunday ... 6 = Saturday.
	Start string `json:"start"` // "09:00".
	End   string `json:"end"`   // "17:00".
}

// Validate checks a window's shape.
func (w Window) Validate() error {
	if len(w.Days) == 0 {
		return errors.New("choose at least one day")
	}
	for _, d := range w.Days {
		if d < 0 || d > 6 {
			return errors.New("days are 0 to 6")
		}
	}
	s, err1 := time.Parse("15:04", w.Start)
	e, err2 := time.Parse("15:04", w.End)
	if err1 != nil || err2 != nil || !e.After(s) {
		return errors.New("the window must start before it ends, as HH:MM")
	}
	return nil
}

// Open reports whether now falls inside the window, and if not, when it next opens.
func (w Window) Open(now time.Time, loc *time.Location) (bool, time.Time) {
	local := now.In(loc)
	s, _ := time.Parse("15:04", w.Start)
	e, _ := time.Parse("15:04", w.End)
	for i := 0; i < 8; i++ {
		day := local.AddDate(0, 0, i)
		start := time.Date(day.Year(), day.Month(), day.Day(), s.Hour(), s.Minute(), 0, 0, loc)
		end := time.Date(day.Year(), day.Month(), day.Day(), e.Hour(), e.Minute(), 0, 0, loc)
		if !slices.Contains(w.Days, int(start.Weekday())) {
			continue
		}
		if i == 0 && !local.Before(start) && local.Before(end) {
			return true, now
		}
		if local.Before(start) {
			return false, start
		}
	}
	return false, now.Add(24 * time.Hour)
}

// Jurisdiction rules, set by platform admins from legal advice.
const (
	OptOutAllowed   = "opt_out_allowed"
	ConsentRequired = "consent_required"
	Blocked         = "blocked"
)

// JurisdictionAllows decides whether a recipient may be emailed. A recipient whose country is
// unknown, or whose country has no rule, is blocked (D-10).
func JurisdictionAllows(country string, rules map[string]string, hasConsent bool) bool {
	if country == "" {
		return false
	}
	switch rules[strings.ToUpper(country)] {
	case OptOutAllowed:
		return true
	case ConsentRequired:
		return hasConsent
	}
	return false
}

// Recipient is who a message goes to. Only these fields can be merged into text.
type Recipient struct {
	FirstName, FullName, Company, Email, Country string
}

// Rendered is a message ready to send.
type Rendered struct {
	Subject, Body string
	Headers       map[string]string
}

// Render merges a template with a recipient and always adds the unsubscribe link and postal
// address, so a template cannot leave them out. Merge values are plain text: no markup is sent.
func Render(subject, body string, r Recipient, unsubscribeURL, oneClickURL, postal, fromName string) Rendered {
	first := r.FirstName
	if first == "" {
		first = strings.Fields(r.FullName + " there")[0]
	}
	rep := strings.NewReplacer("{{first_name}}", oneLine(first), "{{company}}", oneLine(r.Company), "{{sender_name}}", oneLine(fromName))
	out := Rendered{Subject: oneLine(rep.Replace(subject)), Body: rep.Replace(body)}
	out.Body = strings.TrimRight(out.Body, "\n") + "\n\n--\n" + postal + "\nTo stop receiving these emails, unsubscribe: " + unsubscribeURL + "\n"
	out.Headers = map[string]string{
		"List-Unsubscribe":      "<" + oneClickURL + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
	return out
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Step is one sequence step's approved content.
type Step struct {
	Position     int    `json:"position"`
	DelayMinutes int    `json:"delay_minutes"`
	Subject      string `json:"subject"`
	Body         string `json:"body"`
	AIGenerated  bool   `json:"ai_generated"`
}

// Approvable is everything an approver signs off on for a campaign.
type Approvable struct {
	Sender  string `json:"sender"`
	Purpose string `json:"purpose"`
	Postal  string `json:"postal_address"`
	Steps   []Step `json:"steps"`
}

// Hash is the SHA-256 of canonical JSON. A send re-computes it, so an edit after approval stops sends.
func Hash(a Approvable) []byte {
	b, _ := json.Marshal(a)
	sum := sha256.Sum256(b)
	return sum[:]
}

// Gate results. Stop ends the recipient's enrollment; Wait tries again later; Hold pauses the
// campaign for a person to look at.
const (
	Pass = "pass"
	Stop = "stop"
	Wait = "wait"
	Hold = "hold"
)

// Facts are what the gates are checked against, gathered fresh before each send.
type Facts struct {
	Entitled, CampaignRunning, EnrollmentActive bool
	ApprovedHash, CurrentHash                   []byte
	Suppressed, GloballySuppressed              bool
	Country                                     string
	Rules                                       map[string]string
	HasConsent                                  bool
	SenderActive, DomainAuthenticated           bool
	ReputationPaused                            bool
	WindowOpen                                  bool
	SentToday, DailyLimit                       int
	HasEmail                                    bool
}

// Gate is the first gate that failed.
type Gate struct {
	Result, Reason string
}

// Check applies the send gates in order. A send happens only when every gate passes.
func Check(f Facts) Gate {
	switch {
	case !f.Entitled:
		return Gate{Hold, "not_entitled"}
	case !f.CampaignRunning:
		return Gate{Hold, "campaign_not_running"}
	case !f.EnrollmentActive:
		return Gate{Stop, "enrollment_inactive"}
	case len(f.ApprovedHash) == 0 || string(f.ApprovedHash) != string(f.CurrentHash):
		return Gate{Hold, "approval_mismatch"}
	case !f.HasEmail:
		return Gate{Stop, "missing_email"}
	case f.Suppressed || f.GloballySuppressed:
		return Gate{Stop, "suppressed"}
	case !JurisdictionAllows(f.Country, f.Rules, f.HasConsent):
		return Gate{Stop, "jurisdiction_block"}
	case !f.SenderActive || !f.DomainAuthenticated || f.ReputationPaused:
		return Gate{Hold, "sender_not_ready"}
	case !f.WindowOpen:
		return Gate{Wait, "outside_window"}
	case f.DailyLimit > 0 && f.SentToday >= f.DailyLimit:
		return Gate{Wait, "daily_limit"}
	}
	return Gate{Pass, ""}
}
