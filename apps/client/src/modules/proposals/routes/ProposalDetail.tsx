import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { Sparkles } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { proposalApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { proposalPath } from "../paths";
import { type LineInput, type Version, fromMinor, money, toMinor, useActivity, useCatalog, usePropMutation, useProposal, useVersion } from "../api/proposals";

type EditLine = { name: string; pricing_model: LineInput["pricing_model"]; quantity: string; unit: string; setup: string; discount: string; catalog_item_id?: string };
type Section = { heading: string; body: string };

function toEdit(v: Version): { sections: Section[]; lines: EditLine[]; term: string; currency: string; valid: string } {
  return {
    sections: v.sections.length ? v.sections : [{ heading: "", body: "" }],
    lines: v.lines.map((l) => ({ name: l.name, pricing_model: l.pricing_model as LineInput["pricing_model"], quantity: String(l.quantity), unit: fromMinor(l.unit_price_minor, v.currency), setup: fromMinor(l.setup_fee_minor, v.currency), discount: String(l.discount_bps / 100), ...(l.catalog_item_id ? { catalog_item_id: l.catalog_item_id } : {}) })),
    term: String(v.term_months), currency: v.currency, valid: v.valid_until ?? "",
  };
}

/** The draft editor. Totals shown come from the server's calculation, never from this page. */
function Editor({ id, v }: { id: string; v: Version }) {
  const { t } = useTranslation();
  const catalog = useCatalog();
  const [form, setForm] = useState(() => toEdit(v));
  const [suggestion, setSuggestion] = useState<{ index: number; heading: string; body: string }>();
  useEffect(() => setForm(toEdit(v)), [v]);
  const body = () => ({
    sections: form.sections.filter((s) => s.heading.trim() || s.body.trim()), term_months: Number(form.term) || 12, currency: form.currency, ...(form.valid ? { valid_until: form.valid } : {}),
    lines: form.lines.map((l) => ({ name: l.name, pricing_model: l.pricing_model, quantity: Number(l.quantity) || 1, unit_price_minor: toMinor(l.unit, form.currency), setup_fee_minor: toMinor(l.setup, form.currency), discount_bps: Math.round((Number(l.discount) || 0) * 100), ...(l.catalog_item_id ? { catalog_item_id: l.catalog_item_id } : {}) })),
  });
  const save = usePropMutation(() => unwrap(proposalApi.PATCH("/proposals/proposals/{id}/versions/{version}", { params: { path: { id, version: v.version } }, body: body() })));
  const draft = usePropMutation((index: number) => unwrap(proposalApi.POST("/proposals/proposals/{id}/versions/{version}/ai-draft", { params: { path: { id, version: v.version } }, body: { section: form.sections[index]?.heading || "Overview" } })));
  const setLine = (i: number, patch: Partial<EditLine>) => setForm({ ...form, lines: form.lines.map((l, j) => (j === i ? { ...l, ...patch } : l)) });
  return (
    <Card className="p-4">
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); save.mutate(undefined); }}>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label={t("proposals.currency")}>{(ids) => <Input {...ids} maxLength={3} value={form.currency} onChange={(e) => setForm({ ...form, currency: e.target.value.toUpperCase() })} />}</Field>
          <Field label={t("proposals.term")}>{(ids) => <Input {...ids} type="number" min={1} max={120} value={form.term} onChange={(e) => setForm({ ...form, term: e.target.value })} />}</Field>
          <Field label={t("proposals.validUntil")}>{(ids) => <Input {...ids} type="date" value={form.valid} onChange={(e) => setForm({ ...form, valid: e.target.value })} />}</Field>
        </div>
        <section>
          <h3 className="mb-2 text-sm font-medium text-ink">{t("proposals.text")}</h3>
          {form.sections.map((s, i) => (
            <div key={i} className="mb-3 space-y-2 rounded-md border border-line p-3">
              <Field label={t("proposals.heading")}>{(ids) => <Input {...ids} value={s.heading} onChange={(e) => setForm({ ...form, sections: form.sections.map((x, j) => (j === i ? { ...x, heading: e.target.value } : x)) })} />}</Field>
              <label className="block text-sm text-ink" htmlFor={`body-${i}`}>{t("proposals.body")}</label>
              <textarea id={`body-${i}`} className="min-h-24 w-full rounded-md border border-line-strong bg-surface p-2 text-sm" value={s.body} onChange={(e) => setForm({ ...form, sections: form.sections.map((x, j) => (j === i ? { ...x, body: e.target.value } : x)) })} />
              <Button size="sm" variant="ghost" disabled={draft.isPending} onClick={() => draft.mutate(i, { onSuccess: (d) => setSuggestion({ index: i, heading: d.heading, body: d.body }) })}><Sparkles aria-hidden />{t("proposals.aiDraft")}</Button>
              {suggestion?.index === i ? (
                <div className="rounded-md bg-sunken p-2 text-sm">
                  <Badge tone="warn">{t("proposals.aiGenerated")}</Badge>
                  <p className="mt-1 whitespace-pre-wrap">{suggestion.body}</p>
                  <p className="mt-1 text-xs text-ink-faint">{t("proposals.aiNeedsApproval")}</p>
                  <Button size="sm" variant="secondary" className="mt-2" onClick={() => { setForm({ ...form, sections: form.sections.map((x, j) => (j === i ? { ...x, body: suggestion.body } : x)) }); setSuggestion(undefined); }}>{t("proposals.useText")}</Button>
                </div>
              ) : null}
            </div>
          ))}
          {draft.isError ? <ErrorState title={errorText(t, draft.error)} /> : null}
          <Button size="sm" variant="secondary" onClick={() => setForm({ ...form, sections: [...form.sections, { heading: "", body: "" }] })}>{t("proposals.addSection")}</Button>
        </section>
        <section>
          <h3 className="mb-2 text-sm font-medium text-ink">{t("proposals.pricing")}</h3>
          <Table>
            <thead><tr><th>{t("proposals.service")}</th><th>{t("proposals.model")}</th><th>{t("proposals.quantity")}</th><th>{t("proposals.unitPrice")}</th><th>{t("proposals.setupFee")}</th><th>{t("proposals.discount")}</th><th /></tr></thead>
            <tbody>
              {form.lines.map((l, i) => (
                <tr key={i}>
                  <td><Input aria-label={t("proposals.service")} value={l.name} onChange={(e) => setLine(i, { name: e.target.value })} /></td>
                  <td><Select aria-label={t("proposals.model")} value={l.pricing_model} onChange={(e) => setLine(i, { pricing_model: e.target.value as EditLine["pricing_model"] })}>{(["per_user", "per_device", "fixed"] as const).map((m) => <option key={m} value={m}>{t(`proposals.models.${m}`)}</option>)}</Select></td>
                  <td><Input aria-label={t("proposals.quantity")} type="number" min={1} value={l.quantity} onChange={(e) => setLine(i, { quantity: e.target.value })} /></td>
                  <td><Input aria-label={t("proposals.unitPrice")} inputMode="decimal" value={l.unit} onChange={(e) => setLine(i, { unit: e.target.value })} /></td>
                  <td><Input aria-label={t("proposals.setupFee")} inputMode="decimal" value={l.setup} onChange={(e) => setLine(i, { setup: e.target.value })} /></td>
                  <td><Input aria-label={t("proposals.discount")} inputMode="decimal" value={l.discount} onChange={(e) => setLine(i, { discount: e.target.value })} /></td>
                  <td><Button size="sm" variant="ghost" onClick={() => setForm({ ...form, lines: form.lines.filter((_, j) => j !== i) })}>{t("common.remove")}</Button></td>
                </tr>
              ))}
            </tbody>
          </Table>
          <div className="mt-2 flex flex-wrap items-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => setForm({ ...form, lines: [...form.lines, { name: "", pricing_model: "per_user", quantity: "1", unit: "0", setup: "0", discount: "0" }] })}>{t("proposals.addLine")}</Button>
            {(catalog.data?.data.filter((c) => c.status === "active").length ?? 0) > 0 ? (
              <Select aria-label={t("proposals.fromCatalog")} className="max-w-xs" value="" onChange={(e) => {
                const c = catalog.data?.data.find((x) => x.id === e.target.value);
                if (c) setForm({ ...form, lines: [...form.lines, { name: c.name, pricing_model: c.pricing_model, quantity: "1", unit: fromMinor(c.unit_price_minor, form.currency), setup: fromMinor(c.setup_fee_minor, form.currency), discount: "0", catalog_item_id: c.id }] });
              }}>
                <option value="">{t("proposals.fromCatalog")}</option>
                {catalog.data?.data.filter((c) => c.status === "active").map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </Select>
            ) : null}
          </div>
        </section>
        {save.isError ? <ErrorState title={errorText(t, save.error)} /> : null}
        <Button type="submit" disabled={save.isPending}>{t("proposals.saveDraft")}</Button>
      </form>
    </Card>
  );
}

