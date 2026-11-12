import { Badge, Button, Card, Field, Input } from "@msp/ui";
import { unwrap } from "@msp/api-client";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { crmApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useActivities, useCrmMutation, useNotes } from "../api/crm";

/** Notes and the activity timeline for a company or deal. Note text is shown as text, never markup. */
export function Timeline({ subjectType, subjectId, companyId }: { subjectType: "company" | "deal"; subjectId: string; companyId?: string }) {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const canWrite = Boolean(me?.permissions.includes("crm.write"));
  const notes = useNotes(subjectType, subjectId);
  const activities = useActivities(subjectType === "deal" ? { deal_id: subjectId } : { crm_company_id: companyId ?? subjectId });
  const [body, setBody] = useState("");
  const [call, setCall] = useState("");
  const addNote = useCrmMutation(() => unwrap(crmApi.POST("/crm/notes", { body: { subject_type: subjectType, subject_id: subjectId, body } })));
  const logCall = useCrmMutation(() => unwrap(crmApi.POST("/crm/activities", { body: { activity_type: "call", subject_type: subjectType, subject_id: subjectId, summary: call } })));
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card className="p-4">
        <h2 className="mb-2 text-sm font-medium text-ink">{t("crm.notes")}</h2>
        {canWrite ? (
          <form className="mb-3 space-y-2" onSubmit={(e) => { e.preventDefault(); if (body.trim()) addNote.mutate(undefined, { onSuccess: () => setBody("") }); }}>
            <label className="block text-sm text-ink" htmlFor={`note-${subjectId}`}>{t("crm.addNote")}</label>
            <textarea id={`note-${subjectId}`} className="min-h-20 w-full rounded-md border border-line-strong bg-surface p-2 text-sm" value={body} onChange={(e) => setBody(e.target.value)} />
            {addNote.isError ? <p className="text-xs text-danger">{errorText(t, addNote.error)}</p> : null}
            <Button type="submit" size="sm" disabled={!body.trim() || addNote.isPending}>{t("crm.saveNote")}</Button>
          </form>
        ) : null}
        <ul className="space-y-3">
          {(notes.data?.data ?? []).map((n) => (
            <li key={n.id} className="rounded-md bg-sunken p-2 text-sm">
              <p className="whitespace-pre-wrap text-ink">{n.body}</p>
              <p className="mt-1 text-xs text-ink-faint">{new Date(n.created_at).toLocaleString()}</p>
            </li>
          ))}
        </ul>
      </Card>
      <Card className="p-4">
        <h2 className="mb-2 text-sm font-medium text-ink">{t("crm.timeline")}</h2>
        {canWrite ? (
          <form className="mb-3 flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (call.trim()) logCall.mutate(undefined, { onSuccess: () => setCall("") }); }}>
            <div className="flex-1"><Field label={t("crm.logCall")}>{(ids) => <Input {...ids} value={call} onChange={(e) => setCall(e.target.value)} />}</Field></div>
            <Button type="submit" size="sm" variant="secondary" disabled={!call.trim() || logCall.isPending}>{t("crm.log")}</Button>
          </form>
        ) : null}
        <ul className="space-y-2">
          {(activities.data?.data ?? []).map((a) => (
            <li key={a.id} className="flex items-start gap-2 text-sm">
              <Badge>{t(`crm.activityTypes.${a.activity_type}`, { defaultValue: a.activity_type })}</Badge>
              <div>
                <div className="text-ink">{a.summary}</div>
                <div className="text-xs text-ink-faint">{new Date(a.occurred_at).toLocaleString()}{a.source_module && a.source_module !== "crm" ? ` · ${t(`modules.${a.source_module}.name`, { defaultValue: a.source_module })}` : ""}</div>
              </div>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  );
}
