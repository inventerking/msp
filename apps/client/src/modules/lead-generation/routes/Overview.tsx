import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { leadPath } from "../paths";
import { Route, Routes } from "react-router";
import { useMe } from "../../../app/api";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { DataTransfer } from "./DataTransfer";
import { Icp } from "./Icp";
import { Lists } from "./Lists";
import { ProspectDetail } from "./ProspectDetail";
import { Prospects } from "./Prospects";
import { Searches } from "./Searches";

/** Lead Generation: prospects, searches, lists, import and export, and the ideal customer profile. */
export function Overview() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  const tabs = [
    { to: leadPath(), label: t("leads.tabs.prospects"), end: true },
    ...(can("lead.search") ? [{ to: leadPath("search"), label: t("leads.tabs.search") }] : []),
    { to: leadPath("lists"), label: t("leads.tabs.lists") },
    ...(can("lead.import") || can("lead.export") ? [{ to: leadPath("data"), label: t("leads.tabs.data") }] : []),
    { to: leadPath("icp"), label: t("leads.tabs.icp") },
  ];
  return (
    <>
      <PageHeader title={t("modules.lead_generation.name")} description={t("modules.lead_generation.description")} />
      <Tabs items={tabs} />
      <Routes>
        <Route index element={<Prospects />} />
        <Route path="prospects/:id" element={<ProspectDetail />} />
        <Route path="search" element={<Searches />} />
        <Route path="lists" element={<Lists />} />
        <Route path="data" element={<DataTransfer />} />
        <Route path="icp" element={<Icp />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
