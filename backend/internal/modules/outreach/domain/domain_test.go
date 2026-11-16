package domain

import (
	"strings"
	"testing"
	"time"
)

func good() Facts {
	h := []byte("hash")
	return Facts{Entitled: true, CampaignRunning: true, EnrollmentActive: true, ApprovedHash: h, CurrentHash: h, Country: "US", Rules: map[string]string{"US": OptOutAllowed},
		SenderActive: true, DomainAuthenticated: true, WindowOpen: true, DailyLimit: 50, HasEmail: true}
}

func TestEveryGateCanStopASend(t *testing.T) {
	if g := Check(good()); g.Result != Pass {
		t.Fatalf("all gates pass: %+v", g)
	}
	cases := map[string]func(*Facts){
		"not_entitled":         func(f *Facts) { f.Entitled = false },
		"campaign_not_running": func(f *Facts) { f.CampaignRunning = false },
		"enrollment_inactive":  func(f *Facts) { f.EnrollmentActive = false },
		"approval_mismatch":    func(f *Facts) { f.CurrentHash = []byte("edited") },
		"missing_email":        func(f *Facts) { f.HasEmail = false },
		"suppressed":           func(f *Facts) { f.GloballySuppressed = true },
		"jurisdiction_block":   func(f *Facts) { f.Country = "" },
		"sender_not_ready":     func(f *Facts) { f.ReputationPaused = true },
		"outside_window":       func(f *Facts) { f.WindowOpen = false },
		"daily_limit":          func(f *Facts) { f.SentToday = 50 },
	}
	for reason, change := range cases {
		f := good()
		change(&f)
		if g := Check(f); g.Result == Pass || g.Reason != reason {
			t.Errorf("%s: %+v", reason, g)
		}
	}
	f := good()
	f.ApprovedHash = nil
	if Check(f).Result == Pass {
		t.Fatal("a campaign with no approval sent")
	}
}

func TestJurisdiction(t *testing.T) {
	rules := map[string]string{"US": OptOutAllowed, "CA": ConsentRequired, "DE": Blocked}
	for _, c := range []struct {
		country string
		consent bool
		want    bool
	}{
		{"US", false, true}, {"us", false, true}, {"CA", false, false}, {"CA", true, true}, {"DE", true, false}, {"FR", true, false}, {"", true, false},
	} {
		if got := JurisdictionAllows(c.country, rules, c.consent); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestRenderAlwaysAddsUnsubscribeAndAddress(t *testing.T) {
	r := Render("Hi {{first_name}}\r\nBcc: x@evil.example", "Hello {{first_name}} at {{company}}.", Recipient{FullName: "Jo Buyer", Company: "Acme"}, "https://u/1", "https://u/1/one-click", "1 High St, Leeds", "Sam")
	if strings.ContainsAny(r.Subject, "\r\n") || !strings.HasPrefix(r.Subject, "Hi Jo") {
		t.Fatalf("subject: %q", r.Subject)
	}
	if !strings.Contains(r.Body, "Hello Jo at Acme.") || !strings.Contains(r.Body, "https://u/1") || !strings.Contains(r.Body, "1 High St, Leeds") {
		t.Fatalf("body: %q", r.Body)
	}
	if r.Headers["List-Unsubscribe"] != "<https://u/1/one-click>" || r.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Fatalf("headers: %v", r.Headers)
	}
}

func TestWindow(t *testing.T) {
	w := Window{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "17:00"}
	if w.Validate() != nil || (Window{Days: []int{1}, Start: "17:00", End: "09:00"}).Validate() == nil || (Window{Start: "09:00", End: "10:00"}).Validate() == nil {
		t.Fatal("validation")
	}
	london, _ := time.LoadLocation("Europe/London")
	monNoon := time.Date(2026, 11, 9, 12, 0, 0, 0, london)
	if ok, _ := w.Open(monNoon, london); !ok {
		t.Fatal("Monday noon should be open")
	}
	satNoon := time.Date(2026, 11, 14, 12, 0, 0, 0, london)
	ok, next := w.Open(satNoon, london)
	if ok || next.In(london).Weekday() != time.Monday || next.In(london).Hour() != 9 {
		t.Fatalf("Saturday: %v %v", ok, next)
	}
	monEarly := time.Date(2026, 11, 9, 7, 0, 0, 0, london)
	if ok, next := w.Open(monEarly, london); ok || next.In(london).Hour() != 9 || next.In(london).Day() != 9 {
		t.Fatalf("early: %v %v", ok, next)
	}
}

func TestHashCoversContent(t *testing.T) {
	a := Approvable{Sender: "s@x.example", Postal: "1 St", Steps: []Step{{Position: 1, Subject: "Hi", Body: "B"}}}
	b := a
	b.Steps = []Step{{Position: 1, Subject: "Hi", Body: "B changed"}}
	again := a
	again.Steps = []Step{{Position: 1, Subject: "Hi", Body: "B"}}
	if string(Hash(a)) == string(Hash(b)) || string(Hash(a)) != string(Hash(again)) {
		t.Fatal("hash")
	}
}
