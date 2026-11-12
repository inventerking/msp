import { unwrap } from "@msp/api-client";
import { Button, Card, EmptyState, ErrorState, SkeletonRows } from "@msp/ui";
import { CheckSquare } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { crmApi } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { crmPath } from "../paths";
import { useCrmMutation, useTasks } from "../api/crm";

export function Tasks() {
  const { t } = useTranslation();
  const [mine, setMine] = useState(true);
  const tasks = useTasks({ status: "open", mine });
  const complete = useCrmMutation((id: string) => unwrap(crmApi.POST("/crm/tasks/{id}/complete", { params: { path: { id } } })));
  const rows = tasks.data?.data ?? [];
  return (
    <div className="space-y-3">
      <label className="flex items-center gap-2 text-sm text-ink"><input type="checkbox" checked={mine} onChange={(e) => setMine(e.target.checked)} />{t("crm.onlyMine")}</label>
      <Card>
        {tasks.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {tasks.isError ? <div className="p-4"><ErrorState title={errorText(t, tasks.error)} /></div> : null}
        {tasks.isSuccess && rows.length === 0 ? <EmptyState icon={CheckSquare} title={t("crm.noTasks")} /> : null}
        <ul className="divide-y divide-line">
          {rows.map((x) => {
            const overdue = x.due_at && new Date(x.due_at) < new Date();
            return (
              <li key={x.id} className="flex items-center justify-between gap-3 p-3 text-sm">
                <div>
                  <div className="text-ink">{x.title}</div>
                  <div className={overdue ? "text-xs text-danger" : "text-xs text-ink-faint"}>
                    {x.due_at ? t("crm.due", { when: new Date(x.due_at).toLocaleString() }) : t("crm.noDue")}
                    {x.deal_id ? <> · <Link className="hover:underline" to={crmPath(`deals/${x.deal_id}`)}>{t("crm.openDeal")}</Link></> : null}
                  </div>
                </div>
                <Button size="sm" variant="secondary" disabled={complete.isPending} onClick={() => complete.mutate(x.id)}>{t("crm.complete")}</Button>
              </li>
            );
          })}
        </ul>
      </Card>
    </div>
  );
}
