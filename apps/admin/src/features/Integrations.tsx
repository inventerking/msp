import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, errorText, hasRole, useAdmin } from "../app/api";

export function Integrations() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const qc = useQueryClient();
  const canManage = hasRole(me, "super_admin", "integration_admin");
  const providers = useQuery({ queryKey: ["providers"], queryFn: () => unwrap(api.GET("/integrations/providers")) });
  const [editing, setEditing] = useState<string>();
  const [label, setLabel] = useState("api_key");
  const [secret, setSecret] = useState("");
  const [failure, setFailure] = useState<string>();
  const refresh = () => qc.invalidateQueries({ queryKey: ["providers"] });
  const status = useMutation({
    mutationFn: (v: { id: string; status: "enabled" | "disabled" }) => unwrap(api.PUT("/integrations/providers/{id}/status", { params: { path: { id: v.id } }, body: { status: v.status } })),
    onSuccess: refresh,
    onError: (e) => setFailure(errorText(t, e)),
  });
  const save = async () => {
    if (!editing) return;
    setFailure(undefined);
    try {
      await unwrap(api.PUT("/integrations/providers/{id}/credentials", { params: { path: { id: editing } }, body: { label, secret } }));
      setSecret(""); // The secret leaves memory as soon as it is saved.
      setEditing(undefined);
      await refresh();
    } catch (e) {
      setFailure(errorText(t, e));
    }
  };
  return (
    <>
      <PageHeader title={t("integrations.title")} description={t("integrations.subtitle")} />
      {failure ? <div className="mb-4"><ErrorState title={failure} /></div> : null}
      <Card>
        {providers.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {providers.isError ? <div className="p-4"><ErrorState title={errorText(t, providers.error)} onRetry={() => void providers.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {providers.data ? (
          <Table>
            <thead><tr><th>{t("integrations.name")}</th><th>{t("integrations.kind")}</th><th>{t("integrations.status")}</th><th>{t("integrations.credential")}</th><th>{t("integrations.health")}</th>{canManage ? <th /> : null}</tr></thead>
            <tbody>
              {providers.data.data.map((p) => (
                <tr key={p.id}>
                  <td><span className="font-medium">{p.name}</span><br /><code className="text-xs text-ink-faint">{p.key}</code></td>
                  <td>{p.kind}</td>
                  <td className="space-x-1"><Badge tone={p.status === "enabled" ? "ok" : "neutral"}>{p.status}</Badge>{p.circuit_open ? <Badge tone="warn">{t("integrations.circuit")}</Badge> : null}</td>
                  <td>{p.has_credential ? <Badge tone="info">{t("integrations.stored")}</Badge> : <span className="text-ink-faint">{t("integrations.none")}</span>}</td>
                  <td className="text-xs text-ink-soft">{Object.entries(p.calls_24h).map(([k, v]) => `${k}: ${v}`).join(", ") || "–"}</td>
                  {canManage ? (
                    <td className="space-x-1 text-right whitespace-nowrap">
                      <Button size="sm" variant="ghost" onClick={() => status.mutate({ id: p.id, status: p.status === "enabled" ? "disabled" : "enabled" })}>{p.status === "enabled" ? t("integrations.disable") : t("integrations.enable")}</Button>
                      <Button size="sm" variant="secondary" onClick={() => setEditing(p.id)}>{t("integrations.set")}</Button>
                    </td>
                  ) : null}
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
      {editing ? (
        <Card className="mt-4 max-w-lg p-4">
          <div className="space-y-3">
            <Field label={t("integrations.label")}>{(ids) => <Input {...ids} value={label} onChange={(e) => setLabel(e.target.value)} />}</Field>
            <Field label={t("integrations.secret")} hint={t("integrations.secretHint")}>{(ids) => <Input {...ids} type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} />}</Field>
            <div className="flex gap-2">
              <Button disabled={!secret || !label} onClick={() => void save()}>{t("integrations.saveSecret")}</Button>
              <Button variant="secondary" onClick={() => { setEditing(undefined); setSecret(""); }}>{t("common.cancel")}</Button>
            </div>
          </div>
        </Card>
      ) : null}
    </>
  );
}
