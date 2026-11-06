import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { api, errorText } from "../app/api";

export function Tenants() {
  const { t } = useTranslation();
  const q = useInfiniteQuery({
    queryKey: ["tenants"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => unwrap(api.GET("/tenants", { params: { query: { limit: 25, ...(pageParam ? { cursor: pageParam } : {}) } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const rows = q.data?.pages.flatMap((p) => p.data) ?? [];
  return (
    <>
      <PageHeader title={t("tenants.title")} />
      <Card>
        {q.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {q.isError ? <div className="p-4"><ErrorState title={errorText(t, q.error)} onRetry={() => void q.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {q.isSuccess && rows.length === 0 ? <EmptyState title={t("tenants.empty")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("tenants.name")}</th><th>{t("tenants.status")}</th><th>{t("tenants.created")}</th></tr></thead>
            <tbody>
              {rows.map((x) => (
                <tr key={x.id}>
                  <td><Link className="font-medium text-brand hover:underline" to={`/tenants/${x.id}`}>{x.name}</Link></td>
                  <td><Badge tone={x.status === "active" ? "ok" : "warn"}>{t(`tenants.states.${x.status}`, { defaultValue: x.status })}</Badge></td>
                  <td className="text-ink-soft">{new Date(x.created_at).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
        {q.hasNextPage ? <div className="border-t border-line p-3 text-center"><Button variant="secondary" size="sm" onClick={() => void q.fetchNextPage()}>{t("common.loadMore")}</Button></div> : null}
      </Card>
    </>
  );
}
