import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { FileText, Layers, Lightbulb } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { expansionApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { type Contract, type CoverageLine, healthTone, money, toMinor, useCan, useContracts, useCoverage, useCustomer, useRecommendations, useXMutation } from "../api/expansion";
import { RecommendationCard } from "../components/RecommendationCard";

/** One customer: contracts, service coverage, health, and the suggestions for them. */
export function CustomerDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const customer = useCustomer(id);
  const canWrite = useCan("expansion.write");
  const canAnalyze = useCan("expansion.analyze");
  const setHealth = useXMutation((status: string | null) => unwrap(expansionApi.PATCH("/customer-expansion/customers/{id}", { params: { path: { id } }, body: { health_status: status as never } })));
  const analyze = useXMutation(() => unwrap(expansionApi.POST("/customer-expansion/customers/{id}/analyze", { params: { path: { id } } })));
  if (customer.isPending) return <Card><SkeletonRows label={t("common.loading")} /></Card>;
  if (customer.isError) return <Card className="p-4"><ErrorState title={errorText(t, customer.error)} onRetry={() => void customer.refetch()} retryLabel={t("common.retry")} /></Card>;
  const c = customer.data;
  return (
    <div className="space-y-4">
      <Card className="p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold text-ink">{c.crm_company.name}</h2>
            <p className="text-sm text-ink-soft">{c.crm_company.domain ?? ""}</p>
            <p className="mt-2 text-sm">
              {t("expansion.mrr")}: <span className="font-medium tabular-nums">{c.mixed_currency ? t("expansion.mixedCurrency") : money(c.active_mrr_minor, c.currency)}</span>
              <Badge tone="neutral">{t("intelKinds.actual")}</Badge>
            </p>
          </div>
          <div className="flex flex-wrap items-end gap-2">
            <div>
              <span className="block text-xs text-ink-soft">{t("expansion.health")}</span>
              <Badge tone={healthTone(c.health_status)}>{t(`expansion.healthStatus.${c.health_status}`)}</Badge>
              <span className="ml-1 text-xs text-ink-faint">{c.health_source === "manual" ? t("expansion.setByHand") : c.health_as_of ? t("expansion.asOf", { when: new Date(c.health_as_of).toLocaleString() }) : ""}</span>
            </div>
            {canWrite ? (
              <Field label={t("expansion.setHealth")}>{(ids) => (
                <Select {...ids} value={c.health_source === "manual" ? c.health_status : ""} onChange={(e) => setHealth.mutate(e.target.value || null)}>
                  <option value="">{t("expansion.computed")}</option>
                  {(["healthy", "watch", "at_risk"] as const).map((h) => <option key={h} value={h}>{t(`expansion.healthStatus.${h}`)}</option>)}
                </Select>
              )}</Field>
            ) : null}
            {canAnalyze ? <Button disabled={analyze.isPending} onClick={() => analyze.mutate(undefined)}>{analyze.isPending ? t("expansion.analyzing") : t("expansion.analyze")}</Button> : null}
          </div>
        </div>
        {analyze.isError ? <div className="mt-2"><ErrorState title={errorText(t, analyze.error)} /></div> : null}
        {analyze.data ? (
          <div role="status" className="mt-3 rounded-md bg-sunken p-3 text-sm">
            <p>{t("expansion.analysisDone", { created: analyze.data.created, refreshed: analyze.data.refreshed, expired: analyze.data.expired })}</p>
            <ul className="mt-1 flex flex-wrap gap-2">
              {analyze.data.indicators.map((i) => <li key={i.key}><Badge tone={i.polarity === "positive" ? "ok" : i.polarity === "negative" ? "danger" : i.polarity === "neutral" ? "warn" : "neutral"}>{t(`expansion.indicators.${i.key}`)}: {t(`expansion.polarity.${i.polarity}`)}</Badge></li>)}
            </ul>
          </div>
        ) : null}
      </Card>
      <Contracts id={id} canWrite={canWrite} />
      <Coverage id={id} canWrite={canWrite} />
      <CustomerRecommendations id={id} />
    </div>
  );
}

