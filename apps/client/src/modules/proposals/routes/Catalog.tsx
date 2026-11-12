import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { proposalApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { money, toMinor, useCatalog, usePropMutation, useTextTemplates } from "../api/proposals";

/** Services and the SLA and terms texts proposals draw on. Changing a price never alters a sent proposal. */
export function Catalog() {
  const { t } = useTranslation();
  const catalog = useCatalog();
  const texts = useTextTemplates();
  const [item, setItem] = useState({ name: "", pricing_model: "per_user" as "per_user" | "per_device" | "fixed", currency: "GBP", unit: "", setup: "" });
  const [text, setText] = useState({ kind: "sla" as "sla" | "terms", name: "", body: "" });
  const addItem = usePropMutation(() => unwrap(proposalApi.POST("/proposals/catalog", { body: { name: item.name, pricing_model: item.pricing_model, currency: item.currency, unit_price_minor: toMinor(item.unit, item.currency), setup_fee_minor: toMinor(item.setup || "0", item.currency) } })));
  const addText = usePropMutation(() => unwrap(proposalApi.POST("/proposals/text-templates", { body: text })));
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card className="p-4">
        <h2 className="mb-2 text-sm font-medium text-ink">{t("proposals.services")}</h2>
        <Table>
          <thead><tr><th>{t("proposals.service")}</th><th>{t("proposals.model")}</th><th>{t("proposals.unitPrice")}</th></tr></thead>
          <tbody>{(catalog.data?.data ?? []).map((c) => <tr key={c.id}><td>{c.name}</td><td>{t(`proposals.models.${c.pricing_model}`)}</td><td>{money(c.unit_price_minor, c.currency)}</td></tr>)}</tbody>
        </Table>
        <form className="mt-3 grid gap-2 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); addItem.mutate(undefined, { onSuccess: () => setItem({ ...item, name: "", unit: "", setup: "" }) }); }}>
          <Field label={t("proposals.service")}>{(ids) => <Input {...ids} value={item.name} onChange={(e) => setItem({ ...item, name: e.target.value })} />}</Field>
          <Field label={t("proposals.model")}>{(ids) => <Select {...ids} value={item.pricing_model} onChange={(e) => setItem({ ...item, pricing_model: e.target.value as typeof item.pricing_model })}>{(["per_user", "per_device", "fixed"] as const).map((m) => <option key={m} value={m}>{t(`proposals.models.${m}`)}</option>)}</Select>}</Field>
          <Field label={t("proposals.currency")}>{(ids) => <Input {...ids} maxLength={3} value={item.currency} onChange={(e) => setItem({ ...item, currency: e.target.value.toUpperCase() })} />}</Field>
          <Field label={t("proposals.unitPrice")}>{(ids) => <Input {...ids} inputMode="decimal" value={item.unit} onChange={(e) => setItem({ ...item, unit: e.target.value })} />}</Field>
          <Field label={t("proposals.setupFee")}>{(ids) => <Input {...ids} inputMode="decimal" value={item.setup} onChange={(e) => setItem({ ...item, setup: e.target.value })} />}</Field>
          <div className="flex items-end"><Button type="submit" disabled={!item.name.trim() || !item.unit || addItem.isPending}>{t("proposals.addService")}</Button></div>
        </form>
        {addItem.isError ? <ErrorState title={errorText(t, addItem.error)} /> : null}
      </Card>
      <Card className="p-4">
        <h2 className="mb-2 text-sm font-medium text-ink">{t("proposals.texts")}</h2>
        <ul className="mb-3 space-y-1 text-sm">{(texts.data?.data ?? []).map((x) => <li key={x.id}>{x.name} <span className="text-xs text-ink-faint">({t(`proposals.textKinds.${x.kind}`)}, v{x.version})</span></li>)}</ul>
        <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); addText.mutate(undefined, { onSuccess: () => setText({ ...text, name: "", body: "" }) }); }}>
          <div className="grid gap-2 sm:grid-cols-2">
            <Field label={t("proposals.textKind")}>{(ids) => <Select {...ids} value={text.kind} onChange={(e) => setText({ ...text, kind: e.target.value as "sla" | "terms" })}><option value="sla">{t("proposals.textKinds.sla")}</option><option value="terms">{t("proposals.textKinds.terms")}</option></Select>}</Field>
            <Field label={t("proposals.textName")}>{(ids) => <Input {...ids} value={text.name} onChange={(e) => setText({ ...text, name: e.target.value })} />}</Field>
          </div>
          <label className="block text-sm text-ink" htmlFor="text-body">{t("proposals.body")}</label>
          <textarea id="text-body" className="min-h-24 w-full rounded-md border border-line-strong bg-surface p-2 text-sm" value={text.body} onChange={(e) => setText({ ...text, body: e.target.value })} />
          <p className="text-xs text-ink-faint">{t("proposals.textsNote")}</p>
          <Button type="submit" disabled={!text.name.trim() || !text.body.trim() || addText.isPending}>{t("proposals.addText")}</Button>
        </form>
      </Card>
    </div>
  );
}
