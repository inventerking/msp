import { ApiError, unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Select, SkeletonRows, Table } from "@msp/ui";
import { AlertTriangle, Sparkles } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { leadPath } from "../paths";
import { Link, useParams } from "react-router";
import { leadApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { LicenceBadge, QualityBadge, ScoreBadge, StatusBadge } from "../components/Labels";
import { type Score, useLeadMutation, useLists, useProspect, useScore } from "../api/leads";

function show(v: unknown): string {
  if (Array.isArray(v)) return v.join(", ");
  if (v === null || v === undefined) return "";
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}

function ScoreCard({ id, canWrite }: { id: string; canWrite: boolean }) {
  const { t } = useTranslation();
  const rescore = useLeadMutation(() => unwrap(leadApi.POST("/lead-generation/prospects/{id}/rescore", { params: { path: { id } } })));
  const explain = useLeadMutation(() => unwrap(leadApi.POST("/lead-generation/prospects/{id}/explain", { params: { path: { id } } })));
  const score = useScore(id, explain.isSuccess);
  const notScored = score.error instanceof ApiError && score.error.code === "not_scored";
  const s: Score | undefined = score.data;
  return (
    <Card className="p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-ink">{t("leads.scoreTitle")}</h2>
        {canWrite ? (
          <div className="flex gap-2">
            <Button size="sm" variant="secondary" disabled={rescore.isPending} onClick={() => rescore.mutate(undefined)}>{t("leads.rescore")}</Button>
            {s ? <Button size="sm" variant="secondary" disabled={explain.isPending || explain.isSuccess} onClick={() => explain.mutate(undefined)}><Sparkles aria-hidden />{t("leads.explain")}</Button> : null}
          </div>
        ) : null}
      </div>
      {rescore.isError ? <ErrorState title={errorText(t, rescore.error)} /> : null}
      {explain.isError ? <ErrorState title={errorText(t, explain.error)} /> : null}
      {score.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
      {notScored ? <p className="text-sm text-ink-soft">{t("leads.notScoredBody")} <Link className="underline" to={leadPath("icp")}>{t("leads.tabs.icp")}</Link></p> : null}
      {score.isError && !notScored ? <ErrorState title={errorText(t, score.error)} /> : null}
      {s ? (
        <>
          <div className="mb-3 flex items-center gap-3">
            <ScoreBadge score={s.value} coverage={s.coverage} />
            <span className="text-xs text-ink-faint">{t("leads.model", { name: s.model_name, version: s.model_version })}</span>
          </div>
          <p className="mb-3 text-xs text-ink-faint">{t("leads.unknownMeansNoData")}</p>
          <Table>
            <thead><tr><th>{t("leads.factor")}</th><th>{t("leads.result")}</th><th>{t("leads.points")}</th></tr></thead>
            <tbody>
              {s.factors.map((f) => (
                <tr key={f.key}>
                  <td>{t(`leads.factors.${f.key}`, { defaultValue: f.key })}</td>
                  <td><Badge tone={f.state === "matched" ? "ok" : f.state === "partial" ? "info" : f.state === "not_matched" ? "danger" : "neutral"}>{t(`leads.factorState.${f.state}`)}</Badge></td>
                  <td className="text-ink-soft">{f.earned === undefined ? t("leads.noData") : `${f.earned} / ${f.weight}`}</td>
                </tr>
              ))}
            </tbody>
          </Table>
          {s.explanation ? (
            <div className="mt-3 rounded-md border border-line bg-sunken p-3">
              <div className="mb-1"><Badge tone="warn">{s.explanation_is_ai ? t("leads.aiGenerated") : t("leads.explanation")}</Badge></div>
              <p className="text-sm text-ink">{s.explanation}</p>
            </div>
          ) : explain.isSuccess ? <p className="mt-3 text-sm text-ink-soft" role="status">{t("leads.explaining")}</p> : null}
        </>
      ) : null}
    </Card>
  );
}

export function ProspectDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { data: me } = useMe();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  const prospect = useProspect(id);
  const lists = useLists();
  const [listId, setListId] = useState("");
  const update = useLeadMutation((status: "new" | "qualified" | "disqualified") => unwrap(leadApi.PATCH("/lead-generation/prospects/{id}", { params: { path: { id } }, body: { status } })));
  const enrich = useLeadMutation((kinds: ("company" | "contact")[]) => unwrap(leadApi.POST("/lead-generation/prospects/{id}/enrich", { params: { path: { id } }, body: { kinds } })));
  const convert = useLeadMutation(() => unwrap(leadApi.POST("/lead-generation/prospects/{id}/convert", { params: { path: { id } } })));
  const addToList = useLeadMutation((list: string) => unwrap(leadApi.POST("/lead-generation/lists/{id}/members", { params: { path: { id: list } }, body: { prospect_ids: [id] } })));
  if (prospect.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (prospect.isError) return <ErrorState title={errorText(t, prospect.error)} onRetry={() => void prospect.refetch()} retryLabel={t("common.retry")} />;
  const p = prospect.data;
  const busy = p.enrichments.some((e) => e.status === "queued" || e.status === "running");
  return (
    <div className="space-y-4">
      <Link to={leadPath()} className="text-sm text-ink-soft hover:underline">← {t("leads.backToProspects")}</Link>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold text-ink">{p.display_name}</h2>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-sm text-ink-soft">
            {p.domain ? <span>{p.domain}</span> : null}
            <StatusBadge status={p.status} />
            <QualityBadge quality={p.data_quality} />
          </div>
        </div>
        {can("lead.write") && p.status !== "converted" ? (
          <div className="flex flex-wrap items-end gap-2">
            <Field label={t("leads.statusLabel")}>
              {(ids) => (
                <Select {...ids} value={p.status === "enriching" ? "new" : p.status} onChange={(e) => update.mutate(e.target.value as "new" | "qualified" | "disqualified")}>
                  {(["new", "qualified", "disqualified"] as const).map((s) => <option key={s} value={s}>{t(`leads.status.${s}`)}</option>)}
                </Select>
              )}
            </Field>
            <Button variant="secondary" disabled={convert.isPending} onClick={() => convert.mutate(undefined)}>{t("leads.convert")}</Button>
          </div>
        ) : null}
      </div>
      {p.is_competitor ? (
        <div role="note" className="flex items-start gap-2 rounded-md border border-warn/30 bg-warn-soft p-3 text-sm text-warn"><AlertTriangle className="mt-0.5 size-4" aria-hidden />{t("leads.isCompetitor")}</div>
      ) : null}
      {convert.isError ? <ErrorState title={errorText(t, convert.error)} /> : null}
      {update.isError ? <ErrorState title={errorText(t, update.error)} /> : null}
      <div className="grid gap-4 lg:grid-cols-2">
        <ScoreCard id={id} canWrite={can("lead.write")} />
        <Card className="p-4">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-sm font-medium text-ink">{t("leads.enrichment")}</h2>
            {can("lead.enrich") ? (
              <div className="flex gap-2">
                <Button size="sm" variant="secondary" disabled={busy || enrich.isPending} onClick={() => enrich.mutate(["company"])}>{t("leads.enrichCompany")}</Button>
                <Button size="sm" variant="secondary" disabled={busy || enrich.isPending} onClick={() => enrich.mutate(["contact"])}>{t("leads.findContacts")}</Button>
              </div>
            ) : null}
          </div>
          <p className="mb-2 text-xs text-ink-faint">{t("leads.enrichCost")}</p>
          {enrich.isError ? <ErrorState title={errorText(t, enrich.error)} /> : null}
          {p.enrichments.length === 0 ? <p className="text-sm text-ink-soft">{t("leads.noEnrichment")}</p> : (
            <ul className="space-y-1 text-sm">
              {p.enrichments.map((e) => (
                <li key={e.id} className="flex items-center gap-2">
                  <span className="w-20 text-ink-soft">{t(`leads.kinds.${e.kind}`)}</span>
                  <StatusBadge status={e.status} />
                  <span className="text-xs text-ink-faint">{new Date(e.created_at).toLocaleString()}</span>
                </li>
              ))}
            </ul>
          )}
          {can("lead.write") && (lists.data?.data.length ?? 0) > 0 ? (
            <form className="mt-4 flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (listId) addToList.mutate(listId); }}>
              <div className="flex-1">
                <Field label={t("leads.addToList")}>
                  {(ids) => (
                    <Select {...ids} value={listId} onChange={(e) => setListId(e.target.value)}>
                      <option value="">{t("leads.chooseList")}</option>
                      {(lists.data?.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
                    </Select>
                  )}
                </Field>
              </div>
              <Button type="submit" size="sm" disabled={!listId || addToList.isPending}>{t("leads.add")}</Button>
              {addToList.isSuccess ? <span role="status" className="text-xs text-ok">{t("common.saved")}</span> : null}
            </form>
          ) : null}
        </Card>
      </div>
      <Card>
        <h2 className="px-4 pt-4 text-sm font-medium text-ink">{t("leads.facts")}</h2>
        {p.attributes.length === 0 ? <EmptyState title={t("leads.noFacts")} /> : (
          <Table>
            <thead><tr><th>{t("leads.attribute")}</th><th>{t("leads.value")}</th><th>{t("leads.source")}</th><th>{t("leads.licenceLabel")}</th><th>{t("leads.observed")}</th></tr></thead>
            <tbody>
              {p.attributes.map((a) => (
                <tr key={a.attribute}>
                  <td>{t(`leads.attributes.${a.attribute}`, { defaultValue: a.attribute })}</td>
                  <td>{show(a.value)}</td>
                  <td className="text-ink-soft">{t(`leads.sources.${a.source_key}`, { defaultValue: a.source_key })}</td>
                  <td><LicenceBadge licence={a.licence_class} /></td>
                  <td className="whitespace-nowrap text-ink-soft">{new Date(a.observed_at).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
      <Card>
        <h2 className="px-4 pt-4 text-sm font-medium text-ink">{t("leads.contacts")}</h2>
        {p.contacts.length === 0 ? <EmptyState title={t("leads.noContacts")} /> : (
          <Table>
            <thead><tr><th>{t("leads.role")}</th><th>{t("leads.decisionMaker")}</th><th>{t("leads.confidence")}</th><th>{t("leads.source")}</th></tr></thead>
            <tbody>
              {p.contacts.map((c) => (
                <tr key={c.contact_id}>
                  <td>{c.role_title ?? "—"}</td>
                  <td>{c.is_decision_maker ? t("leads.yes") : t("leads.no")}</td>
                  <td>{c.confidence === undefined ? "—" : `${Math.round(c.confidence * 100)}%`}</td>
                  <td className="text-ink-soft">{t(`leads.sources.${c.source_key}`, { defaultValue: c.source_key })}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
        <p className="px-4 pb-4 text-xs text-ink-faint">{t("leads.contactsNote")}</p>
      </Card>
    </div>
  );
}
