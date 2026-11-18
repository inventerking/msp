import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select } from "@msp/ui";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { api } from "../app/api";
import { errorText } from "../app/errors";
import { money } from "./Billing";

function Page({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <main className="mx-auto max-w-3xl p-4 sm:p-8">
      <h1 className="mb-4 text-xl font-semibold text-ink">{title}</h1>
      {children}
    </main>
  );
}

/** The test payment provider's checkout. No money moves: it exists only where fake providers are on. */
export function FakeCheckout() {
  const { t } = useTranslation();
  const { ref = "" } = useParams();
  const info = useQuery({ queryKey: ["fake-checkout", ref], queryFn: () => unwrap(api.GET("/public/fake-checkout/{ref}", { params: { path: { ref } } })) });
  const done = useMutation({
    mutationFn: (outcome: "pay" | "pay_card_declines_later" | "decline") => unwrap(api.POST("/public/fake-checkout/{ref}/complete", { params: { path: { ref } }, body: { outcome } })),
    onSuccess: () => void info.refetch(),
  });
  const c = info.data;
  return (
    <Page title={t("checkout.title")}>
      <p role="note" className="mb-4 rounded-md bg-warn-soft p-3 text-sm text-warn">{t("checkout.testOnly")}</p>
      {info.isError ? <ErrorState title={errorText(t, info.error)} /> : null}
      {c ? (
        <Card className="p-4">
          <p className="text-sm text-ink-soft">{c.description}</p>
          <p className="my-2 text-2xl font-semibold tabular-nums">{money(c.amount_minor, c.currency)}</p>
          {c.status === "open" ? (
            <div className="flex flex-wrap gap-2">
              <Button disabled={done.isPending} onClick={() => done.mutate("pay")}>{t("checkout.pay")}</Button>
              <Button variant="secondary" disabled={done.isPending} onClick={() => done.mutate("pay_card_declines_later")}>{t("checkout.payDeclinesLater")}</Button>
              <Button variant="secondary" disabled={done.isPending} onClick={() => done.mutate("decline")}>{t("checkout.decline")}</Button>
            </div>
          ) : (
            <div role="status" className="space-y-2">
              <Badge tone={c.status === "succeeded" ? "ok" : "danger"}>{t(`checkout.status.${c.status}`)}</Badge>
              {c.return_url ? <p><a className="text-brand underline" href={c.return_url}>{t("checkout.back")}</a></p> : null}
            </div>
          )}
          {done.isError ? <div className="mt-2"><ErrorState title={errorText(t, done.error)} /></div> : null}
        </Card>
      ) : null}
    </Page>
  );
}

/** The current legal documents, as published by the platform operator. Text is shown as text. */
export function LegalDocuments() {
  const { t } = useTranslation();
  const docs = useQuery({ queryKey: ["legal"], queryFn: () => unwrap(api.GET("/legal/documents")) });
  return (
    <Page title={t("legal.title")}>
      {docs.isError ? <ErrorState title={errorText(t, docs.error)} /> : null}
      {docs.data && docs.data.data.length === 0 ? <p className="text-sm text-ink-soft">{t("legal.none")}</p> : null}
      <div className="space-y-4">
        {docs.data?.data.map((d) => (
          <Card key={d.id} className="p-4">
            <h2 id={d.kind} className="text-lg font-medium">{d.title}</h2>
            <p className="mb-2 text-xs text-ink-faint">{t("legal.version", { version: d.version, date: new Date(d.published_at).toLocaleDateString() })}</p>
            <div className="whitespace-pre-wrap text-sm text-ink">{d.body}</div>
          </Card>
        ))}
      </div>
      <p className="mt-6 text-sm"><Link className="text-brand underline" to="/report-abuse">{t("abuse.link")}</Link></p>
    </Page>
  );
}

const categories = ["spam", "phishing", "malware", "privacy", "harassment", "unauthorized_scanning", "other"] as const;

/** Anyone can report abuse of the platform. Reports go to the platform operator, not to the tenant. */
export function ReportAbuse() {
  const { t } = useTranslation();
  const [form, setForm] = useState({ category: "spam" as (typeof categories)[number], details: "", subject_url: "", reporter_email: "" });
  const send = useMutation({
    mutationFn: () => unwrap(api.POST("/public/abuse-reports", { body: {
      category: form.category, details: form.details.trim(),
      ...(form.subject_url.trim() ? { subject_url: form.subject_url.trim() } : {}),
      ...(form.reporter_email.trim() ? { reporter_email: form.reporter_email.trim() } : {}),
    } })),
  });
  return (
    <Page title={t("abuse.title")}>
      <p className="mb-4 text-sm text-ink-soft">{t("abuse.intro")}</p>
      {send.isSuccess ? <p role="status" className="rounded-md bg-ok-soft p-3 text-sm text-ok">{t("abuse.thanks")}</p> : (
        <Card className="p-4">
          <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); send.mutate(); }}>
            <Field label={t("abuse.category")}>{(ids) => <Select {...ids} value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value as (typeof categories)[number] })}>{categories.map((c) => <option key={c} value={c}>{t(`abuse.categories.${c}`)}</option>)}</Select>}</Field>
            <Field label={t("abuse.details")}>{(ids) => <textarea {...ids} className="min-h-32 w-full rounded-md border border-line-strong bg-surface p-2 text-sm" maxLength={5000} value={form.details} onChange={(e) => setForm({ ...form, details: e.target.value })} />}</Field>
            <Field label={t("abuse.url")}>{(ids) => <Input {...ids} inputMode="url" value={form.subject_url} onChange={(e) => setForm({ ...form, subject_url: e.target.value })} />}</Field>
            <Field label={t("abuse.email")}>{(ids) => <Input {...ids} type="email" value={form.reporter_email} onChange={(e) => setForm({ ...form, reporter_email: e.target.value })} />}</Field>
            {send.isError ? <ErrorState title={errorText(t, send.error)} /> : null}
            <Button type="submit" disabled={!form.details.trim() || send.isPending}>{t("abuse.send")}</Button>
          </form>
        </Card>
      )}
    </Page>
  );
}
