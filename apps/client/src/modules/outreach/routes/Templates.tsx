import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input } from "@msp/ui";
import { Mail } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { outreachApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCan, useOMutation, useTemplates } from "../api/outreach";

const area = "min-h-32 w-full rounded-md border border-line-strong bg-surface p-2 text-sm";

/** Email templates. Each edit is a new version, so an approved campaign keeps exactly what was approved. */
export function Templates() {
  const { t } = useTranslation();
  const canWrite = useCan("outreach.write");
  const templates = useTemplates();
  const [form, setForm] = useState({ name: "", subject: "", body: "", ai_request_id: "" });
  const [purpose, setPurpose] = useState("");
  const [editing, setEditing] = useState<{ id: string; subject: string; body: string }>();
  const create = useOMutation(() => unwrap(outreachApi.POST("/outreach/templates", { body: { name: form.name, subject: form.subject, body: form.body, ...(form.ai_request_id ? { ai_request_id: form.ai_request_id } : {}) } })));
  const draft = useOMutation(() => unwrap(outreachApi.POST("/outreach/templates/draft", { body: { purpose } })));
  const version = useOMutation(() => unwrap(outreachApi.POST("/outreach/templates/{id}/versions", { params: { path: { id: editing!.id } }, body: { subject: editing!.subject, body: editing!.body } })));
  const list = templates.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canWrite ? (
        <Card className="p-4">
          <h2 className="mb-1 text-sm font-medium text-ink">{t("outreach.newTemplate")}</h2>
          <p className="mb-3 text-xs text-ink-soft">{t("outreach.mergeHelp")}</p>
          <form className="mb-3 flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); draft.mutate(undefined, { onSuccess: (d) => setForm({ ...form, subject: d.subject, body: d.body, ai_request_id: d.ai_request_id }) }); }}>
            <div className="flex-1"><Field label={t("outreach.draftPurpose")}>{(ids) => <Input {...ids} value={purpose} onChange={(e) => setPurpose(e.target.value)} />}</Field></div>
            <Button type="submit" variant="secondary" disabled={!purpose || draft.isPending}>{t("outreach.draftWithAI")}</Button>
          </form>
          {draft.isError ? <ErrorState title={errorText(t, draft.error)} /> : null}
          <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: () => setForm({ name: "", subject: "", body: "", ai_request_id: "" }) }); }}>
            {form.ai_request_id ? <p role="note"><Badge tone="warn">{t("outreach.aiGenerated")}</Badge> <span className="text-xs text-ink-soft">{t("outreach.aiReview")}</span></p> : null}
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label={t("outreach.templateName")}>{(ids) => <Input {...ids} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
              <Field label={t("outreach.subject")}>{(ids) => <Input {...ids} value={form.subject} onChange={(e) => setForm({ ...form, subject: e.target.value })} />}</Field>
            </div>
            <label className="block text-sm text-ink" htmlFor="tpl-body">{t("outreach.body")}</label>
            <textarea id="tpl-body" className={area} value={form.body} onChange={(e) => setForm({ ...form, body: e.target.value })} />
            {create.isError ? <ErrorState title={errorText(t, create.error)} /> : null}
            <Button type="submit" disabled={!form.name || !form.subject || !form.body || create.isPending}>{t("outreach.saveTemplate")}</Button>
          </form>
        </Card>
      ) : null}
      {templates.isSuccess && list.length === 0 ? <Card><EmptyState icon={Mail} title={t("outreach.noTemplates")} description={t("outreach.noTemplatesHelp")} /></Card> : null}
      {list.map((tpl) => (
        <Card key={tpl.id} className="p-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="font-medium text-ink">{tpl.name} <span className="text-xs text-ink-faint">{t("outreach.version", { n: tpl.version ?? 1 })}</span> {tpl.ai_generated ? <Badge tone="warn">{t("outreach.aiGenerated")}</Badge> : null}</h3>
            {canWrite ? <Button size="sm" variant="ghost" onClick={() => setEditing({ id: tpl.id, subject: tpl.subject ?? "", body: tpl.body ?? "" })}>{t("outreach.newVersion")}</Button> : null}
          </div>
          {editing?.id === tpl.id ? (
            <form className="mt-2 space-y-2" onSubmit={(e) => { e.preventDefault(); version.mutate(undefined, { onSuccess: () => setEditing(undefined) }); }}>
              <Field label={t("outreach.subject")}>{(ids) => <Input {...ids} value={editing.subject} onChange={(e) => setEditing({ ...editing, subject: e.target.value })} />}</Field>
              <textarea aria-label={t("outreach.body")} className={area} value={editing.body} onChange={(e) => setEditing({ ...editing, body: e.target.value })} />
              <div className="flex gap-2"><Button type="submit" disabled={version.isPending}>{t("outreach.saveVersion")}</Button><Button type="button" variant="ghost" onClick={() => setEditing(undefined)}>{t("common.cancel")}</Button></div>
            </form>
          ) : (
            <div className="mt-2 text-sm">
              <p className="font-medium text-ink">{tpl.subject}</p>
              <p className="whitespace-pre-wrap text-ink-soft">{tpl.body}</p>
            </div>
          )}
        </Card>
      ))}
    </div>
  );
}
