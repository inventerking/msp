import { Card, Field, Select } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { NoPermission } from "../../../shell/StatusPages";
import { monthsOf, useCan, useMemberNames, useSection } from "../api/intel";
import { useRange } from "../components/Range";
import { BreakdownTable, SectionFrame } from "../components/Tables";

/** Results by team member. It shows individuals' performance, so it needs its own permission and every view is logged. */
export function Team() {
  const { t } = useTranslation();
  const allowed = useCan("sales_intel.team");
  const view = useSection("/sales-intelligence/team", useRange(), allowed);
  const names = useMemberNames(useCan("team.read"));
  const [month, setMonth] = useState("");
  if (!allowed) return <NoPermission />;
  return (
    <SectionFrame query={view}>
      {(v) => {
        const months = monthsOf(v);
        const period = month || months[months.length - 1] || v.to;
        const label = (id: string) => (id === "unassigned" ? t("intel.unassigned") : names.data?.[id] ?? t("intel.formerMember"));
        return (
          <Card className="p-4">
            <p className="mb-3 text-xs text-ink-soft">{t("intel.teamHelp")}</p>
            <div className="mb-2 flex justify-end">
              <Field label={t("intel.month")}>{(ids) => <Select {...ids} value={period} onChange={(e) => setMonth(e.target.value)}>{months.map((m) => <option key={m} value={m}>{m.slice(0, 7)}</option>)}</Select>}</Field>
            </div>
            <BreakdownTable view={v} dimension="owner" metrics={["deals_won", "deals_lost", "win_rate", "won_deal_mrr"]} period={period} label={label} />
          </Card>
        );
      }}
    </SectionFrame>
  );
}
