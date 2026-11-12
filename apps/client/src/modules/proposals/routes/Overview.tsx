import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { useMe } from "../../../app/api";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { proposalPath } from "../paths";
import { Catalog } from "./Catalog";
import { ProposalDetail } from "./ProposalDetail";
import { Proposals } from "./Proposals";

/** Proposals: offers with prices calculated on the server, approval, sending and signature. */
export function Overview() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const tabs = [{ to: proposalPath(), label: t("proposals.tabs.proposals"), end: true }, ...(me?.permissions.includes("proposal.configure") ? [{ to: proposalPath("catalog"), label: t("proposals.tabs.catalog") }] : [])];
  return (
    <>
      <PageHeader title={t("modules.proposals.name")} description={t("modules.proposals.description")} />
      <Tabs items={tabs} />
      <Routes>
        <Route index element={<Proposals />} />
        <Route path="catalog" element={<Catalog />} />
        <Route path=":id" element={<ProposalDetail />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
