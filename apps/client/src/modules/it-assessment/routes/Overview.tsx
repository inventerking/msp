import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { assessPath } from "../paths";
import { AssessmentDetail, Assessments } from "./Assessments";
import { Authorizations } from "./Authorizations";

/** IT Assessment: authorized, non-invasive checks of a domain's public email and web set-up. */
export function Overview() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader title={t("modules.it_assessment.name")} description={t("modules.it_assessment.description")} />
      <Tabs items={[{ to: assessPath(), label: t("assess.tabs.assessments"), end: true }, { to: assessPath("authorizations"), label: t("assess.tabs.authorizations") }]} />
      <Routes>
        <Route index element={<Assessments />} />
        <Route path="authorizations" element={<Authorizations />} />
        <Route path=":id" element={<AssessmentDetail />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
