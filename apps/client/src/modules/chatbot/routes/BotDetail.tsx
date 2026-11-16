import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, SkeletonRows } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router";
import { chatbotApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useBot, useCan, useCMutation, useKnowledge, useQuestions } from "../api/chatbot";

const area = "min-h-28 w-full rounded-md border border-line-strong bg-surface p-2 text-sm";
type Q = { field_key: string; question: string; weight: number; required: boolean };

/** One bot: where it may appear, what it knows, what it asks, and how to install it. */
export function BotDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const canConfigure = useCan("chatbot.configure");
  const bot = useBot(id);
  const knowledge = useKnowledge(id);
  const questions = useQuestions(id);
  const [settings, setSettings] = useState<{ origins: string; disclosure: string; business: string; greeting: string }>();
  const [doc, setDoc] = useState({ kind: "faq" as "faq" | "document" | "service_catalog", title: "", content: "" });
  const [qs, setQs] = useState<Q[] | null>(null);
  const save = useCMutation((body: { allowed_origins?: string[]; disclosure_text?: string; persona?: { business_name?: string; greeting?: string }; status?: "draft" | "active" | "paused" }) => unwrap(chatbotApi.PATCH("/chatbot/bots/{id}", { params: { path: { id } }, body })));
  const addDoc = useCMutation(() => unwrap(chatbotApi.POST("/chatbot/bots/{id}/knowledge", { params: { path: { id } }, body: doc })));
  const removeDoc = useCMutation((kid: string) => unwrap(chatbotApi.DELETE("/chatbot/knowledge/{id}", { params: { path: { id: kid } } })));
  const saveQs = useCMutation(() => unwrap(chatbotApi.PUT("/chatbot/bots/{id}/questions", { params: { path: { id } }, body: { questions: qs ?? [] } })));
  if (bot.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (bot.isError) return <ErrorState title={errorText(t, bot.error)} />;
  const b = bot.data;
  const form = settings ?? { origins: b.allowed_origins.join(", "), disclosure: b.disclosure_text, business: b.persona.business_name ?? "", greeting: b.persona.greeting ?? "" };
  const list = qs ?? (questions.data?.data ?? []);
  const failure = [save, addDoc, removeDoc, saveQs].find((m) => m.isError)?.error;
  const snippet = `<script src="${window.location.origin}/chat.js" data-bot="${b.public_key}" defer></script>`;
  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold text-ink">{b.name}</h2>
        <Badge tone={b.status === "active" ? "ok" : "neutral"}>{t(`chat.botStatus.${b.status}`)}</Badge>
        {b.stats ? <span className="text-xs text-ink-soft">{t("chat.stats", { conversations: b.stats.conversations, leads: b.stats.leads, handoffs: b.stats.handoffs })}</span> : null}
        {canConfigure ? <Button size="sm" variant={b.status === "active" ? "secondary" : "primary"} onClick={() => save.mutate({ status: b.status === "active" ? "paused" : "active" })}>{b.status === "active" ? t("chat.pause") : t("chat.activate")}</Button> : null}
      </header>
      {failure ? <ErrorState title={errorText(t, failure)} /> : null}
      <Card className="p-4">
        <h3 className="mb-2 text-sm font-medium text-ink">{t("chat.settings")}</h3>
        <div className="grid gap-2 sm:grid-cols-2">
          <Field label={t("chat.allowedOrigins")}>{(ids) => <Input {...ids} disabled={!canConfigure} value={form.origins} onChange={(e) => setSettings({ ...form, origins: e.target.value })} />}</Field>
          <Field label={t("chat.businessName")}>{(ids) => <Input {...ids} disabled={!canConfigure} value={form.business} onChange={(e) => setSettings({ ...form, business: e.target.value })} />}</Field>
          <Field label={t("chat.greeting")}>{(ids) => <Input {...ids} disabled={!canConfigure} value={form.greeting} onChange={(e) => setSettings({ ...form, greeting: e.target.value })} />}</Field>
          <Field label={t("chat.disclosure")}>{(ids) => <Input {...ids} disabled={!canConfigure} value={form.disclosure} onChange={(e) => setSettings({ ...form, disclosure: e.target.value })} />}</Field>
        </div>
        <p className="mt-1 text-xs text-ink-soft">{t("chat.disclosureHelp")}</p>
        {canConfigure ? <Button className="mt-2" disabled={!settings || save.isPending} onClick={() => save.mutate({ allowed_origins: form.origins.split(",").map((o) => o.trim()).filter(Boolean), disclosure_text: form.disclosure, persona: { business_name: form.business, greeting: form.greeting } }, { onSuccess: () => setSettings(undefined) })}>{t("common.save")}</Button> : null}
      </Card>
      <Card className="p-4">
        <h3 className="mb-1 text-sm font-medium text-ink">{t("chat.install")}</h3>
        <p className="mb-2 text-xs text-ink-soft">{t("chat.installHelp")}</p>
        <pre className="overflow-x-auto rounded-md bg-sunken p-2 font-mono text-xs" data-testid="bot-snippet">{snippet}</pre>
      </Card>
      <Card className="p-4">
        <h3 className="mb-1 text-sm font-medium text-ink">{t("chat.knowledge")}</h3>
        <p className="mb-2 text-xs text-ink-soft">{t("chat.knowledgeHelp")}</p>
        <ul className="mb-3 space-y-1 text-sm">
          {(knowledge.data?.data ?? []).map((k) => <li key={k.id} className="flex items-center justify-between">{k.title} <span className="text-xs text-ink-faint">{t(`chat.kinds.${k.kind}`)} · {t("chat.chunks", { count: k.chunks })}</span>{canConfigure ? <Button size="sm" variant="ghost" onClick={() => removeDoc.mutate(k.id)}>{t("common.remove")}</Button> : null}</li>)}
        </ul>
        {canConfigure ? (
          <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); addDoc.mutate(undefined, { onSuccess: () => setDoc({ ...doc, title: "", content: "" }) }); }}>
            <div className="grid gap-2 sm:grid-cols-2">
              <Field label={t("chat.knowledgeTitle")}>{(ids) => <Input {...ids} value={doc.title} onChange={(e) => setDoc({ ...doc, title: e.target.value })} />}</Field>
              <Field label={t("chat.kind")}>{(ids) => <Select {...ids} value={doc.kind} onChange={(e) => setDoc({ ...doc, kind: e.target.value as typeof doc.kind })}>{(["faq", "document", "service_catalog"] as const).map((k) => <option key={k} value={k}>{t(`chat.kinds.${k}`)}</option>)}</Select>}</Field>
            </div>
            <label className="block text-sm text-ink" htmlFor="kb-content">{t("chat.content")}</label>
            <textarea id="kb-content" className={area} value={doc.content} onChange={(e) => setDoc({ ...doc, content: e.target.value })} />
            <Button type="submit" disabled={!doc.title || !doc.content || addDoc.isPending}>{t("chat.addKnowledge")}</Button>
          </form>
        ) : null}
      </Card>
      <Card className="p-4">
        <h3 className="mb-1 text-sm font-medium text-ink">{t("chat.questions")}</h3>
        <p className="mb-2 text-xs text-ink-soft">{t("chat.questionsHelp")}</p>
        {list.map((q, i) => (
          <div key={i} className="mb-2 grid gap-2 sm:grid-cols-4 sm:items-end">
            <Field label={t("chat.fieldKey")}>{(ids) => <Input {...ids} disabled={!canConfigure} value={q.field_key} onChange={(e) => setQs(list.map((x, j) => (j === i ? { ...x, field_key: e.target.value } : x)))} />}</Field>
            <Field label={t("chat.question")}>{(ids) => <Input {...ids} disabled={!canConfigure} value={q.question} onChange={(e) => setQs(list.map((x, j) => (j === i ? { ...x, question: e.target.value } : x)))} />}</Field>
            <Field label={t("chat.weight")}>{(ids) => <Input {...ids} type="number" min={0} max={100} disabled={!canConfigure} value={q.weight} onChange={(e) => setQs(list.map((x, j) => (j === i ? { ...x, weight: Number(e.target.value) } : x)))} />}</Field>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" disabled={!canConfigure} checked={q.required} onChange={(e) => setQs(list.map((x, j) => (j === i ? { ...x, required: e.target.checked } : x)))} />{t("chat.required")}</label>
          </div>
        ))}
        {canConfigure ? (
          <div className="flex gap-2">
            <Button variant="secondary" disabled={list.length >= 10} onClick={() => setQs([...list, { field_key: "", question: "", weight: 10, required: false }])}>{t("chat.addQuestion")}</Button>
            <Button disabled={!qs || saveQs.isPending} onClick={() => saveQs.mutate(undefined, { onSuccess: () => setQs(null) })}>{t("common.save")}</Button>
          </div>
        ) : null}
      </Card>
    </div>
  );
}
