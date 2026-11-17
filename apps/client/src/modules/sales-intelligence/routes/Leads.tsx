import { Card, Field, Select } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { monthsOf, useSection } from "../api/intel";
import { useRange } from "../components/Range";
import { BreakdownTable, MonthTable, SectionFrame, SeriesChart } from "../components/Tables";

/** Where leads come from, and how many qualify. */
export function Leads() {
  const { t } = useTranslation();
  const view = useSection("/sales-intelligence/leads", useRange());
  const [month, setMonth] = useState("");
  return (
    <SectionFrame query={view}>
      {(v) => {
        const months = monthsOf(v);
        const period = month || months[months.length - 1] || v.to;
        return (
          <>
            <Card className="p-4"><SeriesChart view={v} metric="leads_created" /></Card>
            <Card className="p-4"><MonthTable view={v} metrics={["leads_created", "leads_qualified", "leads_disqualified", "leads_converted", "qualification_rate"]} /></Card>
            <Card className="p-4">
              <div className="mb-2 flex flex-wrap items-end justify-between gap-2">
                <h2 className="text-sm font-medium text-ink">{t("intel.bySource")}</h2>
                <Field label={t("intel.month")}>{(ids) => <Select {...ids} value={period} onChange={(e) => setMonth(e.target.value)}>{months.map((m) => <option key={m} value={m}>{m.slice(0, 7)}</option>)}</Select>}</Field>
              </div>
              <BreakdownTable view={v} dimension="source" metrics={["leads_created", "leads_qualified", "qualification_rate"]} period={period} label={(s) => t(`intel.sources.${s}`, { defaultValue: s })} />
            </Card>
          </>
        );
      }}
    </SectionFrame>
  );
}
