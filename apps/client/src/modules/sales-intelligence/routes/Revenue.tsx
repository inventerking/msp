import { Card } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { useSection } from "../api/intel";
import { useRange } from "../components/Range";
import { MonthTable, SectionFrame, SeriesChart } from "../components/Tables";
import { Commentary } from "./Dashboard";

/** MRR and ARR from contracts, and how each month's movements add up to the change. */
export function Revenue() {
  const { t } = useTranslation();
  const view = useSection("/sales-intelligence/revenue", useRange());
  return (
    <SectionFrame query={view}>
      {(v) => (
        <>
          <Card className="grid gap-4 p-4 lg:grid-cols-2">
            <SeriesChart view={v} metric="mrr" />
            <SeriesChart view={v} metric="net_new_mrr" />
          </Card>
          <Card className="p-4">
            <h2 className="mb-1 text-sm font-medium text-ink">{t("intel.movements")}</h2>
            <p className="mb-2 text-xs text-ink-soft">{t("intel.movementsHelp")}</p>
            <MonthTable view={v} metrics={["mrr", "arr", "new_mrr", "expansion_mrr", "reactivation_mrr", "contraction_mrr", "churned_mrr", "net_new_mrr", "new_customers", "churned_customers"]} />
          </Card>
          <Commentary scope="revenue" />
        </>
      )}
    </SectionFrame>
  );
}
