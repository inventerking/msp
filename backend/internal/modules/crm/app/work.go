package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/modules/crm/repo"
	"mspengine/internal/platform/audit"
)

// subjectExists checks that a note, attachment, task or meeting refers to a record of this tenant.
func subjectExists(ctx context.Context, q *repo.Queries, tenant uuid.UUID, kind string, id uuid.UUID) error {
	var err error
	switch kind {
	case "company":
		_, err = q.GetCompany(ctx, repo.GetCompanyParams{TenantID: tenant, ID: id})
	case "contact":
		_, err = q.GetContact(ctx, repo.GetContactParams{TenantID: tenant, ID: id})
	case "deal":
		_, err = q.GetDeal(ctx, repo.GetDealParams{TenantID: tenant, ID: id})
	default:
		return apperr.Validation(apperr.FieldError{Field: "subject_type", Code: "invalid"})
	}
	if err != nil {
		return apperr.Validation(apperr.FieldError{Field: "subject_id", Code: "unknown_subject"})
	}
	return nil
}

// TaskInput creates a task.
type TaskInput struct {
	Title, Description   string
	Assignee             *uuid.UUID
	CRMCompanyID, DealID *uuid.UUID
	DueAt, RemindAt      *time.Time
}

// CreateTask adds a task, with an optional reminder for its assignee.
func (s *Service) CreateTask(ctx context.Context, p authctx.Principal, in TaskInput) (repo.GetTaskRow, error) {
	if err := s.checkMember(ctx, p.TenantID, in.Assignee, "assignee_user_id"); err != nil {
		return repo.GetTaskRow{}, err
	}
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if in.CRMCompanyID != nil {
			if err := subjectExists(ctx, q, p.TenantID, "company", *in.CRMCompanyID); err != nil {
				return err
			}
		}
		if in.DealID != nil {
			if err := subjectExists(ctx, q, p.TenantID, "deal", *in.DealID); err != nil {
				return err
			}
			if in.CRMCompanyID == nil { // A deal's task belongs to the deal's company too.
				company, err := q.CompanyForDeal(ctx, repo.CompanyForDealParams{TenantID: p.TenantID, ID: *in.DealID})
				if err != nil {
					return err
				}
				in.CRMCompanyID = &company
			}
		}
		var desc *string
		if strings.TrimSpace(in.Description) != "" {
			desc = &in.Description
		}
		var err error
		if id, err = q.InsertTask(ctx, repo.InsertTaskParams{TenantID: p.TenantID, Title: in.Title, Description: desc, AssigneeUserID: in.Assignee, CrmCompanyID: in.CRMCompanyID, DealID: in.DealID, DueAt: in.DueAt, CreatedBy: uid(p)}); err != nil {
			return err
		}
		if in.RemindAt != nil {
			who := p.UserID
			if in.Assignee != nil {
				who = *in.Assignee
			}
			if err := q.InsertReminder(ctx, repo.InsertReminderParams{TenantID: p.TenantID, TaskID: &id, UserID: who, RemindAt: *in.RemindAt}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.task_created", "task", &id, nil))
	})
	if err != nil {
		return repo.GetTaskRow{}, err
	}
	return s.Task(ctx, p, id)
}

// Task returns one task.
func (s *Service) Task(ctx context.Context, p authctx.Principal, id uuid.UUID) (repo.GetTaskRow, error) {
	var out repo.GetTaskRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.GetTask(ctx, repo.GetTaskParams{TenantID: p.TenantID, ID: id})
		return notFound(err)
	})
	return out, err
}

// TaskFilter narrows the task list.
type TaskFilter struct {
	Status                         *string
	Assignee, DealID, CRMCompanyID *uuid.UUID
}

// Tasks lists tasks, soonest due first.
func (s *Service) Tasks(ctx context.Context, p authctx.Principal, f TaskFilter) ([]repo.ListTasksRow, error) {
	var out []repo.ListTasksRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListTasks(ctx, repo.ListTasksParams{TenantID: p.TenantID, Status: f.Status, Assignee: f.Assignee, DealID: f.DealID, CrmCompanyID: f.CRMCompanyID})
		return err
	})
	return out, err
}

