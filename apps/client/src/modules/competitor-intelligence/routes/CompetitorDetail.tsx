import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ciPath } from "../paths";
import { Link, useParams } from "react-router";
import { competitorApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { EvidenceBadge, EvidenceKey, StateBadge } from "../components/Evidence";
import { type Observation, useChanges, useCiKey, useCiMutation, useCompetitor, useConfig, useObservations, useRuns, useSources } from "../api/competitors";

function Facts({ id }: { id: string }) {
  const { t } = useTranslation();
  const [history, setHistory] = useState(false);
  const obs = useObservations(id, history);
  const groups = new Map<string, Observation[]>();
  for (const o of obs.data?.data ?? []) groups.set(o.attribute_type, [...(groups.get(o.attribute_type) ?? []), o]);
  return (
    <Card className="p-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-ink">{t("competitors.facts")}</h2>
        <label className="flex items-center gap-2 text-xs text-ink-soft"><input type="checkbox" checked={history} onChange={(e) => setHistory(e.target.checked)} />{t("competitors.showHistory")}</label>
      </div>
      <EvidenceKey />
      {obs.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
      {obs.isError ? <ErrorState title={errorText(t, obs.error)} /> : null}
      {obs.isSuccess && groups.size === 0 ? <EmptyState title={t("competitors.noFacts")} description={t("competitors.noFactsBody")} /> : null}
      <div className="mt-3 space-y-4">
        {[...groups.entries()].map(([attr, list]) => (
          <section key={attr}>
            <h3 className="mb-1 text-xs font-medium tracking-wide text-ink-faint uppercase">{t(`competitors.attributes.${attr}`, { defaultValue: attr })}</h3>
            <ul className="space-y-1.5">
              {list.map((o) => (
                <li key={o.id} className={o.superseded_by_id ? "text-ink-faint line-through" : "text-ink"}>
                  <div className="flex flex-wrap items-center gap-2 text-sm">
                    <EvidenceBadge evidence={o.evidence_class} />
                    {/* Text from web pages is shown escaped, never as markup. */}
                    <span>{o.value}</span>
                  </div>
                  <div className="text-xs text-ink-faint">
                    {o.source_url ? <>{t("competitors.from")} <span className="break-all">{o.source_url}</span>, </> : null}
                    {new Date(o.source_fetched_at ?? o.observed_at).toLocaleDateString()}
                    {o.quote && o.evidence_class === "observed" ? <> · “{o.quote}”</> : null}
                  </div>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </Card>
  );
}

function Changes({ id }: { id: string }) {
  const { t } = useTranslation();
  const changes = useChanges(id);
  const ack = useCiMutation((cid: string) => unwrap(competitorApi.POST("/competitor-intelligence/changes/{id}/acknowledge", { params: { path: { id: cid } } })));
  const rows = changes.data?.data ?? [];
  return (
    <Card className="p-4">
      <h2 className="mb-2 text-sm font-medium text-ink">{t("competitors.changes")}</h2>
      {changes.isError ? <ErrorState title={errorText(t, changes.error)} /> : null}
      {changes.isSuccess && rows.length === 0 ? <p className="text-sm text-ink-soft">{t("competitors.noChanges")}</p> : null}
      <ul className="space-y-2">
        {rows.map((c) => (
          <li key={c.id} className="flex items-start justify-between gap-2 text-sm">
            <div>
              <div className={c.acknowledged_at ? "text-ink-soft" : "font-medium text-ink"}>{c.summary}</div>
              <div className="text-xs text-ink-faint">{new Date(c.created_at).toLocaleString()}</div>
            </div>
            {!c.acknowledged_at ? <Button size="sm" variant="ghost" disabled={ack.isPending} onClick={() => ack.mutate(c.id)}>{t("competitors.acknowledge")}</Button> : null}
          </li>
        ))}
      </ul>
    </Card>
  );
}

function Research({ id }: { id: string }) {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  const runs = useRuns(id);
  const sources = useSources(id);
  const config = useConfig(id);
  const [url, setUrl] = useState("");
  const [kind, setKind] = useState<"website" | "pricing_page" | "service_page" | "public_directory">("pricing_page");
  const start = useCiMutation(() => unwrap(competitorApi.POST("/competitor-intelligence/competitors/{id}/research-runs", { params: { path: { id } } })));
  const addSource = useCiMutation(() => unwrap(competitorApi.POST("/competitor-intelligence/competitors/{id}/sources", { params: { path: { id } }, body: { url, kind } })));
  const removeSource = useCiMutation((sid: string) => unwrap(competitorApi.DELETE("/competitor-intelligence/sources/{id}", { params: { path: { id: sid } } })));
  const setConfig = useCiMutation((b: { cadence: "manual" | "weekly" | "monthly"; enabled: boolean }) => unwrap(competitorApi.PUT("/competitor-intelligence/competitors/{id}/research-config", { params: { path: { id } }, body: b })));
  const busy = Boolean(runs.data?.data.some((r) => r.status === "queued" || r.status === "running"));
  // When a run finishes, everything it may have found is fetched again.
  const qc = useQueryClient();
  const key = useCiKey();
  const wasBusy = useRef(false);
  useEffect(() => {
    if (wasBusy.current && !busy) void qc.invalidateQueries({ queryKey: key("competitor", id) });
    wasBusy.current = busy;
  }, [busy, qc, key, id]);
  return (
    <Card className="p-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-ink">{t("competitors.research")}</h2>
        {can("competitor.research") ? <Button size="sm" disabled={busy || start.isPending} onClick={() => start.mutate(undefined)}>{busy ? t("competitors.researching") : t("competitors.researchNow")}</Button> : null}
      </div>
      <p className="mb-3 text-xs text-ink-faint">{t("competitors.researchNote")}</p>
      {start.isError ? <ErrorState title={errorText(t, start.error)} /> : null}
      <h3 className="mt-2 text-xs font-medium text-ink-faint uppercase">{t("competitors.sources")}</h3>
      <ul className="mt-1 space-y-1 text-sm">
        {(sources.data?.data ?? []).map((s) => (
          <li key={s.id} className="flex flex-wrap items-center gap-2">
            <StateBadge state={s.access_status} />
            <span className="break-all">{s.url}</span>
            {can("competitor.write") ? <Button size="sm" variant="ghost" onClick={() => removeSource.mutate(s.id)}>{t("common.remove")}</Button> : null}
          </li>
        ))}
      </ul>
      {can("competitor.write") ? (
        <form className="mt-3 flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); addSource.mutate(undefined, { onSuccess: () => setUrl("") }); }}>
          <div className="min-w-56 flex-1"><Field label={t("competitors.sourceUrl")} error={addSource.isError ? errorText(t, addSource.error) : undefined}>{(ids) => <Input {...ids} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://" />}</Field></div>
          <Field label={t("competitors.sourceKind")}>
            {(ids) => (
              <Select {...ids} value={kind} onChange={(e) => setKind(e.target.value as typeof kind)}>
                {(["website", "pricing_page", "service_page", "public_directory"] as const).map((k) => <option key={k} value={k}>{t(`competitors.kinds.${k}`)}</option>)}
              </Select>
            )}
          </Field>
          <Button type="submit" size="sm" variant="secondary" disabled={!url || addSource.isPending}>{t("competitors.addSource")}</Button>
        </form>
      ) : null}
      <h3 className="mt-4 text-xs font-medium text-ink-faint uppercase">{t("competitors.schedule")}</h3>
      {config.data ? (
        <div className="mt-1 flex flex-wrap items-end gap-2">
          <Field label={t("competitors.cadence")}>
            {(ids) => (
              <Select {...ids} disabled={!can("competitor.configure")} value={config.data.enabled ? config.data.cadence : "manual"}
                onChange={(e) => { const c = e.target.value as "manual" | "weekly" | "monthly"; setConfig.mutate({ cadence: c, enabled: c !== "manual" }); }}>
                {(["manual", "weekly", "monthly"] as const).map((c) => <option key={c} value={c}>{t(`competitors.cadences.${c}`)}</option>)}
              </Select>
            )}
          </Field>
          {config.data.next_run_at && config.data.enabled ? <span className="pb-2 text-xs text-ink-faint">{t("competitors.nextRun", { when: new Date(config.data.next_run_at).toLocaleString() })}</span> : null}
        </div>
      ) : null}
      {setConfig.isError ? <ErrorState title={errorText(t, setConfig.error)} /> : null}
      <h3 className="mt-4 text-xs font-medium text-ink-faint uppercase">{t("competitors.runs")}</h3>
      {(runs.data?.data.length ?? 0) === 0 ? <p className="text-sm text-ink-soft">{t("competitors.noRuns")}</p> : (
        <Table>
          <thead><tr><th>{t("competitors.started")}</th><th>{t("competitors.statusLabel")}</th><th>{t("competitors.pages")}</th></tr></thead>
          <tbody>
            {(runs.data?.data ?? []).map((r) => (
              <tr key={r.id}><td className="whitespace-nowrap">{new Date(r.created_at).toLocaleString()}{r.scheduled ? ` · ${t("competitors.scheduled")}` : ""}</td><td><StateBadge state={r.status} /></td><td>{r.pages_fetched}</td></tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  );
}

export function CompetitorDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { data: me } = useMe();
  const comp = useCompetitor(id);
  const update = useCiMutation((monitoring: boolean) => unwrap(competitorApi.PATCH("/competitor-intelligence/competitors/{id}", { params: { path: { id } }, body: { monitoring_enabled: monitoring } })));
  if (comp.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (comp.isError) return <ErrorState title={errorText(t, comp.error)} onRetry={() => void comp.refetch()} retryLabel={t("common.retry")} />;
  const c = comp.data;
  return (
    <div className="space-y-4">
      <Link to={ciPath()} className="text-sm text-ink-soft hover:underline">← {t("competitors.back")}</Link>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold text-ink">{c.display_name}</h2>
          {c.domain ? <div className="text-sm text-ink-soft">{c.domain}</div> : null}
        </div>
        {me?.permissions.includes("competitor.write") ? (
          <label className="flex items-center gap-2 text-sm text-ink">
            <input type="checkbox" checked={c.monitoring_enabled} disabled={update.isPending} onChange={(e) => update.mutate(e.target.checked)} />
            {t("competitors.monitorChanges")}
          </label>
        ) : null}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Facts id={id} />
        <div className="space-y-4">
          <Changes id={id} />
          <Research id={id} />
        </div>
      </div>
    </div>
  );
}
