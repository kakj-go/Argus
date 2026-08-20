import { useMemo, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { useApi } from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  Dialog,
  Field,
  Input,
  KeyValueGrid,
  PageShell,
} from "@argus/ui";
import "../i18n/account";
import "../styles/account.css";
import { formatDateTime } from "../components/settings/shared";

export function AccountPage() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const session = useEnterpriseAuthStore((state) => state.session);
  const clearAuth = useEnterpriseAuthStore((state) => state.clear);
  const restoreAuth = useEnterpriseAuthStore((state) => state.restore);
  const [securityError, setSecurityError] = useState<string | null>(null);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [enrollment, setEnrollment] = useState<Awaited<ReturnType<typeof api.auth.enrollTotp>> | null>(null);
  const [enrollmentCode, setEnrollmentCode] = useState("");
  const [proofCode, setProofCode] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const [reason, setReason] = useState("");
  const [ticketRef, setTicketRef] = useState("");
  const [breakGlassError, setBreakGlassError] = useState<string | null>(null);

  const breakGlass = useQuery({
    queryKey: ["account", "break-glass"],
    queryFn: () => api.auth.listBreakGlassSessions(),
    enabled: Boolean(session),
  });
  const passwordSchema = useMemo(
    () =>
      z
        .object({
          currentPassword: z.string().min(1, t("account.password.required")),
          nextPassword: z.string().min(12, t("account.password.weak")).regex(/[a-zA-Z]/, t("account.password.weak")).regex(/\d/, t("account.password.weak")),
          confirmPassword: z.string().min(1, t("account.password.required")),
        })
        .refine((value) => value.nextPassword === value.confirmPassword, {
          message: t("account.password.mismatch"),
          path: ["confirmPassword"],
        }),
    [t],
  );
  type PasswordForm = z.infer<typeof passwordSchema>;
  const passwordForm = useForm<PasswordForm>({
    resolver: zodResolver(passwordSchema),
    defaultValues: { currentPassword: "", nextPassword: "", confirmPassword: "" },
  });

  if (!session) return null;
  const { user } = session;

  const changePassword = passwordForm.handleSubmit(async (value) => {
    setPasswordError(null);
    try {
      await api.auth.changePassword({ current_password: value.currentPassword, new_password: value.nextPassword, expected_version: user.version });
      clearAuth();
      window.location.assign("/login");
    } catch {
      setPasswordError(t("account.password.failed"));
    }
  });

  const beginEnrollment = async () => {
    setSecurityError(null);
    try {
      setEnrollment(await api.auth.enrollTotp());
      setEnrollmentCode("");
    } catch {
      setSecurityError(t("account.mfa.failed"));
    }
  };

  const verifyEnrollment = async () => {
    if (!enrollment?.enrollment_id) return;
    setSecurityError(null);
    try {
      const result = await api.auth.verifyTotpEnrollment({ enrollment_id: enrollment.enrollment_id, code: enrollmentCode.trim() });
      setEnrollment(null);
      await restoreAuth(api);
      setRecoveryCodes(result.codes);
    } catch {
      setSecurityError(t("account.mfa.invalid"));
    }
  };

  const prove = async (operation: "step-up" | "regenerate" | "disable") => {
    setSecurityError(null);
    try {
      if (operation === "step-up") await api.auth.stepUp({ code: proofCode.trim() });
      if (operation === "regenerate") {
        const result = await api.auth.regenerateRecoveryCodes({ code: proofCode.trim() });
        await restoreAuth(api);
        setRecoveryCodes(result.codes);
      } else {
        if (operation === "disable") await api.auth.disableTotp({ code: proofCode.trim() });
        await restoreAuth(api);
      }
      setProofCode("");
    } catch {
      setSecurityError(t("account.mfa.invalid"));
    }
  };

  const createBreakGlass = async () => {
    setBreakGlassError(null);
    try {
      await api.auth.createBreakGlassSession({ reason: reason.trim(), ticket_ref: ticketRef.trim() });
      setReason("");
      setTicketRef("");
      await queryClient.invalidateQueries({ queryKey: ["account", "break-glass"] });
    } catch {
      setBreakGlassError(t("account.breakGlass.failed"));
    }
  };

  const revokeBreakGlass = async (id: string) => {
    setBreakGlassError(null);
    try {
      await api.auth.revokeBreakGlassSession(id);
      await queryClient.invalidateQueries({ queryKey: ["account", "break-glass"] });
    } catch {
      setBreakGlassError(t("account.breakGlass.failed"));
    }
  };

  return (
    <PageShell description={t("account.description")} title={t("account.title")}>
      <div className="argus-account-stack">
        <Card>
          <CardHeader title={t("account.profile.title")} />
          <CardContent>
            <KeyValueGrid columns={2} items={[
              { label: t("account.profile.username"), value: <code className="argus-mono">{user.username}</code> },
              { label: t("account.profile.displayName"), value: user.display_name },
              { label: t("account.profile.email"), value: user.email ?? "-" },
              { label: t("account.profile.department"), value: "department_id" in user ? <code className="argus-mono">{user.department_id}</code> : "-" },
            ]} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader action={<Badge tone={session.mfa_state === "enabled" ? "success" : "warning"}>{t(`account.mfa.state.${session.mfa_state}`)}</Badge>} title={t("account.mfa.title")} />
          <CardContent>
            <div className="argus-account-security">
              {securityError && <Alert description={securityError} title={t("account.mfa.title")} tone="danger" />}
              <p>{t("account.mfa.description")}</p>
              {session.mfa_state !== "enabled" ? (
                <Button onClick={() => void beginEnrollment()} variant="primary">{t("account.mfa.enroll")}</Button>
              ) : (
                <div className="argus-account-security__proof">
                  <Field label={t("account.mfa.proof")}><Input autoComplete="one-time-code" onChange={(event) => setProofCode(event.target.value)} value={proofCode} /></Field>
                  <div className="argus-account-security__actions">
                    <Button disabled={!proofCode.trim()} onClick={() => void prove("step-up")}>{t("account.mfa.stepUp")}</Button>
                    <Button disabled={!proofCode.trim()} onClick={() => void prove("regenerate")} variant="secondary">{t("account.mfa.regenerate")}</Button>
                    <Button disabled={!proofCode.trim()} onClick={() => void prove("disable")} variant="danger">{t("account.mfa.disable")}</Button>
                  </div>
                </div>
              )}
              {session.step_up_expires_at && <Alert description={formatDateTime(session.step_up_expires_at)} title={t("account.mfa.stepUpActive")} tone="success" />}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.breakGlass.title")} />
          <CardContent>
            <div className="argus-account-break-glass">
              <p>{t("account.breakGlass.description")}</p>
              {breakGlassError && <Alert description={breakGlassError} title={t("account.breakGlass.title")} tone="danger" />}
              <div className="argus-account-break-glass__form">
                <Field label={t("account.breakGlass.reason")}><Input onChange={(event) => setReason(event.target.value)} value={reason} /></Field>
                <Field label={t("account.breakGlass.ticket")}><Input onChange={(event) => setTicketRef(event.target.value)} value={ticketRef} /></Field>
                <Button disabled={reason.trim().length < 8 || !ticketRef.trim()} onClick={() => void createBreakGlass()} variant="danger">{t("account.breakGlass.create")}</Button>
              </div>
              {(breakGlass.data ?? []).filter((item) => item.status === "active").map((item) => (
                <div className="argus-account-break-glass__item" key={item.id}>
                  <div><strong>{item.ticket_ref}</strong><div>{item.reason}</div><small>{t("account.breakGlass.expires")}: {formatDateTime(item.expires_at)}</small></div>
                  <Button onClick={() => void revokeBreakGlass(item.id)} variant="secondary">{t("account.breakGlass.revoke")}</Button>
                </div>
              ))}
              {!breakGlass.isLoading && !(breakGlass.data ?? []).some((item) => item.status === "active") && <p>{t("account.breakGlass.none")}</p>}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.password.title")} />
          <CardContent>
            <form className="argus-account-password-form" onSubmit={changePassword}>
              {passwordError && <Alert description={passwordError} title={t("account.password.title")} tone="danger" />}
              <Field error={passwordForm.formState.errors.currentPassword?.message} label={t("account.password.current")}><Input autoComplete="current-password" type="password" {...passwordForm.register("currentPassword")} /></Field>
              <Field error={passwordForm.formState.errors.nextPassword?.message} hint={t("account.password.rule")} label={t("account.password.next")}><Input autoComplete="new-password" type="password" {...passwordForm.register("nextPassword")} /></Field>
              <Field error={passwordForm.formState.errors.confirmPassword?.message} label={t("account.password.confirm")}><Input autoComplete="new-password" type="password" {...passwordForm.register("confirmPassword")} /></Field>
              <Button disabled={passwordForm.formState.isSubmitting} type="submit" variant="primary">{t("account.password.submit")}</Button>
            </form>
          </CardContent>
        </Card>
      </div>

      <Dialog description={t("account.mfa.enrollmentDescription")} footer={<Button disabled={enrollmentCode.trim().length !== 6} onClick={() => void verifyEnrollment()} variant="primary">{t("account.mfa.verify")}</Button>} onOpenChange={(open) => { if (!open) setEnrollment(null); }} open={Boolean(enrollment)} title={t("account.mfa.enrollmentTitle")}>
        {enrollment && <div className="argus-account-enrollment"><Field label={t("account.mfa.secret")}><code className="argus-mono argus-account-secret">{enrollment.secret}</code></Field><Field label={t("account.mfa.code")}><Input autoComplete="one-time-code" autoFocus inputMode="numeric" maxLength={6} onChange={(event) => setEnrollmentCode(event.target.value)} value={enrollmentCode} /></Field></div>}
      </Dialog>
      <Dialog description={t("account.mfa.recoveryDescription")} footer={<Button onClick={() => setRecoveryCodes(null)} variant="primary">OK</Button>} onOpenChange={(open) => { if (!open) setRecoveryCodes(null); }} open={Boolean(recoveryCodes)} title={t("account.mfa.recoveryTitle")}>
        <div className="argus-account-recovery-codes" role="list">{recoveryCodes?.map((code) => <code className="argus-mono" key={code} role="listitem">{code}</code>)}</div>
      </Dialog>
    </PageShell>
  );
}
