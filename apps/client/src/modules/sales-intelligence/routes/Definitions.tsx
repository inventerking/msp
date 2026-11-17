import { Card, ErrorState, SkeletonRows, Table } from "@msp/ui";
import { useTranslation } from "react-i18next";
import { errorText } from "../../../app/errors";
import { useDefinitions } from "../api/intel";
import { KindBadge } from "../components/Value";

/** How every number is calculated, so anyone can check what a metric means. */
export function Definitions() {
  const { t } = useTranslation();
  const defs = useDefinitions();
  if (defs.isPending) return <Card><SkeletonRows label={t("common.loading")} /></Card>;
  if (defs.isError) return <Card className="p-4"><ErrorState title={errorText(t, defs.error)} /></Card>;
  return (
    <Card>
      <p className="p-4 text-sm text-ink-soft">{t("intel.definitionsHelp", { days: defs.data.attribution_days, months: defs.data.roi_horizon_months })}</p>
      <Table>
        <thead><tr><th>{t("intel.metric")}</th><th>{t("intel.kind")}</th><th>{t("intel.definition")}</th></tr></thead>
        <tbody>
          {defs.data.data.map((d) => <tr key={d.key}><td className="whitespace-nowrap">{d.name}</td><td><KindBadge kind={d.value_kind} /></td><td className="text-sm text-ink-soft">{d.description}</td></tr>)}
        </tbody>
      </Table>
    </Card>
  );
}
