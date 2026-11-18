// Package notification tells users things, in the app and by email. In-app storage happens in the
// caller's transaction. Email follows as a job, so an email outage cannot fail the triggering work.
package notification

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/events"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/kernel/mailer"
	"mspengine/internal/kernel/realtime"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/notification/repo"
)

// JobDeliver is the job kind that emails a notification.
const JobDeliver = "notification.deliver"

// Changed is the realtime signal for a new notification.
const Changed = "notification.created"

// Security notices cannot be turned off. They are sent directly by the identity service, and any
// key listed here also ignores preferences.
var mandatory = []string{"security.notice"}

// Service implements notifications.
type Service struct {
	pools *db.Pools
	mail  mailer.Mailer
	hub   *realtime.Hub
}

// New creates the service.
func New(pools *db.Pools, m mailer.Mailer, hub *realtime.Hub) *Service {
	return &Service{pools: pools, mail: m, hub: hub}
}

func render(text string, data map[string]string) (string, error) {
	t, err := template.New("n").Option("missingkey=zero").Parse(text)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Notify stores an in-app notification and queues its email, in the caller's transaction. The
// idempotency key means a replayed event notifies nobody twice.
func (s *Service) Notify(ctx context.Context, tx pgx.Tx, tenantID, userID uuid.UUID, key string, data map[string]string, idempotencyKey string) error {
	q := repo.New(tx)
	tmpl, err := q.GetTemplate(ctx, repo.GetTemplateParams{Key: key, Channel: "in_app"})
	if err != nil {
		return fmt.Errorf("template %s: %w", key, err)
	}
	title, err := render(tmpl.Body, data)
	if err != nil {
		return err
	}
	id, err := q.InsertNotification(ctx, repo.InsertNotificationParams{TenantID: &tenantID, RecipientID: userID, NotificationKey: key, Title: title, IdempotencyKey: idempotencyKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // Already notified.
	}
	if err != nil {
		return err
	}
	if !slices.Contains(mandatory, key) {
		enabled, err := q.GetPreference(ctx, repo.GetPreferenceParams{TenantID: tenantID, UserID: userID, NotificationKey: key, Channel: "email"})
		if err == nil && !enabled {
			return nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	payload := map[string]any{"notification_id": id, "data": data}
	if _, _, err := jobs.Enqueue(ctx, tx, jobs.Spec{TenantID: &tenantID, Kind: JobDeliver, Queue: jobs.QueueCritical, Payload: payload, IdempotencyKey: "deliver:" + id.String()}); err != nil {
		return err
	}
	s.hub.Publish(ctx, tenantID, Changed)
	return nil
}

// HandleDeliver emails a notification, and records the attempt.
func (s *Service) HandleDeliver(ctx context.Context, j jobs.Job) error {
	var p struct {
		NotificationID uuid.UUID         `json:"notification_id"`
		Data           map[string]string `json:"data"`
	}
	if err := j.Decode(&p); err != nil {
		return err
	}
	if j.TenantID == nil {
		return jobs.Permanent(errors.New("delivery job has no tenant"))
	}
	var msg mailer.Message
	err := db.TenantTx(ctx, s.pools.App, *j.TenantID, uuid.Nil, func(tx pgx.Tx) error {
		q := repo.New(tx)
		n, err := q.GetNotification(ctx, p.NotificationID)
		if err != nil {
			return err
		}
		tmpl, err := q.GetTemplate(ctx, repo.GetTemplateParams{Key: n.NotificationKey, Channel: "email"})
		if errors.Is(err, pgx.ErrNoRows) {
			return jobs.Permanent(fmt.Errorf("no email template for %s", n.NotificationKey))
		}
		if err != nil {
			return err
		}
		to, err := identity.UserEmail(ctx, tx, n.RecipientID)
		if err != nil {
			return err
		}
		body, err := render(tmpl.Body, p.Data)
		if err != nil {
			return err
		}
		subject := n.Title
		if tmpl.Subject != nil {
			subject = *tmpl.Subject
		}
		msg = mailer.Message{To: to, Subject: subject, Body: body}
		return nil
	})
	if err != nil {
		return err
	}
	sendErr := s.mail.Send(ctx, msg)
	status := "sent"
	var code *string
	if sendErr != nil {
		status = "failed"
		c := "send_failed"
		code = &c
	}
	if err := db.TenantTx(ctx, s.pools.App, *j.TenantID, uuid.Nil, func(tx pgx.Tx) error {
		return repo.New(tx).InsertAttempt(ctx, repo.InsertAttemptParams{NotificationID: p.NotificationID, Channel: "email", Status: status, Attempt: j.Attempt, ErrorCode: code})
	}); err != nil {
		return err
	}
	return sendErr
}

// Consumer maps platform events to notifications for members who handle billing.
func (s *Service) Consumer() events.Consumer {
	return events.Consumer{
		Name:  "notification.platform_events",
		Types: []string{"platform.entitlement.activated", "platform.entitlement.suspended", "platform.entitlement.expired", "platform.entitlement.revoked", "platform.credits.low"},
		Class: events.Lifecycle,
		Handler: func(ctx context.Context, tx pgx.Tx, e events.Event) error {
			if e.TenantID == nil {
				return nil
			}
			var p struct {
				ModuleKey string `json:"module_key"`
			}
			if err := e.Decode(&p); err != nil {
				return err
			}
			key, data := platformNotice(e.Type, p.ModuleKey)
			users, err := identity.MembersWithPermission(ctx, tx, *e.TenantID, "billing.read")
			if err != nil {
				return err
			}
			for _, u := range users {
				if err := s.Notify(ctx, tx, *e.TenantID, u, key, data, e.ID.String()); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// platformNotice picks the template and data for a platform event. Only entitlement events carry a
// status in their type; slicing other types the same way once panicked on platform.credits.low.
func platformNotice(eventType, module string) (string, map[string]string) {
	if eventType == "platform.credits.low" {
		return "credits.low", map[string]string{"meter": "credit"}
	}
	status, _ := strings.CutPrefix(eventType, "platform.entitlement.")
	return "entitlement.changed", map[string]string{"module": module, "status": status}
}

// List returns the caller's notifications and unread count.
func (s *Service) List(ctx context.Context, p authctx.Principal) ([]repo.ListNotificationsRow, int32, error) {
	var rows []repo.ListNotificationsRow
	var unread int32
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		q := repo.New(tx)
		var err error
		if rows, err = q.ListNotifications(ctx, repo.ListNotificationsParams{TenantID: &p.TenantID, RecipientID: p.UserID}); err != nil {
			return err
		}
		unread, err = q.CountUnread(ctx, repo.CountUnreadParams{TenantID: &p.TenantID, RecipientID: p.UserID})
		return err
	})
	return rows, unread, err
}

// MarkRead marks one of the caller's notifications. Anyone else's is not found, even in one tenant.
func (s *Service) MarkRead(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		n, err := repo.New(tx).MarkRead(ctx, repo.MarkReadParams{TenantID: &p.TenantID, RecipientID: p.UserID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound()
		}
		return nil
	})
}

// MarkAllRead clears the caller's unread notifications.
func (s *Service) MarkAllRead(ctx context.Context, p authctx.Principal) error {
	return db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		return repo.New(tx).MarkAllRead(ctx, repo.MarkAllReadParams{TenantID: &p.TenantID, RecipientID: p.UserID})
	})
}

// SetPreference turns one notification channel on or off for the caller.
func (s *Service) SetPreference(ctx context.Context, p authctx.Principal, key, channel string, enabled bool) error {
	if slices.Contains(mandatory, key) && !enabled {
		return apperr.Conflict("notification_required", "Security notices cannot be turned off")
	}
	return db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		return repo.New(tx).UpsertPreference(ctx, repo.UpsertPreferenceParams{TenantID: p.TenantID, UserID: p.UserID, NotificationKey: key, Channel: channel, Enabled: enabled})
	})
}

// Preferences lists the caller's settings.
func (s *Service) Preferences(ctx context.Context, p authctx.Principal) ([]repo.ListPreferencesRow, error) {
	var out []repo.ListPreferencesRow
	err := db.TenantTx(ctx, s.pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		var err error
		out, err = repo.New(tx).ListPreferences(ctx, repo.ListPreferencesParams{TenantID: p.TenantID, UserID: p.UserID})
		return err
	})
	return out, err
}