function Contracts({ id, canWrite }: { id: string; canWrite: boolean }) {
  const { t } = useTranslation();
  const contracts = useContracts(id);
  const [form, setForm] = useState({ start_date: new Date().toISOString().slice(0, 10), end_date: "", currency: "GBP", mrr: "", notice_days: "", auto_renew: false });
  const create = useXMutation(() => {
    const mrr = toMinor(form.mrr, form.currency) ?? 0;
    return unwrap(expansionApi.POST("/customer-expansion/customers/{id}/contracts", {
      params: { path: { id } },
      body: { status: "active", start_date: form.start_date, currency: form.currency, mrr_minor: mrr, auto_renew: form.auto_renew, ...(form.end_date ? { end_date: form.end_date } : {}), ...(form.notice_days ? { notice_days: Number(form.notice_days) } : {}) },
    }));
  });
  const update = useXMutation((v: { id: string; body: Record<string, unknown> }) => unwrap(expansionApi.PATCH("/customer-expansion/contracts/{id}", { params: { path: { id: v.id } }, body: v.body })));
  const list = contracts.data?.data ?? [];
  return (
    <Card className="p-4">
      <h3 className="mb-2 text-sm font-medium text-ink">{t("expansion.contracts")}</h3>
      {contracts.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
      {contracts.isError ? <ErrorState title={errorText(t, contracts.error)} /> : null}
      {contracts.isSuccess && list.length === 0 ? <EmptyState icon={FileText} title={t("expansion.noContracts")} description={t("expansion.noContractsHelp")} /> : null}
      {list.length ? (
        <Table>
          <thead><tr><th>{t("expansion.status")}</th><th className="text-right">{t("expansion.mrr")}</th><th>{t("expansion.start")}</th><th>{t("expansion.end")}</th><th>{t("expansion.source")}</th><th><span className="sr-only">{t("expansion.actions")}</span></th></tr></thead>
          <tbody>
            {list.map((k: Contract) => (
              <tr key={k.id}>
                <td><Badge tone={k.status === "active" ? "ok" : k.status === "draft" ? "warn" : "neutral"}>{t(`expansion.contractStatus.${k.status}`)}</Badge>{k.review_reason ? <div className="mt-1 text-xs text-warn">{t(`expansion.review.${k.review_reason}`)}</div> : null}</td>
                <td className="text-right tabular-nums">{money(k.mrr_minor, k.currency)}</td>
                <td>{new Date(k.start_date).toLocaleDateString()}</td>
                <td>{k.end_date ? new Date(k.end_date).toLocaleDateString() : t("expansion.openEnded")}{k.auto_renew ? <div className="text-xs text-ink-faint">{t("expansion.autoRenews")}</div> : null}</td>
                <td className="text-xs text-ink-soft">{t(`expansion.sources.${k.source}`)}</td>
                <td className="space-x-1 text-right whitespace-nowrap">
                  {canWrite && k.review_reason ? <Button size="sm" variant="secondary" onClick={() => update.mutate({ id: k.id, body: { clear_review: true } })}>{t("expansion.markReviewed")}</Button> : null}
                  {canWrite && k.status === "draft" ? <Button size="sm" variant="secondary" onClick={() => update.mutate({ id: k.id, body: { status: "active" } })}>{t("expansion.activate")}</Button> : null}
                  {canWrite && (k.status === "active" || k.status === "renewing") ? <Button size="sm" variant="secondary" onClick={() => update.mutate({ id: k.id, body: { status: "ended" } })}>{t("expansion.endContract")}</Button> : null}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : null}
      {update.isError ? <ErrorState title={errorText(t, update.error)} /> : null}
      {canWrite ? (
        <form className="mt-4 grid gap-2 border-t border-line pt-4 sm:grid-cols-3" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: () => setForm({ ...form, mrr: "", end_date: "", notice_days: "" }) }); }}>
          <Field label={t("expansion.start")}>{(ids) => <Input {...ids} type="date" value={form.start_date} onChange={(e) => setForm({ ...form, start_date: e.target.value })} />}</Field>
          <Field label={t("expansion.end")}>{(ids) => <Input {...ids} type="date" value={form.end_date} onChange={(e) => setForm({ ...form, end_date: e.target.value })} />}</Field>
          <Field label={t("expansion.noticeDays")}>{(ids) => <Input {...ids} inputMode="numeric" value={form.notice_days} onChange={(e) => setForm({ ...form, notice_days: e.target.value.replace(/\D/g, "") })} />}</Field>
          <Field label={t("expansion.currency")}>{(ids) => <Input {...ids} maxLength={3} value={form.currency} onChange={(e) => setForm({ ...form, currency: e.target.value.toUpperCase() })} />}</Field>
          <Field label={t("expansion.monthlyValue")}>{(ids) => <Input {...ids} inputMode="decimal" value={form.mrr} onChange={(e) => setForm({ ...form, mrr: e.target.value })} />}</Field>
          <label className="flex items-end gap-2 pb-2 text-sm"><input type="checkbox" checked={form.auto_renew} onChange={(e) => setForm({ ...form, auto_renew: e.target.checked })} />{t("expansion.autoRenew")}</label>
          {create.isError ? <div className="sm:col-span-3"><ErrorState title={errorText(t, create.error)} /></div> : null}
          <div><Button type="submit" disabled={create.isPending || toMinor(form.mrr, form.currency || "GBP") === undefined || form.currency.length !== 3}>{t("expansion.addContract")}</Button></div>
        </form>
      ) : null}
    </Card>
  );
}

const coverages = ["active", "absent", "declined", "unknown"] as const;

function Coverage({ id, canWrite }: { id: string; canWrite: boolean }) {
  const { t } = useTranslation();
  const coverage = useCoverage(id);
  const [draft, setDraft] = useState<Record<string, CoverageLine["coverage"]>>({});
  useEffect(() => {
    if (coverage.data) setDraft(Object.fromEntries(coverage.data.data.map((l) => [l.tenant_service_id, l.coverage])));
  }, [coverage.data]);
  const save = useXMutation(() => {
    const lines = coverage.data?.data ?? [];
    const items = lines.map((l) => {
      const c = draft[l.tenant_service_id] ?? l.coverage;
      // A recorded price is kept while coverage stays active.
      const keepPrice = c === "active" && l.coverage === "active" && l.mrr_minor !== undefined && l.currency;
      return { tenant_service_id: l.tenant_service_id, coverage: c, ...(l.quantity !== undefined ? { quantity: l.quantity } : {}), ...(keepPrice ? { mrr_minor: l.mrr_minor, currency: l.currency } : {}) };
    });
    return unwrap(expansionApi.PUT("/customer-expansion/customers/{id}/services", { params: { path: { id } }, body: { items } }));
  });
  const lines = coverage.data?.data ?? [];
  const changed = lines.some((l) => draft[l.tenant_service_id] && draft[l.tenant_service_id] !== l.coverage);
  return (
    <Card className="p-4">
      <h3 className="text-sm font-medium text-ink">{t("expansion.coverageTitle")}</h3>
      <p className="mb-2 text-xs text-ink-soft">{t("expansion.coverageHelp")}</p>
      {coverage.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
      {coverage.isError ? <ErrorState title={errorText(t, coverage.error)} /> : null}
      {coverage.isSuccess && lines.length === 0 ? <EmptyState icon={Layers} title={t("expansion.noServices")} description={t("expansion.noServicesHelp")} action={<Link className="text-sm text-brand underline" to="/settings">{t("expansion.openSettings")}</Link>} /> : null}
      {lines.length ? (
        <Table>
          <thead><tr><th>{t("expansion.service")}</th><th>{t("expansion.coverage")}</th><th className="text-right">{t("expansion.mrr")}</th></tr></thead>
          <tbody>
            {lines.map((l) => (
              <tr key={l.tenant_service_id}>
                <td>{l.service_name}{l.service_status === "retired" ? <span className="ml-1 text-xs text-ink-faint">{t("services.statuses.retired")}</span> : null}</td>
                <td>
                  {canWrite ? (
                    <Select aria-label={t("expansion.coverageFor", { service: l.service_name })} value={draft[l.tenant_service_id] ?? l.coverage} onChange={(e) => setDraft({ ...draft, [l.tenant_service_id]: e.target.value as CoverageLine["coverage"] })}>
                      {coverages.map((c) => <option key={c} value={c}>{t(`expansion.coverageStates.${c}`)}</option>)}
                    </Select>
                  ) : t(`expansion.coverageStates.${l.coverage}`)}
                </td>
                <td className="text-right tabular-nums">{money(l.mrr_minor, l.currency)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : null}
      {canWrite && lines.length ? <div className="mt-3"><Button disabled={!changed || save.isPending} onClick={() => save.mutate(undefined)}>{t("expansion.saveCoverage")}</Button></div> : null}
      {save.isError ? <ErrorState title={errorText(t, save.error)} /> : null}
    </Card>
  );
}

function CustomerRecommendations({ id }: { id: string }) {
  const { t } = useTranslation();
  const recs = useRecommendations({ customer_id: id, status: "open" });
  const list = recs.data?.data ?? [];
  return (
    <Card className="p-4">
      <h3 className="mb-2 text-sm font-medium text-ink">{t("expansion.openRecommendations")}</h3>
      {recs.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
      {recs.isError ? <ErrorState title={errorText(t, recs.error)} /> : null}
      {recs.isSuccess && list.length === 0 ? <EmptyState icon={Lightbulb} title={t("expansion.noRecommendations")} description={t("expansion.runAnalysis")} /> : null}
      <div className="space-y-3">{list.map((r) => <RecommendationCard key={r.id} r={r} showCustomer={false} />)}</div>
    </Card>
  );
}