// TaskPatch changes a task.
type TaskPatch struct {
	Title, Description, Status *string
	Assignee                   *uuid.UUID
	SetAssignee                bool
	DueAt                      *time.Time
	SetDue                     bool
}

// UpdateTask changes a task. Completing a task records an activity.
func (s *Service) UpdateTask(ctx context.Context, p authctx.Principal, id uuid.UUID, in TaskPatch) (repo.GetTaskRow, error) {
	if in.SetAssignee {
		if err := s.checkMember(ctx, p.TenantID, in.Assignee, "assignee_user_id"); err != nil {
			return repo.GetTaskRow{}, err
		}
	}
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		before, err := q.GetTask(ctx, repo.GetTaskParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		if _, err := q.UpdateTask(ctx, repo.UpdateTaskParams{TenantID: p.TenantID, ID: id, Title: in.Title, Description: in.Description, Status: in.Status,
			SetAssignee: in.SetAssignee, AssigneeUserID: in.Assignee, SetDue: in.SetDue, DueAt: in.DueAt}); err != nil {
			return err
		}
		if in.Status != nil && *in.Status == "done" && before.Status != "done" {
			if _, err := q.InsertActivity(ctx, repo.InsertActivityParams{TenantID: p.TenantID, ActivityType: "task", CrmCompanyID: before.CrmCompanyID, DealID: before.DealID, ActorUserID: uid(p), Summary: "Completed task: " + before.Title, SourceModule: ptr(Module)}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.task_updated", "task", &id, map[string]any{"status": in.Status}))
	})
	if err != nil {
		return repo.GetTaskRow{}, err
	}
	return s.Task(ctx, p, id)
}

// DeleteTask removes a task.
func (s *Service) DeleteTask(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		n, err := q.DeleteTask(ctx, repo.DeleteTaskParams{TenantID: p.TenantID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		return err
	})
}

// Notes. Bodies are plain text, stored as written and only ever shown escaped or sanitized.

// Notes lists the notes on a record.
func (s *Service) Notes(ctx context.Context, p authctx.Principal, kind string, subject uuid.UUID) ([]repo.ListNotesRow, error) {
	var out []repo.ListNotesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if err := subjectExists(ctx, q, p.TenantID, kind, subject); err != nil {
			return notFoundSubject(err)
		}
		var err error
		out, err = q.ListNotes(ctx, repo.ListNotesParams{TenantID: p.TenantID, SubjectType: kind, SubjectID: subject})
		return err
	})
	return out, err
}

func notFoundSubject(err error) error {
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Status == 422 {
		return apperr.NotFound()
	}
	return err
}

// AddNote adds a note, and records it on the timeline.
func (s *Service) AddNote(ctx context.Context, p authctx.Principal, kind string, subject uuid.UUID, body string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := subjectExists(ctx, q, p.TenantID, kind, subject); err != nil {
			return err
		}
		row, err := q.InsertNote(ctx, repo.InsertNoteParams{TenantID: p.TenantID, SubjectType: kind, SubjectID: subject, Body: body, CreatedBy: uid(p)})
		if err != nil {
			return err
		}
		id = row.ID
		a := repo.InsertActivityParams{TenantID: p.TenantID, ActivityType: "note", ActorUserID: uid(p), Summary: "Added a note", SourceModule: ptr(Module)}
		if err := s.linkActivity(ctx, q, p.TenantID, &a, kind, subject); err != nil {
			return err
		}
		if _, err := q.InsertActivity(ctx, a); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.note_added", "note", &id, map[string]any{"subject_type": kind}))
	})
	return id, err
}

