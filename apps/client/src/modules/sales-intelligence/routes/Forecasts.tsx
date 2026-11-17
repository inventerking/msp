import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Select, SkeletonRows } from "@msp/ui";
import { LineChart } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { intelApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { type Forecast, formatValue, useCan, useForecasts, useIMutation } from "../api/intel";
import { BarChart } from "../components/BarChart";
import { KindBadge } from "../components/Value";

/** Forecasts with their ranges and assumptions. They are always labelled as forecasts. */
export function Forecasts() {
  const { t } = useTranslation();
  const canRun = useCan("sales_intel.forecast");
  const forecasts = useForecasts();
  const [horizon, setHorizon] = useState(6);
  const run = useIMutation((kind: "revenue" | "pipeline") => unwrap(intelApi.POST("/sales-intelligence/forecasts", { body: { kind, horizon_months: horizon } })));
  const list = forecasts.data?.data ?? [];
  const latest = (kind: string) => list.find((f) => f.kind === kind && (f.status === "completed" || f.status === "queued" || f.status === "running")) ?? list.find((f) => f.kind === kind);
  return (
    <div className="space-y-4">
      {canRun ? (
        <Card className="flex flex-wrap items-end gap-2 p-4">
          <Field label={t("intel.horizon")}>{(ids) => <Select {...ids} value={horizon} onChange={(e) => setHorizon(Number(e.target.value))}>{[3, 6, 12, 24].map((h) => <option key={h} value={h}>{t("intel.months", { count: h })}</option>)}</Select>}</Field>
          <Button disabled={run.isPending} onClick={() => run.mutate("revenue")}>{t("intel.runRevenue")}</Button>
          <Button variant="secondary" disabled={run.isPending} onClick={() => run.mutate("pipeline")}>{t("intel.runPipeline")}</Button>
        </Card>
      ) : null}
      {run.isError ? <ErrorState title={errorText(t, run.error)} /> : null}
      {forecasts.isPending ? <Card><SkeletonRows label={t("common.loading")} /></Card> : null}
      {forecasts.isError ? <ErrorState title={errorText(t, forecasts.error)} onRetry={() => void forecasts.refetch()} retryLabel={t("common.retry")} /> : null}
      {forecasts.isSuccess && list.length === 0 ? <Card><EmptyState icon={LineChart} title={t("intel.noForecasts")} description={t("intel.noForecastsHelp")} /></Card> : null}
      {(["revenue", "pipeline"] as const).map((k) => {
        const f = latest(k);
        return f ? <ForecastCard key={k} f={f} previous={list.find((x) => x.kind === k && x.previous)} /> : null;
      })}
    </div>
  );
}

function ForecastCard({ f, previous }: { f: Forecast; previous?: Forecast }) {
  const { t } = useTranslation();
  const currency = f.currency;
  return (
    <Card className="p-4">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-medium text-ink">{t(`intel.forecastKinds.${f.kind}`)}</h2>
        <KindBadge kind="forecast" />
        <Badge tone={f.status === "completed" ? "ok" : f.status === "failed" ? "danger" : "neutral"}>{t(`intel.forecastStatus.${f.status}`)}</Badge>
        <span className="text-xs text-ink-faint">{t("intel.forecastAsOf", { date: f.as_of, method: f.method_version })}</span>
      </div>
      {f.status === "failed" ? (
        <>
          <p className="text-sm text-ink-soft">{t(`intel.forecastErrors.${f.error_code ?? "forecast_failed"}`, { defaultValue: t("intel.forecastErrors.forecast_failed") })}</p>
          {previous?.status === "completed" ? <p className="mt-1 text-xs text-ink-faint">{t("intel.previousShown")}</p> : null}
        </>
      ) : null}
      {f.status === "completed" ? (
        <>
          <BarChart title={t(`intel.forecastKinds.${f.kind}`)} unit="money" currency={currency} bars={f.points.map((p) => ({ period: p.period, value: p.expected, low: p.low, high: p.high, kind: "forecast" }))} />
          <ul className="mt-2 grid gap-1 text-sm sm:grid-cols-3">
            {f.points.map((p) => <li key={p.period}><span className="text-ink-soft">{p.period.slice(0, 7)}</span>: <span className="italic">{formatValue("money", p.expected, currency)}</span> <span className="text-xs text-ink-faint">({formatValue("money", p.low, currency)} – {formatValue("money", p.high, currency)})</span></li>)}
          </ul>
          <details className="mt-3 text-sm">
            <summary className="cursor-pointer text-ink-soft">{t("intel.assumptions")}</summary>
            <pre className="mt-2 overflow-x-auto rounded-md bg-sunken p-3 text-xs">{JSON.stringify(f.assumptions, null, 2)}</pre>
          </details>
        </>
      ) : null}
    </Card>
  );
}
