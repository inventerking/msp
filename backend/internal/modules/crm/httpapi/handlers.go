// Package httpapi serves CRM's contract. The route guard has already authenticated, checked the
// entitlement and permission, and validated the request.
package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/httpx"
	"mspengine/internal/modules/crm/app"
	"mspengine/internal/modules/crm/domain"
	"mspengine/internal/modules/crm/repo"
)

// Handlers implements the generated server interface.
type Handlers struct{ S *app.Service }

var _ ServerInterface = Handlers{}

func principal(r *http.Request) authctx.Principal { return authctx.PrincipalFrom(r.Context()) }

func fail(w http.ResponseWriter, r *http.Request, err error) { httpx.Error(w, r, err) }

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func i32(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p) //nolint:gosec // The contract bounds these.
	return &v
}

func date(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	return &d.Time
}

func apiDate(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	return &openapi_types.Date{Time: *t}
}

func ok(w http.ResponseWriter, v any)      { httpx.JSON(w, http.StatusOK, v) }
func created(w http.ResponseWriter, v any) { httpx.JSON(w, http.StatusCreated, v) }
func done(w http.ResponseWriter)           { w.WriteHeader(http.StatusNoContent) }

// Companies.

func companyOut(c repo.GetCompanyRow) Company {
	return Company{Id: c.ID, CompanyId: c.CompanyID, Name: c.Name, Domain: c.Domain, Lifecycle: Lifecycle(c.Lifecycle), OwnerUserId: c.OwnerUserID, SourceModule: c.SourceModule, UpdatedAt: c.UpdatedAt}
}

// ListCompanies handles GET /crm/companies.
func (h Handlers) ListCompanies(w http.ResponseWriter, r *http.Request, params ListCompaniesParams) {
	limit, before, err := httpx.Page(params.Limit, params.Cursor)
	if err != nil {
		fail(w, r, err)
		return
	}
	f := app.CompanyFilter{Query: params.Q}
	if params.Lifecycle != nil {
		l := string(*params.Lifecycle)
		f.Lifecycle = &l
	}
	rows, err := h.S.Companies(r.Context(), principal(r), f, limit+1, before)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := CompanyList{Data: []Company{}}
	for i, c := range rows {
		if int32(i) == limit { //nolint:gosec // Small.
			next := rows[i-1].ID.String()
			out.NextCursor = &next
			break
		}
		x := companyOut(repo.GetCompanyRow{ID: c.ID, CompanyID: c.CompanyID, Name: c.Name, Domain: c.Domain, Lifecycle: c.Lifecycle, OwnerUserID: c.OwnerUserID, SourceModule: c.SourceModule, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt})
		n := int(c.OpenDeals)
		x.OpenDeals = &n
		out.Data = append(out.Data, x)
	}
	ok(w, out)
}

// CreateCompany handles POST /crm/companies.
func (h Handlers) CreateCompany(w http.ResponseWriter, r *http.Request) {
	var b CompanyInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	lc := ""
	if b.Lifecycle != nil {
		lc = string(*b.Lifecycle)
	}
	c, err := h.S.CreateCompany(r.Context(), principal(r), b.Name, b.Domain, lc, b.OwnerUserId)
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, companyOut(c))
}

// GetCompany handles GET /crm/companies/{id}.
func (h Handlers) GetCompany(w http.ResponseWriter, r *http.Request, id Id) {
	c, err := h.S.Company(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, companyOut(c))
}

// UpdateCompany handles PATCH /crm/companies/{id}.
func (h Handlers) UpdateCompany(w http.ResponseWriter, r *http.Request, id Id) {
	var b CompanyPatch
	keys, good := httpx.DecodePatch(w, r, &b)
	if !good {
		return
	}
	var lc *string
	if b.Lifecycle != nil {
		s := string(*b.Lifecycle)
		lc = &s
	}
	c, err := h.S.UpdateCompany(r.Context(), principal(r), id, b.Name, lc, keys["owner_user_id"], b.OwnerUserId)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, companyOut(c))
}

