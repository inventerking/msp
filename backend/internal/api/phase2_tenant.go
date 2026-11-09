package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mspengine/internal/api/tenantapi"
	"mspengine/internal/kernel/apperr"
	"mspengine/internal/kernel/authctx"
	"mspengine/internal/kernel/db"
	"mspengine/internal/kernel/jobs"
	"mspengine/internal/platform/files"
	"mspengine/internal/platform/identity"
	"mspengine/internal/platform/integration"
)

func (h tenantHandlers) GetUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := authctx.PrincipalFrom(ctx).TenantID
	rows, err := h.commercial.Balances(ctx, tenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	ai, err := h.AI.Usage(ctx, tenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.UsageSummary{Meters: []tenantapi.UsageMeter{}, Ai: []tenantapi.AIUsage{}}
	for _, m := range rows {
		out.Meters = append(out.Meters, tenantapi.UsageMeter{MeterKey: m.MeterKey, Name: m.Name, Balance: m.Balance, Reserved: m.Reserved})
	}
	for _, a := range ai {
		out.Ai = append(out.Ai, tenantapi.AIUsage{ModuleKey: a.ModuleKey, Requests: a.Requests, Credits: a.Credits, Tokens: a.Tokens})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) SearchCompanies(w http.ResponseWriter, r *http.Request, params tenantapi.SearchCompaniesParams) {
	rows, err := h.Shared.SearchCompanies(r.Context(), params.Q)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.CompanyList{Data: []tenantapi.Company{}}
	for _, c := range rows {
		out.Data = append(out.Data, tenantapi.Company{Id: c.ID, DisplayName: c.DisplayName, PrimaryDomain: c.PrimaryDomain, CountryCode: c.CountryCode, EmployeeBand: c.EmployeeBand})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) EraseContact(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	job, err := h.Shared.RequestErasure(r.Context(), authctx.PrincipalFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, tenantapi.JobRef{JobId: job})
}

func (h tenantHandlers) ExportContact(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	out, err := h.Shared.ExportContact(r.Context(), authctx.PrincipalFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) jobJSON(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id uuid.UUID) error {
	j, err := jobs.GetForTenant(r.Context(), tx, id)
	if err != nil {
		return err
	}
	out := tenantapi.Job{Id: j.ID, Kind: j.Kind, Status: j.Status, Attempt: int(j.Attempt), CreatedAt: j.CreatedAt, FinishedAt: j.FinishedAt}
	if len(j.Progress) > 0 {
		var p map[string]any
		if err := json.Unmarshal(j.Progress, &p); err == nil {
			out.Progress = &p
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (h tenantHandlers) GetJob(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	p := authctx.PrincipalFrom(r.Context())
	if err := db.TenantTx(r.Context(), h.Pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error { return h.jobJSON(w, r, tx, id) }); err != nil {
		writeError(w, r, err)
	}
}

func (h tenantHandlers) CancelJob(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	p := authctx.PrincipalFrom(r.Context())
	err := db.TenantTx(r.Context(), h.Pools.App, p.TenantID, p.UserID, func(tx pgx.Tx) error {
		if _, err := jobs.GetForTenant(r.Context(), tx, id); err != nil {
			return err
		}
		if _, err := jobs.RequestCancel(r.Context(), tx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return h.jobJSON(w, r, tx, id)
	})
	if err != nil {
		writeError(w, r, err)
	}
}

func (h tenantHandlers) CreateUpload(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.CreateUpload
	if !decode(w, r, &b) {
		return
	}
	id, err := h.Files.CreateUpload(r.Context(), authctx.PrincipalFrom(r.Context()), b.Purpose, b.FileName, b.SizeBytes)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, tenantapi.Upload{Id: id, UploadUrl: tenantBase + "/files/uploads/" + id.String() + "/content"})
}

func (h tenantHandlers) UploadContent(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, files.MaxBytes))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, r, apperr.New(http.StatusRequestEntityTooLarge, "file_too_large", "That file is too large"))
		return
	}
	if err != nil {
		writeError(w, r, apperr.New(http.StatusBadRequest, "malformed_request", "The upload could not be read"))
		return
	}
	if err := h.Files.Upload(r.Context(), authctx.PrincipalFrom(r.Context()), id, body); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) GetFile(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	o, err := h.Files.Meta(r.Context(), authctx.PrincipalFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	name := "file"
	if o.OriginalName != nil {
		name = *o.OriginalName
	}
	writeJSON(w, http.StatusOK, tenantapi.FileInfo{Id: o.ID, Name: name, Status: o.Status, ContentType: o.ContentType, SizeBytes: o.SizeBytes, CreatedAt: o.CreatedAt})
}

// serveFile always downloads as an attachment, and a sandbox stops any content running as a page.
func serveFile(w http.ResponseWriter, d files.Download) {
	h := w.Header()
	h.Set("Content-Type", d.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.Name}))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Content-Length", strconv.Itoa(len(d.Body)))
	_, _ = w.Write(d.Body)
}

func (h tenantHandlers) DownloadFile(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	d, err := h.Files.Download(r.Context(), authctx.PrincipalFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	serveFile(w, d)
}

func (h tenantHandlers) DownloadSharedFile(w http.ResponseWriter, r *http.Request, token string) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	d, err := h.Files.RedeemGrant(r.Context(), token)
	if err != nil {
		writeError(w, r, err)
		return
	}
	serveFile(w, d)
}

func (h tenantHandlers) ListNotifications(w http.ResponseWriter, r *http.Request) {
	rows, unread, err := h.Notify.List(r.Context(), authctx.PrincipalFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.NotificationList{Data: []tenantapi.Notification{}, Unread: int(unread)}
	for _, n := range rows {
		out.Data = append(out.Data, tenantapi.Notification{Id: n.ID, Key: n.NotificationKey, Title: n.Title, Link: n.Link, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) MarkNotificationRead(w http.ResponseWriter, r *http.Request, id tenantapi.Id) {
	if err := h.Notify.MarkRead(r.Context(), authctx.PrincipalFrom(r.Context()), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := h.Notify.MarkAllRead(r.Context(), authctx.PrincipalFrom(r.Context())); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h tenantHandlers) ListNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Notify.Preferences(r.Context(), authctx.PrincipalFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.NotificationPreferenceList{Data: []tenantapi.NotificationPreference{}}
	for _, p := range rows {
		out.Data = append(out.Data, tenantapi.NotificationPreference{Key: p.NotificationKey, Channel: tenantapi.NotificationPreferenceChannel(p.Channel), Enabled: p.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h tenantHandlers) SetNotificationPreference(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.NotificationPreference
	if !decode(w, r, &b) {
		return
	}
	if err := h.Notify.SetPreference(r.Context(), authctx.PrincipalFrom(r.Context()), b.Key, string(b.Channel), b.Enabled); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReceiveWebhook checks size first, and the provider-kind path segment is informational only.
func (h tenantHandlers) ReceiveWebhook(w http.ResponseWriter, r *http.Request, _ string, pathToken string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, integration.MaxWebhookBytes))
	if err != nil {
		writeError(w, r, apperr.New(http.StatusRequestEntityTooLarge, "payload_too_large", "That request is too large"))
		return
	}
	if err := h.Integrate.ReceiveWebhook(r.Context(), pathToken, r.Header, body, r.Header.Get("X-Webhook-Id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func icpOut(icp identity.ICP, configured bool) tenantapi.Icp {
	out := tenantapi.Icp{Configured: configured, Name: icp.Name, Industries: nonNilStrings(icp.Industries), Countries: nonNilStrings(icp.Countries), Technologies: nonNilStrings(icp.Technologies), Version: int(icp.Version)}
	if icp.MinEmployees != nil {
		v := int(*icp.MinEmployees)
		out.MinEmployees = &v
	}
	if icp.MaxEmployees != nil {
		v := int(*icp.MaxEmployees)
		out.MaxEmployees = &v
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (h tenantHandlers) GetIcp(w http.ResponseWriter, r *http.Request) {
	icp, found, err := h.identity.DefaultICP(r.Context(), authctx.PrincipalFrom(r.Context()).TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, icpOut(icp, found))
}

func (h tenantHandlers) PutIcp(w http.ResponseWriter, r *http.Request) {
	var b tenantapi.IcpInput
	if !decode(w, r, &b) {
		return
	}
	in := identity.ICP{}
	if b.Name != nil {
		in.Name = *b.Name
	}
	if b.Industries != nil {
		in.Industries = *b.Industries
	}
	if b.Countries != nil {
		in.Countries = *b.Countries
	}
	if b.Technologies != nil {
		in.Technologies = *b.Technologies
	}
	if b.MinEmployees != nil {
		v := int32(*b.MinEmployees) //nolint:gosec // The contract bounds this.
		in.MinEmployees = &v
	}
	if b.MaxEmployees != nil {
		v := int32(*b.MaxEmployees) //nolint:gosec // The contract bounds this.
		in.MaxEmployees = &v
	}
	out, err := h.identity.SaveDefaultICP(r.Context(), authctx.PrincipalFrom(r.Context()), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, icpOut(out, true))
}

func (h tenantHandlers) GetReference(w http.ResponseWriter, r *http.Request) {
	cs, is, err := h.Shared.Reference(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := tenantapi.Reference{Countries: []tenantapi.CodeName{}, Industries: []tenantapi.CodeName{}}
	for _, c := range cs {
		out.Countries = append(out.Countries, tenantapi.CodeName{Code: c.Code, Name: c.Name})
	}
	for _, i := range is {
		out.Industries = append(out.Industries, tenantapi.CodeName{Code: i.Code, Name: i.Name})
	}
	writeJSON(w, http.StatusOK, out)
}
