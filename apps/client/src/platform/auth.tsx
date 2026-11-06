import { ApiError, unwrap } from "@msp/api-client";
import { Button, Card, ErrorState, Field, Input } from "@msp/ui";
import { emailOnlySchema, loginSchema, mfaCodeSchema, registerSchema, resetSchema } from "@msp/validation";
import { zodResolver } from "@hookform/resolvers/zod";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router";
import type { z } from "zod";
import { api, queryClient, resetSession, useMe } from "../app/api";
import { applyServerError, errorText } from "../app/errors";

export function AuthCard({ title, body, children }: { title: string; body?: ReactNode; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <main className="flex min-h-dvh items-center justify-center bg-canvas p-4">
      <div className="w-full max-w-sm">
        <p className="mb-5 text-center text-sm font-semibold tracking-tight text-brand">{t("app.name")}</p>
        <Card className="p-6">
          <h1 className="text-lg font-semibold">{title}</h1>
          {body ? <p className="mt-1 text-sm text-ink-soft">{body}</p> : null}
          <div className="mt-5">{children}</div>
        </Card>
      </div>
    </main>
  );
}

/** Only local paths are accepted, so a crafted link cannot redirect a user off the site. */
function safeNext(next: string | null): string {
  return next && next.startsWith("/") && !next.startsWith("//") ? next : "/";
}

export function Login() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const [failure, setFailure] = useState<string>();
  const next = safeNext(params.get("next"));
  const form = useForm<z.infer<typeof loginSchema>>({ resolver: zodResolver(loginSchema), defaultValues: { email: "", password: "" } });
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    setFailure(undefined);
    try {
      const res = await unwrap(api.POST("/auth/login", { body: values }));
      await resetSession();
      await navigate(res.mfa_required ? `/mfa?next=${encodeURIComponent(next)}` : next);
    } catch (err) {
      setFailure(errorText(t, err));
    }
  });
  return (
    <AuthCard title={t("auth.signInTitle")}>
      <form onSubmit={(ev) => void submit(ev)} className="space-y-4" noValidate>
        {failure ? <ErrorState title={failure} /> : null}
        <Field label={t("auth.email")} error={e.email?.message && t(e.email.message)}>{(ids) => <Input {...ids} type="email" autoComplete="username" {...form.register("email")} />}</Field>
        <Field label={t("auth.password")} error={e.password?.message && t(e.password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="current-password" {...form.register("password")} />}</Field>
        <Button type="submit" className="w-full" disabled={form.formState.isSubmitting}>{t("auth.signIn")}</Button>
      </form>
      <div className="mt-4 flex justify-between text-sm">
        <Link className="text-brand hover:underline" to="/forgot-password">{t("auth.forgot")}</Link>
        <Link className="text-brand hover:underline" to={`/register?next=${encodeURIComponent(next)}`}>{t("auth.createAccount")}</Link>
      </div>
    </AuthCard>
  );
}

function CheckEmail({ email }: { email: string }) {
  const { t } = useTranslation();
  const [resent, setResent] = useState(false);
  return (
    <AuthCard title={t("auth.checkEmailTitle")} body={t("auth.checkEmailBody")}>
      {resent ? <p role="status" className="text-sm text-ok">{t("auth.resent")}</p> : (
        <Button variant="secondary" className="w-full" onClick={() => void unwrap(api.POST("/auth/verify-email/resend", { body: { email } })).finally(() => setResent(true))}>{t("auth.resend")}</Button>
      )}
      <p className="mt-4 text-center text-sm"><Link className="text-brand hover:underline" to="/login">{t("auth.backToSignIn")}</Link></p>
    </AuthCard>
  );
}

export function Register() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const [sentTo, setSentTo] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const form = useForm<z.infer<typeof registerSchema>>({ resolver: zodResolver(registerSchema), defaultValues: { display_name: "", email: "", password: "" } });
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    setFailure(undefined);
    try {
      await unwrap(api.POST("/auth/register", { body: values }));
      setSentTo(values.email);
    } catch (err) {
      setFailure(applyServerError(t, err, form.setError, ["email", "password", "display_name"]));
    }
  });
  if (sentTo) return <CheckEmail email={sentTo} />;
  return (
    <AuthCard title={t("auth.registerTitle")}>
      <form onSubmit={(ev) => void submit(ev)} className="space-y-4" noValidate>
        {failure ? <ErrorState title={failure} /> : null}
        <Field label={t("auth.yourName")} error={e.display_name?.message && t(e.display_name.message)}>{(ids) => <Input {...ids} autoComplete="name" {...form.register("display_name")} />}</Field>
        <Field label={t("auth.email")} error={e.email?.message && t(e.email.message)}>{(ids) => <Input {...ids} type="email" autoComplete="username" {...form.register("email")} />}</Field>
        <Field label={t("auth.password")} hint={t("auth.passwordHint")} error={e.password?.message && t(e.password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="new-password" {...form.register("password")} />}</Field>
        <Button type="submit" className="w-full" disabled={form.formState.isSubmitting}>{t("auth.createAccount")}</Button>
      </form>
      <p className="mt-4 text-center text-sm text-ink-soft">{t("auth.haveAccount")} <Link className="text-brand hover:underline" to={`/login?next=${encodeURIComponent(safeNext(params.get("next")))}`}>{t("auth.signIn")}</Link></p>
    </AuthCard>
  );
}

