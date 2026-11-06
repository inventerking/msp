import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, Input, PageHeader, Select, SkeletonRows, Table } from "@msp/ui";
import { inviteSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { MailPlus } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { api, tenantKey, useMe } from "../app/api";
import { applyServerError, errorText } from "../app/errors";

export function Team() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  const members = useQuery({ queryKey: tenantKey(me, "members"), queryFn: () => unwrap(api.GET("/tenant/members")), enabled: can("team.read") });
  const roles = useQuery({ queryKey: tenantKey(me, "roles"), queryFn: () => unwrap(api.GET("/tenant/roles")), enabled: can("team.read") });
  const invites = useQuery({ queryKey: tenantKey(me, "invitations"), queryFn: () => unwrap(api.GET("/tenant/invitations")), enabled: can("team.manage") });
  const refresh = () => qc.invalidateQueries({ queryKey: tenantKey(me) });
  const [removing, setRemoving] = useState<{ id: string; name: string }>();
  const [actionError, setActionError] = useState<string>();
  const remove = useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/tenant/members/{id}", { params: { path: { id } } })),
    onSuccess: async () => { setRemoving(undefined); await refresh(); },
  });
  const changeRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) => unwrap(api.PATCH("/tenant/members/{id}", { params: { path: { id } }, body: { role } })),
    onSuccess: refresh,
    onError: (e) => setActionError(errorText(t, e)),
    onMutate: () => setActionError(undefined),
  });
  const revoke = useMutation({ mutationFn: (id: string) => unwrap(api.DELETE("/tenant/invitations/{id}", { params: { path: { id } } })), onSuccess: refresh });
  const form = useForm<z.infer<typeof inviteSchema>>({ resolver: zodResolver(inviteSchema), defaultValues: { email: "", role: "viewer" } });
  const [inviteError, setInviteError] = useState<string>();
  const e = form.formState.errors;
  const invite = form.handleSubmit(async (values) => {
    setInviteError(undefined);
    try {
      await unwrap(api.POST("/tenant/invitations", { body: values }));
      form.reset();
      await refresh();
    } catch (err) {
      setInviteError(applyServerError(t, err, form.setError, ["email", "role"]));
    }
  });
  return (
    <>
      <PageHeader title={t("team.title")} description={t("team.subtitle")} />
      {can("team.manage") ? (
        <Card className="mb-6 p-4">
          <form onSubmit={(ev) => void invite(ev)} className="grid items-start gap-3 sm:grid-cols-[1fr_12rem_auto]" noValidate>
            <Field label={t("team.email")} error={e.email?.message && t(e.email.message)}>{(ids) => <Input {...ids} type="email" {...form.register("email")} />}</Field>
            <Field label={t("team.role")} error={e.role?.message && t(e.role.message)}>
              {(ids) => <Select {...ids} {...form.register("role")}>{roles.data?.data.map((r) => <option key={r.id} value={r.key}>{t(`roles.${r.key}`, { defaultValue: r.name })}</option>)}</Select>}
            </Field>
            <Button type="submit" className="sm:mt-6.5" disabled={form.formState.isSubmitting}><MailPlus />{t("team.invite")}</Button>
          </form>
          {inviteError ? <div className="mt-3"><ErrorState title={inviteError} /></div> : null}
        </Card>
      ) : null}
      {actionError ? <div className="mb-4"><ErrorState title={actionError} /></div> : null}
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("team.members")}</h2>
        <Card>
          {members.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
          {members.isError ? <div className="p-4"><ErrorState title={errorText(t, members.error)} onRetry={() => void members.refetch()} retryLabel={t("common.retry")} /></div> : null}
          {members.data ? (
            <Table>
              <thead><tr><th>{t("team.name")}</th><th>{t("team.email")}</th><th>{t("team.role")}</th>{can("team.manage") ? <th /> : null}</tr></thead>
              <tbody>
                {members.data.data.map((m) => {
                  const self = m.user_id === me?.user.id;
                  return (
                    <tr key={m.id}>
                      <td>{m.display_name} {self ? <Badge>{t("team.you")}</Badge> : null}</td>
                      <td className="text-ink-soft">{m.email}</td>
                      <td>
                        {can("team.manage") && !self ? (
                          <Select aria-label={t("team.role")} className="h-8 w-44" value={m.role} onChange={(ev) => changeRole.mutate({ id: m.id, role: ev.target.value })}>
                            {roles.data?.data.map((r) => <option key={r.id} value={r.key}>{t(`roles.${r.key}`, { defaultValue: r.name })}</option>)}
                          </Select>
                        ) : t(`roles.${m.role}`, { defaultValue: m.role })}
                      </td>
                      {can("team.manage") ? <td className="text-right">{self ? null : <Button variant="ghost" size="sm" onClick={() => setRemoving({ id: m.id, name: m.display_name })}>{t("common.remove")}</Button>}</td> : null}
                    </tr>
                  );
                })}
              </tbody>
            </Table>
          ) : null}
        </Card>
      </section>
      {can("team.manage") ? (
        <section>
          <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("team.invitations")}</h2>
          <Card>
            {invites.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
            {invites.data?.data.length === 0 ? <EmptyState title={t("team.noInvitations")} /> : null}
            {invites.data?.data.length ? (
              <Table>
                <thead><tr><th>{t("team.email")}</th><th>{t("team.role")}</th><th>{t("team.expires")}</th><th /></tr></thead>
                <tbody>
                  {invites.data.data.map((i) => (
                    <tr key={i.id}>
                      <td>{i.email}</td><td>{t(`roles.${i.role}`, { defaultValue: i.role })}</td><td className="text-ink-soft">{new Date(i.expires_at).toLocaleDateString()}</td>
                      <td className="text-right"><Button variant="ghost" size="sm" onClick={() => revoke.mutate(i.id)}>{t("team.revoke")}</Button></td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            ) : null}
          </Card>
        </section>
      ) : null}
      <ConfirmDialog
        open={Boolean(removing)} danger title={t("team.removeTitle", { name: removing?.name })} description={t("team.removeBody")}
        confirmLabel={t("common.remove")} cancelLabel={t("common.cancel")} busy={remove.isPending} error={remove.error ? errorText(t, remove.error) : undefined}
        onCancel={() => setRemoving(undefined)} onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </>
  );
}
