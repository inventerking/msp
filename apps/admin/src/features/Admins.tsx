import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, PageHeader, Select, SkeletonRows, Table } from "@msp/ui";
import { createAdminSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { api, errorText } from "../app/api";

const roles = ["super_admin", "platform_support", "billing_admin", "integration_admin", "platform_analyst"] as const;

export function Admins() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const admins = useQuery({ queryKey: ["admins"], queryFn: () => unwrap(api.GET("/admins")) });
  const form = useForm<z.infer<typeof createAdminSchema>>({ resolver: zodResolver(createAdminSchema), defaultValues: { display_name: "", email: "", password: "", role: "platform_support" } });
  const [failure, setFailure] = useState<string>();
  const e = form.formState.errors;
  const submit = form.handleSubmit(async ({ role, ...values }) => {
    setFailure(undefined);
    try {
      await unwrap(api.POST("/admins", { body: { ...values, roles: [role as (typeof roles)[number]] } }));
      form.reset();
      await qc.invalidateQueries({ queryKey: ["admins"] });
    } catch (err) {
      setFailure(errorText(t, err));
    }
  });
  return (
    <>
      <PageHeader title={t("admins.title")} description={t("admins.subtitle")} />
      <Card className="mb-6">
        {admins.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {admins.isError ? <div className="p-4"><ErrorState title={errorText(t, admins.error)} onRetry={() => void admins.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {admins.data ? (
          <Table>
            <thead><tr><th>{t("admins.name")}</th><th>{t("admins.email")}</th><th>{t("admins.roles")}</th><th>{t("admins.status")}</th></tr></thead>
            <tbody>{admins.data.data.map((a) => <tr key={a.id}><td>{a.display_name}</td><td className="text-ink-soft">{a.email}</td><td className="space-x-1">{a.roles.map((r) => <Badge key={r}>{t(`admins.roleNames.${r}`, { defaultValue: r })}</Badge>)}</td><td>{a.status}</td></tr>)}</tbody>
          </Table>
        ) : null}
      </Card>
      <Card className="p-4">
        <h2 className="mb-3 text-sm font-medium">{t("admins.add")}</h2>
        {failure ? <div className="mb-3"><ErrorState title={failure} /></div> : null}
        <form onSubmit={(ev) => void submit(ev)} noValidate className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t("admins.name")} error={e.display_name?.message && t(e.display_name.message)}>{(ids) => <Input {...ids} {...form.register("display_name")} />}</Field>
            <Field label={t("admins.email")} error={e.email?.message && t(e.email.message)}>{(ids) => <Input {...ids} type="email" {...form.register("email")} />}</Field>
            <Field label={t("admins.initialPassword")} hint={t("admins.passwordHint")} error={e.password?.message && t(e.password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="new-password" {...form.register("password")} />}</Field>
            <Field label={t("admins.role")}>{(ids) => <Select {...ids} {...form.register("role")}>{roles.map((r) => <option key={r} value={r}>{t(`admins.roleNames.${r}`)}</option>)}</Select>}</Field>
          </div>
          <Button type="submit" disabled={form.formState.isSubmitting}>{t("admins.create")}</Button>
        </form>
      </Card>
    </>
  );
}
