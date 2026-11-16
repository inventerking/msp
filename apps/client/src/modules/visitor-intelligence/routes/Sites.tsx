import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { Eye } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { visitorApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCan, useSites, useVMutation } from "../api/visitors";
import { visitorPath } from "../paths";

type Mode = "opt_in" | "opt_out" | "cookieless_only";

/** Tracked websites. Each has a public key for its script tag, and its own consent mode. */
export function Sites() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canConfigure = useCan("visitor.configure");
  const sites = useSites();
  const [form, setForm] = useState({ domain: "", consent_mode: "opt_in" as Mode });
  const create = useVMutation(() => unwrap(visitorApi.POST("/visitor-intelligence/sites", { body: form })));
  const list = sites.data?.data ?? [];
  return (
    <div className="space-y-4">
      <p role="note" className="rounded-md bg-sunken p-3 text-sm text-ink-soft">{t("visitors.testOnlyNotice")}</p>
      {canConfigure ? (
        <Card className="p-4">
          <h2 className="mb-3 text-sm font-medium text-ink">{t("visitors.addSite")}</h2>
          <form className="grid gap-2 sm:grid-cols-3 sm:items-end" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: (s) => navigate(visitorPath(`sites/${s.id}`)) }); }}>
            <Field label={t("visitors.domain")}>{(ids) => <Input {...ids} value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} />}</Field>
            <Field label={t("visitors.consentMode")}>{(ids) => <Select {...ids} value={form.consent_mode} onChange={(e) => setForm({ ...form, consent_mode: e.target.value as Mode })}>{(["opt_in", "opt_out", "cookieless_only"] as const).map((m) => <option key={m} value={m}>{t(`visitors.modes.${m}`)}</option>)}</Select>}</Field>
            <Button type="submit" disabled={!form.domain || create.isPending}>{t("visitors.add")}</Button>
          </form>
          <p className="mt-2 text-xs text-ink-soft">{t(`visitors.modeHelp.${form.consent_mode}`)}</p>
          {create.isError ? <ErrorState title={errorText(t, create.error)} /> : null}
        </Card>
      ) : null}
      <Card>
        {sites.isSuccess && list.length === 0 ? <EmptyState icon={Eye} title={t("visitors.noSites")} description={t("visitors.noSitesHelp")} /> : (
          <Table>
            <thead><tr><th>{t("visitors.domain")}</th><th>{t("visitors.consentMode")}</th><th>{t("visitors.status")}</th><th>{t("visitors.lastEvent")}</th></tr></thead>
            <tbody>
              {list.map((s) => (
                <tr key={s.id}>
                  <td><Link className="text-brand underline" to={visitorPath(`sites/${s.id}`)}>{s.domain}</Link></td>
                  <td>{t(`visitors.modes.${s.consent_mode}`)}</td>
                  <td><Badge tone={s.status === "active" ? "ok" : "neutral"}>{t(`visitors.siteStatus.${s.status}`)}</Badge></td>
                  <td>{s.last_event_at ? new Date(s.last_event_at).toLocaleString() : t("visitors.never")}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
