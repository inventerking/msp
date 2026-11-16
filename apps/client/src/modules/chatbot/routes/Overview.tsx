import { PageHeader } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { Route, Routes } from "react-router";
import { NotFound } from "../../../shell/StatusPages";
import { Tabs } from "../components/Tabs";
import { chatPath } from "../paths";
import { BotDetail } from "./BotDetail";
import { Bots } from "./Bots";
import { ConversationDetail, Conversations } from "./Conversations";

/** Chatbot: website chat answered from the workspace's own knowledge, with a person a click away. */
export function Overview() {
  const { t } = useTranslation();
  return (
    <>
      <PageHeader title={t("modules.chatbot.name")} description={t("modules.chatbot.description")} />
      <Tabs items={[{ to: chatPath(), label: t("chat.tabs.bots"), end: true }, { to: chatPath("conversations"), label: t("chat.tabs.conversations") }]} />
      <Routes>
        <Route index element={<Bots />} />
        <Route path="bots/:id" element={<BotDetail />} />
        <Route path="conversations" element={<Conversations />} />
        <Route path="conversations/:id" element={<ConversationDetail />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </>
  );
}
