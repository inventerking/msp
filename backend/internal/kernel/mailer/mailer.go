// Package mailer sends transactional email. It is the minimal Phase 1 adapter; the Phase 2
// notification service will wrap it with retries and delivery records.
package mailer

import (
	"context"
	"fmt"
	"mime"
	"net/smtp"
	"strings"
	"time"
)

// Message is one plain-text email.
type Message struct {
	To      string
	Subject string
	Body    string
	// From overrides the relay's default sender, for mail sent on a tenant's behalf.
	From string
	// Headers adds headers such as List-Unsubscribe. Names and values are checked for line breaks.
	Headers map[string]string
}

// Mailer sends email.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends through an SMTP relay. Locally this is Mailpit, which never delivers externally.
type SMTP struct {
	Addr string
	From string
}

// Send builds and sends the message. Header values are checked so input cannot inject headers.
func (s SMTP) Send(_ context.Context, m Message) error {
	from := s.From
	if m.From != "" {
		from = m.From
	}
	check := []string{m.To, m.Subject, from}
	for k, v := range m.Headers {
		check = append(check, k, v)
	}
	for _, v := range check {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("mail header contains a line break")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n", from, m.To, mime.QEncoding.Encode("utf-8", m.Subject), time.Now().UTC().Format(time.RFC1123Z))
	for k, v := range m.Headers {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", m.Body)
	// The envelope sender stays the relay's own address; From is what the recipient sees.
	return smtp.SendMail(s.Addr, nil, s.From, []string{m.To}, []byte(b.String()))
}