// DeleteCompany handles DELETE /crm/companies/{id}.
func (h Handlers) DeleteCompany(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteCompany(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}

// Contacts.

func contactOut(c app.ContactView) Contact {
	return Contact{Id: c.ID, ContactId: c.ContactID, CrmCompanyId: c.CrmCompanyID, IsPrimary: c.IsPrimary, FullName: c.Person.FullName, JobTitle: c.Person.JobTitle, Email: c.Person.Email, Phone: c.Person.Phone, Erased: c.Person.Erased}
}

// ListContacts handles GET /crm/contacts.
func (h Handlers) ListContacts(w http.ResponseWriter, r *http.Request, params ListContactsParams) {
	rows, err := h.S.Contacts(r.Context(), principal(r), params.CrmCompanyId)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ContactList{Data: []Contact{}}
	for _, c := range rows {
		out.Data = append(out.Data, contactOut(c))
	}
	ok(w, out)
}

// CreateContact handles POST /crm/contacts.
func (h Handlers) CreateContact(w http.ResponseWriter, r *http.Request) {
	var b ContactInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	c, err := h.S.CreateContact(r.Context(), principal(r), app.ContactInput{Email: string(deref(b.Email)), FullName: deref(b.FullName), JobTitle: deref(b.JobTitle), CRMCompanyID: b.CrmCompanyId, Primary: deref(b.IsPrimary)})
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, contactOut(c))
}

// GetContact handles GET /crm/contacts/{id}.
func (h Handlers) GetContact(w http.ResponseWriter, r *http.Request, id Id) {
	c, err := h.S.Contact(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, contactOut(c))
}

// UpdateContact handles PATCH /crm/contacts/{id}.
func (h Handlers) UpdateContact(w http.ResponseWriter, r *http.Request, id Id) {
	var b ContactPatch
	keys, good := httpx.DecodePatch(w, r, &b)
	if !good {
		return
	}
	c, err := h.S.UpdateContact(r.Context(), principal(r), id, keys["crm_company_id"], b.CrmCompanyId, b.IsPrimary)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, contactOut(c))
}

