import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { outreachPath } from "../paths";
import { Audiences } from "./Audiences";
import { CampaignDetail } from "./CampaignDetail";
import { Campaigns } from "./Campaigns";
import { Inbox } from "./Inbox";
import { Sending } from "./Sending";
import { Suppressions } from "./Suppressions";
import { Templates } from "./Templates";

/** Outreach: email sequences that send only after approval, and only when every send check passes. */
export function Overview() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader title={t("modules.outreach.name")} description={t("modules.outreach.description")} />
      <Tabs
        items={[
          { to: outreachPath(), label: t("outreach.tabs.campaigns"), end: true },
          { to: outreachPath("templates"), label: t("outreach.tabs.templates") },
          { to: outreachPath("audiences"), label: t("outreach.tabs.audiences") },
          { to: outreachPath("inbox"), label: t("outreach.tabs.inbox") },
          { to: outreachPath("suppressions"), label: t("outreach.tabs.suppressions") },
          { to: outreachPath("sending"), label: t("outreach.tabs.sending") },
        ]}
      />
      <Routes>
        <Route index element={<Campaigns />} />
        <Route path="templates" element={<Templates />} />
        <Route path="audiences" element={<Audiences />} />
        <Route path="inbox" element={<Inbox />} />
        <Route path="suppressions" element={<Suppressions />} />
        <Route path="sending" element={<Sending />} />
        <Route path="campaigns/:id" element={<CampaignDetail />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
