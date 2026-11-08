import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, errorText, hasRole, useAdmin } from "../app/api";

const area = "min-h-28 w-full rounded-md border border-line-strong bg-surface p-2 font-mono text-xs";

export function Ai() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const qc = useQueryClient();
  const canManage = hasRole(me, "super_admin", "integration_admin");
  const routes = useQuery({ queryKey: ["ai-routes"], queryFn: () => unwrap(api.GET("/ai/routes")) });
  const prompts = useQuery({ queryKey: ["ai-prompts"], queryFn: () => unwrap(api.GET("/ai/prompts")) });
  const [editing, setEditing] = useState<string>();
  const [system, setSystem] = useState("");
  const [user, setUser] = useState("{{content}}");
  const [draft, setDraft] = useState<{ id: string; prompt: string; version: number }>();
  const [failure, setFailure] = useState<string>();
  const createDraft = async () => {
    if (!editing) return;
    setFailure(undefined);
    try {
      const r = await unwrap(api.POST("/ai/prompts/{id}/versions", { params: { path: { id: editing } }, body: { system_text: system, user_text: user } }));
      const p = prompts.data?.data.find((x) => x.id === editing);
      setDraft({ id: r.id, prompt: editing, version: (p?.versions ?? 0) + 1 });
      setEditing(undefined);
      await qc.invalidateQueries({ queryKey: ["ai-prompts"] });
    } catch (e) {
      setFailure(errorText(t, e));
    }
  };
  const activate = async () => {
    if (!draft) return;
    setFailure(undefined);
    try {
      await unwrap(api.POST("/ai/prompt-versions/{id}/activate", { params: { path: { id: draft.id } } }));
      setDraft(undefined);
      await qc.invalidateQueries({ queryKey: ["ai-prompts"] });
    } catch (e) {
      setFailure(errorText(t, e));
    }
  };
  return (
    <>
      <PageHeader title={t("ai.title")} description={t("ai.subtitle")} />
      {failure ? <div className="mb-4"><ErrorState title={failure} /></div> : null}
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("ai.routes")}</h2>
        <Card>
          {routes.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
          {routes.data ? (
            <Table>
              <thead><tr><th>{t("ai.task")}</th><th>{t("ai.provider")}</th><th>{t("ai.model")}</th><th>{t("ai.limits")}</th><th /></tr></thead>
              <tbody>{routes.data.data.map((r) => <tr key={r.id}><td><code className="text-xs">{r.task_key}</code></td><td>{r.provider_key}</td><td>{r.model}</td><td className="tabular-nums text-ink-soft">{r.max_input_tokens} / {r.max_output_tokens}</td><td><Badge tone={r.status === "active" ? "ok" : "neutral"}>{r.status}</Badge></td></tr>)}</tbody>
            </Table>
          ) : null}
        </Card>
      </section>
      <section>
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("ai.prompts")}</h2>
        <Card>
          {prompts.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
          {prompts.data ? (
            <Table>
              <thead><tr><th>{t("ai.task")}</th><th>{t("ai.class")}</th><th>{t("ai.active")}</th><th>{t("ai.versions")}</th>{canManage ? <th /> : null}</tr></thead>
              <tbody>
                {prompts.data.data.map((p) => (
                  <tr key={p.id}>
                    <td><code className="text-xs">{p.task_key}</code></td>
                    <td><Badge tone={p.action_class === "informational" ? "neutral" : "warn"}>{t(`ai.classes.${p.action_class}`, { defaultValue: p.action_class })}</Badge></td>
                    <td className="tabular-nums">v{p.active_version}</td>
                    <td className="tabular-nums">{p.versions}</td>
                    {canManage ? <td className="text-right"><Button size="sm" variant="secondary" onClick={() => setEditing(p.id)}>{t("ai.newVersion")}</Button></td> : null}
                  </tr>
                ))}
              </tbody>
            </Table>
          ) : null}
        </Card>
        {editing ? (
          <Card className="mt-4 p-4">
            <div className="space-y-3">
              <Field label={t("ai.system")}>{(ids) => <textarea {...ids} className={area} value={system} onChange={(e) => setSystem(e.target.value)} />}</Field>
              <Field label={t("ai.user")}>{(ids) => <textarea {...ids} className={area} value={user} onChange={(e) => setUser(e.target.value)} />}</Field>
              <div className="flex gap-2"><Button disabled={!system || !user} onClick={() => void createDraft()}>{t("ai.create")}</Button><Button variant="secondary" onClick={() => setEditing(undefined)}>{t("common.cancel")}</Button></div>
            </div>
          </Card>
        ) : null}
        {draft ? (
          <Card className="mt-4 flex flex-wrap items-center justify-between gap-3 p-4">
            <p className="text-sm text-ink-soft">{t("ai.draftSaved")}</p>
            <Button onClick={() => void activate()}>{t("ai.activate", { v: draft.version })}</Button>
          </Card>
        ) : null}
      </section>
    </>
  );
}
