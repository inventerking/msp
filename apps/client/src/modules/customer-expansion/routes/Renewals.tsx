import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Select, SkeletonRows, Table } from "@msp/ui";
import { CalendarClock } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { expansionApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { money, useCan, useRenewals, useXMutation } from "../api/expansion";
import { expansionPath } from "../paths";

/** Contract ends and notice deadlines coming up, raised 90, 60 and 30 days ahead. */
export function Renewals() {
  const { t } = useTranslation();
  const canWrite = useCan("expansion.write");
  const [status, setStatus] = useState<"open" | "acknowledged" | "resolved" | "">("open");
  const renewals = useRenewals(status || undefined);
  const ack = useXMutation((id: string) => unwrap(expansionApi.POST("/customer-expansion/renewal-alerts/{id}/acknowledge", { params: { path: { id } } })));
  const list = renewals.data?.data ?? [];
  const daysLeft = (d: string) => Math.round((new Date(d).getTime() - Date.now()) / 86_400_000);
  return (
    <div className="space-y-4">
      <Field label={t("expansion.status")}>{(ids) => (
        <Select {...ids} value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
          <option value="">{t("expansion.allStatuses")}</option>
          {(["open", "acknowledged", "resolved"] as const).map((s) => <option key={s} value={s}>{t(`expansion.alertStatus.${s}`)}</option>)}
        </Select>
      )}</Field>
      <Card>
        {renewals.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {renewals.isError ? <div className="p-4"><ErrorState title={errorText(t, renewals.error)} onRetry={() => void renewals.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {renewals.isSuccess && list.length === 0 ? <EmptyState icon={CalendarClock} title={t("expansion.noRenewals")} /> : null}
        {list.length ? (
          <Table>
            <thead><tr><th>{t("expansion.customer")}</th><th>{t("expansion.alertKind")}</th><th>{t("expansion.due")}</th><th className="text-right">{t("expansion.mrr")}</th><th>{t("expansion.status")}</th><th><span className="sr-only">{t("expansion.actions")}</span></th></tr></thead>
            <tbody>
              {list.map((a) => (
                <tr key={a.id}>
                  <td><Link className="text-brand underline" to={expansionPath(`customers/${a.customer_id}`)}>{a.customer_name}</Link></td>
                  <td>{t(`expansion.alertKinds.${a.kind}`)}{a.auto_renew ? <div className="text-xs text-ink-faint">{t("expansion.autoRenews")}</div> : null}</td>
                  <td>{new Date(a.due_date).toLocaleDateString()} <span className="text-xs text-ink-soft">{t("expansion.inDays", { count: daysLeft(a.due_date) })}</span></td>
                  <td className="text-right tabular-nums">{money(a.mrr_minor, a.currency)}</td>
                  <td><Badge tone={a.status === "open" ? "warn" : "neutral"}>{t(`expansion.alertStatus.${a.status}`)}</Badge></td>
                  <td className="text-right">{canWrite && a.status === "open" ? <Button size="sm" variant="secondary" disabled={ack.isPending} onClick={() => ack.mutate(a.id)}>{t("expansion.acknowledge")}</Button> : null}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
        {ack.isError ? <div className="p-4"><ErrorState title={errorText(t, ack.error)} /></div> : null}
      </Card>
    </div>
  );
}
