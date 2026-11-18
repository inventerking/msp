import { unwrap } from "@msp/api-client";
import type { TenantComponents } from "@msp/types";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Receipt, ShoppingCart } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, tenantKey, useMe } from "../app/api";
import { errorText } from "../app/errors";

type Offer = TenantComponents["schemas"]["PlanOffer"];

/** Shows minor units in a currency, using the browser's formatting for that currency. */
export function money(minor: number | undefined | null, currency: string | undefined): string {
  if (minor === undefined || minor === null || !currency) return "—";
  const digits = new Intl.NumberFormat(undefined, { style: "currency", currency }).resolvedOptions().maximumFractionDigits ?? 2;
  return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(minor / 10 ** digits);
}

function useBillingKey() {
  const { data: me } = useMe();
  return (...rest: unknown[]) => tenantKey(me, "billing", ...rest);
}

function useCan(p: string) {
  const { data: me } = useMe();
  return Boolean(me?.permissions.includes(p));
}

const statusTone = (s: string) => (s === "approved" || s === "paid" ? "ok" : s === "rejected" || s === "cancelled" || s === "void" || s === "uncollectible" ? "danger" : s === "under_review" ? "info" : "warn");

/** Buying a plan, the purchases made, and invoices. Payment is taken at submission; an admin then reviews it. */
export function BillingSections() {
  return (
    <>
      <Buy />
      <Purchases />
      <Invoices />
    </>
  );
}

function Buy() {
  const { t } = useTranslation();
  const key = useBillingKey();
  const qc = useQueryClient();
  const canBuy = useCan("billing.manage");
  const offers = useQuery({ queryKey: key("plans"), queryFn: () => unwrap(api.GET("/billing/plans")) });
  const list = useMemo(() => offers.data?.data ?? [], [offers.data]);
  const [choice, setChoice] = useState("");
  const [form, setForm] = useState({ quantity: "1", method: "card" as "card" | "bank_transfer", promo: "" });
  const [idem, setIdem] = useState(() => crypto.randomUUID());
  const offer: Offer | undefined = list.find((o) => `${o.plan_id}:${o.currency}` === choice);
  const submit = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/billing/purchases", {
        params: { header: { "Idempotency-Key": idem } },
        body: { plan_id: offer!.plan_id, currency: offer!.currency, quantity: Number(form.quantity), payment_method: form.method, ...(form.promo.trim() ? { promotion_code: form.promo.trim() } : {}) },
      })),
    onSuccess: (p) => {
      setIdem(crypto.randomUUID());
      void qc.invalidateQueries({ queryKey: key() });
      if (p.checkout_url) window.location.assign(p.checkout_url);
    },
  });
  if (!canBuy) return null;
  return (
    <section className="mb-6">
      <h2 className="mb-1 text-sm font-medium text-ink-soft">{t("billing.buy")}</h2>
      <p className="mb-2 text-xs text-ink-faint">{t("billing.buyNote")}</p>
      <Card className="p-4">
        {offers.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
        {offers.isError ? <ErrorState title={errorText(t, offers.error)} onRetry={() => void offers.refetch()} retryLabel={t("common.retry")} /> : null}
        {offers.isSuccess && list.length === 0 ? <EmptyState icon={ShoppingCart} title={t("billing.noPlans")} /> : null}
        {list.length ? (
          <form className="grid gap-3 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); submit.mutate(); }}>
            <div className="sm:col-span-2">
              <Field label={t("billing.plan")}>{(ids) => (
                <Select {...ids} value={choice} onChange={(e) => setChoice(e.target.value)}>
                  <option value="">{t("billing.choosePlan")}</option>
                  {list.map((o) => (
                    <option key={`${o.plan_id}:${o.currency}`} value={`${o.plan_id}:${o.currency}`}>
                      {o.product_name} · {o.plan_name} · {money(o.amount_minor, o.currency)} {t(`billing.per.${o.billing_period}`)}{o.trial_days ? ` · ${t("billing.trial", { count: o.trial_days })}` : ""}
                    </option>
                  ))}
                </Select>
              )}</Field>
            </div>
            <Field label={t("billing.quantity")}>{(ids) => <Input {...ids} inputMode="numeric" value={form.quantity} onChange={(e) => setForm({ ...form, quantity: e.target.value.replace(/\D/g, "") })} />}</Field>
            <Field label={t("billing.paymentMethod")}>{(ids) => (
              <Select {...ids} value={form.method} onChange={(e) => setForm({ ...form, method: e.target.value as "card" | "bank_transfer" })}>
                {offers.data?.card_available ? <option value="card">{t("billing.methods.card")}</option> : null}
                <option value="bank_transfer">{t("billing.methods.bank_transfer")}</option>
              </Select>
            )}</Field>
            <Field label={t("billing.promotion")}>{(ids) => <Input {...ids} maxLength={64} value={form.promo} onChange={(e) => setForm({ ...form, promo: e.target.value })} />}</Field>
            {offer ? (
              <div className="rounded-md bg-sunken p-3 text-sm sm:col-span-2">
                <p>{t("billing.summary", { amount: money(offer.amount_minor * Math.max(1, Number(form.quantity) || 1), offer.currency) })}</p>
                {typeof offer.usage_limits.seats === "number" ? <p className="text-xs text-ink-soft">{t("billing.seats", { count: offer.usage_limits.seats * Math.max(1, Number(form.quantity) || 1) })}</p> : null}
                <p className="text-xs text-ink-faint">{offers.data?.tax_note}</p>
              </div>
            ) : null}
            {submit.isError ? <div className="sm:col-span-2"><ErrorState title={errorText(t, submit.error)} /></div> : null}
            {form.method === "card" && !offers.data?.card_available ? null : (
              <div><Button type="submit" disabled={!offer || !(Number(form.quantity) > 0) || submit.isPending}>{form.method === "card" ? t("billing.payByCard") : t("billing.requestInvoice")}</Button></div>
            )}
          </form>
        ) : null}
      </Card>
    </section>
  );
}

