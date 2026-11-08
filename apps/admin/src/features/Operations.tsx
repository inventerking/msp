import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, errorText } from "../app/api";

export function Operations() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const stats = useQuery({ queryKey: ["job-stats"], queryFn: () => unwrap(api.GET("/jobs/stats")), refetchInterval: 15_000 });
  const dead = useQuery({ queryKey: ["dead-letters"], queryFn: () => unwrap(api.GET("/dead-letters")) });
  const [deciding, setDeciding] = useState<{ id: string; requeue: boolean }>();
  const decide = useMutation({
    mutationFn: (v: { id: string; requeue: boolean; reason: string }) =>
      unwrap(v.requeue ? api.POST("/dead-letters/{id}/requeue", { params: { path: { id: v.id } }, body: { reason: v.reason } }) : api.POST("/dead-letters/{id}/discard", { params: { path: { id: v.id } }, body: { reason: v.reason } })),
    onSuccess: async () => { setDeciding(undefined); await qc.invalidateQueries({ queryKey: ["dead-letters"] }); await qc.invalidateQueries({ queryKey: ["job-stats"] }); },
  });
  return (
    <>
      <PageHeader title={t("ops.title")} description={t("ops.subtitle")} />
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("ops.queues")}</h2>
        <Card>
          {stats.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
          {stats.isError ? <div className="p-4"><ErrorState title={errorText(t, stats.error)} /></div> : null}
          {stats.data ? (
            <>
              {stats.data.queues.length === 0 ? <EmptyState title={t("ops.quiet")} /> : (
                <Table>
                  <thead><tr><th>{t("ops.queue")}</th><th>{t("ops.status")}</th><th>{t("ops.jobs")}</th><th>{t("ops.oldest")}</th></tr></thead>
                  <tbody>{stats.data.queues.map((q) => <tr key={q.queue + q.status}><td><code className="text-xs">{q.queue}</code></td><td>{q.status}</td><td className="tabular-nums">{q.jobs}</td><td className="tabular-nums">{q.oldest_seconds}</td></tr>)}</tbody>
                </Table>
              )}
              <div className="flex flex-wrap gap-4 border-t border-line p-4 text-sm text-ink-soft">
                <span>{t("ops.unpublished", { n: stats.data.oldest_unpublished_seconds })}</span>
                <span>{t("ops.pending", { n: stats.data.pending_deliveries })}</span>
                <span>{t("ops.deadEvents", { n: stats.data.dead_deliveries })}</span>
              </div>
            </>
          ) : null}
        </Card>
      </section>
      <section>
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("ops.dead")}</h2>
        <Card>
          {dead.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
          {dead.data?.data.length === 0 ? <EmptyState title={t("ops.noDead")} /> : null}
          {dead.data?.data.length ? (
            <Table>
              <thead><tr><th>{t("ops.when")}</th><th>{t("ops.origin")}</th><th>{t("ops.kind")}</th><th>{t("ops.error")}</th><th /></tr></thead>
              <tbody>
                {dead.data.data.map((d) => (
                  <tr key={d.id}>
                    <td className="whitespace-nowrap text-ink-soft">{new Date(d.created_at).toLocaleString()}</td>
                    <td><Badge>{d.origin}</Badge></td>
                    <td><code className="text-xs">{d.kind}</code></td>
                    <td className="max-w-sm truncate text-xs text-ink-soft" title={d.error}>{d.error}</td>
                    <td className="space-x-1 text-right whitespace-nowrap">
                      <Button size="sm" variant="secondary" onClick={() => setDeciding({ id: d.id, requeue: true })}>{t("ops.requeue")}</Button>
                      <Button size="sm" variant="ghost" onClick={() => setDeciding({ id: d.id, requeue: false })}>{t("ops.discard")}</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          ) : null}
        </Card>
      </section>
      <ConfirmDialog
        open={Boolean(deciding)} danger={deciding ? !deciding.requeue : false}
        title={deciding?.requeue ? t("ops.requeueTitle") : t("ops.discardTitle")} reasonLabel={t("tenant.reason")}
        confirmLabel={deciding?.requeue ? t("ops.requeue") : t("ops.discard")} cancelLabel={t("common.cancel")}
        busy={decide.isPending} error={decide.error ? errorText(t, decide.error) : undefined}
        onCancel={() => { setDeciding(undefined); decide.reset(); }} onConfirm={(reason) => deciding && decide.mutate({ ...deciding, reason })}
      />
    </>
  );
}
