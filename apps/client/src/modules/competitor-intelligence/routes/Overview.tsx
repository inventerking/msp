import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { ciPath } from "../paths";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { CompetitorDetail } from "./CompetitorDetail";
import { Competitors } from "./Competitors";
import { Opportunities } from "./Opportunities";
import { ReportView, Reports } from "./Reports";

/** Competitor Intelligence: competitors, their observed facts and changes, opportunities and reports. */
export function Overview() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader title={t("modules.competitor_intelligence.name")} description={t("modules.competitor_intelligence.description")} />
      <Tabs items={[{ to: ciPath(), label: t("competitors.tabs.competitors"), end: true }, { to: ciPath("opportunities"), label: t("competitors.tabs.opportunities") }, { to: ciPath("reports"), label: t("competitors.tabs.reports") }]} />
      <Routes>
        <Route index element={<Competitors />} />
        <Route path="competitors/:id" element={<CompetitorDetail />} />
        <Route path="opportunities" element={<Opportunities />} />
        <Route path="reports" element={<Reports />} />
        <Route path="reports/:id" element={<ReportView />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