function Purchases() {
  const { t } = useTranslation();
  const key = useBillingKey();
  const qc = useQueryClient();
  const canManage = useCan("billing.manage");
  const purchases = useQuery({ queryKey: key("purchases"), queryFn: () => unwrap(api.GET("/billing/purchases")) });
  const cancel = useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/billing/purchases/{id}/cancel", { params: { path: { id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key() }),
  });
  const pay = useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/billing/invoices/{id}/pay", { params: { path: { id } } })),
    onSuccess: (r) => window.location.assign(r.checkout_url),
  });
  const list = purchases.data?.data ?? [];
  if (purchases.isSuccess && list.length === 0) return null;
  return (
    <section className="mb-6">
      <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("billing.purchases")}</h2>
      <Card>
        {purchases.isError ? <div className="p-4"><ErrorState title={errorText(t, purchases.error)} /></div> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("billing.plan")}</th><th>{t("billing.status")}</th><th className="text-right">{t("billing.total")}</th><th>{t("billing.submitted")}</th><th><span className="sr-only">{t("billing.actions")}</span></th></tr></thead>
            <tbody>
              {list.map((p) => (
                <tr key={p.id}>
                  <td>{p.plan_name}</td>
                  <td>
                    <Badge tone={statusTone(p.status)}>{t(`billing.purchaseStatus.${p.status}`)}</Badge>
                    {p.rejection_reason ? <div className="mt-1 text-xs text-ink-soft">{p.rejection_reason}</div> : null}
                    {p.bank_instructions ? <div className="mt-1 max-w-sm whitespace-pre-wrap text-xs text-ink-soft">{p.bank_instructions}</div> : null}
                  </td>
                  <td className="text-right tabular-nums">{money(p.quote.total_minor, p.quote.currency)}</td>
                  <td className="text-ink-soft">{new Date(p.created_at).toLocaleDateString()}</td>
                  <td className="space-x-1 text-right whitespace-nowrap">
                    {canManage && p.status === "payment_pending" && p.payment_method === "card" && p.invoice_id ? <Button size="sm" disabled={pay.isPending} onClick={() => pay.mutate(p.invoice_id!)}>{t("billing.payNow")}</Button> : null}
                    {canManage && p.status === "payment_pending" ? <Button size="sm" variant="secondary" disabled={cancel.isPending} onClick={() => cancel.mutate(p.id)}>{t("billing.cancel")}</Button> : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
        {cancel.isError || pay.isError ? <div className="p-4"><ErrorState title={errorText(t, cancel.error ?? pay.error)} /></div> : null}
      </Card>
    </section>
  );
}

function Invoices() {
  const { t } = useTranslation();
  const key = useBillingKey();
  const canManage = useCan("billing.manage");
  const invoices = useQuery({ queryKey: key("invoices"), queryFn: () => unwrap(api.GET("/billing/invoices")) });
  const [open, setOpen] = useState<string>();
  const detail = useQuery({ queryKey: key("invoice", open), enabled: Boolean(open), queryFn: () => unwrap(api.GET("/billing/invoices/{id}", { params: { path: { id: open! } } })) });
  const pay = useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/billing/invoices/{id}/pay", { params: { path: { id } } })),
    onSuccess: (r) => window.location.assign(r.checkout_url),
  });
  const list = invoices.data?.data ?? [];
  return (
    <section className="mb-6">
      <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("billing.invoices")}</h2>
      <Card>
        {invoices.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
        {invoices.isError ? <div className="p-4"><ErrorState title={errorText(t, invoices.error)} /></div> : null}
        {invoices.isSuccess && list.length === 0 ? <EmptyState icon={Receipt} title={t("billing.noInvoices")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("billing.number")}</th><th>{t("billing.status")}</th><th className="text-right">{t("billing.total")}</th><th>{t("billing.due")}</th><th><span className="sr-only">{t("billing.actions")}</span></th></tr></thead>
            <tbody>
              {list.map((i) => (
                <tr key={i.id}>
                  <td><button type="button" className="text-brand underline" aria-expanded={open === i.id} onClick={() => setOpen(open === i.id ? undefined : i.id)}>{i.number}</button></td>
                  <td><Badge tone={statusTone(i.status)}>{t(`billing.invoiceStatus.${i.status}`)}</Badge></td>
                  <td className="text-right tabular-nums">{money(i.total_minor, i.currency)}</td>
                  <td className="text-ink-soft">{i.due_at ? new Date(i.due_at).toLocaleDateString() : "—"}</td>
                  <td className="text-right">{canManage && i.status === "issued" ? <Button size="sm" disabled={pay.isPending} onClick={() => pay.mutate(i.id)}>{t("billing.payByCard")}</Button> : null}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
        {open && detail.data ? (
          <div className="border-t border-line p-4 text-sm">
            <Table>
              <thead><tr><th>{t("billing.line")}</th><th className="text-right">{t("billing.quantity")}</th><th className="text-right">{t("billing.amount")}</th></tr></thead>
              <tbody>
                {detail.data.lines.map((l, n) => (
                  <tr key={n}>
                    <td>{l.description}{l.period_start ? <div className="text-xs text-ink-faint">{new Date(l.period_start).toLocaleDateString()} – {l.period_end ? new Date(l.period_end).toLocaleDateString() : ""}</div> : null}</td>
                    <td className="text-right tabular-nums">{l.quantity}</td>
                    <td className="text-right tabular-nums">{money(l.amount_minor, detail.data.invoice.currency)}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
            <p className="mt-2 text-xs text-ink-faint">{t("billing.taxNotCalculated")}</p>
            {detail.data.bank_instructions ? <p className="mt-2 whitespace-pre-wrap rounded-md bg-sunken p-3 text-xs">{detail.data.bank_instructions}</p> : null}
          </div>
        ) : null}
        {pay.isError ? <div className="p-4"><ErrorState title={errorText(t, pay.error)} /></div> : null}
      </Card>
    </section>
  );
}
