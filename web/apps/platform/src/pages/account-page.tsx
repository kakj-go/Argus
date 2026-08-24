import { useMemo, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  apiErrorRequestId,
  formConstraint,
  formatApiError,
  passwordPolicyRuleFromError,
  presentApiFormError,
  validatePasswordPolicy,
  useApi,
} from "@argus/api-client";
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

const enrollmentCodeConstraint = formConstraint("TotpVerifyRequest", "code");
const proofCodeConstraint = formConstraint("MfaCodeRequest", "code");

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
  const [enrollment, setEnrollment] = useState<Awaited<
    ReturnType<typeof api.auth.enrollTotp>
  > | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const passwordSchema = useMemo(
    () =>
      z
        .object({
          currentPassword: z.string().min(1, t("account.password.required")),
          nextPassword: z.string().min(1, t("account.password.required")),
          confirmPassword: z.string().min(1, t("account.password.required")),
        })
        .superRefine((value, context) => {
          const rule = validatePasswordPolicy(value.nextPassword, {
            username: session?.user.username,
            email: session?.user.email,
            previousPassword: value.currentPassword,
          });
          if (rule) {
            context.addIssue({
              code: "custom",
              path: ["nextPassword"],
              message: t(`account.passwordPolicy.${rule}`),
            });
          }
          if (value.nextPassword !== value.confirmPassword) {
            context.addIssue({
              code: "custom",
              path: ["confirmPassword"],
              message: t("account.password.mismatch"),
            });
          }
        }),
    [session?.user.email, session?.user.username, t],
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
  const enrollmentSchema = useMemo(
    () =>
      z.object({
        code: z
          .string()
          .trim()
          .min(1, t("account.mfa.codeRequired"))
          .regex(
            new RegExp(enrollmentCodeConstraint.pattern ?? "^[0-9]{6}$"),
            t("account.mfa.codeInvalid"),
          ),
      }),
    [t],
  );
  type EnrollmentForm = z.infer<typeof enrollmentSchema>;
  const enrollmentForm = useForm<EnrollmentForm>({
    resolver: zodResolver(enrollmentSchema),
    defaultValues: { code: "" },
  });
  const proofSchema = useMemo(
    () =>
      z.object({
        code: z
          .string()
          .trim()
          .min(
            proofCodeConstraint.minLength ?? 6,
            t("account.mfa.proofInvalid"),
          )
          .max(
            proofCodeConstraint.maxLength ?? 64,
            t("account.mfa.proofInvalid"),
          ),
      }),
    [t],
  );
  type ProofForm = z.infer<typeof proofSchema>;
  const proofForm = useForm<ProofForm>({
    resolver: zodResolver(proofSchema),
    defaultValues: { code: "" },
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
    } catch (reason) {
      const rule = passwordPolicyRuleFromError(reason);
      const message = rule
        ? t(`account.passwordPolicy.${rule}`)
        : t("account.password.failed");
      const requestId = apiErrorRequestId(reason);
      setSubmitError(
        requestId
          ? `${message} ${t("account.requestReference", { requestId })}`
          : message,
      );
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
      enrollmentForm.reset();
    } catch (error) {
      setMfaError(
        formatApiError(error, t("account.mfa.failed"), (requestId) =>
          t("account.requestReference", { requestId }),
        ),
      );
    }
  };

  const verifyEnrollment = enrollmentForm.handleSubmit(async (value) => {
    if (!enrollment?.enrollment_id) return;
    enrollmentForm.clearErrors();
    try {
      const result = await api.auth.verifyTotpEnrollment({
        enrollment_id: enrollment.enrollment_id,
        code: value.code,
      });
      setRecoveryCodes(result.codes);
      setEnrollment(null);
      enrollmentForm.reset();
      await restoreAuth(api);
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("account.mfa.invalid"),
        fieldMap: { code: "code" },
        requestReference: (requestId) =>
          t("account.requestReference", { requestId }),
        setFieldError: (field, message) =>
          enrollmentForm.setError(
            field,
            { message, type: "server" },
            { shouldFocus: true },
          ),
        setFormError: (message) =>
          enrollmentForm.setError("root", { message, type: "server" }),
      });
    }
  });

  const prove = proofForm.handleSubmit(async (value, event) => {
    proofForm.clearErrors();
    const submitter = (event?.nativeEvent as SubmitEvent | undefined)
      ?.submitter;
    const operation =
      submitter instanceof HTMLButtonElement && submitter.value === "regenerate"
        ? "regenerate"
        : "step-up";
    try {
      if (operation === "regenerate") {
        const result = await api.auth.regenerateRecoveryCodes({
          code: value.code,
        });
        setRecoveryCodes(result.codes);
      } else {
        await api.auth.stepUp({ code: value.code });
        await restoreAuth(api);
      }
      proofForm.reset();
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("account.mfa.invalid"),
        fieldMap: { code: "code" },
        requestReference: (requestId) =>
          t("account.requestReference", { requestId }),
        setFieldError: (field, message) =>
          proofForm.setError(
            field,
            { message, type: "server" },
            { shouldFocus: true },
          ),
        setFormError: (message) =>
          proofForm.setError("root", { message, type: "server" }),
      });
    }
  });

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
            action={
              <Badge
                tone={session.mfa_state === "enabled" ? "success" : "warning"}
              >
                {t(`account.mfa.state.${session.mfa_state}`)}
              </Badge>
            }
            title={t("account.mfa.title")}
          />
          <CardContent>
            <div className="argus-account-mfa">
              {mfaError && (
                <Alert
                  description={mfaError}
                  title={t("account.mfa.title")}
                  tone="danger"
                />
              )}
              <p>{t("account.mfa.description")}</p>
              {session.mfa_state !== "enabled" ? (
                <Button
                  onClick={() => void beginEnrollment()}
                  variant="primary"
                >
                  {t("account.mfa.enroll")}
                </Button>
              ) : (
                <form className="argus-account-mfa__proof" onSubmit={prove}>
                  {proofForm.formState.errors.root?.message && (
                    <Alert
                      description={proofForm.formState.errors.root.message}
                      title={t("account.mfa.title")}
                      tone="danger"
                    />
                  )}
                  <Field
                    requirement="required"
                    error={proofForm.formState.errors.code?.message}
                    label={t("account.mfa.proof")}
                  >
                    <Input
                      autoComplete="one-time-code"
                      {...proofForm.register("code")}
                    />
                  </Field>
                  <div className="argus-account-mfa__actions">
                    <Button
                      disabled={proofForm.formState.isSubmitting}
                      type="submit"
                      value="step-up"
                    >
                      {t("account.mfa.stepUp")}
                    </Button>
                    <Button
                      disabled={proofForm.formState.isSubmitting}
                      type="submit"
                      value="regenerate"
                      variant="secondary"
                    >
                      {t("account.mfa.regenerate")}
                    </Button>
                  </div>
                </form>
              )}
              {session.step_up_expires_at && (
                <Alert
                  description={formatDateTime(
                    session.step_up_expires_at,
                    i18n.language,
                  )}
                  title={t("account.mfa.stepUpActive")}
                  tone="success"
                />
              )}
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
                requirement="required"
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
                requirement="required"
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
                requirement="required"
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
        onOpenChange={(open) => {
          if (!open) {
            setEnrollment(null);
            enrollmentForm.reset();
          }
        }}
        open={Boolean(enrollment)}
        title={t("account.mfa.enrollmentTitle")}
        footer={
          <Button
            disabled={enrollmentForm.formState.isSubmitting}
            form="platform-mfa-enrollment-form"
            type="submit"
            variant="primary"
          >
            {t("account.mfa.verify")}
          </Button>
        }
      >
        {enrollment && (
          <form
            className="argus-account-mfa__enrollment"
            id="platform-mfa-enrollment-form"
            onSubmit={verifyEnrollment}
          >
            {enrollmentForm.formState.errors.root?.message && (
              <Alert
                description={enrollmentForm.formState.errors.root.message}
                title={t("account.mfa.enrollmentTitle")}
                tone="danger"
              />
            )}
            <Field requirement="none" label={t("account.mfa.secret")}>
              <code className="argus-mono argus-account-mfa__secret">
                {enrollment.secret}
              </code>
            </Field>
            <Field
              requirement="required"
              error={enrollmentForm.formState.errors.code?.message}
              label={t("account.mfa.code")}
            >
              <Input
                autoComplete="one-time-code"
                autoFocus
                inputMode="numeric"
                maxLength={6}
                {...enrollmentForm.register("code")}
              />
            </Field>
          </form>
        )}
      </Dialog>
      <Dialog
        description={t("account.mfa.recoveryDescription")}
        onOpenChange={(open) => {
          if (!open) setRecoveryCodes(null);
        }}
        open={Boolean(recoveryCodes)}
        title={t("account.mfa.recoveryTitle")}
        footer={
          <Button onClick={() => setRecoveryCodes(null)} variant="primary">
            {t("common.close")}
          </Button>
        }
      >
        <div className="argus-account-mfa__codes" role="list">
          {recoveryCodes?.map((code) => (
            <code className="argus-mono" key={code} role="listitem">
              {code}
            </code>
          ))}
        </div>
      </Dialog>
    </PageShell>
  );
}
