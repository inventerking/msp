import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, Field, Input, SkeletonRows, Table } from "@msp/ui";
import { Radar } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ciPath } from "../paths";
import { Link } from "react-router";
import { competitorApi, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useCiMutation, useCompetitors } from "../api/competitors";

export function Competitors() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const can = (p: string) => Boolean(me?.permissions.includes(p));
  const list = useCompetitors();
  const [domain, setDomain] = useState("");
  const add = useCiMutation((d: { domain: string; display_name?: string }) => unwrap(competitorApi.POST("/competitor-intelligence/competitors", { body: { monitoring_enabled: false, ...d } })));
  const discover = useCiMutation(() => unwrap(competitorApi.POST("/competitor-intelligence/discover", { body: {} })));
  const rows = list.data?.data ?? [];
  return (
    <div className="space-y-4">
      {can("competitor.write") ? (
        <Card className="p-4">
          <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); if (domain.trim()) add.mutate({ domain: domain.trim() }, { onSuccess: () => setDomain("") }); }}>
            <div className="min-w-64 flex-1">
              <Field label={t("competitors.addByDomain")} hint={t("competitors.addHint")} error={add.isError ? errorText(t, add.error) : undefined}>
                {(ids) => <Input {...ids} value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="rival-msp.com" />}
              </Field>
            </div>
            <Button type="submit" disabled={add.isPending}>{t("competitors.add")}</Button>
            {can("competitor.research") ? <Button variant="secondary" disabled={discover.isPending} onClick={() => discover.mutate(undefined)}>{t("competitors.discover")}</Button> : null}
          </form>
          {discover.isError ? <div className="mt-3"><ErrorState title={errorText(t, discover.error)} /></div> : null}
          {discover.data ? (
            <div className="mt-4">
              <h3 className="mb-2 text-sm font-medium text-ink">{t("competitors.suggestions")}</h3>
              {discover.data.data.length === 0 ? <p className="text-sm text-ink-soft">{t("competitors.noSuggestions")}</p> : (
                <ul className="divide-y divide-line rounded-md border border-line">
                  {discover.data.data.map((c) => (
                    <li key={c.domain} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                      <span><span className="font-medium text-ink">{c.display_name}</span> <span className="text-ink-faint">{c.domain} · {c.country ?? "—"}</span></span>
                      <Button size="sm" variant="secondary" disabled={add.isPending} onClick={() => add.mutate({ domain: c.domain, display_name: c.display_name }, { onSuccess: () => discover.mutate(undefined) })}>{t("competitors.track")}</Button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ) : null}
        </Card>
      ) : null}
      <Card>
        {list.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {list.isError ? <div className="p-4"><ErrorState title={errorText(t, list.error)} onRetry={() => void list.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {list.isSuccess && rows.length === 0 ? <EmptyState icon={Radar} title={t("competitors.none")} description={t("competitors.noneBody")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("competitors.competitor")}</th><th>{t("competitors.monitoring")}</th><th>{t("competitors.lastResearched")}</th><th>{t("competitors.openChanges")}</th></tr></thead>
            <tbody>
              {rows.map((c) => (
                <tr key={c.id}>
                  <td><Link className="font-medium text-ink hover:underline" to={ciPath(`competitors/${c.id}`)}>{c.display_name}</Link>{c.domain ? <div className="text-xs text-ink-faint">{c.domain}</div> : null}</td>
                  <td>{c.monitoring_enabled ? t("competitors.on") : t("competitors.off")}</td>
                  <td className="text-ink-soft">{c.last_researched_at ? new Date(c.last_researched_at).toLocaleString() : t("competitors.never")}</td>
                  <td>{c.open_changes}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}
