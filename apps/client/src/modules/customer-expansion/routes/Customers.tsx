import { ApiError, unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { Users } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { api, expansionApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { healthTone, money, useCan, useCustomers, useXMutation } from "../api/expansion";
import { expansionPath } from "../paths";

const healths = ["healthy", "watch", "at_risk", "unknown"] as const;

/** Uploads a CSV for import and waits until the malware scan has cleared it. */
async function uploadForImport(file: File): Promise<string> {
  const created = await unwrap(api.POST("/files/uploads", { body: { purpose: "csv_import", file_name: file.name, size_bytes: file.size } }));
  await unwrap(api.PUT("/files/uploads/{id}/content", { params: { path: { id: created.id } }, body: file as never, bodySerializer: (b: unknown) => b as BodyInit, headers: { "Content-Type": "application/octet-stream" } }));
  for (let i = 0; i < 30; i++) {
    const info = await unwrap(api.GET("/files/{id}", { params: { path: { id: created.id } } }));
    if (info.status === "clean") return created.id;
    if (info.status === "quarantined") throw new ApiError(422, "file_quarantined");
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new ApiError(409, "file_not_ready");
}

/** Existing customers: what they pay, when they renew, and how much of their coverage is known. */
export function Customers() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canWrite = useCan("expansion.write");
  const [health, setHealth] = useState("");
  const customers = useCustomers(health || undefined);
  const [form, setForm] = useState({ name: "", domain: "" });
  const add = useXMutation(() => unwrap(expansionApi.POST("/customer-expansion/customers", { body: { name: form.name.trim(), ...(form.domain.trim() ? { domain: form.domain.trim() } : {}) } })));
  const [file, setFile] = useState<File>();
  const importer = useXMutation(async (f: File) => unwrap(expansionApi.POST("/customer-expansion/customers/import", { body: { file_id: await uploadForImport(f) } })));
  const list = customers.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canWrite ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card className="p-4">
            <h2 className="mb-1 text-sm font-medium text-ink">{t("expansion.addCustomer")}</h2>
            <p className="mb-3 text-xs text-ink-soft">{t("expansion.addCustomerHelp")}</p>
            <form className="grid gap-2 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); add.mutate(undefined, { onSuccess: (c) => navigate(expansionPath(`customers/${c.id}`)) }); }}>
              <Field label={t("expansion.companyName")}>{(ids) => <Input {...ids} value={form.name} maxLength={200} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
              <Field label={t("expansion.domain")}>{(ids) => <Input {...ids} value={form.domain} placeholder="example.com" onChange={(e) => setForm({ ...form, domain: e.target.value })} />}</Field>
              {add.isError ? <div className="sm:col-span-2"><ErrorState title={errorText(t, add.error)} /></div> : null}
              <div><Button type="submit" disabled={!form.name.trim() || add.isPending}>{t("expansion.add")}</Button></div>
            </form>
          </Card>
          <Card className="p-4">
            <h2 className="mb-1 text-sm font-medium text-ink">{t("expansion.import")}</h2>
            <p className="mb-3 text-xs text-ink-soft">{t("expansion.importHelp")}</p>
            <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (file) importer.mutate(file); }}>
              <Field label={t("expansion.csvFile")}>{(ids) => <Input {...ids} type="file" accept=".csv,text/csv" onChange={(e) => setFile(e.target.files?.[0])} />}</Field>
              <Button type="submit" variant="secondary" disabled={!file || importer.isPending}>{importer.isPending ? t("expansion.importing") : t("expansion.importButton")}</Button>
            </form>
            {importer.isError ? <div className="mt-2"><ErrorState title={errorText(t, importer.error)} /></div> : null}
            {importer.data ? (
              <div role="status" className="mt-2 text-sm text-ink-soft">
                {t("expansion.imported", { created: importer.data.created, contracts: importer.data.contracts })}
                {importer.data.errors.length ? <ul className="mt-1 list-disc pl-5 text-xs text-danger">{importer.data.errors.slice(0, 20).map((e) => <li key={e}>{e}</li>)}</ul> : null}
              </div>
            ) : null}
          </Card>
        </div>
      ) : null}
      <div className="flex items-end gap-2">
        <Field label={t("expansion.health")}>{(ids) => (
          <Select {...ids} value={health} onChange={(e) => setHealth(e.target.value)}>
            <option value="">{t("expansion.allHealth")}</option>
            {healths.map((h) => <option key={h} value={h}>{t(`expansion.healthStatus.${h}`)}</option>)}
          </Select>
        )}</Field>
      </div>
      <Card>
        {customers.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {customers.isError ? <div className="p-4"><ErrorState title={errorText(t, customers.error)} onRetry={() => void customers.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {customers.isSuccess && list.length === 0 ? <EmptyState icon={Users} title={t("expansion.noCustomers")} description={t("expansion.noCustomersHelp")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("expansion.customer")}</th><th>{t("expansion.health")}</th><th className="text-right">{t("expansion.mrr")}</th><th>{t("expansion.nextRenewal")}</th><th>{t("expansion.coverage")}</th></tr></thead>
            <tbody>
              {list.map((c) => (
                <tr key={c.id}>
                  <td><Link className="text-brand underline" to={expansionPath(`customers/${c.id}`)}>{c.crm_company.name}</Link></td>
                  <td><Badge tone={healthTone(c.health_status)}>{t(`expansion.healthStatus.${c.health_status}`)}</Badge>{c.health_source === "manual" ? <span className="ml-1 text-xs text-ink-faint">{t("expansion.setByHand")}</span> : null}</td>
                  <td className="text-right tabular-nums">{c.mixed_currency ? <span className="text-xs text-ink-soft">{t("expansion.mixedCurrency")}</span> : money(c.active_mrr_minor, c.currency)}</td>
                  <td>{c.renewal_next_date ? new Date(c.renewal_next_date).toLocaleDateString() : "—"}</td>
                  <td className="text-xs text-ink-soft">{t("expansion.coverageSummary", c.coverage_summary)}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}