// linkActivity points an activity at the record, and at the deal's company too.
func (s *Service) linkActivity(ctx context.Context, q *repo.Queries, tenant uuid.UUID, a *repo.InsertActivityParams, kind string, subject uuid.UUID) error {
	switch kind {
	case "company":
		a.CrmCompanyID = &subject
	case "contact":
		c, err := q.GetContact(ctx, repo.GetContactParams{TenantID: tenant, ID: subject})
		if err != nil {
			return err
		}
		a.ContactID, a.CrmCompanyID = &c.ID, c.CrmCompanyID
	case "deal":
		company, err := q.CompanyForDeal(ctx, repo.CompanyForDealParams{TenantID: tenant, ID: subject})
		if err != nil {
			return err
		}
		a.DealID, a.CrmCompanyID = &subject, &company
	}
	return nil
}

// UpdateNote changes a note's text.
func (s *Service) UpdateNote(ctx context.Context, p authctx.Principal, id uuid.UUID, body string) error {
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		n, err := q.UpdateNote(ctx, repo.UpdateNoteParams{TenantID: p.TenantID, ID: id, Body: body})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		return err
	})
}

// DeleteNote removes a note.
func (s *Service) DeleteNote(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteNote(ctx, repo.DeleteNoteParams{TenantID: p.TenantID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.note_deleted", "note", &id, nil))
	})
}

// Attachments are uploaded through File Storage, then linked here.

// Attachments lists a record's files.
func (s *Service) Attachments(ctx context.Context, p authctx.Principal, kind string, subject uuid.UUID) ([]repo.ListAttachmentsRow, error) {
	var out []repo.ListAttachmentsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if err := subjectExists(ctx, q, p.TenantID, kind, subject); err != nil {
			return notFoundSubject(err)
		}
		var err error
		out, err = q.ListAttachments(ctx, repo.ListAttachmentsParams{TenantID: p.TenantID, SubjectType: kind, SubjectID: subject})
		return err
	})
	return out, err
}

// Attach links an uploaded file to a record. The file must be the tenant's own attachment.
func (s *Service) Attach(ctx context.Context, p authctx.Principal, kind string, subject, fileID uuid.UUID) (uuid.UUID, error) {
	meta, err := s.Files.Meta(ctx, p, fileID)
	if err != nil {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "file_id", Code: "unknown_file"})
	}
	if meta.OwnerModule != "attachment" {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "file_id", Code: "wrong_file_purpose"})
	}
	var id uuid.UUID
	err = s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		if err := subjectExists(ctx, q, p.TenantID, kind, subject); err != nil {
			return err
		}
		var err error
		if id, err = q.InsertAttachment(ctx, repo.InsertAttachmentParams{TenantID: p.TenantID, SubjectType: kind, SubjectID: subject, FileID: fileID, CreatedBy: uid(p)}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.attachment_added", "attachment", &id, map[string]any{"subject_type": kind, "file_id": fileID}))
	})
	return id, err
}

// Detach removes a file link. The file itself follows File Storage retention.
func (s *Service) Detach(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	return s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		n, err := q.DeleteAttachment(ctx, repo.DeleteAttachmentParams{TenantID: p.TenantID, ID: id})
		if err == nil && n == 0 {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.attachment_removed", "attachment", &id, nil))
	})
}

// Meetings.

// MeetingInput records a meeting.
type MeetingInput struct {
	Title                string
	StartsAt, EndsAt     time.Time
	CRMCompanyID, DealID *uuid.UUID
	ContactIDs           []uuid.UUID // CRM contact IDs.
	SourceModule         string
	SourceRef            *uuid.UUID
	IdempotencyKey       string
}

// MeetingView is a meeting with its attendees.
type MeetingView struct {
	repo.GetMeetingRow
	Attendees []repo.ListAttendeesRow
}

// CreateMeeting records a meeting, once per idempotency key.
func (s *Service) CreateMeeting(ctx context.Context, p authctx.Principal, in MeetingInput) (MeetingView, error) {
	var id uuid.UUID
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		var err error
		id, err = recordMeeting(ctx, q, tx, p.TenantID, uid(p), in)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.meeting_recorded", "meeting", &id, nil))
	})
	if err != nil {
		return MeetingView{}, err
	}
	return s.Meeting(ctx, p, id)
}

