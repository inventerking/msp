import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, PageHeader, Select, SkeletonRows, Table } from "@msp/ui";
import { planSchema, priceSchema, productSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { api, errorText, hasRole, useAdmin } from "../app/api";

export function Catalog() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const qc = useQueryClient();
  const canView = hasRole(me, "super_admin", "billing_admin", "platform_support", "platform_analyst");
  const canManage = hasRole(me, "super_admin", "billing_admin");
  const modules = useQuery({ queryKey: ["modules"], queryFn: () => unwrap(api.GET("/modules")) });
  const products = useQuery({ queryKey: ["products"], queryFn: () => unwrap(api.GET("/products")), enabled: canView });
  const plans = useQuery({ queryKey: ["plans"], queryFn: () => unwrap(api.GET("/plans")), enabled: canView });
  const [failure, setFailure] = useState<string>();
  const refresh = async () => { await qc.invalidateQueries({ queryKey: ["products"] }); await qc.invalidateQueries({ queryKey: ["plans"] }); };
  const guarded = async (work: () => Promise<unknown>) => {
    setFailure(undefined);
    try {
      await work();
      await refresh();
      return true;
    } catch (e) {
      setFailure(errorText(t, e));
      return false;
    }
  };

  const productForm = useForm<z.input<typeof productSchema>, unknown, z.output<typeof productSchema>>({ resolver: zodResolver(productSchema), defaultValues: { key: "", kind: "bundle", name: "", description: "", module_keys: [] } });
  const pe = productForm.formState.errors;
  const createProduct = productForm.handleSubmit(async (values) => {
    if (await guarded(() => unwrap(api.POST("/products", { body: values })))) productForm.reset();
  });

  const planForm = useForm<z.input<typeof planSchema>, unknown, z.output<typeof planSchema>>({ resolver: zodResolver(planSchema), defaultValues: { product_version_id: "", key: "", name: "", billing_period: "monthly", trial_days: 0, seats: "", ai_credits: "", lead_credits: "" } });
  const ple = planForm.formState.errors;
  const createPlan = planForm.handleSubmit(async ({ seats, ai_credits, lead_credits, ...values }) => {
    const credits: Record<string, number> = {};
    if (typeof ai_credits === "number" && ai_credits > 0) credits["ai_credits"] = ai_credits;
    if (typeof lead_credits === "number" && lead_credits > 0) credits["lead_credits"] = lead_credits;
    const body = { ...values, ...(typeof seats === "number" ? { seats } : {}), ...(Object.keys(credits).length ? { included_credits: credits } : {}) };
    if (await guarded(() => unwrap(api.POST("/plans", { body })))) planForm.reset();
  });

  const [priceFor, setPriceFor] = useState<string>();
  const priceForm = useForm<z.input<typeof priceSchema>, unknown, z.output<typeof priceSchema>>({ resolver: zodResolver(priceSchema), defaultValues: { currency: "USD", amount_minor: 0 } });
  const createPrice = priceForm.handleSubmit(async ({ currency, amount_minor }) => {
    if (!priceFor) return;
    if (await guarded(() => unwrap(api.POST("/plans/{id}/prices", { params: { path: { id: priceFor } }, body: { currency: currency.toUpperCase(), amount_minor } })))) {
      setPriceFor(undefined);
      priceForm.reset();
    }
  });

  return (
    <>
      <PageHeader title={t("catalog.title")} description={t("catalog.subtitle")} />
      {failure ? <div className="mb-4"><ErrorState title={failure} /></div> : null}
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("catalog.modules")}</h2>
        <Card>
          {modules.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
          {modules.data ? <Table><thead><tr><th>{t("catalog.name")}</th><th>{t("catalog.key")}</th><th>{t("catalog.status")}</th></tr></thead><tbody>{modules.data.data.map((m) => <tr key={m.id}><td>{m.name}</td><td><code className="text-xs">{m.key}</code></td><td><Badge tone="ok">{m.status}</Badge></td></tr>)}</tbody></Table> : null}
        </Card>
      </section>
      {canView ? (
        <section className="mb-6">
          <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("catalog.products")}</h2>
          <Card>
            {products.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
            {products.isError ? <div className="p-4"><ErrorState title={errorText(t, products.error)} /></div> : null}
            {products.data ? (
              <Table>
                <thead><tr><th>{t("catalog.name")}</th><th>{t("catalog.kind")}</th><th>{t("catalog.version")}</th><th>{t("catalog.contents")}</th>{canManage ? <th /> : null}</tr></thead>
                <tbody>
                  {products.data.data.map((p) => (
                    <tr key={p.id}>
                      <td className="font-medium">{p.name}</td>
                      <td>{p.kind}</td>
                      <td><Badge tone={p.version_status === "published" ? "ok" : "info"}>{t("catalog.versionLabel", { version: p.version, status: p.version_status })}</Badge></td>
                      <td className="text-xs text-ink-soft">{p.modules.join(", ")}</td>
                      {canManage ? <td className="text-right">{p.version_status === "draft" ? <Button size="sm" variant="secondary" onClick={() => void guarded(() => unwrap(api.POST("/product-versions/{id}/publish", { params: { path: { id: p.version_id } } })))}>{t("catalog.publish")}</Button> : null}</td> : null}
                    </tr>
                  ))}
                </tbody>
              </Table>
            ) : null}
          </Card>
          {canManage ? (
            <Card className="mt-3 p-4">
              <h3 className="mb-1 text-sm font-medium">{t("catalog.newProduct")}</h3>
              <p className="mb-3 text-xs text-ink-faint">{t("catalog.newProductHint")}</p>
              <form onSubmit={(ev) => void createProduct(ev)} noValidate className="space-y-3">
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label={t("catalog.key")} error={pe.key?.message && t(pe.key.message)}>{(ids) => <Input {...ids} {...productForm.register("key")} />}</Field>
                  <Field label={t("catalog.name")} error={pe.name?.message && t(pe.name.message)}>{(ids) => <Input {...ids} {...productForm.register("name")} />}</Field>
                  <Field label={t("catalog.kind")}>{(ids) => <Select {...ids} {...productForm.register("kind")}><option value="module">module</option><option value="bundle">bundle</option><option value="suite">suite</option><option value="addon">addon</option></Select>}</Field>
                </div>
                <Field label={t("catalog.description")}>{(ids) => <Input {...ids} {...productForm.register("description")} />}</Field>
                <fieldset>
                  <legend className="mb-1 text-sm font-medium">{t("catalog.contents")}</legend>
                  <div className="grid gap-1 sm:grid-cols-2 lg:grid-cols-3">
                    {modules.data?.data.map((m) => <label key={m.key} className="flex items-center gap-2 text-sm"><input type="checkbox" value={m.key} {...productForm.register("module_keys")} />{m.name}</label>)}
                  </div>
                  {pe.module_keys?.message ? <p className="mt-1 text-xs text-danger">{t(pe.module_keys.message)}</p> : null}
                </fieldset>
                <Button type="submit" disabled={productForm.formState.isSubmitting}>{t("catalog.createProduct")}</Button>
              </form>
            </Card>
          ) : null}
        </section>
      ) : null}
      {canView ? (
        <section>
          <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("catalog.plans")}</h2>
          <Card>
            {plans.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
            {plans.data ? (
              <Table>
                <thead><tr><th>{t("catalog.name")}</th><th>{t("catalog.period")}</th><th>{t("catalog.prices")}</th>{canManage ? <th /> : null}</tr></thead>
                <tbody>
                  {plans.data.data.map((p) => (
                    <tr key={p.id}>
                      <td>{p.name} <code className="text-xs text-ink-faint">{p.key}</code></td>
                      <td>{p.billing_period}</td>
                      <td className="text-xs text-ink-soft tabular-nums">{p.prices.filter((x) => !x.valid_to).map((x) => `${x.currency} ${x.amount_minor}`).join(", ") || t("catalog.noPrice")}</td>
                      {canManage ? <td className="text-right"><Button size="sm" variant="secondary" onClick={() => setPriceFor(p.id)}>{t("catalog.setPrice")}</Button></td> : null}
                    </tr>
                  ))}
                </tbody>
              </Table>
            ) : null}
          </Card>
          {priceFor ? (
            <Card className="mt-3 p-4">
              <form onSubmit={(ev) => void createPrice(ev)} noValidate className="flex flex-wrap items-start gap-3">
                <Field label={t("catalog.currency")} error={priceForm.formState.errors.currency?.message && t(priceForm.formState.errors.currency.message)}>{(ids) => <Input {...ids} className="w-24" {...priceForm.register("currency")} />}</Field>
                <Field label={t("catalog.amountMinor")} hint={t("catalog.amountHint")}>{(ids) => <Input {...ids} type="number" min={0} className="w-40" {...priceForm.register("amount_minor")} />}</Field>
                <div className="flex gap-2 pt-6.5"><Button type="submit">{t("catalog.savePrice")}</Button><Button variant="ghost" onClick={() => setPriceFor(undefined)}>{t("common.cancel")}</Button></div>
              </form>
            </Card>
          ) : null}
          {canManage ? (
            <Card className="mt-3 p-4">
              <h3 className="mb-3 text-sm font-medium">{t("catalog.newPlan")}</h3>
              <form onSubmit={(ev) => void createPlan(ev)} noValidate className="space-y-3">
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field label={t("catalog.product")} error={ple.product_version_id?.message && t(ple.product_version_id.message)}>
                    {(ids) => <Select {...ids} {...planForm.register("product_version_id")}><option value="">{t("catalog.chooseProduct")}</option>{products.data?.data.filter((p) => p.version_status === "published").map((p) => <option key={p.version_id} value={p.version_id}>{p.name} v{p.version}</option>)}</Select>}
                  </Field>
                  <Field label={t("catalog.key")} error={ple.key?.message && t(ple.key.message)}>{(ids) => <Input {...ids} {...planForm.register("key")} />}</Field>
                  <Field label={t("catalog.name")} error={ple.name?.message && t(ple.name.message)}>{(ids) => <Input {...ids} {...planForm.register("name")} />}</Field>
                  <Field label={t("catalog.period")}>{(ids) => <Select {...ids} {...planForm.register("billing_period")}><option value="monthly">monthly</option><option value="annual">annual</option><option value="custom">custom</option></Select>}</Field>
                  <Field label={t("catalog.trialDays")}>{(ids) => <Input {...ids} type="number" min={0} max={365} {...planForm.register("trial_days")} />}</Field>
                  <Field label={t("catalog.seats")} hint={t("catalog.seatsHint")}>{(ids) => <Input {...ids} type="number" min={1} {...planForm.register("seats")} />}</Field>
                  <Field label={t("catalog.aiCredits")} hint={t("catalog.creditsHint")}>{(ids) => <Input {...ids} type="number" min={0} {...planForm.register("ai_credits")} />}</Field>
                  <Field label={t("catalog.leadCredits")} hint={t("catalog.creditsHint")}>{(ids) => <Input {...ids} type="number" min={0} {...planForm.register("lead_credits")} />}</Field>
                </div>
                <Button type="submit" disabled={planForm.formState.isSubmitting}>{t("catalog.createPlan")}</Button>
              </form>
            </Card>
          ) : null}
        </section>
      ) : null}
    </>
  );
}
