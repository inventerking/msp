import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, SkeletonRows, Table } from "@msp/ui";
import { MessageCircle } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { chatbotApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCan, useCMutation, useConversation, useConversations, useHandoffs } from "../api/chatbot";
import { chatPath } from "../paths";

const tone = (s: string) => (s === "awaiting_human" ? "warn" : s === "closed" ? "neutral" : "ok");

/** Conversations, with those waiting for a person first. */
export function Conversations() {
  const { t } = useTranslation();
  const convs = useConversations();
  const handoffs = useHandoffs();
  const list = convs.data?.data ?? [];
  return (
    <div className="space-y-4">
      {(handoffs.data?.data.length ?? 0) > 0 ? (
        <Card className="p-4">
          <h2 className="mb-2 text-sm font-medium text-ink">{t("chat.waiting")}</h2>
          <ul className="space-y-1 text-sm">{handoffs.data!.data.map((h) => <li key={h.id}><Link className="text-brand underline" to={chatPath(`conversations/${h.conversation_id}`)}>{h.bot_name}</Link> · {t(`chat.handoffReason.${h.reason}`, { defaultValue: h.reason })} · {new Date(h.created_at).toLocaleString()}</li>)}</ul>
        </Card>
      ) : null}
      <Card>
        {convs.isSuccess && list.length === 0 ? <EmptyState icon={MessageCircle} title={t("chat.noConversations")} description={t("chat.noConversationsHelp")} /> : (
          <Table>
            <thead><tr><th>{t("chat.bot")}</th><th>{t("chat.status")}</th><th>{t("chat.messages")}</th><th>{t("chat.lastMessage")}</th></tr></thead>
            <tbody>
              {list.map((c) => (
                <tr key={c.id}>
                  <td><Link className="text-brand underline" to={chatPath(`conversations/${c.id}`)}>{c.bot_name}</Link>{c.contact_id ? <Badge tone="ok">{t("chat.lead")}</Badge> : null}</td>
                  <td><Badge tone={tone(c.status)}>{t(`chat.convStatus.${c.status}`)}</Badge></td>
                  <td>{c.messages}</td>
                  <td>{c.last_message_at ? new Date(c.last_message_at).toLocaleString() : ""}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}

/** A transcript. Replying takes over from the bot. */
export function ConversationDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const canHandle = useCan("chatbot.handle");
  const conv = useConversation(id);
  const [text, setText] = useState("");
  const reply = useCMutation(() => unwrap(chatbotApi.POST("/chatbot/conversations/{id}/reply", { params: { path: { id } }, body: { message: text } })));
  const close = useCMutation(() => unwrap(chatbotApi.POST("/chatbot/conversations/{id}/close", { params: { path: { id } } })));
  if (conv.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (conv.isError) return <ErrorState title={errorText(t, conv.error)} />;
  const c = conv.data;
  return (
    <div className="space-y-4">
      <header className="flex items-center gap-2"><h2 className="text-lg font-semibold">{c.bot_name}</h2><Badge tone={tone(c.status)}>{t(`chat.convStatus.${c.status}`)}</Badge></header>
      <Card className="p-4">
        <ol className="space-y-2" aria-label={t("chat.transcript")}>
          {c.transcript.map((m) => (
            <li key={m.id} className={m.role === "visitor" ? "text-right" : ""}>
              <span className="block text-xs text-ink-faint">{t(`chat.roles.${m.role}`)} · {new Date(m.created_at).toLocaleTimeString()} {m.ai_generated ? <Badge tone="warn">{t("chat.aiGenerated")}</Badge> : null} {m.safety_flags.length > 0 ? <Badge tone="danger">{t("chat.flagged")}</Badge> : null}</span>
              <span className="text-sm whitespace-pre-wrap">{m.content}</span>
            </li>
          ))}
        </ol>
      </Card>
      {c.qualification ? (
        <Card className="p-4">
          <h3 className="mb-2 text-sm font-medium">{t("chat.qualification")} <Badge tone="neutral">{t(`chat.qualStatus.${c.qualification.status}`, { defaultValue: c.qualification.status })}</Badge> {c.qualification.score != null ? t("chat.score", { score: c.qualification.score }) : null}</h3>
          <dl className="grid gap-1 text-sm sm:grid-cols-2">{Object.entries(c.qualification.answers).map(([k, v]) => <div key={k}><dt className="text-ink-soft">{k}</dt><dd>{v}</dd></div>)}</dl>
        </Card>
      ) : null}
      {canHandle && c.status !== "closed" ? (
        <Card className="p-4">
          <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); reply.mutate(undefined, { onSuccess: () => setText("") }); }}>
            <label className="block text-sm" htmlFor="agent-reply">{t("chat.yourReply")}</label>
            <textarea id="agent-reply" className="min-h-20 w-full rounded-md border border-line-strong bg-surface p-2 text-sm" value={text} onChange={(e) => setText(e.target.value)} />
            {reply.isError ? <ErrorState title={errorText(t, reply.error)} /> : null}
            <div className="flex gap-2"><Button type="submit" disabled={!text || reply.isPending}>{t("chat.sendReply")}</Button><Button type="button" variant="ghost" onClick={() => close.mutate(undefined)}>{t("chat.close")}</Button></div>
          </form>
        </Card>
      ) : null}
    </div>
  );
}
