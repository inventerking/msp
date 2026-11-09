import { fetchFile, saveBlob, unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select } from "@msp/ui";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, leadApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { StatusBadge } from "../components/Labels";
import { pollWhile, useLeadKey, useLeadMutation, useLists } from "../api/leads";

const mappingFields = ["domain", "name", "country", "industry", "employees"] as const;
type MappingField = (typeof mappingFields)[number];

/** Reads the header row, so columns can be picked rather than typed. */
async function headers(file: File): Promise<string[]> {
  const first = (await file.slice(0, 8192).text()).split(/\r?\n/)[0] ?? "";
  return first.replace(/^﻿/, "").split(",").map((h) => h.trim().replace(/^"|"$/g, "")).filter(Boolean);
}

async function upload(file: File): Promise<string> {
  const created = await unwrap(api.POST("/files/uploads", { body: { purpose: "csv_import", file_name: file.name, size_bytes: file.size } }));
  await unwrap(api.PUT("/files/uploads/{id}/content", { params: { path: { id: created.id } }, body: file as never, bodySerializer: (b: unknown) => b as BodyInit, headers: { "Content-Type": "application/octet-stream" } }));
  return created.id;
}

/** Downloads through the API, so the session and permission checks apply. */
async function download(path: string, name: string) {
  saveBlob(await fetchFile(path), name);
}

function Import() {
  const { t } = useTranslation();
  const key = useLeadKey();
  const lists = useLists();
  const [file, setFile] = useState<File>();
  const [cols, setCols] = useState<string[]>([]);
  const [mapping, setMapping] = useState<Partial<Record<MappingField, string>>>({});
  const [listId, setListId] = useState("");
  const [attested, setAttested] = useState(false);
  const [importId, setImportId] = useState<string>();
  const start = useLeadMutation(async () => {
    if (!file || !mapping.domain) throw new Error("missing");
    const fileId = await upload(file);
    // The file is scanned before it can be read, so the import waits for that in the background.
    const body = { file_id: fileId, lawful_source_attested: true as const, mapping: { domain: mapping.domain, ...Object.fromEntries(Object.entries(mapping).filter(([k, v]) => k !== "domain" && v)) }, ...(listId ? { list_id: listId } : {}) };
    return unwrap(leadApi.POST("/lead-generation/imports", { body }));
  });
  const status = useQuery({
    queryKey: key("import", importId),
    enabled: Boolean(importId),
    queryFn: () => unwrap(leadApi.GET("/lead-generation/imports/{id}", { params: { path: { id: importId ?? "" } } })),
    refetchInterval: (q) => pollWhile(q.state.data?.status),
  });
  const imp = status.data;
  return (
    <Card className="p-4">
      <h2 className="text-sm font-medium text-ink">{t("leads.importTitle")}</h2>
      <p className="mb-4 text-sm text-ink-soft">{t("leads.importBody")}</p>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); start.mutate(undefined, { onSuccess: (r) => setImportId(r.id) }); }}>
        <Field label={t("leads.csvFile")}>
          {(ids) => <Input {...ids} type="file" accept=".csv,text/csv" onChange={(e) => { const f = e.target.files?.[0]; setFile(f); setMapping({}); if (f) void headers(f).then(setCols); }} />}
        </Field>
        {cols.length ? (
          <div className="grid gap-3 sm:grid-cols-3">
            {mappingFields.map((f) => (
              <Field key={f} label={t(`leads.map.${f}`)} {...(f === "domain" ? { hint: t("leads.required") } : {})}>
                {(ids) => (
                  <Select {...ids} value={mapping[f] ?? ""} onChange={(e) => setMapping((m) => ({ ...m, [f]: e.target.value }))}>
                    <option value="">{t("leads.notMapped")}</option>
                    {cols.map((c) => <option key={c} value={c}>{c}</option>)}
                  </Select>
                )}
              </Field>
            ))}
            <Field label={t("leads.addToList")}>
              {(ids) => (
                <Select {...ids} value={listId} onChange={(e) => setListId(e.target.value)}>
                  <option value="">{t("leads.noList")}</option>
                  {(lists.data?.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
                </Select>
              )}
            </Field>
          </div>
        ) : null}
        <label className="flex items-start gap-2 text-sm text-ink">
          <input type="checkbox" className="mt-1" checked={attested} onChange={(e) => setAttested(e.target.checked)} />
          <span>{t("leads.attest")}</span>
        </label>
        {start.isError ? <ErrorState title={errorText(t, start.error)} /> : null}
        <Button type="submit" disabled={!file || !mapping.domain || !attested || start.isPending}>{t("leads.startImport")}</Button>
      </form>
      {imp ? (
        <div className="mt-4 rounded-md border border-line p-3 text-sm" role="status">
          <div className="mb-2 flex items-center gap-2"><StatusBadge status={imp.status} /></div>
          {imp.rows_total !== undefined ? (
            <p>{t("leads.importResult", { total: imp.rows_total, imported: imp.rows_imported ?? 0, duplicate: imp.rows_duplicate ?? 0, rejected: imp.rows_rejected ?? 0 })}</p>
          ) : null}
          {imp.report_file_id ? <Button className="mt-2" size="sm" variant="secondary" onClick={() => void download(`/lead-generation/imports/${imp.id}/report`, "import-report.csv")}>{t("leads.downloadReport")}</Button> : null}
        </div>
      ) : null}
    </Card>
  );
}

