import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { crmPath } from "../paths";
import { Board } from "./Board";
import { Companies, CompanyDetail } from "./Companies";
import { DealDetail } from "./DealDetail";
import { Tasks } from "./Tasks";

/** CRM: the pipeline board, companies and their people, deals, and tasks. */
export function Overview() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader title={t("modules.crm.name")} description={t("modules.crm.description")} />
      <Tabs items={[{ to: crmPath(), label: t("crm.tabs.pipeline"), end: true }, { to: crmPath("companies"), label: t("crm.tabs.companies") }, { to: crmPath("tasks"), label: t("crm.tabs.tasks") }]} />
      <Routes>
        <Route index element={<Board />} />
        <Route path="companies" element={<Companies />} />
        <Route path="companies/:id" element={<CompanyDetail />} />
        <Route path="deals/:id" element={<DealDetail />} />
        <Route path="tasks" element={<Tasks />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