// DeleteContact handles DELETE /crm/contacts/{id}.
func (h Handlers) DeleteContact(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteContact(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}

// Pipelines.

func stageOut(s repo.ListStagesRow) Stage {
	return Stage{Id: s.ID, Key: s.Key, Name: s.Name, Position: int(s.Position), Kind: StageKind(s.Kind), DefaultProbability: int(s.DefaultProbability)}
}

func (h Handlers) pipelines(w http.ResponseWriter, r *http.Request, status int) {
	ps, err := h.S.Pipelines(r.Context(), principal(r))
	if err != nil {
		fail(w, r, err)
		return
	}
	out := PipelineList{Data: []Pipeline{}}
	for _, p := range ps {
		x := Pipeline{Id: p.ID, Name: p.Name, IsDefault: p.IsDefault, Status: PipelineStatus(p.Status), Stages: []Stage{}}
		for _, s := range p.Stages {
			x.Stages = append(x.Stages, stageOut(s))
		}
		out.Data = append(out.Data, x)
	}
	httpx.JSON(w, status, out)
}

// ListPipelines handles GET /crm/pipelines.
func (h Handlers) ListPipelines(w http.ResponseWriter, r *http.Request) {
	h.pipelines(w, r, http.StatusOK)
}

// CreatePipeline handles POST /crm/pipelines.
func (h Handlers) CreatePipeline(w http.ResponseWriter, r *http.Request) {
	var b PipelineInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	if _, err := h.S.CreatePipeline(r.Context(), principal(r), b.Name); err != nil {
		fail(w, r, err)
		return
	}
	h.pipelines(w, r, http.StatusCreated)
}

// UpdatePipeline handles PATCH /crm/pipelines/{id}.
func (h Handlers) UpdatePipeline(w http.ResponseWriter, r *http.Request, id Id) {
	var b PipelinePatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	if err := h.S.UpdatePipeline(r.Context(), principal(r), id, b.Name, status); err != nil {
		fail(w, r, err)
		return
	}
	h.pipelines(w, r, http.StatusOK)
}

// SetStages handles PUT /crm/pipelines/{id}/stages.
func (h Handlers) SetStages(w http.ResponseWriter, r *http.Request, id Id) {
	var b StagesInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	in := make([]app.StageInput, len(b.Stages))
	for i, s := range b.Stages {
		in[i] = app.StageInput{ID: s.Id, Stage: domain.Stage{Key: s.Key, Name: s.Name, Kind: string(s.Kind), Probability: s.DefaultProbability}}
	}
	if err := h.S.SetStages(r.Context(), principal(r), id, in); err != nil {
		fail(w, r, err)
		return
	}
	h.pipelines(w, r, http.StatusOK)
}

type total struct {
	Currency      string `json:"currency"`
	Count         int    `json:"count"`
	MRR           int64  `json:"mrr_minor"`
	ContractValue int64  `json:"contract_value_minor"`
}

type boardColumn struct {
	Stage  Stage       `json:"stage"`
	Deals  []BoardDeal `json:"deals"`
	Totals []total     `json:"totals"`
}

// GetBoard handles GET /crm/pipelines/{id}/board. Totals are kept per currency.
func (h Handlers) GetBoard(w http.ResponseWriter, r *http.Request, id Id) {
	b, err := h.S.Board(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	cols := make([]boardColumn, len(b.Stages))
	index := map[uuid.UUID]int{}
	for i, s := range b.Stages {
		cols[i] = boardColumn{Stage: stageOut(s), Deals: []BoardDeal{}, Totals: []total{}}
		index[s.ID] = i
	}
	for _, d := range b.Deals {
		i, found := index[d.StageID]
		if !found {
			continue
		}
		c := &cols[i]
		c.Deals = append(c.Deals, BoardDeal{Id: d.ID, Title: d.Title, StageId: d.StageID, CrmCompanyId: d.CrmCompanyID, CompanyName: d.CompanyName, OwnerUserId: d.OwnerUserID, Currency: d.Currency,
			ContractValueMinor: d.ContractValueMinor, ExpectedMrrMinor: d.ExpectedMrrMinor, Probability: int(d.Probability), ExpectedCloseDate: apiDate(d.ExpectedCloseDate), Status: d.Status, RowVersion: int(d.RowVersion)})
		k := -1
		for j := range c.Totals {
			if c.Totals[j].Currency == d.Currency {
				k = j
			}
		}
		if k < 0 {
			c.Totals = append(c.Totals, total{Currency: d.Currency})
			k = len(c.Totals) - 1
		}
		c.Totals[k].Count++
		c.Totals[k].MRR += deref(d.ExpectedMrrMinor)
		c.Totals[k].ContractValue += deref(d.ContractValueMinor)
	}
	ok(w, map[string]any{"stages": cols})
}

// Deals.

func dealOut(d repo.GetDealRow) Deal {
	return Deal{Id: d.ID, Title: d.Title, PipelineId: d.PipelineID, StageId: d.StageID, StageName: d.StageName, CrmCompanyId: d.CrmCompanyID, CompanyName: d.CompanyName,
		PrimaryContactId: d.PrimaryContactID, OwnerUserId: d.OwnerUserID, Currency: d.Currency, ContractValueMinor: d.ContractValueMinor, ExpectedMrrMinor: d.ExpectedMrrMinor,
		Probability: int(d.Probability), ExpectedCloseDate: apiDate(d.ExpectedCloseDate), Status: DealStatus(d.Status), ClosedAt: d.ClosedAt, LossReason: d.LossReason,
		SourceModule: d.SourceModule, RowVersion: int(d.RowVersion), UpdatedAt: d.UpdatedAt}
}

// ListDeals handles GET /crm/deals.
func (h Handlers) ListDeals(w http.ResponseWriter, r *http.Request, params ListDealsParams) {
	f := app.DealFilter{PipelineID: params.PipelineId, CRMCompanyID: params.CrmCompanyId}
	if params.Status != nil {
		s := string(*params.Status)
		f.Status = &s
	}
	rows, err := h.S.Deals(r.Context(), principal(r), f)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := DealList{Data: []Deal{}}
	for _, d := range rows {
		out.Data = append(out.Data, dealOut(repo.GetDealRow{ID: d.ID, PipelineID: d.PipelineID, StageID: d.StageID, CrmCompanyID: d.CrmCompanyID, Title: d.Title, OwnerUserID: d.OwnerUserID, Currency: d.Currency,
			ContractValueMinor: d.ContractValueMinor, ExpectedMrrMinor: d.ExpectedMrrMinor, Probability: d.Probability, ExpectedCloseDate: d.ExpectedCloseDate, Status: d.Status,
			RowVersion: d.RowVersion, UpdatedAt: d.UpdatedAt, CompanyName: d.CompanyName, StageName: d.StageName}))
	}
	ok(w, out)
}

// CreateDeal handles POST /crm/deals.
func (h Handlers) CreateDeal(w http.ResponseWriter, r *http.Request) {
	var b DealInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	d, err := h.S.CreateDeal(r.Context(), principal(r), app.DealInput{CRMCompanyID: &b.CrmCompanyId, PipelineID: b.PipelineId, StageID: b.StageId, PrimaryContactID: b.PrimaryContactId, OwnerUserID: b.OwnerUserId,
		Title: b.Title, Currency: b.Currency, ContractValue: b.ContractValueMinor, ExpectedMRR: b.ExpectedMrrMinor, Probability: i32(b.Probability), ExpectedClose: date(b.ExpectedCloseDate)})
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, dealOut(d))
}

// GetDeal handles GET /crm/deals/{id}.
func (h Handlers) GetDeal(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.DealDetail(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	type entry struct {
		ToStageName string     `json:"to_stage_name"`
		Reason      *string    `json:"reason,omitempty"`
		ChangedBy   *uuid.UUID `json:"changed_by,omitempty"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	hist := []entry{}
	for _, x := range d.History {
		hist = append(hist, entry{x.ToStageName, x.Reason, x.ChangedBy, x.CreatedAt})
	}
	ok(w, struct {
		Deal
		History []entry `json:"history"`
	}{dealOut(d.GetDealRow), hist})
}

// UpdateDeal handles PATCH /crm/deals/{id}. A stale row_version gets 409 stale_version.
func (h Handlers) UpdateDeal(w http.ResponseWriter, r *http.Request, id Id) {
	var b DealPatch
	keys, good := httpx.DecodePatch(w, r, &b)
	if !good {
		return
	}
	d, err := h.S.UpdateDeal(r.Context(), principal(r), id, app.DealPatch{Title: b.Title, Currency: b.Currency, ContractValue: b.ContractValueMinor, SetValue: keys["contract_value_minor"],
		ExpectedMRR: b.ExpectedMrrMinor, SetMRR: keys["expected_mrr_minor"], Probability: i32(b.Probability), ExpectedClose: date(b.ExpectedCloseDate), SetClose: keys["expected_close_date"],
		Owner: b.OwnerUserId, SetOwner: keys["owner_user_id"], PrimaryContact: b.PrimaryContactId, SetContact: keys["primary_contact_id"], RowVersion: int32(b.RowVersion)}) //nolint:gosec // A version number.
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, dealOut(d))
}

// DeleteDeal handles DELETE /crm/deals/{id}.
func (h Handlers) DeleteDeal(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteDeal(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}

// MoveDeal handles POST /crm/deals/{id}/move.
func (h Handlers) MoveDeal(w http.ResponseWriter, r *http.Request, id Id) {
	var b MoveDeal
	if !httpx.Decode(w, r, &b) {
		return
	}
	d, err := h.S.MoveDeal(r.Context(), principal(r), id, b.StageId, int32(b.RowVersion), b.Reason) //nolint:gosec // A version number.
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, dealOut(d))
}

// WinDeal handles POST /crm/deals/{id}/win.
func (h Handlers) WinDeal(w http.ResponseWriter, r *http.Request, id Id) {
	var b WinDeal
	if !httpx.Decode(w, r, &b) {
		return
	}
	d, err := h.S.CloseDeal(r.Context(), principal(r), id, true, int32(b.RowVersion), nil) //nolint:gosec // A version number.
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, dealOut(d))
}

// LoseDeal handles POST /crm/deals/{id}/lose.
func (h Handlers) LoseDeal(w http.ResponseWriter, r *http.Request, id Id) {
	var b LoseDeal
	if !httpx.Decode(w, r, &b) {
		return
	}
	d, err := h.S.CloseDeal(r.Context(), principal(r), id, false, int32(b.RowVersion), &b.LossReason) //nolint:gosec // A version number.
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, dealOut(d))
}

// ReopenDeal handles POST /crm/deals/{id}/reopen.
func (h Handlers) ReopenDeal(w http.ResponseWriter, r *http.Request, id Id) {
	d, err := h.S.ReopenDeal(r.Context(), principal(r), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, dealOut(d))
}

// Tasks.

func taskOut(t repo.GetTaskRow) Task {
	return Task{Id: t.ID, Title: t.Title, Description: t.Description, AssigneeUserId: t.AssigneeUserID, CrmCompanyId: t.CrmCompanyID, DealId: t.DealID, DueAt: t.DueAt, Status: TaskStatus(t.Status), CompletedAt: t.CompletedAt, CreatedAt: t.CreatedAt}
}

// ListTasks handles GET /crm/tasks.
func (h Handlers) ListTasks(w http.ResponseWriter, r *http.Request, params ListTasksParams) {
	f := app.TaskFilter{DealID: params.DealId, CRMCompanyID: params.CrmCompanyId}
	if params.Status != nil {
		s := string(*params.Status)
		f.Status = &s
	}
	if deref(params.Mine) {
		me := principal(r).UserID
		f.Assignee = &me
	}
	rows, err := h.S.Tasks(r.Context(), principal(r), f)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := TaskList{Data: []Task{}}
	for _, t := range rows {
		out.Data = append(out.Data, taskOut(repo.GetTaskRow(t)))
	}
	ok(w, out)
}

// CreateTask handles POST /crm/tasks.
func (h Handlers) CreateTask(w http.ResponseWriter, r *http.Request) {
	var b TaskInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	t, err := h.S.CreateTask(r.Context(), principal(r), app.TaskInput{Title: b.Title, Description: deref(b.Description), Assignee: b.AssigneeUserId, CRMCompanyID: b.CrmCompanyId, DealID: b.DealId, DueAt: b.DueAt, RemindAt: b.RemindAt})
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, taskOut(t))
}

// UpdateTask handles PATCH /crm/tasks/{id}.
func (h Handlers) UpdateTask(w http.ResponseWriter, r *http.Request, id Id) {
	var b TaskPatch
	keys, good := httpx.DecodePatch(w, r, &b)
	if !good {
		return
	}
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	t, err := h.S.UpdateTask(r.Context(), principal(r), id, app.TaskPatch{Title: b.Title, Description: b.Description, Status: status, Assignee: b.AssigneeUserId, SetAssignee: keys["assignee_user_id"], DueAt: b.DueAt, SetDue: keys["due_at"]})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, taskOut(t))
}

// CompleteTask handles POST /crm/tasks/{id}/complete.
func (h Handlers) CompleteTask(w http.ResponseWriter, r *http.Request, id Id) {
	s := "done"
	t, err := h.S.UpdateTask(r.Context(), principal(r), id, app.TaskPatch{Status: &s})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, taskOut(t))
}

// DeleteTask handles DELETE /crm/tasks/{id}.
func (h Handlers) DeleteTask(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteTask(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}

// Notes and attachments.

// ListNotes handles GET /crm/notes.
func (h Handlers) ListNotes(w http.ResponseWriter, r *http.Request, params ListNotesParams) {
	rows, err := h.S.Notes(r.Context(), principal(r), string(params.SubjectType), params.SubjectId)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := NoteList{Data: []Note{}}
	for _, n := range rows {
		out.Data = append(out.Data, Note{Id: n.ID, Body: n.Body, CreatedBy: n.CreatedBy, CreatedAt: n.CreatedAt, UpdatedAt: &n.UpdatedAt})
	}
	ok(w, out)
}

// CreateNote handles POST /crm/notes.
func (h Handlers) CreateNote(w http.ResponseWriter, r *http.Request) {
	var b NoteInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.AddNote(r.Context(), principal(r), string(b.SubjectType), b.SubjectId, b.Body)
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, NoteRef{Id: id})
}

// UpdateNote handles PATCH /crm/notes/{id}.
func (h Handlers) UpdateNote(w http.ResponseWriter, r *http.Request, id Id) {
	var b NotePatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	if err := h.S.UpdateNote(r.Context(), principal(r), id, b.Body); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}

// DeleteNote handles DELETE /crm/notes/{id}.
func (h Handlers) DeleteNote(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.DeleteNote(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}

// ListAttachments handles GET /crm/attachments.
func (h Handlers) ListAttachments(w http.ResponseWriter, r *http.Request, params ListAttachmentsParams) {
	rows, err := h.S.Attachments(r.Context(), principal(r), string(params.SubjectType), params.SubjectId)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := AttachmentList{Data: []Attachment{}}
	for _, a := range rows {
		out.Data = append(out.Data, Attachment{Id: a.ID, FileId: a.FileID, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt})
	}
	ok(w, out)
}

// CreateAttachment handles POST /crm/attachments.
func (h Handlers) CreateAttachment(w http.ResponseWriter, r *http.Request) {
	var b AttachmentInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	id, err := h.S.Attach(r.Context(), principal(r), string(b.SubjectType), b.SubjectId, b.FileId)
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, AttachmentRef{Id: id})
}

// DeleteAttachment handles DELETE /crm/attachments/{id}.
func (h Handlers) DeleteAttachment(w http.ResponseWriter, r *http.Request, id Id) {
	if err := h.S.Detach(r.Context(), principal(r), id); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}

// Meetings.

type attendee struct {
	ContactID *uuid.UUID `json:"contact_id,omitempty"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
}

type meetingOut struct {
	Meeting
	Attendees []attendee `json:"attendees"`
}

func toMeeting(m repo.GetMeetingRow, att []repo.ListAttendeesRow) meetingOut {
	out := meetingOut{Meeting: Meeting{Id: m.ID, Title: m.Title, CrmCompanyId: m.CrmCompanyID, DealId: m.DealID, StartsAt: m.StartsAt, EndsAt: m.EndsAt, Status: MeetingStatus(m.Status), Outcome: m.Outcome, SourceModule: m.SourceModule}, Attendees: []attendee{}}
	for _, a := range att {
		if a.MeetingID == m.ID {
			out.Attendees = append(out.Attendees, attendee{a.ContactID, a.UserID})
		}
	}
	return out
}

// ListMeetings handles GET /crm/meetings.
func (h Handlers) ListMeetings(w http.ResponseWriter, r *http.Request, params ListMeetingsParams) {
	rows, err := h.S.Meetings(r.Context(), principal(r), params.CrmCompanyId, params.DealId, params.From)
	if err != nil {
		fail(w, r, err)
		return
	}
	out := []meetingOut{}
	for _, m := range rows {
		out = append(out, toMeeting(repo.GetMeetingRow(m), nil))
	}
	ok(w, map[string]any{"data": out})
}

// CreateMeeting handles POST /crm/meetings.
func (h Handlers) CreateMeeting(w http.ResponseWriter, r *http.Request) {
	var b MeetingInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	m, err := h.S.CreateMeeting(r.Context(), principal(r), app.MeetingInput{Title: b.Title, StartsAt: b.StartsAt, EndsAt: b.EndsAt, CRMCompanyID: b.CrmCompanyId, DealID: b.DealId, ContactIDs: deref(b.ContactIds)})
	if err != nil {
		fail(w, r, err)
		return
	}
	created(w, toMeeting(m.GetMeetingRow, m.Attendees))
}

// UpdateMeeting handles PATCH /crm/meetings/{id}.
func (h Handlers) UpdateMeeting(w http.ResponseWriter, r *http.Request, id Id) {
	var b MeetingPatch
	if !httpx.Decode(w, r, &b) {
		return
	}
	var status *string
	if b.Status != nil {
		s := string(*b.Status)
		status = &s
	}
	m, err := h.S.UpdateMeeting(r.Context(), principal(r), id, app.MeetingPatch{Title: b.Title, Status: status, Outcome: b.Outcome, StartsAt: b.StartsAt, EndsAt: b.EndsAt})
	if err != nil {
		fail(w, r, err)
		return
	}
	ok(w, toMeeting(m.GetMeetingRow, m.Attendees))
}

// Activities.

// ListActivities handles GET /crm/activities.
func (h Handlers) ListActivities(w http.ResponseWriter, r *http.Request, params ListActivitiesParams) {
	rows, err := h.S.Activities(r.Context(), principal(r), app.ActivityFilter{CRMCompanyID: params.CrmCompanyId, DealID: params.DealId, ContactID: params.ContactId})
	if err != nil {
		fail(w, r, err)
		return
	}
	out := ActivityList{Data: []Activity{}}
	for _, a := range rows {
		out.Data = append(out.Data, Activity{Id: a.ID, ActivityType: a.ActivityType, CrmCompanyId: a.CrmCompanyID, ContactId: a.ContactID, DealId: a.DealID, ActorUserId: a.ActorUserID, Summary: a.Summary, SourceModule: a.SourceModule, OccurredAt: a.OccurredAt})
	}
	ok(w, out)
}

// LogActivity handles POST /crm/activities.
func (h Handlers) LogActivity(w http.ResponseWriter, r *http.Request) {
	var b ActivityInput
	if !httpx.Decode(w, r, &b) {
		return
	}
	if err := h.S.LogActivity(r.Context(), principal(r), string(b.ActivityType), string(b.SubjectType), b.SubjectId, b.Summary, b.OccurredAt); err != nil {
		fail(w, r, err)
		return
	}
	done(w)
}