func recordMeeting(ctx context.Context, q *repo.Queries, tx pgx.Tx, tenant uuid.UUID, actor *uuid.UUID, in MeetingInput) (uuid.UUID, error) {
	if !in.EndsAt.After(in.StartsAt) {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "ends_at", Code: "before_start"})
	}
	if in.IdempotencyKey != "" {
		if id, err := q.FindMeetingByKey(ctx, repo.FindMeetingByKeyParams{TenantID: tenant, IdempotencyKey: &in.IdempotencyKey}); err == nil {
			return id, nil
		}
	}
	if in.DealID != nil && in.CRMCompanyID == nil {
		company, err := q.CompanyForDeal(ctx, repo.CompanyForDealParams{TenantID: tenant, ID: *in.DealID})
		if err != nil {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "deal_id", Code: "unknown_deal"})
		}
		in.CRMCompanyID = &company
	}
	if in.CRMCompanyID != nil {
		if err := subjectExists(ctx, q, tenant, "company", *in.CRMCompanyID); err != nil {
			return uuid.Nil, err
		}
	}
	var key, src *string
	if in.IdempotencyKey != "" {
		key = &in.IdempotencyKey
	}
	if in.SourceModule != "" {
		src = &in.SourceModule
	}
	id, err := q.InsertMeeting(ctx, repo.InsertMeetingParams{TenantID: tenant, CrmCompanyID: in.CRMCompanyID, DealID: in.DealID, Title: in.Title, StartsAt: in.StartsAt, EndsAt: in.EndsAt, OrganizerUserID: actor, SourceModule: src, SourceRef: in.SourceRef, IdempotencyKey: key})
	if errors.Is(err, pgx.ErrNoRows) && key != nil {
		return q.FindMeetingByKey(ctx, repo.FindMeetingByKeyParams{TenantID: tenant, IdempotencyKey: key})
	}
	if err != nil {
		return uuid.Nil, err
	}
	for _, c := range in.ContactIDs {
		cc, err := q.GetContact(ctx, repo.GetContactParams{TenantID: tenant, ID: c})
		if err != nil {
			return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "contact_ids", Code: "unknown_contact"})
		}
		if err := q.InsertAttendee(ctx, repo.InsertAttendeeParams{TenantID: tenant, MeetingID: id, ContactID: &cc.ID}); err != nil {
			return uuid.Nil, err
		}
	}
	if actor != nil {
		if err := q.InsertAttendee(ctx, repo.InsertAttendeeParams{TenantID: tenant, MeetingID: id, UserID: actor}); err != nil {
			return uuid.Nil, err
		}
	}
	if _, err := q.InsertActivity(ctx, repo.InsertActivityParams{TenantID: tenant, ActivityType: "meeting", CrmCompanyID: in.CRMCompanyID, DealID: in.DealID, ActorUserID: actor, Summary: "Meeting: " + in.Title, OccurredAt: &in.StartsAt, SourceModule: src}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	return id, emit(ctx, tx, tenant, "meeting.scheduled", "meeting", id, map[string]any{"meeting_id": id, "crm_company_id": in.CRMCompanyID, "deal_id": in.DealID})
}

// Meeting returns one meeting.
func (s *Service) Meeting(ctx context.Context, p authctx.Principal, id uuid.UUID) (MeetingView, error) {
	var out MeetingView
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		if out.GetMeetingRow, err = q.GetMeeting(ctx, repo.GetMeetingParams{TenantID: p.TenantID, ID: id}); err != nil {
			return notFound(err)
		}
		out.Attendees, err = q.ListAttendees(ctx, repo.ListAttendeesParams{TenantID: p.TenantID, MeetingIds: []uuid.UUID{id}})
		return err
	})
	return out, err
}

