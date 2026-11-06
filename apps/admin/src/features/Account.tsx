import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input, PageHeader } from "@msp/ui";
import { changePasswordSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { api, errorText } from "../app/api";

export function Account() {
  const { t } = useTranslation();
  const form = useForm<z.infer<typeof changePasswordSchema>>({ resolver: zodResolver(changePasswordSchema), defaultValues: { current_password: "", new_password: "" } });
  const [failure, setFailure] = useState<string>();
  const [done, setDone] = useState(false);
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    setFailure(undefined);
    setDone(false);
    try {
      await unwrap(api.POST("/auth/password/change", { body: values }));
      form.reset();
      setDone(true);
    } catch (err) {
      setFailure(errorText(t, err));
    }
  });
  return (
    <>
      <PageHeader title={t("account.title")} description={t("account.subtitle")} />
      <Card className="max-w-md p-4">
        <form onSubmit={(ev) => void submit(ev)} noValidate className="space-y-3">
          {failure ? <ErrorState title={failure} /> : null}
          <Field label={t("account.current")} error={e.current_password?.message && t(e.current_password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="current-password" {...form.register("current_password")} />}</Field>
          <Field label={t("account.new")} hint={t("account.hint")} error={e.new_password?.message && t(e.new_password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="new-password" {...form.register("new_password")} />}</Field>
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={form.formState.isSubmitting}>{t("account.submit")}</Button>
            {done ? <span role="status" className="text-sm text-ok">{t("account.done")}</span> : null}
          </div>
        </form>
      </Card>
    </>
  );
}
