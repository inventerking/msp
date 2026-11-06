import { unwrap } from "@msp/api-client";
import { Button, cn, Select } from "@msp/ui";
import { CreditCard, LayoutDashboard, Lock, LogOut, Menu, Settings as SettingsIcon, TriangleAlert, User, Users, X, type LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { NavLink, Outlet, useNavigate } from "react-router";
import { api, queryClient, resetSession, useMe } from "../app/api";
import { navState } from "../app/moduleStatus";
import { modules } from "../modules";
import type { ModuleGroup } from "../modules/types";

const groups: ModuleGroup[] = ["prospect", "engage", "sell", "grow"];

function Item({ to, icon: Icon, label, trailing, end, onNavigate }: { to: string; icon: LucideIcon; label: string; trailing?: React.ReactNode; end?: boolean; onNavigate: () => void }) {
  return (
    <NavLink
      to={to}
      end={end ?? false}
      onClick={onNavigate}
      className={({ isActive }) =>
        cn("flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-rail-ink hover:bg-rail-hover hover:text-rail-ink-strong", isActive && "bg-rail-hover font-medium text-rail-ink-strong")
      }
    >
      <Icon className="size-4 shrink-0" aria-hidden />
      <span className="truncate">{label}</span>
      {trailing ? <span className="ml-auto">{trailing}</span> : null}
    </NavLink>
  );
}

/** Refreshes access when a platform admin changes it. A lost message only delays the update. */
function useEntitlementSignal(tenantId: string | undefined) {
  useEffect(() => {
    if (!tenantId) return;
    const source = new EventSource("/api/v1/events/stream");
    const refresh = () => void queryClient.invalidateQueries({ queryKey: ["me"] });
    source.addEventListener("tenant.entitlements_changed", refresh);
    source.addEventListener("tenant.status_changed", refresh);
    return () => source.close();
  }, [tenantId]);
}

export function Shell() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  useEntitlementSignal(me?.tenant?.id);
  if (!me?.tenant) return null;
  const close = () => setOpen(false);
  const can = (p: string) => me.permissions.includes(p);

  const switchTenant = async (id: string) => {
    await unwrap(api.POST("/me/active-tenant", { body: { tenant_id: id } }));
    await resetSession(); // Drop everything cached for the previous workspace.
    await navigate("/");
  };
  const signOut = async () => {
    await unwrap(api.POST("/auth/logout"));
    queryClient.clear();
    await navigate("/login");
  };

  return (
    <div className="flex min-h-dvh">
      {open ? <button aria-label={t("nav.close")} className="fixed inset-0 z-20 bg-black/40 lg:hidden" onClick={close} /> : null}
      <aside className={cn("fixed inset-y-0 left-0 z-30 flex w-64 flex-col bg-rail transition-transform lg:static lg:translate-x-0", open ? "translate-x-0" : "-translate-x-full")}>
        <div className="flex h-14 items-center justify-between px-4">
          <span className="text-sm font-semibold tracking-tight text-rail-ink-strong">{t("app.name")}</span>
          <button aria-label={t("nav.close")} className="text-rail-ink lg:hidden" onClick={close}><X className="size-5" /></button>
        </div>
        <nav aria-label={t("nav.workspace")} className="flex-1 space-y-5 overflow-y-auto px-2.5 pb-4">
          <Item to="/" end icon={LayoutDashboard} label={t("nav.dashboard")} onNavigate={close} />
          {groups.map((g) => (
            <div key={g}>
              <p className="mb-1 px-2.5 text-[11px] font-medium tracking-wider text-rail-ink/60 uppercase">{t(`nav.${g}`)}</p>
              <div className="space-y-0.5">
                {modules.filter((m) => m.group === g).map((m) => {
                  const state = navState(me.modules.find((x) => x.key === m.key)?.status);
                  const trailing = state === "locked" ? <Lock aria-label={t("nav.locked")} className="size-3.5 opacity-60" /> : state === "attention" ? <TriangleAlert aria-label={t("nav.locked")} className="size-3.5 text-amber-400" /> : undefined;
                  return <Item key={m.key} to={`/${m.path}`} icon={m.icon} label={t(`modules.${m.key}.name`)} trailing={trailing} onNavigate={close} />;
                })}
              </div>
            </div>
          ))}
          <div className="space-y-0.5 border-t border-white/10 pt-3">
            <Item to="/team" icon={Users} label={t("nav.team")} onNavigate={close} />
            {can("settings.manage") ? <Item to="/settings" icon={SettingsIcon} label={t("nav.settings")} onNavigate={close} /> : null}
            {can("billing.read") ? <Item to="/subscription" icon={CreditCard} label={t("nav.subscription")} onNavigate={close} /> : null}
          </div>
        </nav>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-3 border-b border-line bg-surface px-4">
          <Button variant="ghost" size="icon" className="lg:hidden" aria-label={t("nav.menu")} onClick={() => setOpen(true)}><Menu /></Button>
          {me.memberships.length > 1 ? (
            <Select aria-label={t("nav.workspace")} className="max-w-56" value={me.tenant.id} onChange={(e) => void switchTenant(e.target.value)}>
              {me.memberships.map((m) => <option key={m.tenant_id} value={m.tenant_id}>{m.tenant_name}</option>)}
            </Select>
          ) : (
            <span className="truncate text-sm font-medium text-ink">{me.tenant.name}</span>
          )}
          <div className="ml-auto flex items-center gap-1">
            <Button asChild variant="ghost" size="sm"><NavLink to="/profile"><User />{me.user.display_name}</NavLink></Button>
            <Button variant="ghost" size="icon" aria-label={t("common.signOut")} title={t("common.signOut")} onClick={() => void signOut()}><LogOut /></Button>
          </div>
        </header>
        <main className="mx-auto w-full max-w-6xl flex-1 p-4 sm:p-6"><Outlet /></main>
      </div>
    </div>
  );
}
