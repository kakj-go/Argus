import { useMemo, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { useApi } from "@argus/api-client";
import { usePlatformAuthStore } from "@argus/auth";
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  DataTable,
  Dialog,
  Field,
  Input,
  KeyValueGrid,
  PageShell,
} from "@argus/ui";
import { formatDateTime } from "../lib/format";

type LoginSessionRow = {
  id: string;
  device: string;
  ip: string;
  lastActiveAt?: string;
  current: boolean;
};

/** 我的账号：账号信息、真实改密和当前登录会话。MFA 在 M8 接入。 */
export function AccountPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const session = usePlatformAuthStore((state) => state.session);
  const clearAuth = usePlatformAuthStore((state) => state.clear);
  const restoreAuth = usePlatformAuthStore((state) => state.restore);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [mfaError, setMfaError] = useState<string | null>(null);
  const [enrollment, setEnrollment] = useState<Awaited<ReturnType<typeof api.auth.enrollTotp>> | null>(null);
  const [mfaCode, setMfaCode] = useState("");
  const [proofCode, setProofCode] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const passwordSchema = useMemo(
    () =>
      z
        .object({
          currentPassword: z.string().min(1, t("account.password.required")),
          nextPassword: z
            .string()
            .min(12, t("account.password.weak"))
            .regex(/[a-zA-Z]/, t("account.password.weak"))
            .regex(/\d/, t("account.password.weak")),
          confirmPassword: z.string().min(1, t("account.password.required")),
        })
        .refine((value) => value.nextPassword === value.confirmPassword, {
          message: t("account.password.mismatch"),
          path: ["confirmPassword"],
        }),
    [t],
  );
  type PasswordForm = z.infer<typeof passwordSchema>;
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<PasswordForm>({
    resolver: zodResolver(passwordSchema),
    defaultValues: {
      currentPassword: "",
      nextPassword: "",
      confirmPassword: "",
    },
  });

  if (!session) return null;
  const { user } = session;

  const handlePasswordSubmit = handleSubmit(async (values) => {
    setSubmitError(null);
    try {
      await api.auth.changePassword({
        current_password: values.currentPassword,
        new_password: values.nextPassword,
        expected_version: user.version,
      });
      clearAuth();
      window.location.assign("/login");
    } catch {
      setSubmitError(t("account.password.failed"));
    }
  });

  const sessionRows: LoginSessionRow[] = [
    {
      id: "sess-current",
      device: "macOS · Chrome",
      ip: "10.0.0.2",
      lastActiveAt: session.session.issued_at,
      current: true,
    },
  ];

  const beginEnrollment = async () => {
    setMfaError(null);
    try {
      setEnrollment(await api.auth.enrollTotp());
      setMfaCode("");
    } catch {
      setMfaError(t("account.mfa.failed"));
    }
  };

  const verifyEnrollment = async () => {
    if (!enrollment?.enrollment_id) return;
    setMfaError(null);
    try {
      const result = await api.auth.verifyTotpEnrollment({ enrollment_id: enrollment.enrollment_id, code: mfaCode.trim() });
      setRecoveryCodes(result.codes);
      setEnrollment(null);
      await restoreAuth(api);
    } catch {
      setMfaError(t("account.mfa.invalid"));
    }
  };

  const regenerateCodes = async () => {
    setMfaError(null);
    try {
      const result = await api.auth.regenerateRecoveryCodes({ code: proofCode.trim() });
      setRecoveryCodes(result.codes);
      setProofCode("");
    } catch {
      setMfaError(t("account.mfa.invalid"));
    }
  };

  const stepUp = async () => {
    setMfaError(null);
    try {
      await api.auth.stepUp({ code: proofCode.trim() });
      setProofCode("");
      await restoreAuth(api);
    } catch {
      setMfaError(t("account.mfa.invalid"));
    }
  };

  return (
    <PageShell
      description={t("account.description")}
      title={t("account.title")}
    >
      <div className="argus-platform-stack">
        <Card>
          <CardHeader title={t("account.profile.title")} />
          <CardContent>
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: t("account.profile.username"),
                  value: <code className="argus-mono">{user.username}</code>,
                },
                {
                  label: t("account.profile.displayName"),
                  value: user.display_name,
                },
                {
                  label: t("account.profile.email"),
                  value: user.email ?? t("common.none"),
                },
                {
                  label: t("account.profile.role"),
                  value: (
                    <Badge tone="accent">
                      {"role" in user ? user.role : "platform_super_admin"}
                    </Badge>
                  ),
                },
                {
                  label: t("account.profile.lastLogin"),
                  value: formatDateTime(
                    session.session.issued_at,
                    i18n.language,
                  ),
                },
              ]}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader
            action={<Badge tone={session.mfa_state === "enabled" ? "success" : "warning"}>{t(`account.mfa.state.${session.mfa_state}`)}</Badge>}
            title={t("account.mfa.title")}
          />
          <CardContent>
            <div className="argus-account-mfa">
              {mfaError && <Alert description={mfaError} title={t("account.mfa.title")} tone="danger" />}
              <p>{t("account.mfa.description")}</p>
              {session.mfa_state !== "enabled" ? (
                <Button onClick={() => void beginEnrollment()} variant="primary">{t("account.mfa.enroll")}</Button>
              ) : (
                <div className="argus-account-mfa__proof">
                  <Field label={t("account.mfa.proof")}>
                    <Input autoComplete="one-time-code" inputMode="numeric" onChange={(event) => setProofCode(event.target.value)} value={proofCode} />
                  </Field>
                  <div className="argus-account-mfa__actions">
                    <Button disabled={!proofCode.trim()} onClick={() => void stepUp()}>{t("account.mfa.stepUp")}</Button>
                    <Button disabled={!proofCode.trim()} onClick={() => void regenerateCodes()} variant="secondary">{t("account.mfa.regenerate")}</Button>
                  </div>
                </div>
              )}
              {session.step_up_expires_at && <Alert description={formatDateTime(session.step_up_expires_at, i18n.language)} title={t("account.mfa.stepUpActive")} tone="success" />}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.password.title")} />
          <CardContent>
            <form
              className="argus-account-password-form"
              onSubmit={handlePasswordSubmit}
            >
              {submitError && (
                <Alert
                  description={submitError}
                  title={t("account.password.title")}
                  tone="danger"
                />
              )}
              <Field
                error={errors.currentPassword?.message}
                label={t("account.password.current")}
              >
                <Input
                  autoComplete="current-password"
                  {...register("currentPassword")}
                  type="password"
                />
              </Field>
              <Field
                error={errors.nextPassword?.message}
                hint={t("account.password.rule")}
                label={t("account.password.next")}
              >
                <Input
                  autoComplete="new-password"
                  {...register("nextPassword")}
                  type="password"
                />
              </Field>
              <Field
                error={errors.confirmPassword?.message}
                label={t("account.password.confirm")}
              >
                <Input
                  autoComplete="new-password"
                  {...register("confirmPassword")}
                  type="password"
                />
              </Field>
              <Button disabled={isSubmitting} type="submit" variant="primary">
                {t("account.password.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.sessions.title")} />
          <CardContent>
            <DataTable<LoginSessionRow>
              columns={[
                {
                  key: "device",
                  header: t("account.sessions.device"),
                },
                {
                  key: "ip",
                  header: t("account.sessions.ip"),
                  render: (row) => <code className="argus-mono">{row.ip}</code>,
                },
                {
                  key: "lastActiveAt",
                  header: t("account.sessions.lastActive"),
                  render: (row) =>
                    formatDateTime(row.lastActiveAt, i18n.language),
                },
                {
                  key: "current",
                  header: t("common.status"),
                  render: (row) =>
                    row.current ? (
                      <Badge tone="success">
                        {t("account.sessions.current")}
                      </Badge>
                    ) : (
                      t("common.none")
                    ),
                },
              ]}
              data={sessionRows}
              getRowKey={(row) => row.id}
            />
          </CardContent>
        </Card>
      </div>
      <Dialog
        description={t("account.mfa.enrollmentDescription")}
        onOpenChange={(open) => { if (!open) setEnrollment(null); }}
        open={Boolean(enrollment)}
        title={t("account.mfa.enrollmentTitle")}
        footer={<Button disabled={mfaCode.trim().length !== 6} onClick={() => void verifyEnrollment()} variant="primary">{t("account.mfa.verify")}</Button>}
      >
        {enrollment && <div className="argus-account-mfa__enrollment">
          <Field label={t("account.mfa.secret")}><code className="argus-mono argus-account-mfa__secret">{enrollment.secret}</code></Field>
          <Field label={t("account.mfa.code")}><Input autoComplete="one-time-code" autoFocus inputMode="numeric" maxLength={6} onChange={(event) => setMfaCode(event.target.value)} value={mfaCode} /></Field>
        </div>}
      </Dialog>
      <Dialog
        description={t("account.mfa.recoveryDescription")}
        onOpenChange={(open) => { if (!open) setRecoveryCodes(null); }}
        open={Boolean(recoveryCodes)}
        title={t("account.mfa.recoveryTitle")}
        footer={<Button onClick={() => setRecoveryCodes(null)} variant="primary">{t("common.close")}</Button>}
      >
        <div className="argus-account-mfa__codes" role="list">
          {recoveryCodes?.map((code) => <code className="argus-mono" key={code} role="listitem">{code}</code>)}
        </div>
      </Dialog>
    </PageShell>
  );
}
