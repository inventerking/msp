import { Card, ErrorState, SkeletonRows, Table } from "@msp/ui";
import type { UseQueryResult } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { errorText } from "../../../app/errors";
import { formatValue, monthLabel, monthsOf, type Series, seriesOf, valueAt, type View } from "../api/intel";
import { BarChart } from "./BarChart";
import { AsOf, Flags, KindBadge, Num, Unavailable } from "./Value";

/** Loading, error, unavailable and as-of handling shared by every analytics screen. */
export function SectionFrame({ query, children }: { query: UseQueryResult<View>; children: (v: View) => ReactNode }) {
  const { t } = useTranslation();
  if (query.isPending) return <Card><SkeletonRows label={t("common.loading")} /></Card>;
  if (query.isError) return <Card className="p-4"><ErrorState title={errorText(t, query.error)} onRetry={() => void query.refetch()} retryLabel={t("common.retry")} /></Card>;
  const v = query.data;
  return (
    <div className="space-y-4">
      <AsOf asOf={v.as_of} factsAsOf={v.facts_as_of} />
      {v.available ? children(v) : <Card className="p-4"><Unavailable reason={v.reason} /></Card>}
    </div>
  );
}

/** A chart of one metric over the months shown. */
export function SeriesChart({ view, metric, dimension = "all", dimValue }: { view: View; metric: string; dimension?: string; dimValue?: string }) {
  const s = seriesOf(view, metric, dimension);
  if (!s) return null;
  const bars = monthsOf(view).map((m) => ({ period: m, value: valueAt(s, m, dimValue)?.value ?? null, kind: s.value_kind }));
  return <BarChart title={s.name} bars={bars} unit={s.unit} currency={s.currency ?? view.currency} />;
}

/** Metrics as rows and months as columns. Each row has one kind, shown at its start. */
export function MonthTable({ view, metrics }: { view: View; metrics: string[] }) {
  const { t } = useTranslation();
  const months = monthsOf(view);
  const rows = metrics.map((k) => seriesOf(view, k)).filter((s): s is Series => Boolean(s));
  if (!rows.length) return <p className="text-sm text-ink-soft">{t("intel.noData")}</p>;
  return (
    <div className="overflow-x-auto">
      <Table>
        <thead><tr><th>{t("intel.metric")}</th>{months.map((m) => <th key={m} className="text-right">{monthLabel(m)}</th>)}</tr></thead>
        <tbody>
          {rows.map((s) => (
            <tr key={s.key}>
              <th scope="row" className="text-left font-normal">{s.name} <KindBadge kind={s.value_kind} /></th>
              {months.map((m) => {
                const p = valueAt(s, m);
                return <td key={m} className="text-right"><Num kind={s.value_kind}>{formatValue(s.unit, p?.value, s.currency ?? view.currency)}</Num><Flags flags={p?.flags} /></td>;
              })}
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}

/** Dimension values as rows, for one month, with one column per metric. */
export function BreakdownTable({ view, dimension, metrics, period, label }: { view: View; dimension: string; metrics: string[]; period: string; label: (v: string) => string }) {
  const { t } = useTranslation();
  const series = metrics.map((k) => seriesOf(view, k, dimension)).filter((s): s is Series => Boolean(s));
  const values = [...new Set(series.flatMap((s) => s.points.filter((p) => p.period === period).map((p) => p.dimension_value ?? "")))].sort();
  if (!values.length) return <p className="text-sm text-ink-soft">{t("intel.noData")}</p>;
  return (
    <div className="overflow-x-auto">
      <Table>
        <thead><tr><th>{t(`intel.dimensions.${dimension}`)}</th>{series.map((s) => <th key={s.key} className="text-right">{s.name} <KindBadge kind={s.value_kind} /></th>)}</tr></thead>
        <tbody>
          {values.map((v) => (
            <tr key={v}>
              <td>{label(v)}</td>
              {series.map((s) => {
                const p = valueAt(s, period, v);
                return <td key={s.key} className="text-right"><Num kind={s.value_kind}>{formatValue(s.unit, p?.value, s.currency ?? view.currency)}</Num><Flags flags={p?.flags} /></td>;
              })}
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
}
