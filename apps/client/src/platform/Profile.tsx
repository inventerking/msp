import { ApiError, unwrap } from "@msp/api-client";
import { Badge, Button, Card, ConfirmDialog, ErrorState, Field, Input, PageHeader, SkeletonRows, Table } from "@msp/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api, tenantKey, useMe } from "../app/api";
import { errorText } from "../app/errors";

export function Profile() {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const qc = useQueryClient();
  const sessions = useQuery({ queryKey: tenantKey(me, "sessions"), queryFn: () => unwrap(api.GET("/auth/sessions")) });
  const revoke = useMutation({ mutationFn: (id: string) => unwrap(api.DELETE("/auth/sessions/{id}", { params: { path: { id } } })), onSuccess: () => qc.invalidateQueries({ queryKey: tenantKey(me, "sessions") }) });
  const [setup, setSetup] = useState<{ secret: string; url: string }>();
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState<string[]>();
  const [failure, setFailure] = useState<string>();
  const [confirmOff, setConfirmOff] = useState(false);
  const [reauth, setReauth] = useState<() => Promise<void>>();
  const [password, setPassword] = useState("");
  const [reauthError, setReauthError] = useState<string>();

  /** Runs a sensitive action, asking for the password first if the API says it is needed. */
  const sensitive = async (action: () => Promise<void>) => {
    setFailure(undefined);
    try {
      await action();
    } catch (e) {
      if (e instanceof ApiError && e.code === "recent_auth_required") setReauth(() => action);
      else setFailure(errorText(t, e));
    }
  };
  const enroll = () => sensitive(async () => {
    const r = await unwrap(api.POST("/auth/mfa/totp/enroll"));
    setSetup({ secret: r.secret, url: r.otpauth_url });
  });
  const confirm = async () => {
    setFailure(undefined);
    try {
      const r = await unwrap(api.POST("/auth/mfa/totp/confirm", { body: { code } }));
      setRecovery(r.recovery_codes);
      setSetup(undefined);
      setCode("");
      await qc.invalidateQueries({ queryKey: ["me"] });
    } catch (e) {
      setFailure(errorText(t, e));
    }
  };
  const disable = () => sensitive(async () => {
    await unwrap(api.DELETE("/auth/mfa"));
    setConfirmOff(false);
    await qc.invalidateQueries({ queryKey: ["me"] });
  });
  const submitReauth = async () => {
    setReauthError(undefined);
    try {
      await unwrap(api.POST("/auth/reauthenticate", { body: { password } }));
      const action = reauth;
      setReauth(undefined);
      setPassword("");
      await action?.();
    } catch (e) {
      setReauthError(errorText(t, e));
    }
  };

  return (
    <>
      <PageHeader title={t("profile.title")} description={t("profile.subtitle")} />
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("profile.mfa")}</h2>
        <Card className="p-4">
          {failure ? <div className="mb-3"><ErrorState title={failure} /></div> : null}
          {recovery ? (
            <div>
              <p className="text-sm font-medium">{t("profile.recoveryTitle")}</p>
              <p className="mt-1 text-sm text-ink-soft">{t("profile.recoveryBody")}</p>
              <ul className="my-3 grid gap-1 rounded-md bg-sunken p-3 font-mono text-xs sm:grid-cols-2">{recovery.map((c) => <li key={c}>{c}</li>)}</ul>
              <Button onClick={() => setRecovery(undefined)}>{t("profile.recoveryDone")}</Button>
            </div>
          ) : setup ? (
            <div className="max-w-md space-y-3">
              <p className="text-sm text-ink-soft">{t("profile.mfaSetup")}</p>
              <div><p className="text-xs text-ink-faint">{t("profile.mfaKey")}</p><code data-testid="mfa-secret" className="text-sm break-all">{setup.secret}</code></div>
              <a className="text-sm text-brand hover:underline" href={setup.url}>{t("profile.mfaOpen")}</a>
              <Field label={t("auth.mfaCode")}>{(ids) => <Input {...ids} autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} />}</Field>
              <Button disabled={code.trim().length < 6} onClick={() => void confirm()}>{t("profile.mfaConfirm")}</Button>
            </div>
          ) : (
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <Badge tone={me?.user.mfa_enabled ? "ok" : "neutral"}>{me?.user.mfa_enabled ? t("profile.mfaOn") : t("profile.mfaOff")}</Badge>
                <p className="mt-1 text-sm text-ink-soft">{t("profile.mfaBody")}</p>
              </div>
              {me?.user.mfa_enabled ? <Button variant="secondary" onClick={() => setConfirmOff(true)}>{t("profile.mfaDisable")}</Button> : <Button onClick={() => void enroll()}>{t("profile.mfaEnable")}</Button>}
            </div>
          )}
        </Card>
      </section>
      <section>
        <h2 className="mb-2 text-sm font-medium text-ink-soft">{t("profile.sessions")}</h2>
        <Card>
          {sessions.isPending ? <SkeletonRows rows={2} label={t("common.loading")} /> : null}
          {sessions.isError ? <div className="p-4"><ErrorState title={errorText(t, sessions.error)} onRetry={() => void sessions.refetch()} retryLabel={t("common.retry")} /></div> : null}
          {sessions.data ? (
            <Table>
              <thead><tr><th>{t("profile.device")}</th><th>{t("profile.signedIn")}</th><th /></tr></thead>
              <tbody>
                {sessions.data.data.map((s) => (
                  <tr key={s.id}>
                    <td className="max-w-md truncate text-ink-soft">{s.user_agent ?? t("profile.unknownDevice")} {s.current ? <Badge tone="info">{t("profile.thisDevice")}</Badge> : null}</td>
                    <td className="whitespace-nowrap text-ink-soft">{new Date(s.created_at).toLocaleString()}</td>
                    <td className="text-right">{s.current ? null : <Button variant="ghost" size="sm" onClick={() => revoke.mutate(s.id)}>{t("profile.revoke")}</Button>}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
          ) : null}
        </Card>
      </section>
      <ConfirmDialog open={confirmOff} danger title={t("profile.mfaDisableTitle")} description={t("profile.mfaDisableBody")} confirmLabel={t("profile.mfaDisable")} cancelLabel={t("common.cancel")} onCancel={() => setConfirmOff(false)} onConfirm={() => void disable()} />
      {reauth ? (
        <div role="dialog" aria-modal="true" aria-label={t("profile.reauthTitle")} className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4">
          <Card className="w-full max-w-sm p-5">
            <h2 className="text-base font-semibold">{t("profile.reauthTitle")}</h2>
            <p className="mt-1 mb-3 text-sm text-ink-soft">{t("profile.reauthBody")}</p>
            <Field label={t("auth.password")} error={reauthError}>{(ids) => <Input {...ids} type="password" autoComplete="current-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />}</Field>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="secondary" onClick={() => { setReauth(undefined); setPassword(""); }}>{t("common.cancel")}</Button>
              <Button disabled={!password} onClick={() => void submitReauth()}>{t("profile.confirm")}</Button>
            </div>
          </Card>
        </div>
      ) : null}
    </>
  );
}
