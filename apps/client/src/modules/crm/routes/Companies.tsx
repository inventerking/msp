import { unwrap } from "@msp/api-client";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, Select, SkeletonRows, Table } from "@msp/ui";
import { useQuery } from "@tanstack/react-query";
import { Building2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router";
import { api, crmApi, tenantKey, useMe } from "../../../app/api";
import { errorText } from "../../../app/errors";
import { Timeline } from "../components/Timeline";
import { crmPath } from "../paths";
import { money, useCompanies, useCompany, useContacts, useCrmMutation, useDeals } from "../api/crm";

export function Companies() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const [q, setQ] = useState("");
  const list = useCompanies(q.trim());
  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");
  const create = useCrmMutation(() => unwrap(crmApi.POST("/crm/companies", { body: { name, ...(domain.trim() ? { domain } : {}) } })));
  const rows = list.data?.data ?? [];
  return (
    <div className="space-y-4">
      {me?.permissions.includes("crm.write") ? (
        <Card className="p-4">
          <form className="grid gap-3 sm:grid-cols-[2fr_2fr_auto] sm:items-end" onSubmit={(e) => { e.preventDefault(); create.mutate(undefined, { onSuccess: () => { setName(""); setDomain(""); } }); }}>
            <Field label={t("crm.companyName")}>{(ids) => <Input {...ids} value={name} onChange={(e) => setName(e.target.value)} />}</Field>
            <Field label={t("crm.website")} hint={t("crm.websiteHint")}>{(ids) => <Input {...ids} value={domain} onChange={(e) => setDomain(e.target.value)} />}</Field>
            <Button type="submit" disabled={!name.trim() || create.isPending}>{t("crm.addCompany")}</Button>
          </form>
          {create.isError ? <div className="mt-2"><ErrorState title={errorText(t, create.error)} /></div> : null}
        </Card>
      ) : null}
      <div className="max-w-sm"><Field label={t("crm.search")}>{(ids) => <Input {...ids} value={q} onChange={(e) => setQ(e.target.value)} />}</Field></div>
      <Card>
        {list.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {list.isError ? <div className="p-4"><ErrorState title={errorText(t, list.error)} /></div> : null}
        {list.isSuccess && rows.length === 0 ? <EmptyState icon={Building2} title={t("crm.noCompanies")} /> : null}
        {rows.length ? (
          <Table>
            <thead><tr><th>{t("crm.company")}</th><th>{t("crm.lifecycle")}</th><th>{t("crm.openDeals")}</th><th>{t("crm.source")}</th></tr></thead>
            <tbody>
              {rows.map((c) => (
                <tr key={c.id}>
                  <td><Link className="font-medium text-ink hover:underline" to={crmPath(`companies/${c.id}`)}>{c.name}</Link>{c.domain ? <div className="text-xs text-ink-faint">{c.domain}</div> : null}</td>
                  <td><Badge tone={c.lifecycle === "customer" ? "ok" : "neutral"}>{t(`crm.lifecycles.${c.lifecycle}`)}</Badge></td>
                  <td>{c.open_deals ?? 0}</td>
                  <td className="text-ink-soft">{c.source_module ? t(`modules.${c.source_module}.name`, { defaultValue: c.source_module }) : t("crm.manual")}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : null}
      </Card>
    </div>
  );
}

export function CompanyDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { data: me } = useMe();
  const canWrite = Boolean(me?.permissions.includes("crm.write"));
  const company = useCompany(id);
  const contacts = useContacts(id);
  const deals = useDeals({ crm_company_id: id });
  const reference = useQuery({ queryKey: tenantKey(me, "reference"), queryFn: () => unwrap(api.GET("/reference")), staleTime: 3_600_000 });
  const [person, setPerson] = useState({ full_name: "", email: "", job_title: "", country_code: "" });
  const addPerson = useCrmMutation(() => unwrap(crmApi.POST("/crm/contacts", { body: { crm_company_id: id, ...Object.fromEntries(Object.entries(person).filter(([, v]) => v.trim())) } })));
  if (company.isPending) return <SkeletonRows label={t("common.loading")} />;
  if (company.isError) return <ErrorState title={errorText(t, company.error)} />;
  const c = company.data;
  return (
    <div className="space-y-4">
      <Link to={crmPath("companies")} className="text-sm text-ink-soft hover:underline">← {t("crm.allCompanies")}</Link>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold text-ink">{c.name}</h2>
        <Badge tone={c.lifecycle === "customer" ? "ok" : "neutral"}>{t(`crm.lifecycles.${c.lifecycle}`)}</Badge>
        {c.domain ? <span className="text-sm text-ink-soft">{c.domain}</span> : null}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="p-4">
          <h3 className="mb-2 text-sm font-medium text-ink">{t("crm.people")}</h3>
          <ul className="space-y-1 text-sm">
            {(contacts.data?.data ?? []).map((p) => (
              <li key={p.id}>{p.erased ? <span className="text-ink-faint">{t("crm.erased")}</span> : <><span className="text-ink">{p.full_name ?? p.email}</span> <span className="text-ink-faint">{p.job_title}</span> {p.is_primary ? <Badge>{t("crm.primary")}</Badge> : null}</>}</li>
            ))}
          </ul>
          {canWrite ? (
            <form className="mt-3 grid gap-2 sm:grid-cols-2" onSubmit={(e) => { e.preventDefault(); addPerson.mutate(undefined, { onSuccess: () => setPerson({ full_name: "", email: "", job_title: "", country_code: "" }) }); }}>
              <Field label={t("crm.fullName")}>{(ids) => <Input {...ids} value={person.full_name} onChange={(e) => setPerson({ ...person, full_name: e.target.value })} />}</Field>
              <Field label={t("crm.email")}>{(ids) => <Input {...ids} type="email" value={person.email} onChange={(e) => setPerson({ ...person, email: e.target.value })} />}</Field>
              <Field label={t("crm.jobTitle")}>{(ids) => <Input {...ids} value={person.job_title} onChange={(e) => setPerson({ ...person, job_title: e.target.value })} />}</Field>
              <Field label={t("crm.country")}>{(ids) => <Select {...ids} value={person.country_code} onChange={(e) => setPerson({ ...person, country_code: e.target.value })}><option value="">{t("crm.countryUnknown")}</option>{(reference.data?.countries ?? []).map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</Select>}</Field>
              <div><Button type="submit" size="sm" disabled={addPerson.isPending || (!person.full_name.trim() && !person.email.trim())}>{t("crm.addPerson")}</Button></div>
              {addPerson.isError ? <p className="text-xs text-danger sm:col-span-3">{errorText(t, addPerson.error)}</p> : null}
            </form>
          ) : null}
        </Card>
        <Card className="p-4">
          <h3 className="mb-2 text-sm font-medium text-ink">{t("crm.deals")}</h3>
          <ul className="space-y-1 text-sm">
            {(deals.data?.data ?? []).map((d) => (
              <li key={d.id} className="flex items-center justify-between gap-2">
                <Link className="text-ink hover:underline" to={crmPath(`deals/${d.id}`)}>{d.title}</Link>
                <span className="text-xs text-ink-soft">{d.stage_name} · {money(d.expected_mrr_minor, d.currency)}</span>
              </li>
            ))}
          </ul>
        </Card>
      </div>
      <Timeline subjectType="company" subjectId={id} />
    </div>
  );
}
