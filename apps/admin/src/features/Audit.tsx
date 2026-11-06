import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, errorText } from "../app/api";

export function Audit() {
  const { t } = useTranslation();
  const q = useInfiniteQuery({
    queryKey: ["audit"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => unwrap(api.GET("/audit-events", { params: { query: { limit: 50, ...(pageParam ? { cursor: pageParam } : {}) } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const rows = q.data?.pages.flatMap((p) => p.data) ?? [];
  return (
    <>
      <PageHeader title={t("audit.title")} description={t("audit.subtitle")} />
      <Card>
        {q.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {q.isError ? <div className="p-4"><ErrorState title={errorText(t, q.error)} onRetry={() => void q.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {q.isSuccess && rows.length === 0 ? <EmptyState title={t("audit.empty")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("audit.when")}</th><th>{t("audit.action")}</th><th>{t("audit.actor")}</th><th>{t("audit.tenant")}</th><th>{t("audit.outcome")}</th></tr></thead>
            <tbody>
              {rows.map((e) => <tr key={e.id}><td className="whitespace-nowrap text-ink-soft">{new Date(e.occurred_at).toLocaleString()}</td><td><code className="text-xs">{e.action}</code></td><td>{e.actor_type}</td><td className="font-mono text-xs text-ink-faint">{e.tenant_id?.slice(0, 8) ?? "–"}</td><td>{e.outcome}</td></tr>)}
            </tbody>
          </Table>
        ) : null}
        {q.hasNextPage ? <div className="border-t border-line p-3 text-center"><Button variant="secondary" size="sm" onClick={() => void q.fetchNextPage()}>{t("common.loadMore")}</Button></div> : null}
      </Card>
    </>
  );
}
