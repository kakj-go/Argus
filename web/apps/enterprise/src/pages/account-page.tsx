import { useMemo, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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

const enrollmentCodeConstraint = formConstraint("TotpVerifyRequest", "code");
const proofCodeConstraint = formConstraint("MfaCodeRequest", "code");
const breakGlassReasonConstraint = formConstraint("BreakGlassCreate", "reason");
const breakGlassTicketConstraint = formConstraint(
  "BreakGlassCreate",
  "ticket_ref",
);

export function AccountPage() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const session = useEnterpriseAuthStore((state) => state.session);
  const clearAuth = useEnterpriseAuthStore((state) => state.clear);
  const restoreAuth = useEnterpriseAuthStore((state) => state.restore);
  const [securityError, setSecurityError] = useState<string | null>(null);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [enrollment, setEnrollment] = useState<Awaited<
    ReturnType<typeof api.auth.enrollTotp>
  > | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const [breakGlassCommandError, setBreakGlassCommandError] = useState<
    string | null
  >(null);

  const breakGlass = useQuery({
    queryKey: ["account", "break-glass"],
    queryFn: () => api.auth.listBreakGlassSessions(),
    enabled: Boolean(session),
  });
  const departments = useQuery({
    queryKey: ["account", "departments"],
    queryFn: () => api.org.listDepartments(),
    enabled: Boolean(session),
  });
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
  const passwordForm = useForm<PasswordForm>({
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
  const breakGlassSchema = useMemo(
    () =>
      z.object({
        reason: z
          .string()
          .trim()
          .min(
            breakGlassReasonConstraint.minLength ?? 8,
            t("account.breakGlass.reasonInvalid"),
          )
          .max(
            breakGlassReasonConstraint.maxLength ?? 2048,
            t("account.breakGlass.reasonInvalid"),
          ),
        ticketRef: z
          .string()
          .trim()
          .min(
            breakGlassTicketConstraint.minLength ?? 1,
            t("account.breakGlass.ticketRequired"),
          )
          .max(
            breakGlassTicketConstraint.maxLength ?? 256,
            t("account.breakGlass.ticketInvalid"),
          ),
      }),
    [t],
  );
  type BreakGlassForm = z.infer<typeof breakGlassSchema>;
  const breakGlassForm = useForm<BreakGlassForm>({
    resolver: zodResolver(breakGlassSchema),
    defaultValues: { reason: "", ticketRef: "" },
  });

  if (!session) return null;
  const { user } = session;
  const departmentName =
    "department_id" in user
      ? departments.data?.find((item) => item.id === user.department_id)?.name
      : undefined;

  const changePassword = passwordForm.handleSubmit(async (value) => {
    setPasswordError(null);
    try {
      await api.auth.changePassword({
        current_password: value.currentPassword,
        new_password: value.nextPassword,
        expected_version: user.version,
      });
      clearAuth();
      window.location.assign("/login");
    } catch (error) {
      const rule = passwordPolicyRuleFromError(error);
      const message = rule
        ? t(`account.passwordPolicy.${rule}`)
        : t("account.password.failed");
      const requestId = apiErrorRequestId(error);
      setPasswordError(
        requestId
          ? `${message} ${t("account.requestReference", { requestId })}`
          : message,
      );
    }
  });

  const beginEnrollment = async () => {
    setSecurityError(null);
    try {
      setEnrollment(await api.auth.enrollTotp());
      enrollmentForm.reset();
    } catch (error) {
      setSecurityError(
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
      setEnrollment(null);
      enrollmentForm.reset();
      await restoreAuth(api);
      setRecoveryCodes(result.codes);
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
    const requestedOperation =
      submitter instanceof HTMLButtonElement ? submitter.value : "step-up";
    const operation = ["step-up", "regenerate", "disable"].includes(
      requestedOperation,
    )
      ? (requestedOperation as "step-up" | "regenerate" | "disable")
      : "step-up";
    try {
      if (operation === "step-up") await api.auth.stepUp({ code: value.code });
      if (operation === "regenerate") {
        const result = await api.auth.regenerateRecoveryCodes({
          code: value.code,
        });
        await restoreAuth(api);
        setRecoveryCodes(result.codes);
      } else {
        if (operation === "disable")
          await api.auth.disableTotp({ code: value.code });
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

  const createBreakGlass = breakGlassForm.handleSubmit(async (value) => {
    setBreakGlassCommandError(null);
    breakGlassForm.clearErrors();
    try {
      await api.auth.createBreakGlassSession({
        reason: value.reason,
        ticket_ref: value.ticketRef,
      });
      breakGlassForm.reset();
      await queryClient.invalidateQueries({
        queryKey: ["account", "break-glass"],
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("account.breakGlass.failed"),
        fieldMap: { reason: "reason", ticket_ref: "ticketRef" },
        requestReference: (requestId) =>
          t("account.requestReference", { requestId }),
        setFieldError: (field, message) =>
          breakGlassForm.setError(
            field,
            { message, type: "server" },
            { shouldFocus: true },
          ),
        setFormError: (message) =>
          breakGlassForm.setError("root", { message, type: "server" }),
      });
    }
  });

  const revokeBreakGlass = async (id: string) => {
    setBreakGlassCommandError(null);
    try {
      await api.auth.revokeBreakGlassSession(id);
      await queryClient.invalidateQueries({
        queryKey: ["account", "break-glass"],
      });
    } catch (error) {
      setBreakGlassCommandError(
        formatApiError(error, t("account.breakGlass.failed"), (requestId) =>
          t("account.requestReference", { requestId }),
        ),
      );
    }
  };

  return (
    <PageShell
      description={t("account.description")}
      title={t("account.title")}
    >
      <div className="argus-account-stack">
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
                { label: t("account.profile.email"), value: user.email ?? "-" },
                {
                  label: t("account.profile.department"),
                  value: departmentName ?? "-",
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
            <div className="argus-account-security">
              {securityError && (
                <Alert
                  description={securityError}
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
                <form
                  className="argus-account-security__proof"
                  onSubmit={prove}
                >
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
                  <div className="argus-account-security__actions">
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
                    <Button
                      disabled={proofForm.formState.isSubmitting}
                      type="submit"
                      value="disable"
                      variant="danger"
                    >
                      {t("account.mfa.disable")}
                    </Button>
                  </div>
                </form>
              )}
              {session.step_up_expires_at && (
                <Alert
                  description={formatDateTime(session.step_up_expires_at)}
                  title={t("account.mfa.stepUpActive")}
                  tone="success"
                />
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.breakGlass.title")} />
          <CardContent>
            <div className="argus-account-break-glass">
              <p>{t("account.breakGlass.description")}</p>
              {breakGlassCommandError && (
                <Alert
                  description={breakGlassCommandError}
                  title={t("account.breakGlass.title")}
                  tone="danger"
                />
              )}
              <form
                className="argus-account-break-glass__form"
                onSubmit={createBreakGlass}
              >
                {breakGlassForm.formState.errors.root?.message && (
                  <Alert
                    description={breakGlassForm.formState.errors.root.message}
                    title={t("account.breakGlass.title")}
                    tone="danger"
                  />
                )}
                <Field
                  requirement="required"
                  error={breakGlassForm.formState.errors.reason?.message}
                  label={t("account.breakGlass.reason")}
                >
                  <Input
                    maxLength={breakGlassReasonConstraint.maxLength}
                    {...breakGlassForm.register("reason")}
                  />
                </Field>
                <Field
                  requirement="required"
                  error={breakGlassForm.formState.errors.ticketRef?.message}
                  label={t("account.breakGlass.ticket")}
                >
                  <Input
                    maxLength={breakGlassTicketConstraint.maxLength}
                    {...breakGlassForm.register("ticketRef")}
                  />
                </Field>
                <Button
                  disabled={breakGlassForm.formState.isSubmitting}
                  type="submit"
                  variant="danger"
                >
                  {t("account.breakGlass.create")}
                </Button>
              </form>
              {(breakGlass.data ?? [])
                .filter((item) => item.status === "active")
                .map((item) => (
                  <div
                    className="argus-account-break-glass__item"
                    key={item.id}
                  >
                    <div>
                      <strong>{item.ticket_ref}</strong>
                      <div>{item.reason}</div>
                      <small>
                        {t("account.breakGlass.expires")}:{" "}
                        {formatDateTime(item.expires_at)}
                      </small>
                    </div>
                    <Button
                      onClick={() => void revokeBreakGlass(item.id)}
                      variant="secondary"
                    >
                      {t("account.breakGlass.revoke")}
                    </Button>
                  </div>
                ))}
              {!breakGlass.isLoading &&
                !(breakGlass.data ?? []).some(
                  (item) => item.status === "active",
                ) && <p>{t("account.breakGlass.none")}</p>}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.password.title")} />
          <CardContent>
            <form
              className="argus-account-password-form"
              onSubmit={changePassword}
            >
              {passwordError && (
                <Alert
                  description={passwordError}
                  title={t("account.password.title")}
                  tone="danger"
                />
              )}
              <Field
                requirement="required"
                error={passwordForm.formState.errors.currentPassword?.message}
                label={t("account.password.current")}
              >
                <Input
                  autoComplete="current-password"
                  type="password"
                  {...passwordForm.register("currentPassword")}
                />
              </Field>
              <Field
                requirement="required"
                error={passwordForm.formState.errors.nextPassword?.message}
                hint={t("account.password.rule")}
                label={t("account.password.next")}
              >
                <Input
                  autoComplete="new-password"
                  type="password"
                  {...passwordForm.register("nextPassword")}
                />
              </Field>
              <Field
                requirement="required"
                error={passwordForm.formState.errors.confirmPassword?.message}
                label={t("account.password.confirm")}
              >
                <Input
                  autoComplete="new-password"
                  type="password"
                  {...passwordForm.register("confirmPassword")}
                />
              </Field>
              <Button
                disabled={passwordForm.formState.isSubmitting}
                type="submit"
                variant="primary"
              >
                {t("account.password.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>

      <Dialog
        description={t("account.mfa.enrollmentDescription")}
        footer={
          <Button
            disabled={enrollmentForm.formState.isSubmitting}
            form="enterprise-mfa-enrollment-form"
            type="submit"
            variant="primary"
          >
            {t("account.mfa.verify")}
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) {
            setEnrollment(null);
            enrollmentForm.reset();
          }
        }}
        open={Boolean(enrollment)}
        title={t("account.mfa.enrollmentTitle")}
      >
        {enrollment && (
          <form
            className="argus-account-enrollment"
            id="enterprise-mfa-enrollment-form"
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
              <code className="argus-mono argus-account-secret">
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
        footer={
          <Button onClick={() => setRecoveryCodes(null)} variant="primary">
            OK
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) setRecoveryCodes(null);
        }}
        open={Boolean(recoveryCodes)}
        title={t("account.mfa.recoveryTitle")}
      >
        <div className="argus-account-recovery-codes" role="list">
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
