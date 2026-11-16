import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { SiteDetail } from "./SiteDetail";
import { Sites } from "./Sites";

/** Visitor Intelligence: visits, conversions and identified companies, within each site's consent mode. */
export function Overview() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader title={t("modules.visitor_intelligence.name")} description={t("modules.visitor_intelligence.description")} />
      <Routes>
        <Route index element={<Sites />} />
        <Route path="sites/:id" element={<SiteDetail />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
