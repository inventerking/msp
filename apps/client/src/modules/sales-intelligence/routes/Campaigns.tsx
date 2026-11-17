import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { intelApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { formatValue, monthsOf, useCan, useCosts, useIMutation, useSection } from "../api/intel";
import { useRange } from "../components/Range";
import { BreakdownTable, MonthTable, SectionFrame, SeriesChart } from "../components/Tables";
import { Unavailable } from "../components/Value";
import { Commentary } from "./Dashboard";

const categories = ["advertising", "events", "content", "tools", "outreach", "staff", "other"] as const;

/** Outreach results by campaign, and what campaigns cost, for ROI and customer acquisition cost. */
export function Campaigns() {
  const { t } = useTranslation();
  const range = useRange();
  const view = useSection("/sales-intelligence/campaigns", range);
  const costsView = useSection("/sales-intelligence/costs-summary", range);
  return (
    <div className="space-y-4">
      <SectionFrame query={view}>
        {(v) => {
          const months = monthsOf(v);
          const latest = months[months.length - 1] ?? v.to;
          const label = (id: string) => v.labels[id] ?? t("intel.unnamedCampaign");
          return (
            <>
              <Card className="p-4"><SeriesChart view={v} metric="messages_sent" /></Card>
              <Card className="p-4"><MonthTable view={v} metrics={["messages_sent", "messages_bounced", "replies", "unsubscribes", "reply_rate", "bounce_rate"]} /></Card>
              <Card className="p-4">
                <h2 className="mb-2 text-sm font-medium text-ink">{t("intel.byCampaign", { month: latest.slice(0, 7) })}</h2>
                <BreakdownTable view={v} dimension="campaign" metrics={["messages_sent", "replies", "reply_rate", "bounce_rate"]} period={latest} label={label} />
              </Card>
              <Card className="p-4">
                <h2 className="mb-1 text-sm font-medium text-ink">{t("intel.roi")}</h2>
                <p className="mb-2 text-xs text-ink-soft">{t("intel.roiHelp")}</p>
                <BreakdownTable view={v} dimension="campaign" metrics={["campaign_roi"]} period={latest} label={label} />
              </Card>
            </>
          );
        }}
      </SectionFrame>
      <Card className="p-4">
        <h2 className="mb-1 text-sm font-medium text-ink">{t("intel.cac")}</h2>
        {costsView.data ? (costsView.data.available ? <MonthTable view={costsView.data} metrics={["costs", "cac"]} /> : <Unavailable reason={costsView.data.reason} />) : null}
      </Card>
      <Costs />
      <Commentary scope="campaigns" />
    </div>
  );
}

function Costs() {
  const { t } = useTranslation();
  const canCosts = useCan("sales_intel.costs");
  const costs = useCosts(canCosts);
  const [form, setForm] = useState({ period_start: "", period_end: "", category: "advertising", amount: "", currency: "GBP", note: "" });
  const create = useIMutation(() => unwrap(intelApi.POST("/sales-intelligence/costs", { body: {
    period_start: form.period_start, period_end: form.period_end, category: form.category as (typeof categories)[number],
    amount_minor: Math.round(Number(form.amount) * 100), currency: form.currency, ...(form.note ? { note: form.note } : {}),
  } })));
  const remove = useIMutation((id: string) => unwrap(intelApi.DELETE("/sales-intelligence/costs/{id}", { params: { path: { id } } })));
  if (!canCosts) return null;
  const list = costs.data?.data ?? [];
  return (
    <Card className="p-4">
      <h2 className="mb-1 text-sm font-medium text-ink">{t("intel.costs")}</h2>
      <p className="mb-3 text-xs text-ink-soft">{t("intel.costsHelp")}</p>
      <form className="mb-4 grid gap-2 sm:grid-cols-3" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: () => setForm({ ...form, amount: "", note: "" }) }); }}>
        <Field label={t("intel.periodStart")}>{(ids) => <Input {...ids} type="date" value={form.period_start} onChange={(e) => setForm({ ...form, period_start: e.target.value })} />}</Field>
        <Field label={t("intel.periodEnd")}>{(ids) => <Input {...ids} type="date" value={form.period_end} onChange={(e) => setForm({ ...form, period_end: e.target.value })} />}</Field>
        <Field label={t("intel.category")}>{(ids) => <Select {...ids} value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })}>{categories.map((c) => <option key={c} value={c}>{t(`intel.categories.${c}`)}</option>)}</Select>}</Field>
        <Field label={t("intel.amount")}>{(ids) => <Input {...ids} inputMode="decimal" value={form.amount} onChange={(e) => setForm({ ...form, amount: e.target.value })} />}</Field>
        <Field label={t("intel.currency")}>{(ids) => <Input {...ids} maxLength={3} value={form.currency} onChange={(e) => setForm({ ...form, currency: e.target.value.toUpperCase() })} />}</Field>
        <Field label={t("intel.note")}>{(ids) => <Input {...ids} maxLength={500} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />}</Field>
        {create.isError ? <div className="sm:col-span-3"><ErrorState title={errorText(t, create.error)} /></div> : null}
        <div><Button type="submit" disabled={create.isPending || !form.period_start || !form.period_end || !(Number(form.amount) >= 0) || form.amount === "" || form.currency.length !== 3}>{t("intel.addCost")}</Button></div>
      </form>
      {list.length ? (
        <Table>
          <thead><tr><th>{t("intel.period")}</th><th>{t("intel.category")}</th><th className="text-right">{t("intel.amount")}</th><th>{t("intel.note")}</th><th><span className="sr-only">{t("intel.actions")}</span></th></tr></thead>
          <tbody>
            {list.map((c) => (
              <tr key={c.id}>
                <td>{c.period_start} – {c.period_end}</td>
                <td>{t(`intel.categories.${c.category}`, { defaultValue: c.category })}</td>
                <td className="text-right tabular-nums">{formatValue("money", c.amount_minor, c.currency)}</td>
                <td className="text-ink-soft">{c.note ?? ""}</td>
                <td className="text-right"><Button size="sm" variant="secondary" disabled={remove.isPending} onClick={() => remove.mutate(c.id)}>{t("intel.delete")}</Button></td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : <p className="text-sm text-ink-soft">{t("intel.noCosts")}</p>}
    </Card>
  );
}
