import { Card } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { monthsOf, useSection } from "../api/intel";
import { useRange } from "../components/Range";
import { BreakdownTable, MonthTable, SectionFrame, SeriesChart } from "../components/Tables";
import { Commentary } from "./Dashboard";

/** Deals through the pipeline, what is open now, and wins against losses. */
export function Funnel() {
  const { t } = useTranslation();
  const range = useRange();
  const funnel = useSection("/sales-intelligence/funnel", range);
  const wins = useSection("/sales-intelligence/win-loss", range);
  const stage = (s: string) => t(`intel.stages.${s}`, { defaultValue: s.replace(/_/g, " ") });
  return (
    <div className="space-y-4">
      <SectionFrame query={funnel}>
        {(v) => {
          const months = monthsOf(v);
          const latest = months[months.length - 1] ?? v.to;
          return (
            <>
              <Card className="p-4">
                <h2 className="mb-1 text-sm font-medium text-ink">{t("intel.pipelineNow")}</h2>
                <p className="mb-2 text-xs text-ink-soft">{t("intel.pipelineHelp")}</p>
                <BreakdownTable view={v} dimension="stage" metrics={["open_deals", "weighted_pipeline"]} period={latest} label={stage} />
              </Card>
              <Card className="p-4">
                <h2 className="mb-2 text-sm font-medium text-ink">{t("intel.stageReach", { month: latest.slice(0, 7) })}</h2>
                <BreakdownTable view={v} dimension="stage" metrics={["deals_reached_stage"]} period={latest} label={stage} />
              </Card>
            </>
          );
        }}
      </SectionFrame>
      <SectionFrame query={wins}>
        {(v) => (
          <>
            <Card className="grid gap-4 p-4 lg:grid-cols-2">
              <SeriesChart view={v} metric="win_rate" />
              <SeriesChart view={v} metric="won_deal_mrr" />
            </Card>
            <Card className="p-4"><MonthTable view={v} metrics={["deals_won", "deals_lost", "win_rate", "won_deal_mrr", "meeting_conversion", "proposals_issued", "proposal_conversion"]} /></Card>
          </>
        )}
      </SectionFrame>
      <Commentary scope="funnel" />
    </div>
  );
}
