import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { assessmentApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useAMutation, useAuthorizations, useVerifications } from "../api/assessments";

/** Who allowed checks on which domain. The exposure tier needs proof of control of the domain. */
export function Authorizations() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const canRun = Boolean(me?.permissions.includes("assessment.run"));
  const auths = useAuthorizations();
  const verifications = useVerifications();
  const [form, setForm] = useState({ domain: "", tier: "passive" as "passive" | "exposure", authorizer_name: "", authorizer_email: "", authorizer_role: "", verification: "", attested: false });
  const [verifyDomain, setVerifyDomain] = useState("");
  const [instructions, setInstructions] = useState<string>();
  const create = useAMutation(() => unwrap(assessmentApi.POST("/it-assessments/authorizations", { body: { domain: form.domain, tier: form.tier, authorizer_name: form.authorizer_name, authorizer_email: form.authorizer_email, attested: form.attested,
    ...(form.authorizer_role ? { authorizer_role: form.authorizer_role } : {}), ...(form.verification ? { domain_verification_id: form.verification } : {}) } })));
  const startVerify = useAMutation(() => unwrap(assessmentApi.POST("/it-assessments/domain-verifications", { body: { domain: verifyDomain, method: "dns_txt" } })));
  const check = useAMutation((id: string) => unwrap(assessmentApi.POST("/it-assessments/domain-verifications/{id}/check", { params: { path: { id } } })));
  const revoke = useAMutation((id: string) => unwrap(assessmentApi.POST("/it-assessments/authorizations/{id}/revoke", { params: { path: { id } } })));
  const verified = (verifications.data?.data ?? []).filter((v) => v.status === "verified");
  return (
    <div className="space-y-4">
      {canRun ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card className="p-4">
            <h2 className="mb-1 text-sm font-medium text-ink">{t("assess.newAuthorization")}</h2>
            <p className="mb-3 text-xs text-ink-soft">{t("assess.authorizationNote")}</p>
            <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: () => setForm({ ...form, domain: "", attested: false }) }); }}>
              <div className="grid gap-2 sm:grid-cols-2">
                <Field label={t("assess.domain")}>{(ids) => <Input {...ids} value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} />}</Field>
                <Field label={t("assess.tier")}>{(ids) => <Select {...ids} value={form.tier} onChange={(e) => setForm({ ...form, tier: e.target.value as "passive" | "exposure" })}><option value="passive">{t("assess.tiers.passive")}</option><option value="exposure">{t("assess.tiers.exposure")}</option></Select>}</Field>
                <Field label={t("assess.authorizerName")}>{(ids) => <Input {...ids} value={form.authorizer_name} onChange={(e) => setForm({ ...form, authorizer_name: e.target.value })} />}</Field>
                <Field label={t("assess.authorizerEmail")}>{(ids) => <Input {...ids} type="email" value={form.authorizer_email} onChange={(e) => setForm({ ...form, authorizer_email: e.target.value })} />}</Field>
                <Field label={t("assess.authorizerRole")}>{(ids) => <Input {...ids} value={form.authorizer_role} onChange={(e) => setForm({ ...form, authorizer_role: e.target.value })} />}</Field>
                {form.tier === "exposure" ? (
                  <Field label={t("assess.verification")}>{(ids) => <Select {...ids} value={form.verification} onChange={(e) => setForm({ ...form, verification: e.target.value })}><option value="">{t("assess.chooseVerification")}</option>{verified.map((v) => <option key={v.id} value={v.id}>{v.domain}</option>)}</Select>}</Field>
                ) : null}
              </div>
              <p className="text-xs text-ink-soft">{t(`assess.tierHelp.${form.tier}`)}</p>
              <label className="flex items-start gap-2 text-sm text-ink"><input type="checkbox" className="mt-1" checked={form.attested} onChange={(e) => setForm({ ...form, attested: e.target.checked })} /><span>{t("assess.attest")}</span></label>
              {create.isError ? <ErrorState title={errorText(t, create.error)} /> : null}
              <Button type="submit" disabled={!form.attested || !form.domain || !form.authorizer_name || !form.authorizer_email || create.isPending}>{t("assess.recordAuthorization")}</Button>
            </form>
          </Card>
          <Card className="p-4">
            <h2 className="mb-1 text-sm font-medium text-ink">{t("assess.verifyDomain")}</h2>
            <p className="mb-3 text-xs text-ink-soft">{t("assess.verifyNote")}</p>
            <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); startVerify.mutate(undefined, { onSuccess: (v) => setInstructions(v.instructions) }); }}>
              <div className="flex-1"><Field label={t("assess.domain")}>{(ids) => <Input {...ids} value={verifyDomain} onChange={(e) => setVerifyDomain(e.target.value)} />}</Field></div>
              <Button type="submit" variant="secondary" disabled={!verifyDomain || startVerify.isPending}>{t("assess.start")}</Button>
            </form>
            {instructions ? <p role="status" className="mt-2 rounded-md bg-sunken p-2 font-mono text-xs break-all">{instructions}</p> : null}
            <ul className="mt-3 space-y-1 text-sm">
              {(verifications.data?.data ?? []).map((v) => (
                <li key={v.id} className="flex items-center justify-between gap-2">
                  <span>{v.domain} <Badge tone={v.status === "verified" ? "ok" : "neutral"}>{t(`assess.verificationStatus.${v.status}`)}</Badge></span>
                  {v.status === "pending" ? <Button size="sm" variant="ghost" disabled={check.isPending} onClick={() => check.mutate(v.id)}>{t("assess.checkNow")}</Button> : null}
                </li>
              ))}
            </ul>
          </Card>
        </div>
      ) : null}
      <Card>
        <Table>
          <thead><tr><th>{t("assess.domain")}</th><th>{t("assess.tier")}</th><th>{t("assess.authorizedBy")}</th><th>{t("assess.status")}</th><th /></tr></thead>
          <tbody>
            {(auths.data?.data ?? []).map((a) => (
              <tr key={a.id}>
                <td>{a.domain}</td><td>{t(`assess.tiers.${a.tier}`)}</td>
                <td>{a.authorizer_name}<div className="text-xs text-ink-faint">{a.authorizer_email} · {t(`assess.methods.${a.method}`)}</div></td>
                <td><Badge tone={a.status === "active" ? "ok" : "neutral"}>{t(`assess.authStatus.${a.status}`)}</Badge><div className="text-xs text-ink-faint">{t("assess.until", { date: new Date(a.expires_at).toLocaleDateString() })}</div></td>
                <td>{a.status === "active" && canRun ? <Button size="sm" variant="ghost" onClick={() => revoke.mutate(a.id)}>{t("assess.revoke")}</Button> : null}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
    </div>
  );
}
