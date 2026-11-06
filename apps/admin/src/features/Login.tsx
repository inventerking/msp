import { unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input } from "@msp/ui";
import { loginSchema, mfaCodeSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import type { z } from "zod";
import { api, errorText, fetchAdmin, queryClient } from "../app/api";

type Step = { name: "password" } | { name: "verify" } | { name: "enroll"; secret: string; url: string };

export function Login() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [step, setStep] = useState<Step>({ name: "password" });
  const [failure, setFailure] = useState<string>();
  const creds = useForm<z.infer<typeof loginSchema>>({ resolver: zodResolver(loginSchema), defaultValues: { email: "", password: "" } });
  const mfa = useForm<z.infer<typeof mfaCodeSchema>>({ resolver: zodResolver(mfaCodeSchema), defaultValues: { code: "" } });

  const signIn = creds.handleSubmit(async (values) => {
    setFailure(undefined);
    try {
      const r = await unwrap(api.POST("/auth/login", { body: values }));
      await queryClient.fetchQuery({ queryKey: ["admin"], queryFn: fetchAdmin, staleTime: 0 }); // Loads the CSRF token.
      if (r.mfa_enrolled) {
        setStep({ name: "verify" });
      } else {
        const e = await unwrap(api.POST("/auth/mfa/enroll"));
        setStep({ name: "enroll", secret: e.secret, url: e.otpauth_url });
      }
    } catch (err) {
      setFailure(errorText(t, err));
    }
  });
  const verify = mfa.handleSubmit(async ({ code }) => {
    setFailure(undefined);
    try {
      await unwrap(step.name === "enroll" ? api.POST("/auth/mfa/confirm", { body: { code } }) : api.POST("/auth/mfa/verify", { body: { code } }));
      await queryClient.fetchQuery({ queryKey: ["admin"], queryFn: fetchAdmin, staleTime: 0 });
      await navigate("/");
    } catch (err) {
      setFailure(errorText(t, err));
    }
  });

  return (
    <main className="flex min-h-dvh flex-col items-center justify-center bg-canvas p-4">
      <div className="w-full max-w-sm">
        <p className="mb-5 text-center text-sm font-semibold text-brand">{t("shell.title")}</p>
        <Card className="p-6">
          {failure ? <div className="mb-4"><ErrorState title={failure} /></div> : null}
          {step.name === "password" ? (
            <form onSubmit={(ev) => void signIn(ev)} className="space-y-4" noValidate>
              <h1 className="text-lg font-semibold">{t("login.title")}</h1>
              <Field label={t("login.email")} error={creds.formState.errors.email?.message && t(creds.formState.errors.email.message)}>{(ids) => <Input {...ids} type="email" autoComplete="username" {...creds.register("email")} />}</Field>
              <Field label={t("login.password")} error={creds.formState.errors.password?.message && t(creds.formState.errors.password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="current-password" {...creds.register("password")} />}</Field>
              <Button type="submit" className="w-full" disabled={creds.formState.isSubmitting}>{t("login.submit")}</Button>
            </form>
          ) : (
            <form onSubmit={(ev) => void verify(ev)} className="space-y-4" noValidate>
              <h1 className="text-lg font-semibold">{t(step.name === "enroll" ? "login.enrollTitle" : "login.verifyTitle")}</h1>
              {step.name === "enroll" ? (
                <div>
                  <p className="text-sm text-ink-soft">{t("login.enrollBody")}</p>
                  <code data-testid="mfa-secret" className="mt-2 block rounded-md bg-sunken p-2 text-sm break-all">{step.secret}</code>
                  <a className="mt-1 inline-block text-sm text-brand hover:underline" href={step.url}>{t("login.openApp")}</a>
                </div>
              ) : <p className="text-sm text-ink-soft">{t("login.verifyBody")}</p>}
              <Field label={t("login.code")} error={mfa.formState.errors.code?.message && t(mfa.formState.errors.code.message)}>{(ids) => <Input {...ids} autoComplete="one-time-code" autoFocus {...mfa.register("code")} />}</Field>
              <Button type="submit" className="w-full" disabled={mfa.formState.isSubmitting}>{t("login.verify")}</Button>
            </form>
          )}
        </Card>
      </div>
    </main>
  );
}
