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
	for _, v := range []string{m.To, m.Subject, s.From} {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("mail header contains a line break")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.From, m.To, mime.QEncoding.Encode("utf-8", m.Subject), time.Now().UTC().Format(time.RFC1123Z), m.Body)
	return smtp.SendMail(s.Addr, nil, s.From, []string{m.To}, []byte(b.String()))
}
