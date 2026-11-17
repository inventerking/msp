import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, SkeletonRows } from "@msp/ui";
import { BarChart3 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { intelApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { formatValue, useDashboard, useIMutation, useInsights } from "../api/intel";
import { useRange } from "../components/Range";
import { AsOf, Flags, KindBadge, Num, Unavailable } from "../components/Value";

/** The overview: one card per area, with the latest month against the month before. */
export function Dashboard() {
  const { t } = useTranslation();
  const dash = useDashboard(useRange());
  if (dash.isPending) return <Card><SkeletonRows label={t("common.loading")} /></Card>;
  if (dash.isError) return <Card className="p-4"><ErrorState title={errorText(t, dash.error)} onRetry={() => void dash.refetch()} retryLabel={t("common.retry")} /></Card>;
  const d = dash.data;
  const empty = d.sections.every((s) => !s.available || s.cards.every((c) => c.value === undefined || c.value === null || c.value === 0));
  return (
    <div className="space-y-4">
      <AsOf asOf={d.as_of} factsAsOf={d.facts_as_of} />
      {empty ? <Card className="p-4 text-sm text-ink-soft"><BarChart3 aria-hidden className="mb-2 h-5 w-5" />{t("intel.emptyDashboard")}</Card> : null}
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {d.sections.map((s) => (
          <Card key={s.key} className="p-4">
            <h2 className="mb-2 text-sm font-medium text-ink">{t(`intel.sections.${s.key}`)}</h2>
            {!s.available ? <Unavailable reason={s.reason} /> : (
              <dl className="grid grid-cols-2 gap-3">
                {s.cards.map((c) => (
                  <div key={c.key}>
                    <dt className="text-xs text-ink-soft">{c.name}</dt>
                    <dd className="text-lg font-semibold text-ink"><Num kind={c.value_kind}>{formatValue(c.unit, c.value, c.currency ?? d.currency)}</Num></dd>
                    <dd className="flex flex-wrap items-center gap-1 text-xs text-ink-faint">
                      <KindBadge kind={c.value_kind} />
                      {c.previous !== undefined && c.previous !== null ? t("intel.previous", { value: formatValue(c.unit, c.previous, c.currency ?? d.currency) }) : null}
                      <Flags flags={c.flags} />
                    </dd>
                  </div>
                ))}
              </dl>
            )}
          </Card>
        ))}
      </div>
      <Commentary scope="dashboard" />
    </div>
  );
}

/** AI commentary on the metrics. It may only quote numbers from metrics it cites. */
export function Commentary({ scope }: { scope: "dashboard" | "revenue" | "funnel" | "campaigns" }) {
  const { t } = useTranslation();
  const insights = useInsights();
  const ask = useIMutation(() => unwrap(intelApi.POST("/sales-intelligence/insights", { body: { scope } })));
  const latest = insights.data?.data.find((i) => i.scope === scope);
  return (
    <Card className="p-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-ink">{t("intel.commentary")}</h2>
        <Button size="sm" variant="secondary" disabled={ask.isPending} onClick={() => ask.mutate(undefined)}>{ask.isPending ? t("intel.writing") : t("intel.writeCommentary")}</Button>
      </div>
      <p className="mb-2 text-xs text-ink-faint">{t("intel.commentaryHelp")}</p>
      {ask.isError ? <ErrorState title={errorText(t, ask.error)} /> : null}
      {latest ? (
        <div>
          <p className="text-sm text-ink">{latest.commentary}</p>
          <p className="mt-1 text-xs text-ink-faint">{t("intel.cites")}: {latest.metrics.filter((m) => latest.cited_metric_ids.includes(m.id)).map((m) => `${m.key} ${m.period} = ${m.display} (${t(`intelKinds.${m.value_kind}`)})`).join("; ")}</p>
          <p className="mt-1 text-xs text-ink-faint">{t("intel.aiWritten", { when: new Date(latest.created_at).toLocaleString() })}</p>
        </div>
      ) : <p className="text-sm text-ink-soft">{t("intel.noCommentary")}</p>}
    </Card>
  );
}