function Export() {
  const { t } = useTranslation();
  const key = useLeadKey();
  const lists = useLists();
  const [listId, setListId] = useState("");
  const [exportId, setExportId] = useState<string>();
  const start = useLeadMutation(() => unwrap(leadApi.POST("/lead-generation/exports", { body: listId ? { list_id: listId } : {} })));
  const status = useQuery({
    queryKey: key("export", exportId),
    enabled: Boolean(exportId),
    queryFn: () => unwrap(leadApi.GET("/lead-generation/exports/{id}", { params: { path: { id: exportId ?? "" } } })),
    refetchInterval: (q) => pollWhile(q.state.data?.status),
  });
  const exp = status.data;
  return (
    <Card className="p-4">
      <h2 className="text-sm font-medium text-ink">{t("leads.exportTitle")}</h2>
      <p className="mb-4 text-sm text-ink-soft">{t("leads.exportBody")}</p>
      <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); start.mutate(undefined, { onSuccess: (r) => setExportId(r.id) }); }}>
        <div className="min-w-56">
          <Field label={t("leads.exportWhat")}>
            {(ids) => (
              <Select {...ids} value={listId} onChange={(e) => setListId(e.target.value)}>
                <option value="">{t("leads.allProspects")}</option>
                {(lists.data?.data ?? []).map((l) => <option key={l.id} value={l.id}>{l.name}</option>)}
              </Select>
            )}
          </Field>
        </div>
        <Button type="submit" disabled={start.isPending}>{t("leads.startExport")}</Button>
      </form>
      {start.isError ? <div className="mt-3"><ErrorState title={errorText(t, start.error)} /></div> : null}
      {exp ? (
        <div className="mt-4 rounded-md border border-line p-3 text-sm" role="status">
          <div className="mb-2 flex items-center gap-2"><StatusBadge status={exp.status} />{exp.rows_total !== undefined ? <span>{t("leads.rows", { count: exp.rows_total })}</span> : null}</div>
          {exp.values_withheld ? <p className="mb-2"><Badge tone="warn">{t("leads.withheld", { count: exp.values_withheld })}</Badge> <span className="text-xs text-ink-soft">{t("leads.withheldBody")}</span></p> : null}
          {exp.status === "completed" ? <Button size="sm" variant="secondary" onClick={() => void download(`/lead-generation/exports/${exp.id}/download`, "prospects.csv")}>{t("leads.download")}</Button> : null}
        </div>
      ) : null}
    </Card>
  );
}

export function DataTransfer() {
  const { data: me } = useMe();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {can("lead.import") ? <Import /> : null}
      {can("lead.export") ? <Export /> : null}
    </div>
  );
}