function Totals({ v }: { v: Version }) {
  const { t } = useTranslation();
  return (
    <Card className="p-4">
      <dl className="grid gap-2 text-sm sm:grid-cols-4">
        <div><dt className="text-ink-soft">{t("proposals.setupTotal")}</dt><dd className="text-lg font-semibold">{money(v.setup_total_minor, v.currency)}</dd></div>
        <div><dt className="text-ink-soft">{t("proposals.monthly")}</dt><dd className="text-lg font-semibold">{money(v.mrr_total_minor, v.currency)}</dd></div>
        <div><dt className="text-ink-soft">{t("proposals.term")}</dt><dd className="text-lg font-semibold">{t("proposals.months", { count: v.term_months })}</dd></div>
        <div><dt className="text-ink-soft">{t("proposals.tcv")}</dt><dd className="text-lg font-semibold">{money(v.tcv_minor, v.currency)}</dd></div>
      </dl>
      <p className="mt-2 text-xs text-ink-faint">{v.tax_note}</p>
    </Card>
  );
}

export function ProposalDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { data: me } = useMe();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  const proposal = useProposal(id);
  const [picked, setPicked] = useState<number>();
  const versionNo = picked ?? proposal.data?.current_version;
  const version = useVersion(id, versionNo);
  const activity = useActivity(id);
  const [recipient, setRecipient] = useState({ name: "", email: "" });
  const v = version.data;
  const path = { path: { id, version: versionNo ?? 1 } };
  const submit = usePropMutation(() => unwrap(proposalApi.POST("/proposals/proposals/{id}/versions/{version}/submit", { params: path })));
  const approve = usePropMutation(() => unwrap(proposalApi.POST("/proposals/proposals/{id}/versions/{version}/approve", { params: path, body: { content_hash: v?.content_hash ?? "" } })));
  const reject = usePropMutation(() => unwrap(proposalApi.POST("/proposals/proposals/{id}/versions/{version}/reject", { params: path, body: { content_hash: v?.content_hash ?? "" } })));
  const issue = usePropMutation(() => unwrap(proposalApi.POST("/proposals/proposals/{id}/versions/{version}/issue", { params: { ...path, header: { "Idempotency-Key": `issue-${v?.id ?? ""}` } }, body: { recipients: [recipient], content_hash: v?.content_hash ?? "" } })));
  const sign = usePropMutation(() => unwrap(proposalApi.POST("/proposals/proposals/{id}/versions/{version}/signature-requests", { params: { ...path, header: { "Idempotency-Key": `sign-${v?.id ?? ""}` } }, body: { recipients: [recipient] } })));
  const newVersion = usePropMutation(() => unwrap(proposalApi.POST("/proposals/proposals/{id}/versions", { params: { path: { id } } })));
  const revoke = usePropMutation((link: string) => unwrap(proposalApi.POST("/proposals/share-links/{id}/revoke", { params: { path: { id: link } } })));
  if (proposal.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (proposal.isError) return <ErrorState title={errorText(t, proposal.error)} />;
  const p = proposal.data;
  const err = submit.error ?? approve.error ?? reject.error ?? issue.error ?? sign.error ?? newVersion.error;
  return (
    <div className="space-y-4">
      <Link to={proposalPath()} className="text-sm text-ink-soft hover:underline">← {t("proposals.tabs.proposals")}</Link>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold text-ink">{p.title}</h2>
        <Badge>{p.number}</Badge>
        <Badge tone={p.status === "signed" ? "ok" : "info"}>{t(`proposals.statuses.${p.status}`)}</Badge>
        <span className="text-sm text-ink-soft">{p.customer}</span>
      </div>
      <div className="flex flex-wrap items-end gap-2">
        <Field label={t("proposals.version")}>
          {(ids) => <Select {...ids} value={versionNo} onChange={(e) => setPicked(Number(e.target.value))}>{p.versions.map((x) => <option key={x.version} value={x.version}>{t("proposals.versionLabel", { n: x.version, status: t(`proposals.versionStatuses.${x.status}`) })}</option>)}</Select>}
        </Field>
        {can("proposal.write") && p.versions.every((x) => ["issued", "superseded", "expired"].includes(x.status)) ? <Button variant="secondary" onClick={() => newVersion.mutate(undefined, { onSuccess: (nv) => setPicked(nv.version) })}>{t("proposals.newVersion")}</Button> : null}
      </div>
      {err ? <ErrorState title={errorText(t, err)} /> : null}
      {version.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
      {v ? (
        <>
          <Totals v={v} />
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Badge tone={v.status === "issued" ? "ok" : "neutral"}>{t(`proposals.versionStatuses.${v.status}`)}</Badge>
            {v.ai_assisted ? <Badge tone="warn">{t("proposals.aiAssisted")}</Badge> : null}
            {v.needs_approval ? <Badge tone={v.approved ? "ok" : "warn"}>{v.approved ? t("proposals.approved") : t("proposals.needsApproval")}</Badge> : null}
          </div>
          {v.status === "draft" && can("proposal.write") ? <Editor id={id} v={v} /> : null}
          {v.status === "draft" && v.needs_approval && can("proposal.write") ? <Button variant="secondary" disabled={submit.isPending || v.lines.length === 0} onClick={() => submit.mutate(undefined)}>{t("proposals.submit")}</Button> : null}
          {v.status === "pending_approval" && can("proposal.approve") ? (
            <Card className="p-4">
              <p className="mb-2 text-sm">{t("proposals.reviewPrompt")}</p>
              <div className="flex gap-2">
                <Button disabled={approve.isPending} onClick={() => approve.mutate(undefined)}>{t("proposals.approve")}</Button>
                <Button variant="secondary" disabled={reject.isPending} onClick={() => reject.mutate(undefined)}>{t("proposals.reject")}</Button>
              </div>
            </Card>
          ) : null}
          {(v.status === "draft" && !v.needs_approval) || v.status === "approved" || v.status === "issued" ? (
            can("proposal.send") ? (
              <Card className="p-4">
                <h3 className="mb-2 text-sm font-medium text-ink">{v.status === "issued" ? t("proposals.sendForSignature") : t("proposals.issue")}</h3>
                <form className="grid gap-2 sm:grid-cols-[1fr_1fr_auto] sm:items-end" onSubmit={(e) => { e.preventDefault(); if (v.status === "issued") sign.mutate(undefined); else issue.mutate(undefined); }}>
                  <Field label={t("proposals.recipientName")}>{(ids) => <Input {...ids} value={recipient.name} onChange={(e) => setRecipient({ ...recipient, name: e.target.value })} />}</Field>
                  <Field label={t("proposals.recipientEmail")}>{(ids) => <Input {...ids} type="email" value={recipient.email} onChange={(e) => setRecipient({ ...recipient, email: e.target.value })} />}</Field>
                  <Button type="submit" disabled={!recipient.name.trim() || !recipient.email.includes("@") || issue.isPending || sign.isPending}>{v.status === "issued" ? t("proposals.sendForSignature") : t("proposals.issueAndSend")}</Button>
                </form>
                {v.status !== "issued" ? <p className="mt-2 text-xs text-ink-faint">{t("proposals.issueNote")}</p> : null}
                {sign.isSuccess ? <p role="status" className="mt-2 text-xs text-ok">{t("proposals.signatureSent")}</p> : null}
              </Card>
            ) : null
          ) : null}
        </>
      ) : null}
      <Card>
        <h3 className="px-4 pt-4 text-sm font-medium text-ink">{t("proposals.activity")}</h3>
        <Table>
          <thead><tr><th>{t("proposals.recipient")}</th><th>{t("proposals.views")}</th><th>{t("proposals.expires")}</th><th /></tr></thead>
          <tbody>
            {(activity.data?.links ?? []).map((l) => (
              <tr key={l.id}>
                <td>{l.recipient_email}</td><td>{l.views}</td><td>{l.revoked_at ? t("proposals.revoked") : new Date(l.expires_at).toLocaleDateString()}</td>
                <td>{!l.revoked_at && can("proposal.send") ? <Button size="sm" variant="ghost" onClick={() => revoke.mutate(l.id)}>{t("proposals.revoke")}</Button> : null}</td>
              </tr>
            ))}
          </tbody>
        </Table>
        <ul className="p-4 text-xs text-ink-soft">{(activity.data?.signatures ?? []).map((s, i) => <li key={i}>{new Date(s.occurred_at).toLocaleString()}: {t(`proposals.signatureEvents.${s.event_type}`, { defaultValue: s.event_type })}</li>)}</ul>
      </Card>
    </div>
  );
}
