import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { FileSpreadsheet } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { intelApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCan, useDefinitions, useIMutation, useReports } from "../api/intel";

type Dimension = "all" | "source" | "stage" | "campaign";

/** Saved reports, exported as CSV. A download link works for a few minutes. */
export function Reports() {
  const { t } = useTranslation();
  const canExport = useCan("sales_intel.export");
  const reports = useReports();
  const defs = useDefinitions();
  const thisMonth = new Date().toISOString().slice(0, 7);
  const [form, setForm] = useState({ name: "", metrics: [] as string[], from: thisMonth, to: thisMonth, dimension: "all" as Dimension });
  const create = useIMutation(() => unwrap(intelApi.POST("/sales-intelligence/reports", { body: { name: form.name, definition: { metrics: form.metrics, from: `${form.from}-01`, to: `${form.to}-01`, dimension: form.dimension } } })));
  const [exports, setExports] = useState<Record<string, { status: string; url?: string; error?: string }>>({});
  const runExport = async (id: string) => {
    setExports((e) => ({ ...e, [id]: { status: "queued" } }));
    try {
      let exp = await unwrap(intelApi.POST("/sales-intelligence/reports/{id}/export", { params: { path: { id } } }));
      for (let i = 0; i < 30 && exp.status !== "ready" && exp.status !== "failed"; i++) {
        await new Promise((r) => setTimeout(r, 1000));
        exp = await unwrap(intelApi.GET("/sales-intelligence/exports/{id}", { params: { path: { id: exp.id } } }));
      }
      setExports((e) => ({ ...e, [id]: { status: exp.status, ...(exp.download_url ? { url: exp.download_url } : {}) } }));
    } catch (err) {
      setExports((e) => ({ ...e, [id]: { status: "failed", error: errorText(t, err) } }));
    }
  };
  const metricName = (k: string) => defs.data?.data.find((d) => d.key === k)?.name ?? k;
  const list = reports.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canExport ? (
        <Card className="p-4">
          <h2 className="mb-2 text-sm font-medium text-ink">{t("intel.newReport")}</h2>
          <form className="grid gap-2 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: () => setForm({ ...form, name: "", metrics: [] }) }); }}>
            <Field label={t("intel.reportName")}>{(ids) => <Input {...ids} maxLength={120} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
            <Field label={t("intel.breakdown")}>{(ids) => <Select {...ids} value={form.dimension} onChange={(e) => setForm({ ...form, dimension: e.target.value as Dimension })}>{(["all", "source", "stage", "campaign"] as const).map((d) => <option key={d} value={d}>{t(`intel.dimensions.${d}`)}</option>)}</Select>}</Field>
            <Field label={t("intel.from")}>{(ids) => <Input {...ids} type="month" value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} />}</Field>
            <Field label={t("intel.to")}>{(ids) => <Input {...ids} type="month" value={form.to} onChange={(e) => setForm({ ...form, to: e.target.value })} />}</Field>
            <fieldset className="sm:col-span-2">
              <legend className="mb-1 text-sm text-ink-soft">{t("intel.metrics")}</legend>
              <div className="grid max-h-48 gap-1 overflow-y-auto sm:grid-cols-3">
                {(defs.data?.data ?? []).filter((d) => d.dimensions.includes(form.dimension === "all" ? "all" : form.dimension)).map((d) => (
                  <label key={d.key} className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={form.metrics.includes(d.key)} onChange={(e) => setForm({ ...form, metrics: e.target.checked ? [...form.metrics, d.key] : form.metrics.filter((m) => m !== d.key) })} />
                    {d.name}
                  </label>
                ))}
              </div>
            </fieldset>
            {create.isError ? <div className="sm:col-span-2"><ErrorState title={errorText(t, create.error)} /></div> : null}
            <div><Button type="submit" disabled={!form.name.trim() || form.metrics.length === 0 || create.isPending}>{t("intel.saveReport")}</Button></div>
          </form>
        </Card>
      ) : null}
      <Card>
        {reports.isSuccess && list.length === 0 ? <EmptyState icon={FileSpreadsheet} title={t("intel.noReports")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("intel.reportName")}</th><th>{t("intel.metrics")}</th><th>{t("intel.period")}</th><th><span className="sr-only">{t("intel.actions")}</span></th></tr></thead>
            <tbody>
              {list.map((r) => {
                const ex = exports[r.id];
                return (
                  <tr key={r.id}>
                    <td>{r.name}</td>
                    <td className="text-xs text-ink-soft">{r.definition.metrics.map(metricName).join(", ")}</td>
                    <td className="text-xs">{r.definition.from.slice(0, 7)} – {r.definition.to.slice(0, 7)}</td>
                    <td className="text-right whitespace-nowrap">
                      {canExport ? <Button size="sm" variant="secondary" disabled={ex?.status === "queued"} onClick={() => void runExport(r.id)}>{t("intel.exportCsv")}</Button> : null}
                      {ex?.url ? <a className="ml-2 text-sm text-brand underline" href={ex.url}>{t("intel.download")}</a> : null}
                      {ex?.status === "failed" ? <span className="ml-2 text-xs text-danger">{ex.error ?? t("intel.exportFailed")}</span> : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}
