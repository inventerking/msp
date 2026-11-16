import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input, Table } from "@msp/ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { outreachApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCan, useOMutation, useSuppressions } from "../api/outreach";

/** Addresses that are never emailed. Only keyed hashes are kept, so addresses are not listed. */
export function Suppressions() {
  const { t } = useTranslation();
  const canWrite = useCan("outreach.write");
  const canRemove = useCan("outreach.suppression_remove");
  const list = useSuppressions();
  const [email, setEmail] = useState("");
  const add = useOMutation(() => unwrap(outreachApi.POST("/outreach/suppressions", { body: { email } })));
  const remove = useOMutation((id: string) => unwrap(outreachApi.DELETE("/outreach/suppressions/{id}", { params: { path: { id } } })));
  return (
    <div className="space-y-4">
      <p className="text-sm text-ink-soft">{t("outreach.suppressionHelp")}</p>
      {canWrite ? (
        <Card className="p-4">
          <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); add.mutate(undefined, { onSuccess: () => setEmail("") }); }}>
            <div className="flex-1"><Field label={t("outreach.emailToSuppress")}>{(ids) => <Input {...ids} type="email" value={email} onChange={(e) => setEmail(e.target.value)} />}</Field></div>
            <Button type="submit" disabled={!email || add.isPending}>{t("outreach.suppress")}</Button>
          </form>
          {add.isError ? <ErrorState title={errorText(t, add.error)} /> : null}
        </Card>
      ) : null}
      {remove.isError ? <ErrorState title={errorText(t, remove.error)} /> : null}
      <Card>
        <Table>
          <thead><tr><th>{t("outreach.reason")}</th><th>{t("outreach.source")}</th><th>{t("outreach.created")}</th><th /></tr></thead>
          <tbody>
            {(list.data?.data ?? []).map((s) => (
              <tr key={s.id}>
                <td>{t(`outreach.suppressReason.${s.reason}`, { defaultValue: s.reason })}</td><td>{s.source}</td><td>{new Date(s.created_at).toLocaleDateString()}</td>
                <td>{canRemove && s.reason === "manual" ? <Button size="sm" variant="ghost" onClick={() => remove.mutate(s.id)}>{t("common.remove")}</Button> : null}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Card>
    </div>
  );
}
