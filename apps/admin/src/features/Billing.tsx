import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { api, errorText, hasRole, useAdmin } from "../app/api";

function money(minor: number | undefined, currency: string | undefined) {
  if (minor === undefined || !currency) return "—";
  return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(minor / 100);
}

/** Purchases waiting for review, bank transfers to confirm, refunds to make, reconciliation, and the billing settings. */
export function Billing() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const canAct = hasRole(me, "super_admin", "billing_admin");
  return (
    <>
      <PageHeader title={t("billing.title")} description={t("billing.subtitle")} />
      <Queue canAct={canAct} />
      <BankTransfers canAct={canAct} />
      <Refunds canAct={canAct} />
      <Reconciliation canAct={canAct} />
      {canAct ? <Settings /> : null}
    </>
  );
}

function Queue({ canAct }: { canAct: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const queue = useQuery({ queryKey: ["purchases", "under_review"], queryFn: () => unwrap(api.GET("/billing/purchases", { params: { query: { status: "under_review" } } })) });
  const [reason, setReason] = useState<Record<string, string>>({});
  const refresh = () => qc.invalidateQueries({ queryKey: ["purchases"] });
  const approve = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/billing/purchases/{id}/approve", { params: { path: { id } }, body: { reason: reason[id] || "Approved" } })), onSuccess: refresh });
  const reject = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/billing/purchases/{id}/reject", { params: { path: { id } }, body: { reason: reason[id] ?? "" } })), onSuccess: refresh });
  const list = queue.data?.data ?? [];
  return (
    <section className="mb-6">
      <h2 className="mb-1 text-sm font-medium text-ink-soft">{t("billing.queue")}</h2>
      <p className="mb-2 text-xs text-ink-faint">{t("billing.queueHelp", { hours: queue.data?.review_target_hours ?? 24 })}</p>
      <Card>
        {queue.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {queue.isError ? <div className="p-4"><ErrorState title={errorText(t, queue.error)} /></div> : null}
        {queue.isSuccess && list.length === 0 ? <EmptyState title={t("billing.queueEmpty")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("billing.tenant")}</th><th>{t("billing.plan")}</th><th className="text-right">{t("billing.total")}</th><th>{t("billing.waiting")}</th><th>{t("billing.decision")}</th></tr></thead>
            <tbody>
              {list.map((p) => (
                <tr key={p.id}>
                  <td><Link className="text-brand underline" to={`/tenants/${p.tenant_id}`}>{p.tenant_name}</Link></td>
                  <td>{p.plan_name} × {p.quantity}<div className="text-xs text-ink-faint">{t(`billing.methods.${p.payment_method ?? "none"}`)}</div></td>
                  <td className="text-right tabular-nums">{money(Number(p.quote.total_minor), String(p.quote.currency))}</td>
                  <td>{Math.round(p.waiting_hours)} h {p.over_target ? <Badge tone="warn">{t("billing.overTarget")}</Badge> : null}</td>
                  <td>
                    {canAct ? (
                      <div className="flex flex-wrap items-center gap-1">
                        <Input aria-label={t("billing.reasonFor", { tenant: p.tenant_name })} placeholder={t("billing.reason")} value={reason[p.id] ?? ""} onChange={(e) => setReason({ ...reason, [p.id]: e.target.value })} />
                        <Button size="sm" disabled={approve.isPending} onClick={() => approve.mutate(p.id)}>{t("billing.approve")}</Button>
                        <Button size="sm" variant="secondary" disabled={reject.isPending || (reason[p.id] ?? "").trim().length < 3} onClick={() => reject.mutate(p.id)}>{t("billing.reject")}</Button>
                      </div>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
        {approve.isError || reject.isError ? <div className="p-4"><ErrorState title={errorText(t, approve.error ?? reject.error)} /></div> : null}
      </Card>
    </section>
  );
}

function BankTransfers({ canAct }: { canAct: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["bank-transfers"], queryFn: () => unwrap(api.GET("/billing/bank-transfers")) });
  const [ref, setRef] = useState<Record<string, string>>({});
  const paid = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/billing/payments/{id}/mark-paid", { params: { path: { id } }, body: { reference: ref[id] ?? "" } })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ["bank-transfers"] }); void qc.invalidateQueries({ queryKey: ["purchases"] }); } });
  const rows = list.data?.data ?? [];
  return (
    <section className="mb-6">
      <h2 className="mb-1 text-sm font-medium text-ink-soft">{t("billing.bankTransfers")}</h2>
      <p className="mb-2 text-xs text-ink-faint">{t("billing.bankHelp")}</p>
      <Card>
        {list.isSuccess && rows.length === 0 ? <EmptyState title={t("billing.noTransfers")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("billing.tenant")}</th><th>{t("billing.invoice")}</th><th className="text-right">{t("billing.amount")}</th><th>{t("billing.since")}</th><th>{t("billing.confirm")}</th></tr></thead>
            <tbody>
              {rows.map((b) => (
                <tr key={b.payment_id}>
                  <td>{b.tenant_name}</td>
                  <td>{b.invoice_number ?? "—"}</td>
                  <td className="text-right tabular-nums">{money(b.amount_minor, b.currency)}</td>
                  <td>{new Date(b.created_at).toLocaleDateString()}</td>
                  <td>{canAct ? <div className="flex gap-1"><Input aria-label={t("billing.bankReferenceFor", { invoice: b.invoice_number ?? "" })} placeholder={t("billing.bankReference")} value={ref[b.payment_id] ?? ""} onChange={(e) => setRef({ ...ref, [b.payment_id]: e.target.value })} /><Button size="sm" disabled={!ref[b.payment_id]?.trim() || paid.isPending} onClick={() => paid.mutate(b.payment_id)}>{t("billing.markPaid")}</Button></div> : null}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
        {paid.isError ? <div className="p-4"><ErrorState title={errorText(t, paid.error)} /></div> : null}
      </Card>
    </section>
  );
}

function Refunds({ canAct }: { canAct: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["refunds"], queryFn: () => unwrap(api.GET("/billing/refunds")) });
  const [note, setNote] = useState<Record<string, string>>({});
  const refresh = () => qc.invalidateQueries({ queryKey: ["refunds"] });
  const retry = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/billing/payments/{id}/retry-refund", { params: { path: { id } } })), onSuccess: refresh });
  const done = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/billing/payments/{id}/mark-refunded", { params: { path: { id } }, body: { note: note[id] ?? "" } })), onSuccess: refresh });
  const rows = list.data?.data ?? [];
  if (list.isSuccess && rows.length === 0) return null;
  return (
    <section className="mb-6">
      <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("billing.refunds")}</h2>
      <Card>
        <Table>
          <thead><tr><th>{t("billing.tenant")}</th><th className="text-right">{t("billing.amount")}</th><th>{t("billing.method")}</th><th>{t("billing.refundStatus")}</th><th><span className="sr-only">{t("billing.actions")}</span></th></tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.payment_id}>
                <td>{r.tenant_name}</td>
                <td className="text-right tabular-nums">{money(r.amount_minor, r.currency)}</td>
                <td>{t(`billing.methods.${r.method}`)}</td>
                <td><Badge tone={r.refund_status === "failed" ? "danger" : "warn"}>{t(`billing.refundStatuses.${r.refund_status}`)}</Badge></td>
                <td>{canAct ? (r.method === "card" ? <Button size="sm" disabled={retry.isPending} onClick={() => retry.mutate(r.payment_id)}>{t("billing.retryRefund")}</Button> : (
                  <div className="flex gap-1"><Input aria-label={t("billing.refundNote")} placeholder={t("billing.refundNote")} value={note[r.payment_id] ?? ""} onChange={(e) => setNote({ ...note, [r.payment_id]: e.target.value })} /><Button size="sm" disabled={!note[r.payment_id]?.trim() || done.isPending} onClick={() => done.mutate(r.payment_id)}>{t("billing.markRefunded")}</Button></div>
                )) : null}</td>
              </tr>
            ))}
          </tbody>
        </Table>
        {retry.isError || done.isError ? <div className="p-4"><ErrorState title={errorText(t, retry.error ?? done.error)} /></div> : null}
      </Card>
    </section>
  );
}

function Reconciliation({ canAct }: { canAct: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const view = useQuery({ queryKey: ["reconciliation"], queryFn: () => unwrap(api.GET("/billing/reconciliation", { params: { query: { status: "open" } } })) });
  const run = useMutation({ mutationFn: () => unwrap(api.POST("/billing/reconciliation/run")), onSuccess: () => qc.invalidateQueries({ queryKey: ["reconciliation"] }) });
  const [note, setNote] = useState<Record<string, string>>({});
  const resolve = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/billing/reconciliation/issues/{id}/resolve", { params: { path: { id } }, body: { note: note[id] ?? "" } })), onSuccess: () => qc.invalidateQueries({ queryKey: ["reconciliation"] }) });
  const last = view.data?.runs[0];
  return (
    <section className="mb-6">
      <div className="mb-2 flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-ink-soft">{t("billing.reconciliation")}</h2>
        {canAct ? <Button size="sm" variant="secondary" disabled={run.isPending} onClick={() => run.mutate()}>{t("billing.runNow")}</Button> : null}
      </div>
      <Card className="p-4">
        <p className="mb-2 text-xs text-ink-faint">{last ? t("billing.lastRun", { when: new Date(last.created_at).toLocaleString(), checked: last.checked, status: last.status }) : t("billing.neverRun")}</p>
        {view.data && view.data.issues.length === 0 ? <p className="text-sm text-ink-soft">{t("billing.noIssues")}</p> : null}
        {view.data?.issues.length ? (
          <Table>
            <thead><tr><th>{t("billing.issue")}</th><th>{t("billing.providerRef")}</th><th>{t("billing.detail")}</th><th><span className="sr-only">{t("billing.actions")}</span></th></tr></thead>
            <tbody>
              {view.data.issues.map((i) => (
                <tr key={i.id}>
                  <td><Badge tone="danger">{t(`billing.issueKinds.${i.kind}`)}</Badge></td>
                  <td><code className="text-xs">{i.provider_ref}</code></td>
                  <td className="text-xs text-ink-soft">{JSON.stringify(i.detail)}</td>
                  <td>{canAct ? <div className="flex gap-1"><Input aria-label={t("billing.resolution")} placeholder={t("billing.resolution")} value={note[i.id] ?? ""} onChange={(e) => setNote({ ...note, [i.id]: e.target.value })} /><Button size="sm" disabled={!note[i.id]?.trim()} onClick={() => resolve.mutate(i.id)}>{t("billing.resolve")}</Button></div> : null}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
        {run.isError || resolve.isError ? <ErrorState title={errorText(t, run.error ?? resolve.error)} /> : null}
      </Card>
    </section>
  );
}

function Settings() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const policy = useQuery({ queryKey: ["dunning-policy"], queryFn: () => unwrap(api.GET("/billing/dunning-policy")) });
  const bank = useQuery({ queryKey: ["bank-instructions"], queryFn: () => unwrap(api.GET("/billing/bank-instructions")) });
  const [form, setForm] = useState({ retry: "", suspend: "", expire: "", due: "", review: "" });
  const [text, setText] = useState("");
  useEffect(() => {
    if (policy.data) setForm({ retry: policy.data.retry_days.join(", "), suspend: String(policy.data.suspend_after_days), expire: String(policy.data.expire_after_days), due: String(policy.data.bank_transfer_due_days), review: String(policy.data.review_target_hours) });
  }, [policy.data]);
  useEffect(() => { if (bank.data) setText(bank.data.text); }, [bank.data]);
  const save = useMutation({
    mutationFn: () => unwrap(api.PUT("/billing/dunning-policy", { body: { retry_days: form.retry.split(",").map((s) => Number(s.trim())).filter((n) => n > 0), suspend_after_days: Number(form.suspend), expire_after_days: Number(form.expire), bank_transfer_due_days: Number(form.due), review_target_hours: Number(form.review) } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dunning-policy"] }),
  });
  const saveBank = useMutation({ mutationFn: () => unwrap(api.PUT("/billing/bank-instructions", { body: { text } })), onSuccess: () => qc.invalidateQueries({ queryKey: ["bank-instructions"] }) });
  const num = (k: keyof typeof form, label: string) => <Field label={label}>{(ids) => <Input {...ids} inputMode="numeric" value={form[k]} onChange={(e) => setForm({ ...form, [k]: e.target.value })} />}</Field>;
  return (
    <section className="mb-6 grid gap-4 lg:grid-cols-2">
      <Card className="p-4">
        <h2 className="mb-1 text-sm font-medium">{t("billing.dunning")}</h2>
        <p className="mb-3 text-xs text-ink-faint">{t("billing.dunningHelp")}</p>
        <form className="grid gap-2 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
          <Field label={t("billing.retryDays")}>{(ids) => <Input {...ids} value={form.retry} onChange={(e) => setForm({ ...form, retry: e.target.value })} />}</Field>
          {num("suspend", t("billing.suspendAfter"))}
          {num("expire", t("billing.expireAfter"))}
          {num("due", t("billing.bankDue"))}
          {num("review", t("billing.reviewTarget"))}
          {save.isError ? <div className="sm:col-span-2"><ErrorState title={errorText(t, save.error)} /></div> : null}
          <div><Button type="submit" disabled={save.isPending}>{t("billing.save")}</Button>{save.isSuccess ? <span role="status" className="ml-2 text-sm text-ok">{t("billing.saved")}</span> : null}</div>
        </form>
      </Card>
      <Card className="p-4">
        <h2 className="mb-1 text-sm font-medium">{t("billing.bankInstructions")}</h2>
        <p className="mb-3 text-xs text-ink-faint">{t("billing.bankInstructionsHelp")}</p>
        <form onSubmit={(e) => { e.preventDefault(); saveBank.mutate(); }}>
          <textarea aria-label={t("billing.bankInstructions")} className="min-h-28 w-full rounded-md border border-line-strong bg-surface p-2 text-sm" maxLength={2000} value={text} onChange={(e) => setText(e.target.value)} />
          {saveBank.isError ? <ErrorState title={errorText(t, saveBank.error)} /> : null}
          <Button type="submit" disabled={saveBank.isPending}>{t("billing.save")}</Button>{saveBank.isSuccess ? <span role="status" className="ml-2 text-sm text-ok">{t("billing.saved")}</span> : null}
        </form>
      </Card>
    </section>
  );
}
