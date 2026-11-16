import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, PageHeader, Select, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, errorText, hasRole, useAdmin } from "../app/api";

type Rule = "opt_out_allowed" | "consent_required" | "blocked";
type Reason = "legal_request" | "abuse" | "complaint";

/** Outreach rules for the whole platform: which countries may receive email, and on what basis,
 * and addresses no workspace may email. These come from legal advice and change without a release. */
export function OutreachPolicy() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const qc = useQueryClient();
  const isSuper = hasRole(me, "super_admin");
  const rules = useQuery({ queryKey: ["jurisdiction-rules"], queryFn: () => unwrap(api.GET("/outreach/jurisdiction-rules")) });
  const count = useQuery({ queryKey: ["global-suppressions"], queryFn: () => unwrap(api.GET("/outreach/global-suppressions")) });
  const [form, setForm] = useState({ country: "", rule: "opt_out_allowed" as Rule, note: "" });
  const [sup, setSup] = useState({ email: "", reason: "legal_request" as Reason });
  const refresh = () => qc.invalidateQueries({ queryKey: ["jurisdiction-rules"] });
  const set = useMutation({ mutationFn: () => unwrap(api.PUT("/outreach/jurisdiction-rules/{countryCode}", { params: { path: { countryCode: form.country } }, body: { rule: form.rule, ...(form.note ? { note: form.note } : {}) } })), onSuccess: () => { setForm({ ...form, country: "", note: "" }); void refresh(); } });
  const remove = useMutation({ mutationFn: (c: string) => unwrap(api.DELETE("/outreach/jurisdiction-rules/{countryCode}", { params: { path: { countryCode: c } } })), onSuccess: refresh });
  const suppress = useMutation({ mutationFn: () => unwrap(api.POST("/outreach/global-suppressions", { body: sup })), onSuccess: () => { setSup({ ...sup, email: "" }); void qc.invalidateQueries({ queryKey: ["global-suppressions"] }); } });
  const failure = [set, remove, suppress].find((m) => m.isError)?.error;
  return (
    <>
      <PageHeader title={t("outreachPolicy.title")} description={t("outreachPolicy.subtitle")} />
      {failure ? <div className="mb-4"><ErrorState title={errorText(t, failure)} /></div> : null}
      <p role="note" className="mb-4 rounded-md bg-sunken p-3 text-sm text-ink-soft">{t("outreachPolicy.defaultBlocked")}</p>
      {isSuper ? (
        <Card className="mb-4 p-4">
          <form className="grid gap-2 sm:grid-cols-4 sm:items-end" onSubmit={(e) => { e.preventDefault(); set.mutate(); }}>
            <Field label={t("outreachPolicy.country")}>{(ids) => <Input {...ids} maxLength={2} value={form.country} onChange={(e) => setForm({ ...form, country: e.target.value.toUpperCase() })} placeholder="GB" />}</Field>
            <Field label={t("outreachPolicy.rule")}>{(ids) => <Select {...ids} value={form.rule} onChange={(e) => setForm({ ...form, rule: e.target.value as Rule })}>{(["opt_out_allowed", "consent_required", "blocked"] as const).map((r) => <option key={r} value={r}>{t(`outreachPolicy.rules.${r}`)}</option>)}</Select>}</Field>
            <Field label={t("outreachPolicy.note")}>{(ids) => <Input {...ids} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />}</Field>
            <Button type="submit" disabled={form.country.length !== 2 || set.isPending}>{t("outreachPolicy.save")}</Button>
          </form>
        </Card>
      ) : null}
      <Card className="mb-6">
        {rules.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {rules.data ? (
          <Table>
            <thead><tr><th>{t("outreachPolicy.country")}</th><th>{t("outreachPolicy.rule")}</th><th>{t("outreachPolicy.note")}</th><th>{t("outreachPolicy.updated")}</th>{isSuper ? <th /> : null}</tr></thead>
            <tbody>
              {rules.data.data.map((r) => (
                <tr key={r.country_code}>
                  <td>{r.country_code}</td>
                  <td><Badge tone={r.rule === "blocked" ? "neutral" : r.rule === "consent_required" ? "warn" : "ok"}>{t(`outreachPolicy.rules.${r.rule}`)}</Badge></td>
                  <td className="text-sm">{r.note}</td>
                  <td className="text-xs text-ink-soft">{new Date(r.updated_at).toLocaleString()}</td>
                  {isSuper ? <td><Button size="sm" variant="ghost" onClick={() => remove.mutate(r.country_code)}>{t("outreachPolicy.remove")}</Button></td> : null}
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
      <h2 className="mb-1 text-base font-medium text-ink">{t("outreachPolicy.globalTitle")}</h2>
      <p className="mb-3 text-sm text-ink-soft">{t("outreachPolicy.globalHelp", { count: count.data?.count ?? 0 })}</p>
      <Card className="p-4">
        <form className="grid gap-2 sm:grid-cols-3 sm:items-end" onSubmit={(e) => { e.preventDefault(); suppress.mutate(); }}>
          <Field label={t("outreachPolicy.email")}>{(ids) => <Input {...ids} type="email" value={sup.email} onChange={(e) => setSup({ ...sup, email: e.target.value })} />}</Field>
          <Field label={t("outreachPolicy.reason")}>{(ids) => <Select {...ids} value={sup.reason} onChange={(e) => setSup({ ...sup, reason: e.target.value as Reason })}>{(["legal_request", "abuse", "complaint"] as const).map((r) => <option key={r} value={r}>{t(`outreachPolicy.reasons.${r}`)}</option>)}</Select>}</Field>
          <Button type="submit" disabled={!sup.email || suppress.isPending}>{t("outreachPolicy.addGlobal")}</Button>
        </form>
        {suppress.isSuccess ? <p role="status" className="mt-2 text-sm text-ok">{t("outreachPolicy.added")}</p> : null}
      </Card>
    </>
  );
}
