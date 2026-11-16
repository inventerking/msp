import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, SkeletonRows } from "@msp/ui";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router";
import { outreachApi } from "../../../app/api";
import { errorText } from "../../../app/errors";

/** The page an unsubscribe link opens. Opening it changes nothing; the button does. */
export function Unsubscribe() {
  const { t } = useTranslation();
  const { token = "" } = useParams();
  const status = useQuery({ queryKey: ["unsubscribe", token], retry: false, queryFn: () => unwrap(outreachApi.GET("/public/outreach/unsubscribe/{token}", { params: { path: { token } } })) });
  const confirm = useMutation({ mutationFn: () => unwrap(outreachApi.POST("/public/outreach/unsubscribe/{token}", { params: { path: { token } } })), onSuccess: () => void status.refetch() });
  return (
    <main className="mx-auto max-w-md p-6">
      <Card className="p-6">
        <h1 className="mb-2 text-xl font-semibold text-ink">{t("outreach.unsubscribeTitle")}</h1>
        {status.isPending ? <SkeletonRows label={t("common.loading")} /> : null}
        {status.isError ? <ErrorState title={t("outreach.unsubscribeInvalid")} /> : null}
        {status.data?.done ? <p role="status" className="text-sm text-ink">{t("outreach.unsubscribed", { sender: status.data.sender })}</p> : null}
        {status.data && !status.data.done ? (
          <>
            <p className="mb-4 text-sm text-ink-soft">{t("outreach.unsubscribeAsk", { sender: status.data.sender })}</p>
            {confirm.isError ? <ErrorState title={errorText(t, confirm.error)} /> : null}
            <Button disabled={confirm.isPending} onClick={() => confirm.mutate()}>{t("outreach.unsubscribeButton")}</Button>
          </>
        ) : null}
      </Card>
    </main>
  );
}
