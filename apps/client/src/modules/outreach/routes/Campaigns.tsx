import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { Send } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { outreachApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useApprovals, useAudiences, useCampaigns, useCan, useOMutation, useSenders } from "../api/outreach";
import { outreachPath } from "../paths";

export const statusTone = (s: string) => (s === "running" || s === "approved" ? "ok" : s === "pending_approval" || s === "paused" ? "warn" : "neutral");

/** Campaigns, and those waiting for the current user's approval. */
export function Campaigns() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canWrite = useCan("outreach.write");
  const canApprove = useCan("outreach.approve");
  const campaigns = useCampaigns();
  const approvals = useApprovals(canApprove);
  const senders = useSenders();
  const audiences = useAudiences();
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const [form, setForm] = useState({ name: "", sender_account_id: "", audience_id: "", postal_address: "", purpose: "" });
  const create = useOMutation(() => unwrap(outreachApi.POST("/outreach/campaigns", { body: { name: form.name, sender_account_id: form.sender_account_id, postal_address: form.postal_address, purpose: form.purpose, timezone: tz, ...(form.audience_id ? { audience_id: form.audience_id } : {}) } })));
  const list = campaigns.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canApprove && (approvals.data?.data.length ?? 0) > 0 ? (
        <Card className="p-4">
          <h2 className="mb-2 text-sm font-medium text-ink">{t("outreach.waitingApproval")}</h2>
          <ul className="space-y-1 text-sm">{approvals.data!.data.map((a) => <li key={a.id}><Link className="text-brand underline" to={outreachPath(`campaigns/${a.campaign_id}`)}>{a.campaign_name}</Link></li>)}</ul>
        </Card>
      ) : null}
      {canWrite ? (
        <Card className="p-4">
          <h2 className="mb-1 text-sm font-medium text-ink">{t("outreach.newCampaign")}</h2>
          <p className="mb-3 text-xs text-ink-soft">{t("outreach.newCampaignHelp")}</p>
          <form className="grid gap-2 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: (r) => navigate(outreachPath(`campaigns/${r.id}`)) }); }}>
            <Field label={t("outreach.campaignName")}>{(ids) => <Input {...ids} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />}</Field>
            <Field label={t("outreach.sender")}>{(ids) => <Select {...ids} value={form.sender_account_id} onChange={(e) => setForm({ ...form, sender_account_id: e.target.value })}><option value="">{t("outreach.choose")}</option>{(senders.data?.data ?? []).map((s) => <option key={s.id} value={s.id}>{s.from_name} &lt;{s.from_email}&gt;</option>)}</Select>}</Field>
            <Field label={t("outreach.audience")}>{(ids) => <Select {...ids} value={form.audience_id} onChange={(e) => setForm({ ...form, audience_id: e.target.value })}><option value="">{t("outreach.choose")}</option>{(audiences.data?.data ?? []).map((a) => <option key={a.id} value={a.id}>{a.name} ({a.members})</option>)}</Select>}</Field>
            <Field label={t("outreach.postalAddress")}>{(ids) => <Input {...ids} value={form.postal_address} onChange={(e) => setForm({ ...form, postal_address: e.target.value })} />}</Field>
            <div className="sm:col-span-2"><Field label={t("outreach.purpose")}>{(ids) => <Input {...ids} value={form.purpose} onChange={(e) => setForm({ ...form, purpose: e.target.value })} />}</Field></div>
            {create.isError ? <div className="sm:col-span-2"><ErrorState title={errorText(t, create.error)} /></div> : null}
            <div><Button type="submit" disabled={!form.name || !form.sender_account_id || form.postal_address.length < 5 || create.isPending}>{t("outreach.createCampaign")}</Button></div>
          </form>
        </Card>
      ) : null}
      <Card>
        {campaigns.isSuccess && list.length === 0 ? <EmptyState icon={Send} title={t("outreach.noCampaigns")} description={t("outreach.noCampaignsHelp")} /> : (
          <Table>
            <thead><tr><th>{t("outreach.campaignName")}</th><th>{t("outreach.status")}</th><th>{t("outreach.created")}</th></tr></thead>
            <tbody>
              {list.map((c) => (
                <tr key={c.id}>
                  <td><Link className="text-brand underline" to={outreachPath(`campaigns/${c.id}`)}>{c.name}</Link></td>
                  <td><Badge tone={statusTone(c.status)}>{t(`outreach.campaignStatus.${c.status}`)}</Badge></td>
                  <td>{new Date(c.created_at).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
