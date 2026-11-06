import { Badge, Button, Card, EmptyState, PageHeader } from "@msp/ui";
import { Ban, Clock, Lock, SearchX, ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { useMe } from "../app/api";
import { statusTone } from "../app/moduleStatus";

const icons = { inactive: Lock, pending: Clock, suspended: ShieldAlert, expired: Clock, revoked: Ban };

export function ModuleStatusPage({ moduleKey, status }: { moduleKey: string; status: string }) {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const known = status in icons ? (status as keyof typeof icons) : "inactive";
  const canSeeBilling = me?.permissions.includes("billing.read");
  const body = known === "inactive" && !canSeeBilling ? t("moduleStatus.inactiveNoBilling") : t(`moduleStatus.${known}Body`);
  return (
    <>
      <PageHeader title={t(`modules.${moduleKey}.name`)} description={t(`modules.${moduleKey}.description`)} actions={<Badge tone={statusTone(status)}>{t(`status.${known}`)}</Badge>} />
      <Card>
        <EmptyState
          icon={icons[known]}
          title={t(`moduleStatus.${known}Title`)}
          description={body}
          action={(known === "inactive" || known === "expired") && canSeeBilling ? <Button asChild variant="secondary"><Link to="/subscription">{t("moduleStatus.viewPlans")}</Link></Button> : undefined}
        />
      </Card>
    </>
  );
}

export function NoPermission() {
  const { t } = useTranslation();
  return <Card><EmptyState icon={Lock} title={t("moduleStatus.noPermissionTitle")} description={t("moduleStatus.noPermissionBody")} /></Card>;
}

export function NotFound() {
  const { t } = useTranslation();
  return <Card><EmptyState icon={SearchX} title={t("moduleStatus.notFoundTitle")} description={t("moduleStatus.notFoundBody")} action={<Button asChild variant="secondary"><Link to="/">{t("moduleStatus.goHome")}</Link></Button>} /></Card>;
}

export function TenantSuspended() {
  const { t } = useTranslation();
  return (
    <div className="mx-auto mt-24 max-w-lg px-4">
      <Card>
        <EmptyState icon={ShieldAlert} title={t("moduleStatus.tenantSuspendedTitle")} description={t("moduleStatus.tenantSuspendedBody")} action={<Button asChild variant="secondary"><Link to="/workspace">{t("workspace.title")}</Link></Button>} />
      </Card>
    </div>
  );
}
