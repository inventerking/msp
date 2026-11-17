import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { useCan } from "../api/intel";
import { RangePicker, RangeProvider } from "../components/Range";
import { Tabs } from "../components/Tabs";
import { intelPath } from "../paths";
import { Campaigns } from "./Campaigns";
import { Dashboard } from "./Dashboard";
import { Definitions } from "./Definitions";
import { Forecasts } from "./Forecasts";
import { Funnel } from "./Funnel";
import { Leads } from "./Leads";
import { Reports } from "./Reports";
import { Revenue } from "./Revenue";
import { Team } from "./Team";

/** Sales Intelligence: what happened, labelled actual or estimated, and what may happen, labelled forecast. */
export function Overview() {
  const { t } = useTranslation();
  const canTeam = useCan("sales_intel.team");
  return (
    <RangeProvider>
      <PageHeader title={t("modules.sales_intelligence.name")} description={t("modules.sales_intelligence.description")} actions={<RangePicker />} />
      <Tabs
        items={[
          { to: intelPath(), label: t("intel.tabs.dashboard"), end: true },
          { to: intelPath("revenue"), label: t("intel.tabs.revenue") },
          { to: intelPath("leads"), label: t("intel.tabs.leads") },
          { to: intelPath("campaigns"), label: t("intel.tabs.campaigns") },
          { to: intelPath("funnel"), label: t("intel.tabs.funnel") },
          { to: intelPath("forecasts"), label: t("intel.tabs.forecasts") },
          ...(canTeam ? [{ to: intelPath("team"), label: t("intel.tabs.team") }] : []),
          { to: intelPath("reports"), label: t("intel.tabs.reports") },
          { to: intelPath("definitions"), label: t("intel.tabs.definitions") },
        ]}
      />
      <Routes>
        <Route index element={<Dashboard />} />
        <Route path="revenue" element={<Revenue />} />
        <Route path="leads" element={<Leads />} />
        <Route path="campaigns" element={<Campaigns />} />
        <Route path="funnel" element={<Funnel />} />
        <Route path="forecasts" element={<Forecasts />} />
        <Route path="team" element={<Team />} />
        <Route path="reports" element={<Reports />} />
        <Route path="definitions" element={<Definitions />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </RangeProvider>
  );
}
