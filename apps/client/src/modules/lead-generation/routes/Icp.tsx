import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, ErrorState, Field, Input, Select, SkeletonRows } from "@msp/ui";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { useIcp, useLeadMutation, useReference } from "../api/leads";

/** Picks codes from a reference list, showing each chosen one as a removable badge. */
export function CodePicker({ label, options, value, onChange, disabled }: { label: string; options: { code: string; name: string }[]; value: string[]; onChange: (v: string[]) => void; disabled?: boolean }) {
  const { t } = useTranslation();
  return (
    <div>
      <Field label={label}>
        {(ids) => (
          <Select {...ids} value="" disabled={disabled ?? false} onChange={(e) => { if (e.target.value && !value.includes(e.target.value)) onChange([...value, e.target.value]); }}>
            <option value="">{t("leads.addOption")}</option>
            {options.filter((o) => !value.includes(o.code)).map((o) => <option key={o.code} value={o.code}>{o.name}</option>)}
          </Select>
        )}
      </Field>
      <div className="mt-2 flex flex-wrap gap-1.5">
        {value.length === 0 ? <span className="text-xs text-ink-faint">{t("leads.any")}</span> : null}
        {value.map((v) => (
          <button key={v} type="button" disabled={disabled ?? false} onClick={() => onChange(value.filter((x) => x !== v))} aria-label={t("leads.removeOption", { name: options.find((o) => o.code === v)?.name ?? v })}>
            <Badge>{options.find((o) => o.code === v)?.name ?? v} ×</Badge>
          </button>
        ))}
      </div>
    </div>
  );
}

export function Icp() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const canEdit = Boolean(me?.permissions.includes("lead.configure"));
  const icp = useIcp();
  const ref = useReference();
  const [countries, setCountries] = useState<string[]>([]);
  const [industries, setIndustries] = useState<string[]>([]);
  const [tech, setTech] = useState("");
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");
  const d = icp.data;
  useEffect(() => {
    if (!d) return;
    setCountries(d.countries);
    setIndustries(d.industries);
    setTech(d.technologies.join(", "));
    setMin(d.min_employees ? String(d.min_employees) : "");
    setMax(d.max_employees ? String(d.max_employees) : "");
  }, [d]);
  const save = useLeadMutation(() =>
    unwrap(api.PUT("/tenant/icp", { body: { countries, industries, technologies: tech.split(",").map((s) => s.trim()).filter(Boolean), min_employees: min ? Number(min) : null, max_employees: max ? Number(max) : null } })),
  );
  if (icp.isPending || ref.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (icp.isError) return <ErrorState title={errorText(t, icp.error)} onRetry={() => void icp.refetch()} retryLabel={t("common.retry")} />;
  return (
    <Card className="p-4">
      <h2 className="text-sm font-medium text-ink">{t("leads.icpTitle")}</h2>
      <p className="mb-4 text-sm text-ink-soft">{t("leads.icpBody")}</p>
      {!d?.configured ? <p className="mb-4 text-sm text-warn" role="note">{t("leads.icpMissing")}</p> : null}
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); save.mutate(undefined); }}>
        <div className="grid gap-4 sm:grid-cols-2">
          <CodePicker label={t("leads.industries")} disabled={!canEdit} options={(ref.data?.industries ?? []).map((i) => ({ code: i.code, name: t(`industries.${i.code}`, { defaultValue: i.name }) }))} value={industries} onChange={setIndustries} />
          <CodePicker label={t("leads.countries")} disabled={!canEdit} options={ref.data?.countries ?? []} value={countries} onChange={setCountries} />
        </div>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label={t("leads.employeeMin")}>{(ids) => <Input {...ids} type="number" min={1} disabled={!canEdit} value={min} onChange={(e) => setMin(e.target.value)} />}</Field>
          <Field label={t("leads.employeeMax")}>{(ids) => <Input {...ids} type="number" min={1} disabled={!canEdit} value={max} onChange={(e) => setMax(e.target.value)} />}</Field>
          <Field label={t("leads.technologies")} hint={t("leads.technologiesHint")}>{(ids) => <Input {...ids} disabled={!canEdit} value={tech} onChange={(e) => setTech(e.target.value)} />}</Field>
        </div>
        {save.isError ? <ErrorState title={errorText(t, save.error)} /> : null}
        {canEdit ? (
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={save.isPending}>{t("common.save")}</Button>
            {save.isSuccess ? <span role="status" className="text-sm text-ok">{t("leads.icpSaved")}</span> : null}
          </div>
        ) : <p className="text-xs text-ink-faint">{t("leads.icpReadOnly")}</p>}
      </form>
    </Card>
  );
}