export function VerifyEmail() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const [state, setState] = useState<"working" | "done" | "failed" | "retry">("working");
  const [problem, setProblem] = useState<string>();
  const started = useRef(false); // StrictMode runs effects twice, and the token works only once.
  const attempt = useCallback(() => {
    setState("working");
    unwrap(api.POST("/auth/verify-email", { body: { token: params.get("token") ?? "" } })).then(
      () => setState("done"),
      (e: unknown) => {
        // Only a rejected token means the link is bad. Anything else is temporary, and the link still works.
        if (e instanceof ApiError && e.code === "token_invalid") {
          setState("failed");
        } else {
          setProblem(errorText(t, e));
          setState("retry");
        }
      },
    );
  }, [params, t]);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    attempt();
  }, [attempt]);
  if (state === "retry") return <AuthCard title={t("auth.verifying")}><ErrorState title={problem} onRetry={attempt} retryLabel={t("common.retry")} /></AuthCard>;
  if (state === "working") return <AuthCard title={t("auth.verifying")}><p role="status" className="text-sm text-ink-soft">{t("common.loading")}</p></AuthCard>;
  return (
    <AuthCard title={t(state === "done" ? "auth.verifiedTitle" : "auth.verifyFailedTitle")} body={t(state === "done" ? "auth.verifiedBody" : "auth.verifyFailedBody")}>
      <Button asChild className="w-full"><Link to="/login">{t("auth.signIn")}</Link></Button>
    </AuthCard>
  );
}

export function ForgotPassword() {
  const { t } = useTranslation();
  const [sent, setSent] = useState(false);
  const form = useForm<z.infer<typeof emailOnlySchema>>({ resolver: zodResolver(emailOnlySchema), defaultValues: { email: "" } });
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    await unwrap(api.POST("/auth/password/forgot", { body: values })).catch(() => undefined);
    setSent(true); // The same outcome either way, so this cannot reveal which addresses exist.
  });
  if (sent) return <AuthCard title={t("auth.checkEmailTitle")} body={t("auth.checkEmailBody")}><p className="text-center text-sm"><Link className="text-brand hover:underline" to="/login">{t("auth.backToSignIn")}</Link></p></AuthCard>;
  return (
    <AuthCard title={t("auth.forgotTitle")}>
      <form onSubmit={(ev) => void submit(ev)} className="space-y-4" noValidate>
        <Field label={t("auth.email")} error={e.email?.message && t(e.email.message)}>{(ids) => <Input {...ids} type="email" autoComplete="username" {...form.register("email")} />}</Field>
        <Button type="submit" className="w-full" disabled={form.formState.isSubmitting}>{t("auth.sendLink")}</Button>
      </form>
      <p className="mt-4 text-center text-sm"><Link className="text-brand hover:underline" to="/login">{t("auth.backToSignIn")}</Link></p>
    </AuthCard>
  );
}

export function ResetPassword() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const [done, setDone] = useState(false);
  const [failure, setFailure] = useState<string>();
  const form = useForm<z.infer<typeof resetSchema>>({ resolver: zodResolver(resetSchema), defaultValues: { password: "" } });
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    setFailure(undefined);
    try {
      await unwrap(api.POST("/auth/password/reset", { body: { token: params.get("token") ?? "", password: values.password } }));
      queryClient.clear();
      setDone(true);
    } catch (err) {
      setFailure(applyServerError(t, err, form.setError, ["password"]));
    }
  });
  if (done) return <AuthCard title={t("auth.resetDone")}><Button asChild className="w-full"><Link to="/login">{t("auth.signIn")}</Link></Button></AuthCard>;
  return (
    <AuthCard title={t("auth.resetTitle")}>
      <form onSubmit={(ev) => void submit(ev)} className="space-y-4" noValidate>
        {failure ? <ErrorState title={failure} /> : null}
        <Field label={t("auth.newPassword")} hint={t("auth.passwordHint")} error={e.password?.message && t(e.password.message)}>{(ids) => <Input {...ids} type="password" autoComplete="new-password" {...form.register("password")} />}</Field>
        <Button type="submit" className="w-full" disabled={form.formState.isSubmitting}>{t("auth.setPassword")}</Button>
      </form>
    </AuthCard>
  );
}

export function Mfa() {
  const { t } = useTranslation();
  const me = useMe();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [failure, setFailure] = useState<string>();
  const form = useForm<z.infer<typeof mfaCodeSchema>>({ resolver: zodResolver(mfaCodeSchema), defaultValues: { code: "" } });
  const e = form.formState.errors;
  const submit = form.handleSubmit(async (values) => {
    setFailure(undefined);
    try {
      await unwrap(api.POST("/auth/mfa/verify", { body: values }));
      await resetSession();
      await navigate(safeNext(params.get("next")));
    } catch (err) {
      setFailure(err instanceof ApiError ? errorText(t, err) : t("errors.internal_error"));
    }
  });
  if (me.isPending) return null;
  if (!me.data) return <Navigate to="/login" replace />;
  if (!me.data.mfa_required) return <Navigate to="/" replace />;
  return (
    <AuthCard title={t("auth.mfaTitle")} body={t("auth.mfaBody")}>
      <form onSubmit={(ev) => void submit(ev)} className="space-y-4" noValidate>
        {failure ? <ErrorState title={failure} /> : null}
        <Field label={t("auth.mfaCode")} error={e.code?.message && t(e.code.message)}>{(ids) => <Input {...ids} autoComplete="one-time-code" autoFocus {...form.register("code")} />}</Field>
        <Button type="submit" className="w-full" disabled={form.formState.isSubmitting}>{t("auth.verify")}</Button>
      </form>
    </AuthCard>
  );
}