// Meetings lists meetings.
func (s *Service) Meetings(ctx context.Context, p authctx.Principal, company, deal *uuid.UUID, from *time.Time) ([]repo.ListMeetingsRow, error) {
	var out []repo.ListMeetingsRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListMeetings(ctx, repo.ListMeetingsParams{TenantID: p.TenantID, CrmCompanyID: company, DealID: deal, FromTime: from})
		return err
	})
	return out, err
}

// MeetingPatch changes a meeting.
type MeetingPatch struct {
	Title, Status, Outcome *string
	StartsAt, EndsAt       *time.Time
}

// UpdateMeeting changes a meeting. Marking it completed publishes an event.
func (s *Service) UpdateMeeting(ctx context.Context, p authctx.Principal, id uuid.UUID, in MeetingPatch) (MeetingView, error) {
	err := s.tx(ctx, p, func(q *repo.Queries, tx pgx.Tx) error {
		before, err := q.GetMeeting(ctx, repo.GetMeetingParams{TenantID: p.TenantID, ID: id})
		if err != nil {
			return notFound(err)
		}
		start, end := before.StartsAt, before.EndsAt
		if in.StartsAt != nil {
			start = *in.StartsAt
		}
		if in.EndsAt != nil {
			end = *in.EndsAt
		}
		if !end.After(start) {
			return apperr.Validation(apperr.FieldError{Field: "ends_at", Code: "before_start"})
		}
		if _, err := q.UpdateMeeting(ctx, repo.UpdateMeetingParams{TenantID: p.TenantID, ID: id, Title: in.Title, StartsAt: in.StartsAt, EndsAt: in.EndsAt, Status: in.Status, Outcome: in.Outcome}); err != nil {
			return err
		}
		if in.Status != nil && *in.Status == "completed" && before.Status != "completed" {
			if err := emit(ctx, tx, p.TenantID, "meeting.completed", "meeting", id, map[string]any{"meeting_id": id, "crm_company_id": before.CrmCompanyID, "deal_id": before.DealID}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, userAudit(p, "crm.meeting_updated", "meeting", &id, map[string]any{"status": in.Status}))
	})
	if err != nil {
		return MeetingView{}, err
	}
	return s.Meeting(ctx, p, id)
}

// Activities.

// ActivityFilter narrows the timeline.
type ActivityFilter struct {
	CRMCompanyID, DealID, ContactID *uuid.UUID
}

// Activities returns a timeline, newest first.
func (s *Service) Activities(ctx context.Context, p authctx.Principal, f ActivityFilter) ([]repo.ListActivitiesRow, error) {
	var out []repo.ListActivitiesRow
	err := s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.ListActivities(ctx, repo.ListActivitiesParams{TenantID: p.TenantID, CrmCompanyID: f.CRMCompanyID, DealID: f.DealID, ContactID: f.ContactID})
		return err
	})
	return out, err
}

var manualActivities = map[string]bool{"call": true, "email_sent": true, "meeting": true, "note": true}

// LogActivity records a call or other contact by hand.
func (s *Service) LogActivity(ctx context.Context, p authctx.Principal, kind, subjectType string, subject uuid.UUID, summary string, at *time.Time) error {
	if !manualActivities[kind] {
		return apperr.Validation(apperr.FieldError{Field: "activity_type", Code: "invalid"})
	}
	return s.tx(ctx, p, func(q *repo.Queries, _ pgx.Tx) error {
		if err := subjectExists(ctx, q, p.TenantID, subjectType, subject); err != nil {
			return err
		}
		a := repo.InsertActivityParams{TenantID: p.TenantID, ActivityType: kind, ActorUserID: uid(p), Summary: summary, OccurredAt: at, SourceModule: ptr(Module)}
		if err := s.linkActivity(ctx, q, p.TenantID, &a, subjectType, subject); err != nil {
			return err
		}
		_, err := q.InsertActivity(ctx, a)
		return err
	})
}
