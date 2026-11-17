import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { expansionPath } from "../paths";
import { CustomerDetail } from "./CustomerDetail";
import { Customers } from "./Customers";
import { Opportunities } from "./Opportunities";
import { Recommendations } from "./Recommendations";
import { Renewals } from "./Renewals";

/** Customer Expansion: contracts, coverage and renewals for existing customers, with suggestions backed by evidence. */
export function Overview() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader title={t("modules.customer_expansion.name")} description={t("modules.customer_expansion.description")} />
      <Tabs
        items={[
          { to: expansionPath(), label: t("expansion.tabs.customers"), end: true },
          { to: expansionPath("recommendations"), label: t("expansion.tabs.recommendations") },
          { to: expansionPath("renewals"), label: t("expansion.tabs.renewals") },
          { to: expansionPath("opportunities"), label: t("expansion.tabs.opportunities") },
        ]}
      />
      <Routes>
        <Route index element={<Customers />} />
        <Route path="customers/:id" element={<CustomerDetail />} />
        <Route path="recommendations" element={<Recommendations />} />
        <Route path="renewals" element={<Renewals />} />
        <Route path="opportunities" element={<Opportunities />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
