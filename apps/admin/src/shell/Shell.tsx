import { unwrap } from "@msp/api-client";
import { Button, cn, ErrorState, SkeletonRows } from "@msp/ui";
import { Boxes, Building2, CreditCard, KeyRound, LayoutDashboard, LogOut, ScrollText, UserCog, type LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Navigate, NavLink, Outlet, useNavigate } from "react-router";
import { api, hasRole, queryClient, useAdmin } from "../app/api";

export function RequireAdmin() {
  const { t } = useTranslation();
  const me = useAdmin();
  if (me.isPending) return <div className="mx-auto mt-24 max-w-md"><SkeletonRows label={t("common.loading")} /></div>;
  if (me.isError) return <div className="mx-auto mt-24 max-w-md"><ErrorState title={t("errors.internal_error")} onRetry={() => void me.refetch()} retryLabel={t("common.retry")} /></div>;
  if (!me.data || !me.data.mfa_verified) return <Navigate to="/login" replace />;
  return <Outlet />;
}

function Item({ to, icon: Icon, label, end }: { to: string; icon: LucideIcon; label: string; end?: boolean }) {
  return (
    <NavLink to={to} end={end ?? false} className={({ isActive }) => cn("flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm text-rail-ink hover:bg-rail-hover hover:text-white", isActive && "bg-rail-hover font-medium text-white")}>
      <Icon className="size-4" aria-hidden />
      {label}
    </NavLink>
  );
}

export function Shell() {
  const { t } = useTranslation();
  const { data: me } = useAdmin();
  const navigate = useNavigate();
  const signOut = async () => {
    await unwrap(api.POST("/auth/logout"));
    queryClient.clear();
    await navigate("/login");
  };
  return (
    <div className="flex min-h-dvh flex-col">
      <div className="bg-brand px-4 py-1 text-center text-xs font-medium tracking-wide text-white">{t("shell.banner")}</div>
      <div className="flex flex-1">
        <aside className="flex w-56 shrink-0 flex-col bg-rail">
          <div className="flex h-14 items-center px-4 text-sm font-semibold text-white">{t("shell.title")}</div>
          <nav aria-label={t("shell.title")} className="flex-1 space-y-0.5 px-2.5">
            <Item to="/" end icon={LayoutDashboard} label={t("nav.dashboard")} />
            {hasRole(me, "super_admin", "platform_support", "billing_admin", "platform_analyst") ? <Item to="/tenants" icon={Building2} label={t("nav.tenants")} /> : null}
            <Item to="/catalog" icon={Boxes} label={t("nav.catalog")} />
            {hasRole(me, "super_admin", "platform_support", "billing_admin", "platform_analyst") ? <Item to="/subscriptions" icon={CreditCard} label={t("nav.subscriptions")} /> : null}
            {hasRole(me, "super_admin", "platform_support") ? <Item to="/audit" icon={ScrollText} label={t("nav.audit")} /> : null}
            {hasRole(me, "super_admin") ? <Item to="/admins" icon={UserCog} label={t("nav.admins")} /> : null}
            <Item to="/account" icon={KeyRound} label={t("nav.account")} />
          </nav>
          <div className="border-t border-white/10 p-3">
            <p className="truncate text-xs text-rail-ink">{me?.admin.email}</p>
            <Button variant="ghost" size="sm" className="mt-1 w-full justify-start text-rail-ink hover:bg-rail-hover hover:text-white" onClick={() => void signOut()}><LogOut />{t("common.signOut")}</Button>
          </div>
        </aside>
        <main className="min-w-0 flex-1 p-6"><div className="mx-auto max-w-6xl"><Outlet /></div></main>
      </div>
    </div>
  );
}
