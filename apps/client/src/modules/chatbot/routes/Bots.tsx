import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Table } from "@msp/ui";
import { MessageCircle } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { chatbotApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useBots, useCan, useCMutation } from "../api/chatbot";
import { chatPath } from "../paths";

/** Bots. A new bot is a draft; it answers no one until it is activated for named websites. */
export function Bots() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canConfigure = useCan("chatbot.configure");
  const bots = useBots();
  const [name, setName] = useState("");
  const create = useCMutation(() => unwrap(chatbotApi.POST("/chatbot/bots", { body: { name } })));
  const list = bots.data?.data ?? [];
  return (
    <div className="space-y-4">
      <p role="note" className="rounded-md bg-sunken p-3 text-sm text-ink-soft">{t("chat.fakeNotice")}</p>
      {canConfigure ? (
        <Card className="p-4">
          <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: (b) => navigate(chatPath(`bots/${b.id}`)) }); }}>
            <div className="flex-1"><Field label={t("chat.botName")}>{(ids) => <Input {...ids} value={name} onChange={(e) => setName(e.target.value)} />}</Field></div>
            <Button type="submit" disabled={!name || create.isPending}>{t("chat.createBot")}</Button>
          </form>
          {create.isError ? <ErrorState title={errorText(t, create.error)} /> : null}
        </Card>
      ) : null}
      <Card>
        {bots.isSuccess && list.length === 0 ? <EmptyState icon={MessageCircle} title={t("chat.noBots")} description={t("chat.noBotsHelp")} /> : (
          <Table>
            <thead><tr><th>{t("chat.botName")}</th><th>{t("chat.status")}</th><th>{t("chat.websites")}</th></tr></thead>
            <tbody>
              {list.map((b) => (
                <tr key={b.id}>
                  <td><Link className="text-brand underline" to={chatPath(`bots/${b.id}`)}>{b.name}</Link></td>
                  <td><Badge tone={b.status === "active" ? "ok" : "neutral"}>{t(`chat.botStatus.${b.status}`)}</Badge></td>
                  <td className="text-xs">{b.allowed_origins.join(", ")}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
