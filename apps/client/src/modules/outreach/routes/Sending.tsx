import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { outreachApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCan, useDomains, useOMutation, useSenders } from "../api/outreach";

function Check({ value }: { value: string }) {
  const { t } = useTranslation();
  const tone = value === "pass" ? "ok" : value === "none_policy" ? "warn" : "neutral";
  return <Badge tone={tone}>{t(`outreach.check.${value}`, { defaultValue: value })}</Badge>;
}

/** Sending domains and from-addresses. A domain must pass SPF, DKIM and DMARC before it can send. */
export function Sending() {
  const { t } = useTranslation();
  const canConfigure = useCan("outreach.configure");
  const domains = useDomains();
  const senders = useSenders();
  const [domain, setDomain] = useState("");
  const [sender, setSender] = useState({ sending_domain_id: "", from_email: "", from_name: "", daily_limit: 50 });
  const addDomain = useOMutation(() => unwrap(outreachApi.POST("/outreach/domains", { body: { domain } })));
  const check = useOMutation((id: string) => unwrap(outreachApi.POST("/outreach/domains/{id}/check", { params: { path: { id } } })));
  const addSender = useOMutation(() => unwrap(outreachApi.POST("/outreach/senders", { body: sender })));
  const setStatus = useOMutation((v: { id: string; status: "active" | "paused" }) => unwrap(outreachApi.PATCH("/outreach/senders/{id}", { params: { path: { id: v.id } }, body: { status: v.status } })));
  return (
    <div className="space-y-4">
      <p role="note" className="rounded-md bg-sunken p-3 text-sm text-ink-soft">{t("outreach.sinkNotice")}</p>
      <Card>
        <h2 className="px-4 pt-4 text-sm font-medium text-ink">{t("outreach.domains")}</h2>
        {canConfigure ? (
          <form className="flex items-end gap-2 p-4" onSubmit={(e) => { e.preventDefault(); addDomain.mutate(undefined, { onSuccess: () => setDomain("") }); }}>
            <div className="flex-1"><Field label={t("outreach.domain")}>{(ids) => <Input {...ids} value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="mail.example.com" />}</Field></div>
            <Button type="submit" disabled={!domain || addDomain.isPending}>{t("outreach.addDomain")}</Button>
          </form>
        ) : null}
        {addDomain.isError ? <div className="px-4"><ErrorState title={errorText(t, addDomain.error)} /></div> : null}
        <Table>
          <thead><tr><th>{t("outreach.domain")}</th><th>SPF</th><th>DKIM</th><th>DMARC</th><th>{t("outreach.ready")}</th><th /></tr></thead>
          <tbody>
            {(domains.data?.data ?? []).map((d) => (
              <tr key={d.id}>
                <td>{d.domain}</td><td><Check value={d.spf_status} /></td><td><Check value={d.dkim_status} /></td><td><Check value={d.dmarc_status} /></td>
                <td>{d.authenticated ? <Badge tone="ok">{t("outreach.authenticated")}</Badge> : <Badge tone="warn">{t("outreach.notAuthenticated")}</Badge>}</td>
                <td>{canConfigure ? <Button size="sm" variant="ghost" disabled={check.isPending} onClick={() => check.mutate(d.id)}>{t("outreach.recheck")}</Button> : null}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
      <Card>
        <h2 className="px-4 pt-4 text-sm font-medium text-ink">{t("outreach.senders")}</h2>
        {canConfigure ? (
          <form className="grid gap-2 p-4 sm:grid-cols-5 sm:items-end" onSubmit={(e) => { e.preventDefault(); addSender.mutate(undefined, { onSuccess: () => setSender({ ...sender, from_email: "", from_name: "" }) }); }}>
            <Field label={t("outreach.domain")}>{(ids) => <Select {...ids} value={sender.sending_domain_id} onChange={(e) => setSender({ ...sender, sending_domain_id: e.target.value })}><option value="">{t("outreach.choose")}</option>{(domains.data?.data ?? []).map((d) => <option key={d.id} value={d.id}>{d.domain}</option>)}</Select>}</Field>
            <Field label={t("outreach.fromEmail")}>{(ids) => <Input {...ids} type="email" value={sender.from_email} onChange={(e) => setSender({ ...sender, from_email: e.target.value })} />}</Field>
            <Field label={t("outreach.fromName")}>{(ids) => <Input {...ids} value={sender.from_name} onChange={(e) => setSender({ ...sender, from_name: e.target.value })} />}</Field>
            <Field label={t("outreach.dailyLimit")}>{(ids) => <Input {...ids} type="number" min={1} max={2000} value={sender.daily_limit} onChange={(e) => setSender({ ...sender, daily_limit: Number(e.target.value) })} />}</Field>
            <Button type="submit" disabled={!sender.sending_domain_id || !sender.from_email || !sender.from_name || addSender.isPending}>{t("outreach.addSender")}</Button>
          </form>
        ) : null}
        {addSender.isError ? <div className="px-4"><ErrorState title={errorText(t, addSender.error)} /></div> : null}
        <Table>
          <thead><tr><th>{t("outreach.fromEmail")}</th><th>{t("outreach.fromName")}</th><th>{t("outreach.dailyLimit")}</th><th>{t("outreach.status")}</th><th /></tr></thead>
          <tbody>
            {(senders.data?.data ?? []).map((s) => (
              <tr key={s.id}>
                <td>{s.from_email}</td><td>{s.from_name}</td><td>{s.daily_limit}</td>
                <td><Badge tone={s.status === "active" ? "ok" : "neutral"}>{t(`outreach.senderStatus.${s.status}`, { defaultValue: s.status })}</Badge></td>
                <td>{canConfigure ? <Button size="sm" variant="ghost" onClick={() => setStatus.mutate({ id: s.id, status: s.status === "active" ? "paused" : "active" })}>{s.status === "active" ? t("outreach.pause") : t("outreach.resume")}</Button> : null}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
    </div>
  );
}
