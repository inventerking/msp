import { Field, Input } from "@msp/ui";
import { createContext, type ReactNode, useContext, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Range } from "../api/intel";

const Ctx = createContext<{ range: Range; setRange: (r: Range) => void }>({ range: {}, setRange: () => undefined });

/** The months every screen in the module shows. Empty means the last six months. */
export function RangeProvider({ children }: { children: ReactNode }) {
  const [range, setRange] = useState<Range>({});
  return <Ctx.Provider value={{ range, setRange }}>{children}</Ctx.Provider>;
}

export function useRange() {
  return useContext(Ctx).range;
}

export function RangePicker() {
  const { t } = useTranslation();
  const { range, setRange } = useContext(Ctx);
  const month = (v?: string) => (v ? v.slice(0, 7) : "");
  return (
    <div className="flex flex-wrap items-end gap-2">
      <Field label={t("intel.from")}>{(ids) => <Input {...ids} type="month" value={month(range.from)} onChange={(e) => setRange({ ...range, ...(e.target.value ? { from: `${e.target.value}-01` } : { from: undefined }) })} />}</Field>
      <Field label={t("intel.to")}>{(ids) => <Input {...ids} type="month" value={month(range.to)} onChange={(e) => setRange({ ...range, ...(e.target.value ? { to: `${e.target.value}-01` } : { to: undefined }) })} />}</Field>
    </div>
  );
}
