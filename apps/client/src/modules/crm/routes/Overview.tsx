import { Card, EmptyState, ErrorState, PageHeader, SkeletonRows } from "@msp/ui";
import { KanbanSquare } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useStatus } from "../api/status";

export function Overview() {
  const { t } = useTranslation();
  const status = useStatus();
  return (
    <>
      <PageHeader title={t("modules.crm.name")} description={t("modules.crm.description")} />
      <Card>
        {status.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {status.isError ? <div className="p-4"><ErrorState title={t("errors.internal_error")} onRetry={() => void status.refetch()} retryLabel={t("common.retry")} /></div> : null}
        {status.isSuccess ? <EmptyState icon={KanbanSquare} title={t("modules.placeholderTitle")} description={t("modules.placeholderPhase", { phase: 4 })} /> : null}
      </Card>
    </>
  );
}
