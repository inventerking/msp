import { cn } from "@msp/ui";
import { NavLink } from "react-router";

/** Section navigation inside the module. Each tab is a real link, so it works with the keyboard and history. */
export function Tabs({ items }: { items: { to: string; label: string; end?: boolean }[] }) {
  return (
    <nav aria-label="Sections" className="mb-5 flex gap-1 overflow-x-auto border-b border-line">
      {items.map((i) => (
        <NavLink
          key={i.to}
          to={i.to}
          end={i.end ?? false}
          className={({ isActive }) => cn("-mb-px border-b-2 px-3 py-2 text-sm whitespace-nowrap", isActive ? "border-brand font-medium text-ink" : "border-transparent text-ink-soft hover:text-ink")}
        >
          {i.label}
        </NavLink>
      ))}
    </nav>
  );
}
