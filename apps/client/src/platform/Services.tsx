import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Layers } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, tenantKey, useMe } from "../app/api";
import { errorText } from "../app/errors";

/** The services this workspace sells. Proposals price them and Customer Expansion tracks who has them. */
export function Services() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const services = useQuery({ queryKey: tenantKey(me, "services"), queryFn: () => unwrap(api.GET("/tenant/services")) });
  const reference = useQuery({ queryKey: ["reference"], queryFn: () => unwrap(api.GET("/reference")), staleTime: 3_600_000 });
  const [form, setForm] = useState({ name: "", category_key: "" });
  const refresh = () => void qc.invalidateQueries({ queryKey: tenantKey(me, "services") });
  const add = useMutation({
    mutationFn: () => unwrap(api.POST("/tenant/services", { body: { name: form.name.trim(), ...(form.category_key ? { category_key: form.category_key } : {}) } })),
    onSuccess: () => { setForm({ name: "", category_key: "" }); refresh(); },
  });
  const setStatus = useMutation({
    mutationFn: (v: { id: string; status: "active" | "retired" }) => unwrap(api.PATCH("/tenant/services/{id}", { params: { path: { id: v.id } }, body: { status: v.status } })),
    onSuccess: refresh,
  });
  const categories = reference.data?.service_categories ?? [];
  const categoryName = (key?: string) => categories.find((c) => c.code === key)?.name ?? t("services.noCategory");
  const list = services.data?.data ?? [];
  return (
    <section className="mb-6">
      <h2 className="text-sm font-medium text-ink-soft">{t("services.title")}</h2>
      <p className="mb-2 text-xs text-ink-faint">{t("services.body")}</p>
      <Card className="p-4">
        <form className="mb-4 grid gap-2 sm:grid-cols-[1fr_1fr_auto] sm:items-end" onSubmit={(e) => { e.preventDefault(); add.mutate(); }}>
          <Field label={t("services.name")}>{(ids) => <Input {...ids} value={form.name} maxLength={120} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
          <Field label={t("services.category")}>{(ids) => (
            <Select {...ids} value={form.category_key} onChange={(e) => setForm({ ...form, category_key: e.target.value })}>
              <option value="">{t("services.noCategory")}</option>
              {categories.map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}
            </Select>
          )}</Field>
          <Button type="submit" disabled={!form.name.trim() || add.isPending}>{t("services.add")}</Button>
        </form>
        {add.isError ? <ErrorState title={errorText(t, add.error)} /> : null}
        {services.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {services.isError ? <ErrorState title={errorText(t, services.error)} onRetry={() => void services.refetch()} retryLabel={t("common.retry")} /> : null}
        {services.isSuccess && list.length === 0 ? <EmptyState icon={Layers} title={t("services.empty")} description={t("services.emptyHelp")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("services.name")}</th><th>{t("services.category")}</th><th>{t("services.status")}</th><th><span className="sr-only">{t("services.actions")}</span></th></tr></thead>
            <tbody>
              {list.map((s) => (
                <tr key={s.id}>
                  <td>{s.name}</td>
                  <td className="text-ink-soft">{categoryName(s.category_key)}</td>
                  <td><Badge tone={s.status === "active" ? "ok" : "neutral"}>{t(`services.statuses.${s.status}`)}</Badge></td>
                  <td className="text-right">
                    <Button variant="secondary" size="sm" disabled={setStatus.isPending} onClick={() => setStatus.mutate({ id: s.id, status: s.status === "active" ? "retired" : "active" })}>
                      {s.status === "active" ? t("services.retire") : t("services.restore")}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </section>
  );
}
