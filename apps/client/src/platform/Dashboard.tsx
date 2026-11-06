import { Badge, Button, Card, EmptyState, PageHeader } from "@msp/ui";
import { ArrowRight, Boxes } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { useMe } from "../app/api";
import { statusTone } from "../app/moduleStatus";
import { modules } from "../modules";

export function Dashboard() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  if (!me) return null;
  const can = (p: string) => me.permissions.includes(p);
  const statusOf = (key: string) => me.modules.find((m) => m.key === key)?.status ?? "inactive";
  const anyActive = modules.some((m) => statusOf(m.key) === "active");
  const steps = [
    can("settings.manage") ? { to: "/settings", label: t("dashboard.stepProfile") } : null,
    can("team.manage") ? { to: "/team", label: t("dashboard.stepTeam") } : null,
    can("billing.read") && !anyActive ? { to: "/subscription", label: t("dashboard.stepModules") } : null,
  ].filter((s) => s !== null);
  return (
    <>
      <PageHeader title={t("dashboard.title", { name: me.user.display_name })} description={t("dashboard.subtitle")} />
      {steps.length > 0 ? (
        <section className="mb-6">
          <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("dashboard.nextSteps")}</h2>
          <Card className="divide-y divide-line">
            {steps.map((s) => (
              <Link key={s.to} to={s.to} className="flex items-center justify-between px-4 py-3 text-sm hover:bg-sunken">
                {s.label}
                <ArrowRight className="size-4 text-ink-faint" aria-hidden />
              </Link>
            ))}
          </Card>
        </section>
      ) : null}
      <section>
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("dashboard.yourModules")}</h2>
        {!anyActive ? (
          <Card className="mb-3"><EmptyState icon={Boxes} title={t("dashboard.noModules")} description={t("dashboard.noModulesBody")} action={can("billing.read") ? <Button asChild variant="secondary"><Link to="/subscription">{t("moduleStatus.viewPlans")}</Link></Button> : undefined} /></Card>
        ) : null}
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {modules.map((m) => {
            const status = statusOf(m.key);
            return (
              <li key={m.key}>
                <Link to={`/${m.path}`} className="block h-full rounded-lg border border-line bg-surface p-4 hover:border-line-strong">
                  <div className="flex items-center justify-between gap-2">
                    <m.icon className="size-4 text-brand" aria-hidden />
                    <Badge tone={statusTone(status)}>{t(`status.${status}`)}</Badge>
                  </div>
                  <p className="mt-3 text-sm font-medium">{t(`modules.${m.key}.name`)}</p>
                  <p className="mt-1 text-xs text-ink-soft">{t(`modules.${m.key}.description`)}</p>
                </Link>
              </li>
            );
          })}
        </ul>
      </section>
    </>
  );
}
