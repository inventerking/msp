import { unwrap } from "@msp/api-client";
import { Button, cn, EmptyState } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, tenantKey, useMe } from "../app/api";

export function NotificationBell() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const panel = useRef<HTMLDivElement>(null);
  const key = tenantKey(me, "notifications");
  const list = useQuery({ queryKey: key, queryFn: () => unwrap(api.GET("/notifications")), refetchInterval: 60_000 });
  const readAll = useMutation({ mutationFn: () => unwrap(api.POST("/notifications/read-all")), onSuccess: () => qc.invalidateQueries({ queryKey: key }) });
  const read = useMutation({ mutationFn: (id: string) => unwrap(api.POST("/notifications/{id}/read", { params: { path: { id } } })), onSuccess: () => qc.invalidateQueries({ queryKey: key }) });
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent | KeyboardEvent) => {
      if (e instanceof KeyboardEvent ? e.key === "Escape" : !panel.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", close);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", close);
    };
  }, [open]);
  const unread = list.data?.unread ?? 0;
  return (
    <div className="relative" ref={panel}>
      <Button variant="ghost" size="icon" aria-label={t("notifications.open", { n: unread })} aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <Bell />
        {unread > 0 ? <span className="absolute top-1.5 right-1.5 size-2 rounded-full bg-danger" aria-hidden /> : null}
      </Button>
      {open ? (
        <div role="dialog" aria-label={t("notifications.title")} className="absolute right-0 z-30 mt-1 w-80 rounded-lg border border-line bg-surface shadow-lg">
          <div className="flex items-center justify-between border-b border-line px-3 py-2">
            <p className="text-sm font-medium">{t("notifications.title")}</p>
            {unread > 0 ? <Button variant="ghost" size="sm" onClick={() => readAll.mutate()}>{t("notifications.markAll")}</Button> : null}
          </div>
          {list.data?.data.length ? (
            <ul className="max-h-80 overflow-y-auto">
              {list.data.data.map((n) => (
                <li key={n.id}>
                  <button className={cn("block w-full px-3 py-2 text-left text-sm hover:bg-sunken", !n.read_at && "font-medium")} onClick={() => !n.read_at && read.mutate(n.id)}>
                    {n.title}
                    <span className="mt-0.5 block text-xs font-normal text-ink-faint">{new Date(n.created_at).toLocaleString()}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : <EmptyState title={t("notifications.none")} />}
        </div>
      ) : null}
    </div>
  );
}
