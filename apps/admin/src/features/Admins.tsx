import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, PageHeader, Select, SkeletonRows, Table } from "@msp/ui";
import { createAdminSchema, recoverAdminSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { api, errorText, useAdmin } from "../app/api";

const roles = ["super_admin", "platform_support", "billing_admin", "integration_admin", "platform_analyst"] as const;

function Recover({ id, name, onDone, onCancel }: { id: string; name: string; onDone: () => void; onCancel: () => void }) {
  const { t } = useTranslation();
  const form = useForm<z.infer<typeof recoverAdminSchema>>({ resolver: zodResolver(recoverAdminSchema), defaultValues: { new_password: "", reason: "" } });
  const [failure, setFailure] = useState<string>();
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    setFailure(undefined);
    try {
      await unwrap(api.POST("/admins/{id}/recover", { params: { path: { id } }, body: values }));
      onDone();
    } catch (err) {
      setFailure(errorText(t, err));
    }
  });
  return (
    <Card className="mb-6 p-4">
      <h2 className="text-sm font-medium">{t("admins.recoverTitle", { name })}</h2>
      <p className="mt-1 mb-3 text-sm text-ink-soft">{t("admins.recoverBody")}</p>
      {failure ? <div className="mb-3"><ErrorState title={failure} /></div> : null}
      <form onSubmit={(ev) => void submit(ev)} noValidate className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t("admins.newPassword")} hint={t("admins.passwordHint")} error={e.new_password?.message && t(e.new_password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="new-password" {...form.register("new_password")} />}</Field>
          <Field label={t("admins.reason")} error={e.reason?.message && t(e.reason.message)}>{(ids) => <Input {...ids} {...form.register("reason")} />}</Field>
        </div>
        <div className="flex gap-2">
          <Button type="submit" variant="danger" disabled={form.formState.isSubmitting}>{t("admins.recover")}</Button>
          <Button variant="secondary" onClick={onCancel}>{t("common.cancel")}</Button>
        </div>
      </form>
    </Card>
  );
}

export function Admins() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const [recovering, setRecovering] = useState<{ id: string; name: string }>();
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
      {recovering ? <Recover key={recovering.id} {...recovering} onCancel={() => setRecovering(undefined)} onDone={() => setRecovering(undefined)} /> : null}
      <Card className="mb-6">
        {admins.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {admins.isError ? <div className="p-4"><ErrorState title={errorText(t, admins.error)} onRetry={() => void admins.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {admins.data ? (
          <Table>
            <thead><tr><th>{t("admins.name")}</th><th>{t("admins.email")}</th><th>{t("admins.roles")}</th><th>{t("admins.status")}</th><th /></tr></thead>
            <tbody>{admins.data.data.map((a) => <tr key={a.id}><td>{a.display_name}</td><td className="text-ink-soft">{a.email}</td><td className="space-x-1">{a.roles.map((r) => <Badge key={r}>{t(`admins.roleNames.${r}`, { defaultValue: r })}</Badge>)}</td><td>{a.status}</td><td className="text-right">{a.id === me?.admin.id ? <Badge>{t("admins.you")}</Badge> : <Button variant="ghost" size="sm" onClick={() => setRecovering({ id: a.id, name: a.display_name })}>{t("admins.recover")}</Button>}</td></tr>)}</tbody>
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
