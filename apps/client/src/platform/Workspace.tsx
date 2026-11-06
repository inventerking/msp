import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input } from "@msp/ui";
import { workspaceSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Link, Navigate, useNavigate, useParams } from "react-router";
import type { z } from "zod";
import { api, resetSession, useMe } from "../app/api";
import { applyServerError } from "../app/errors";
import { AuthCard } from "./auth";

export function Workspace() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const navigate = useNavigate();
  const [failure, setFailure] = useState<string>();
  const form = useForm<z.infer<typeof workspaceSchema>>({ resolver: zodResolver(workspaceSchema), defaultValues: { name: "" } });
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    setFailure(undefined);
    try {
      await unwrap(api.POST("/tenants", { body: values }));
      await resetSession();
      await navigate("/");
    } catch (err) {
      setFailure(applyServerError(t, err, form.setError, ["name"]));
    }
  });
  const open = async (id: string) => {
    await unwrap(api.POST("/me/active-tenant", { body: { tenant_id: id } }));
    await resetSession();
    await navigate("/");
  };
  return (
    <AuthCard title={me?.memberships.length ? t("workspace.title") : t("workspace.create")} body={me?.memberships.length ? undefined : t("workspace.createBody")}>
      {me?.memberships.length ? (
        <ul className="mb-6 space-y-2">
          {me.memberships.map((m) => (
            <li key={m.tenant_id}>
              <Card className="flex items-center justify-between gap-3 p-3">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{m.tenant_name}</p>
                  <p className="text-xs text-ink-faint">{t("workspace.role", { role: t(`roles.${m.role}`) })}</p>
                </div>
                <Button variant="secondary" size="sm" onClick={() => void open(m.tenant_id)}>{t("workspace.open")}</Button>
              </Card>
            </li>
          ))}
        </ul>
      ) : null}
      <form onSubmit={(ev) => void submit(ev)} className="space-y-4" noValidate>
        {failure ? <ErrorState title={failure} /> : null}
        <Field label={t("workspace.name")} error={e.name?.message && t(e.name.message)}>{(ids) => <Input {...ids} autoComplete="organization" {...form.register("name")} />}</Field>
        <Button type="submit" className="w-full" disabled={form.formState.isSubmitting}>{t("workspace.createButton")}</Button>
      </form>
    </AuthCard>
  );
}

export function AcceptInvite() {
  const { t } = useTranslation();
  const { token = "" } = useParams();
  const me = useMe();
  const navigate = useNavigate();
  const [failed, setFailed] = useState(false);
  if (me.isPending) return null;
  const here = encodeURIComponent(`/invite/${token}`);
  if (!me.data) {
    return (
      <AuthCard title={t("workspace.invite.title")} body={t("workspace.invite.needAccount")}>
        <div className="flex flex-col gap-2">
          <Button asChild><Link to={`/login?next=${here}`}>{t("auth.signIn")}</Link></Button>
          <Button asChild variant="secondary"><Link to={`/register?next=${here}`}>{t("auth.createAccount")}</Link></Button>
        </div>
      </AuthCard>
    );
  }
  if (me.data.mfa_required) return <Navigate to={`/mfa?next=${here}`} replace />;
  const accept = async () => {
    setFailed(false);
    try {
      await unwrap(api.POST("/invitations/accept", { body: { token } }));
      await resetSession();
      await navigate("/");
    } catch {
      setFailed(true);
    }
  };
  return (
    <AuthCard title={t("workspace.invite.title")} body={t("workspace.invite.body")}>
      {failed ? <div className="mb-4"><ErrorState title={t("workspace.invite.failed")} /></div> : null}
      <Button className="w-full" onClick={() => void accept()}>{t("workspace.invite.accept")}</Button>
    </AuthCard>
  );
}
