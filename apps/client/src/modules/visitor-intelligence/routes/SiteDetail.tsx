import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router";
import { visitorApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCan, useGoals, useReport, useSite, useVMutation } from "../api/visitors";

/** One site: its install snippet and settings, goals, and what visitors did. */
export function SiteDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const canConfigure = useCan("visitor.configure");
  const canWrite = useCan("visitor.write");
  const site = useSite(id);
  const goals = useGoals(id);
  const [days, setDays] = useState(30);
  const report = useReport(id, days);
  const [origins, setOrigins] = useState<string>();
  const [goal, setGoal] = useState({ name: "", kind: "url_match" as "url_match" | "custom_event", value: "" });
  const update = useVMutation((body: { allowed_origins?: string[]; status?: "active" | "paused"; ip_handling?: "discard" | "truncate_then_discard" }) => unwrap(visitorApi.PATCH("/visitor-intelligence/sites/{id}", { params: { path: { id } }, body })));
  const addGoal = useVMutation(() => unwrap(visitorApi.POST("/visitor-intelligence/sites/{id}/goals", { params: { path: { id } }, body: goal })));
  const removeGoal = useVMutation((gid: string) => unwrap(visitorApi.DELETE("/visitor-intelligence/goals/{id}", { params: { path: { id: gid } } })));
  const toCRM = useVMutation((cid: string) => unwrap(visitorApi.POST("/visitor-intelligence/companies/{id}/send-to-crm", { params: { path: { id: cid } } })));
  if (site.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (site.isError) return <ErrorState title={errorText(t, site.error)} />;
  const s = site.data;
  const snippet = `<script src="${window.location.origin}/t.js" data-site="${s.site_key}" data-mode="${s.consent_mode}" defer></script>`;
  const failure = [update, addGoal, removeGoal, toCRM].find((m) => m.isError)?.error;
  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold text-ink">{s.domain}</h2>
        <Badge tone={s.status === "active" ? "ok" : "neutral"}>{t(`visitors.siteStatus.${s.status}`)}</Badge>
        <Badge tone="neutral">{t(`visitors.modes.${s.consent_mode}`)}</Badge>
      </header>
      {failure ? <ErrorState title={errorText(t, failure)} /> : null}
      <Card className="p-4">
        <h3 className="mb-1 text-sm font-medium text-ink">{t("visitors.install")}</h3>
        <p className="mb-2 text-xs text-ink-soft">{t("visitors.installHelp")}</p>
        <pre className="overflow-x-auto rounded-md bg-sunken p-2 font-mono text-xs" data-testid="snippet">{snippet}</pre>
        {canConfigure ? (
          <div className="mt-3 grid gap-2 sm:grid-cols-3 sm:items-end">
            <Field label={t("visitors.allowedOrigins")}>{(ids) => <Input {...ids} value={origins ?? s.allowed_origins.join(", ")} onChange={(e) => setOrigins(e.target.value)} />}</Field>
            <Button variant="secondary" disabled={origins === undefined || update.isPending} onClick={() => update.mutate({ allowed_origins: (origins ?? "").split(",").map((o) => o.trim()).filter(Boolean) }, { onSuccess: () => setOrigins(undefined) })}>{t("visitors.saveOrigins")}</Button>
            <Button variant="ghost" onClick={() => update.mutate({ status: s.status === "active" ? "paused" : "active" })}>{s.status === "active" ? t("visitors.pause") : t("visitors.resume")}</Button>
            <Field label={t("visitors.ipHandling")}>{(ids) => <Select {...ids} value={s.ip_handling} onChange={(e) => update.mutate({ ip_handling: e.target.value as "discard" | "truncate_then_discard" })}><option value="discard">{t("visitors.ip.discard")}</option><option value="truncate_then_discard">{t("visitors.ip.truncate_then_discard")}</option></Select>}</Field>
          </div>
        ) : null}
        <p className="mt-2 text-xs text-ink-faint">{t("visitors.privacyNote", { days: s.retention_days })}</p>
      </Card>
      <Card className="p-4">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium text-ink">{t("visitors.activity")}</h3>
          <Select aria-label={t("visitors.period")} value={days} onChange={(e) => setDays(Number(e.target.value))}>{[7, 30, 90].map((d) => <option key={d} value={d}>{t("visitors.lastDays", { count: d })}</option>)}</Select>
        </div>
        {report.data ? (
          <>
            <dl className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4" data-testid="visitor-totals">
              {(["page_views", "sessions", "visitors", "conversions"] as const).map((k) => <div key={k}><dt className="text-ink-soft">{t(`visitors.totals.${k}`)}</dt><dd className="text-lg font-semibold">{report.data[k]}</dd></div>)}
            </dl>
            <div className="mt-4 grid gap-4 lg:grid-cols-2">
              <Table>
                <thead><tr><th>{t("visitors.page")}</th><th>{t("visitors.views")}</th></tr></thead>
                <tbody>{report.data.pages.map((p) => <tr key={p.url}><td className="break-all">{p.url}</td><td>{p.views}</td></tr>)}</tbody>
              </Table>
              <Table>
                <thead><tr><th>{t("visitors.source")}</th><th>{t("visitors.totals.sessions")}</th></tr></thead>
                <tbody>{report.data.sources.map((p) => <tr key={p.source}><td>{p.source}</td><td>{p.sessions}</td></tr>)}</tbody>
              </Table>
            </div>
          </>
        ) : <SkeletonRows label={t("common.loading")} />}
      </Card>
      <Card className="p-4">
        <h3 className="mb-1 text-sm font-medium text-ink">{t("visitors.companies")}</h3>
        <p className="mb-2 text-xs text-ink-soft">{t("visitors.companiesHelp")}</p>
        <Table>
          <thead><tr><th>{t("visitors.company")}</th><th>{t("visitors.visits")}</th><th>{t("visitors.confidence")}</th><th /></tr></thead>
          <tbody>
            {(report.data?.companies ?? []).map((c) => (
              <tr key={c.company_id}>
                <td>{c.name || c.domain}<div className="text-xs text-ink-faint">{c.domain}</div></td>
                <td>{c.visits}</td><td>{Math.round(c.confidence * 100)}%</td>
                <td>{canWrite ? <Button size="sm" variant="ghost" disabled={toCRM.isPending} onClick={() => toCRM.mutate(c.company_id)}>{t("visitors.sendToCRM")}</Button> : null}</td>
              </tr>
            ))}
          </tbody>
        </Table>
        {toCRM.isSuccess ? <p role="status" className="mt-2 text-sm text-ok">{t("visitors.sentToCRM")}</p> : null}
      </Card>
      <Card className="p-4">
        <h3 className="mb-2 text-sm font-medium text-ink">{t("visitors.goals")}</h3>
        <ul className="mb-3 space-y-1 text-sm">
          {(goals.data?.data ?? []).map((g) => <li key={g.id} className="flex items-center justify-between">{g.name} <span className="text-xs text-ink-faint">{t(`visitors.goalKinds.${g.kind}`)}: {g.value}</span>{canConfigure ? <Button size="sm" variant="ghost" onClick={() => removeGoal.mutate(g.id)}>{t("common.remove")}</Button> : null}</li>)}
        </ul>
        {canConfigure ? (
          <form className="grid gap-2 sm:grid-cols-4 sm:items-end" onSubmit={(e) => { e.preventDefault(); addGoal.mutate(undefined, { onSuccess: () => setGoal({ ...goal, name: "", value: "" }) }); }}>
            <Field label={t("visitors.goalName")}>{(ids) => <Input {...ids} value={goal.name} onChange={(e) => setGoal({ ...goal, name: e.target.value })} />}</Field>
            <Field label={t("visitors.goalKind")}>{(ids) => <Select {...ids} value={goal.kind} onChange={(e) => setGoal({ ...goal, kind: e.target.value as "url_match" | "custom_event" })}><option value="url_match">{t("visitors.goalKinds.url_match")}</option><option value="custom_event">{t("visitors.goalKinds.custom_event")}</option></Select>}</Field>
            <Field label={t("visitors.goalValue")}>{(ids) => <Input {...ids} value={goal.value} onChange={(e) => setGoal({ ...goal, value: e.target.value })} />}</Field>
            <Button type="submit" disabled={!goal.name || !goal.value || addGoal.isPending}>{t("visitors.addGoal")}</Button>
          </form>
        ) : null}
      </Card>
    </div>
  );
}
