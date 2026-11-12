import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, SkeletonRows, Table } from "@msp/ui";
import { FileSignature } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { proposalApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { proposalPath } from "../paths";
import { money, usePropMutation, useProposals } from "../api/proposals";

export function Proposals() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const list = useProposals();
  const [title, setTitle] = useState("");
  const [customer, setCustomer] = useState("");
  const create = usePropMutation(() => unwrap(proposalApi.POST("/proposals/proposals", { body: { title, new_company: { name: customer } } })));
  const rows = list.data?.data ?? [];
  return (
    <div className="space-y-4">
      {me?.permissions.includes("proposal.write") ? (
        <Card className="p-4">
          <form className="grid gap-3 sm:grid-cols-[2fr_2fr_auto] sm:items-end" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: (p) => void navigate(proposalPath(p.id)) }); }}>
            <Field label={t("proposals.title")}>{(ids) => <Input {...ids} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
            <Field label={t("proposals.customer")} hint={t("proposals.customerHint")}>{(ids) => <Input {...ids} value={customer} onChange={(e) => setCustomer(e.target.value)} />}</Field>
            <Button type="submit" disabled={!title.trim() || !customer.trim() || create.isPending}>{t("proposals.create")}</Button>
          </form>
          {create.isError ? <div className="mt-2"><ErrorState title={errorText(t, create.error)} /></div> : null}
        </Card>
      ) : null}
      <Card>
        {list.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {list.isError ? <div className="p-4"><ErrorState title={errorText(t, list.error)} /></div> : null}
        {list.isSuccess && rows.length === 0 ? <EmptyState icon={FileSignature} title={t("proposals.none")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("proposals.number")}</th><th>{t("proposals.title")}</th><th>{t("proposals.status")}</th><th>{t("proposals.monthly")}</th><th>{t("proposals.tcv")}</th></tr></thead>
            <tbody>
              {rows.map((p) => (
                <tr key={p.id}>
                  <td className="whitespace-nowrap text-ink-soft">{p.number}</td>
                  <td><Link className="font-medium text-ink hover:underline" to={proposalPath(p.id)}>{p.title}</Link></td>
                  <td><Badge tone={p.status === "signed" ? "ok" : p.status === "declined" ? "danger" : "neutral"}>{t(`proposals.statuses.${p.status}`)}</Badge></td>
                  <td>{p.currency ? money(p.mrr_total_minor, p.currency) : "—"}</td>
                  <td>{p.currency ? money(p.tcv_minor, p.currency) : "—"}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}
