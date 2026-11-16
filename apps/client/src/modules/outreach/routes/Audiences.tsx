import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input, Select, Table } from "@msp/ui";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { crmApi, leadApi, outreachApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useAudiences, useCan, useOKey, useOMutation } from "../api/outreach";

/** Who a campaign may write to. People come from CRM or from a Lead Generation list. */
export function Audiences() {
  const { t } = useTranslation();
  const canWrite = useCan("outreach.write");
  const key = useOKey();
  const audiences = useAudiences();
  const contacts = useQuery({ queryKey: key("crm-contacts"), retry: false, queryFn: () => unwrap(crmApi.GET("/crm/contacts")) });
  const lists = useQuery({ queryKey: key("lead-lists"), retry: false, queryFn: () => unwrap(leadApi.GET("/lead-generation/lists")) });
  const [name, setName] = useState("");
  const [target, setTarget] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [listId, setListId] = useState("");
  const create = useOMutation(() => unwrap(outreachApi.POST("/outreach/audiences", { body: { name } })));
  const add = useOMutation(() => unwrap(outreachApi.POST("/outreach/audiences/{id}/members", { params: { path: { id: target } }, body: { contact_ids: picked, ...(listId ? { list_id: listId } : {}) } })));
  const people = (contacts.data?.data ?? []).filter((c) => c.email && !c.erased);
  return (
    <div className="space-y-4">
      {canWrite ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <Card className="p-4">
            <h2 className="mb-3 text-sm font-medium text-ink">{t("outreach.newAudience")}</h2>
            <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: (r) => { setName(""); setTarget(r.id); } }); }}>
              <div className="flex-1"><Field label={t("outreach.audienceName")}>{(ids) => <Input {...ids} value={name} onChange={(e) => setName(e.target.value)} />}</Field></div>
              <Button type="submit" disabled={!name || create.isPending}>{t("outreach.create")}</Button>
            </form>
          </Card>
          <Card className="p-4">
            <h2 className="mb-3 text-sm font-medium text-ink">{t("outreach.addPeople")}</h2>
            <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); add.mutate(undefined, { onSuccess: () => { setPicked([]); setListId(""); } }); }}>
              <Field label={t("outreach.audience")}>{(ids) => <Select {...ids} value={target} onChange={(e) => setTarget(e.target.value)}><option value="">{t("outreach.choose")}</option>{(audiences.data?.data ?? []).map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}</Select>}</Field>
              {lists.isSuccess && lists.data.data.length > 0 ? (
                <Field label={t("outreach.fromLeadList")}>{(ids) => <Select {...ids} value={listId} onChange={(e) => setListId(e.target.value)}><option value="">{t("outreach.none")}</option>{lists.data.data.map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}</Select>}</Field>
              ) : null}
              <fieldset className="max-h-48 overflow-y-auto rounded-md border border-line p-2">
                <legend className="px-1 text-xs text-ink-soft">{t("outreach.fromCRM")}</legend>
                {people.length === 0 ? <p className="text-xs text-ink-faint">{t("outreach.noCRMPeople")}</p> : null}
                {people.map((c) => (
                  <label key={c.id} className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={picked.includes(c.contact_id)} onChange={(e) => setPicked(e.target.checked ? [...picked, c.contact_id] : picked.filter((x) => x !== c.contact_id))} />
                    {c.full_name ?? c.email} <span className="text-xs text-ink-faint">{c.email}</span>
                  </label>
                ))}
              </fieldset>
              {add.isError ? <ErrorState title={errorText(t, add.error)} /> : null}
              {add.isSuccess ? <p role="status" className="text-sm text-ok">{t("outreach.added", { count: add.data.count })}</p> : null}
              <Button type="submit" disabled={!target || (picked.length === 0 && !listId) || add.isPending}>{t("outreach.addToAudience")}</Button>
            </form>
          </Card>
        </div>
      ) : null}
      <Card>
        <Table>
          <thead><tr><th>{t("outreach.audienceName")}</th><th>{t("outreach.members")}</th><th>{t("outreach.created")}</th></tr></thead>
          <tbody>{(audiences.data?.data ?? []).map((a) => <tr key={a.id}><td>{a.name}</td><td>{a.members}</td><td>{new Date(a.created_at).toLocaleDateString()}</td></tr>)}</tbody>
        </Table>
      </Card>
    </div>
  );
}
