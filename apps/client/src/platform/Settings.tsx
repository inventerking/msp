import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { settingsSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { api, tenantKey, useMe } from "../app/api";
import { applyServerError, errorText } from "../app/errors";
import { NoPermission } from "../shell/StatusPages";

export function Settings() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const allowed = Boolean(me?.permissions.includes("settings.manage"));
  const tenant = useQuery({ queryKey: tenantKey(me, "tenant"), queryFn: () => unwrap(api.GET("/tenant")), enabled: allowed });
  const canAudit = Boolean(me?.permissions.includes("audit.read"));
  const audit = useInfiniteQuery({
    queryKey: tenantKey(me, "audit"),
    enabled: canAudit,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => unwrap(api.GET("/tenant/audit-events", { params: { query: { limit: 25, ...(pageParam ? { cursor: pageParam } : {}) } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const form = useForm<z.infer<typeof settingsSchema>>({ resolver: zodResolver(settingsSchema), defaultValues: { name: "", legal_name: "", website_domain: "", country_code: "", default_timezone: "UTC", reporting_currency: "USD" } });
  const [failure, setFailure] = useState<string>();
  const [saved, setSaved] = useState(false);
  const { reset } = form;
  const d = tenant.data;
  useEffect(() => {
    if (d) reset({ name: d.name, legal_name: d.legal_name ?? "", website_domain: d.website_domain ?? "", country_code: d.country_code ?? "", default_timezone: d.default_timezone, reporting_currency: d.reporting_currency.trim() });
  }, [d, reset]);
  const e = form.formState.errors;
  const submit = form.handleSubmit(async ({ legal_name, website_domain, country_code, ...rest }) => {
    setFailure(undefined);
    setSaved(false);
    try {
      await unwrap(api.PATCH("/tenant", { body: { ...rest, ...(legal_name ? { legal_name } : {}), ...(website_domain ? { website_domain } : {}), ...(country_code ? { country_code } : {}) } }));
      setSaved(true);
      await qc.invalidateQueries({ queryKey: ["me"] });
      await qc.invalidateQueries({ queryKey: tenantKey(me) });
    } catch (err) {
      setFailure(applyServerError(t, err, form.setError, ["name", "legal_name", "website_domain", "country_code", "default_timezone", "reporting_currency"]));
    }
  });
  if (!allowed) return <NoPermission />;
  const events = audit.data?.pages.flatMap((p) => p.data) ?? [];
  const text = (name: "name" | "legal_name" | "website_domain" | "country_code" | "default_timezone" | "reporting_currency", label: string) => {
    const msg = e[name]?.message;
    return <Field label={label} error={msg && t(msg, { defaultValue: msg })}>{(ids) => <Input {...ids} {...form.register(name)} />}</Field>;
  };
  return (
    <>
      <PageHeader title={t("settings.title")} description={t("settings.subtitle")} />
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("settings.profile")}</h2>
        <Card className="p-4">
          {tenant.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
          {tenant.isError ? <ErrorState title={errorText(t, tenant.error)} onRetry={() => void tenant.refetch()} retryLabel={t("common.retry")} /> : null}
          {tenant.data ? (
            <form onSubmit={(ev) => void submit(ev)} noValidate className="space-y-4">
              {failure ? <ErrorState title={failure} /> : null}
              <div className="grid gap-4 sm:grid-cols-2">
                {text("name", t("settings.name"))}
                {text("legal_name", t("settings.legalName"))}
                {text("website_domain", t("settings.website"))}
                {text("country_code", t("settings.country"))}
                {text("default_timezone", t("settings.timezone"))}
                {text("reporting_currency", t("settings.currency"))}
              </div>
              <div className="flex items-center gap-3">
                <Button type="submit" disabled={form.formState.isSubmitting}>{t("common.save")}</Button>
                {saved ? <span role="status" className="text-sm text-ok">{t("common.saved")}</span> : null}
              </div>
            </form>
          ) : null}
        </Card>
      </section>
      {canAudit ? (
        <section>
          <h2 className="text-sm font-medium text-ink-soft">{t("settings.audit")}</h2>
          <p className="mb-2 text-xs text-ink-faint">{t("settings.auditBody")}</p>
          <Card>
            {audit.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
            {audit.isError ? <div className="p-4"><ErrorState title={errorText(t, audit.error)} onRetry={() => void audit.refetch()} retryLabel={t("common.retry")} /></div> : null}
            {audit.isSuccess && events.length === 0 ? <EmptyState title={t("settings.noAudit")} /> : null}
            {events.length ? (
              <Table>
                <thead><tr><th>{t("settings.when")}</th><th>{t("settings.action")}</th><th>{t("settings.actor")}</th></tr></thead>
                <tbody>
                  {events.map((ev) => <tr key={ev.id}><td className="whitespace-nowrap text-ink-soft">{new Date(ev.occurred_at).toLocaleString()}</td><td><code className="text-xs">{ev.action}</code></td><td>{t(`settings.actors.${ev.actor_type}`, { defaultValue: ev.actor_type })}</td></tr>)}
                </tbody>
              </Table>
            ) : null}
            {audit.hasNextPage ? <div className="border-t border-line p-3 text-center"><Button variant="secondary" size="sm" disabled={audit.isFetchingNextPage} onClick={() => void audit.fetchNextPage()}>{t("settings.loadMore")}</Button></div> : null}
          </Card>
        </section>
      ) : null}
    </>
  );
}
