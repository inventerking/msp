import { unwrap } from "@msp/api-client";
import { Card, EmptyState, Select, Table } from "@msp/ui";
import { Inbox as InboxIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { outreachApi } from "../../../app/api";
import { useCan, useOMutation, useReplies } from "../api/outreach";

const classes = ["interested", "not_interested", "out_of_office", "unsubscribe_request", "other"] as const;

/** Replies to outreach. A reply stops that person's sequence at once. */
export function Inbox() {
  const { t } = useTranslation();
  const canWrite = useCan("outreach.write");
  const replies = useReplies();
  const classify = useOMutation((v: { id: string; classification: (typeof classes)[number] }) => unwrap(outreachApi.POST("/outreach/replies/{id}/classify", { params: { path: { id: v.id } }, body: { classification: v.classification } })));
  const list = replies.data?.data ?? [];
  return (
    <Card>
      {replies.isSuccess && list.length === 0 ? <EmptyState icon={InboxIcon} title={t("outreach.noReplies")} description={t("outreach.noRepliesHelp")} /> : (
        <Table>
          <thead><tr><th>{t("outreach.received")}</th><th>{t("outreach.reply")}</th><th>{t("outreach.classification")}</th></tr></thead>
          <tbody>
            {list.map((r) => (
              <tr key={r.id}>
                <td>{new Date(r.received_at).toLocaleString()}</td>
                <td className="whitespace-pre-wrap">{r.snippet}</td>
                <td>
                  {canWrite ? (
                    <Select aria-label={t("outreach.classification")} value={r.classification} onChange={(e) => classify.mutate({ id: r.id, classification: e.target.value as (typeof classes)[number] })}>
                      <option value="unclassified" disabled>{t("outreach.replyClass.unclassified")}</option>
                      {classes.map((c) => <option key={c} value={c}>{t(`outreach.replyClass.${c}`)}</option>)}
                    </Select>
                  ) : t(`outreach.replyClass.${r.classification}`)}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  );
}
