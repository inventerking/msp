import { unwrap } from "@msp/api-client";
import { Button, ErrorState } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, tenantKey, useMe } from "../app/api";
import { errorText } from "../app/errors";

/**
 * Asks the signed-in person to accept documents the platform operator requires, such as terms of
 * service, before they continue. Acceptance is recorded with the document's version. A DPA is
 * accepted by a workspace's owner on the workspace's behalf. Nothing shows when nothing is due.
 */
export function LegalGate() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const key = tenantKey(me, "legal-pending");
  const pending = useQuery({ queryKey: key, queryFn: () => unwrap(api.GET("/me/legal")) });
  const [read, setRead] = useState(false);
  const accept = useMutation({
    mutationFn: (ids: string[]) => unwrap(api.POST("/me/legal/accept", { body: { document_ids: ids } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: key }),
  });
  const docs = pending.data?.data ?? [];
  if (!docs.length) return null;
  return (
    <div role="dialog" aria-modal="true" aria-labelledby="legal-title" className="fixed inset-0 z-50 flex items-center justify-center bg-ink/40 p-4">
      <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg bg-surface p-5 shadow-xl">
        <h2 id="legal-title" className="mb-2 text-lg font-semibold">{t("legal.gateTitle")}</h2>
        <p className="mb-3 text-sm text-ink-soft">{t("legal.gateBody")}</p>
        <div className="space-y-3">
          {docs.map((d) => (
            <details key={d.id} className="rounded-md border border-line p-3">
              <summary className="cursor-pointer text-sm font-medium">{d.title} <span className="text-xs text-ink-faint">{t("legal.version", { version: d.version, date: new Date(d.published_at).toLocaleDateString() })}</span></summary>
              <div className="mt-2 max-h-64 overflow-y-auto whitespace-pre-wrap text-sm">{d.body}</div>
            </details>
          ))}
        </div>
        <label className="mt-4 flex items-center gap-2 text-sm"><input type="checkbox" checked={read} onChange={(e) => setRead(e.target.checked)} />{t("legal.confirmRead")}</label>
        {accept.isError ? <div className="mt-2"><ErrorState title={errorText(t, accept.error)} /></div> : null}
        <div className="mt-4"><Button disabled={!read || accept.isPending} onClick={() => accept.mutate(docs.map((d) => d.id))}>{t("legal.accept")}</Button></div>
      </div>
    </div>
  );
}
