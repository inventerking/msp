import { unwrap } from "@msp/api-client";
import { Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, Input, SkeletonRows, Table } from "@msp/ui";
import { ListChecks } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { leadPath } from "../paths";
import { Link } from "react-router";
import { leadApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useLeadMutation, useLists } from "../api/leads";

export function Lists() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const canWrite = Boolean(me?.permissions.includes("lead.write"));
  const lists = useLists();
  const [name, setName] = useState("");
  const [removing, setRemoving] = useState<{ id: string; name: string }>();
  const create = useLeadMutation((n: string) => unwrap(leadApi.POST("/lead-generation/lists", { body: { name: n } })));
  const remove = useLeadMutation((id: string) => unwrap(leadApi.DELETE("/lead-generation/lists/{id}", { params: { path: { id } } })));
  const rows = lists.data?.data ?? [];
  return (
    <div className="space-y-4">
      {canWrite ? (
        <Card className="p-4">
          <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); if (name.trim()) create.mutate(name.trim(), { onSuccess: () => setName("") }); }}>
            <div className="min-w-64 flex-1">
              <Field label={t("leads.newList")} error={create.isError ? errorText(t, create.error) : undefined}>{(ids) => <Input {...ids} value={name} onChange={(e) => setName(e.target.value)} />}</Field>
            </div>
            <Button type="submit" disabled={create.isPending}>{t("leads.createList")}</Button>
          </form>
        </Card>
      ) : null}
      <Card>
        {lists.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {lists.isError ? <div className="p-4"><ErrorState title={errorText(t, lists.error)} /></div> : null}
        {lists.isSuccess && rows.length === 0 ? <EmptyState icon={ListChecks} title={t("leads.noLists")} description={t("leads.noListsBody")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("leads.listName")}</th><th>{t("leads.members")}</th><th /></tr></thead>
            <tbody>
              {rows.map((l) => (
                <tr key={l.id}>
                  <td><Link className="font-medium text-ink hover:underline" to={`${leadPath()}?list=${l.id}`}>{l.name}</Link></td>
                  <td>{l.member_count}</td>
                  <td className="text-right">{canWrite ? <Button size="sm" variant="ghost" onClick={() => setRemoving({ id: l.id, name: l.name })}>{t("common.remove")}</Button> : null}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
      <ConfirmDialog
        open={Boolean(removing)}
        title={t("leads.removeList", { name: removing?.name ?? "" })}
        description={t("leads.removeListBody")}
        confirmLabel={t("common.remove")}
        cancelLabel={t("common.cancel")}
        danger
        busy={remove.isPending}
        error={remove.isError ? errorText(t, remove.error) : undefined}
        onCancel={() => setRemoving(undefined)}
        onConfirm={() => { if (removing) remove.mutate(removing.id, { onSuccess: () => setRemoving(undefined) }); }}
      />
    </div>
  );
}
