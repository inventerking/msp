import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, PageHeader, Select, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, errorText, hasRole, useAdmin } from "../app/api";

type Kind = "terms" | "privacy" | "dpa" | "acceptable_use" | "subprocessors";
const kinds: Kind[] = ["terms", "privacy", "dpa", "acceptable_use", "subprocessors"];

/** Versions of the legal documents counsel provides. Publishing makes a version current; acceptance
 * is recorded per version. The platform does not write or judge the text. */
export function Legal() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const qc = useQueryClient();
  const isSuper = hasRole(me, "super_admin");
  const docs = useQuery({ queryKey: ["legal"], queryFn: () => unwrap(api.GET("/legal/documents")) });
  const [form, setForm] = useState({ kind: "terms" as Kind, title: "", body: "", requires: true });
  const [editing, setEditing] = useState<string>();
  const [reviewed, setReviewed] = useState(false);
  const refresh = () => qc.invalidateQueries({ queryKey: ["legal"] });
  const save = useMutation({
    mutationFn: () => {
      const body = { kind: form.kind, title: form.title, body: form.body, requires_acceptance: form.requires };
      return editing ? unwrap(api.PATCH("/legal/documents/{id}", { params: { path: { id: editing } }, body })) : unwrap(api.POST("/legal/documents", { body }));
    },
    onSuccess: (d) => { setEditing(d.id); void refresh(); },
  });
  const publish = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/legal/documents/{id}/publish", { params: { path: { id } }, body: { counsel_reviewed: reviewed } })), onSuccess: () => { setEditing(undefined); setForm({ ...form, title: "", body: "" }); void refresh(); } });
  const open = async (id: string) => {
    const d = await unwrap(api.GET("/legal/documents/{id}", { params: { path: { id } } }));
    setForm({ kind: d.kind as Kind, title: d.title, body: d.body ?? "", requires: d.requires_acceptance });
    setEditing(d.status === "draft" ? d.id : undefined);
  };
  return (
    <>
      <PageHeader title={t("legal.title")} description={t("legal.subtitle")} />
      <p role="note" className="mb-4 rounded-md bg-warn-soft p-3 text-sm text-warn">{t("legal.counselNote")}</p>
      <Card className="mb-4">
        {docs.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {docs.isError ? <div className="p-4"><ErrorState title={errorText(t, docs.error)} /></div> : null}
        {docs.data?.data.length ? (
          <Table>
            <thead><tr><th>{t("legal.document")}</th><th>{t("legal.version")}</th><th>{t("legal.status")}</th><th>{t("legal.counsel")}</th><th className="text-right">{t("legal.accepted")}</th></tr></thead>
            <tbody>
              {docs.data.data.map((d) => (
                <tr key={d.id}>
                  <td><button type="button" className="text-brand underline" onClick={() => void open(d.id)}>{d.title}</button><div className="text-xs text-ink-faint">{t(`legal.kinds.${d.kind}`)}</div></td>
                  <td>{d.version}</td>
                  <td><Badge tone={d.status === "published" ? "ok" : "neutral"}>{t(`legal.statuses.${d.status}`)}</Badge></td>
                  <td>{d.counsel_reviewed ? t("legal.reviewed") : t("legal.notReviewed")}</td>
                  <td className="text-right tabular-nums">{d.accepted_count}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
      {isSuper ? (
        <Card className="p-4">
          <h2 className="mb-2 text-sm font-medium">{editing ? t("legal.editDraft") : t("legal.newDraft")}</h2>
          <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label={t("legal.kind")}>{(ids) => <Select {...ids} value={form.kind} disabled={Boolean(editing)} onChange={(e) => setForm({ ...form, kind: e.target.value as Kind })}>{kinds.map((k) => <option key={k} value={k}>{t(`legal.kinds.${k}`)}</option>)}</Select>}</Field>
              <Field label={t("legal.docTitle")}>{(ids) => <Input {...ids} maxLength={200} value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />}</Field>
            </div>
            <Field label={t("legal.text")}>{(ids) => <textarea {...ids} className="min-h-64 w-full rounded-md border border-line-strong bg-surface p-2 font-mono text-sm" value={form.body} onChange={(e) => setForm({ ...form, body: e.target.value })} />}</Field>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.requires} onChange={(e) => setForm({ ...form, requires: e.target.checked })} />{t("legal.requiresAcceptance")}</label>
            {save.isError || publish.isError ? <ErrorState title={errorText(t, save.error ?? publish.error)} /> : null}
            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={!form.title.trim() || !form.body.trim() || save.isPending}>{t("legal.saveDraft")}</Button>
              {editing ? (
                <>
                  <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={reviewed} onChange={(e) => setReviewed(e.target.checked)} />{t("legal.counselReviewed")}</label>
                  <Button type="button" variant="secondary" disabled={publish.isPending} onClick={() => publish.mutate(editing)}>{t("legal.publish")}</Button>
                </>
              ) : null}
            </div>
          </form>
        </Card>
      ) : null}
    </>
  );
}
