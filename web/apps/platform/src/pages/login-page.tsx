import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  apiErrorRequestId,
  MfaRequiredError,
  PasswordChangeRequiredError,
  passwordPolicyRuleFromError,
  validatePasswordPolicy,
  useApi,
} from "@argus/api-client";
import { usePlatformAuthStore } from "@argus/auth";
import { AppearanceControls, Button, Field, Input } from "@argus/ui";

/**
 * 平台超管登录页（无外壳）。Mock 同样验证账号密码；
 * 登录后校验 platformRole，非平台超管立即登出并拒绝进入。
 */
export function LoginPage() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const login = usePlatformAuthStore((state) => state.login);
  const logout = usePlatformAuthStore((state) => state.logout);
  const completePasswordChange = usePlatformAuthStore(
    (state) => state.completePasswordChange,
  );
  const completeMfaLogin = usePlatformAuthStore(
    (state) => state.completeMfaLogin,
  );
  const search = useSearch({ strict: false }) as { redirect?: string };
  const [challengeId, setChallengeId] = useState<string | null>(null);
  const [mfaChallengeId, setMfaChallengeId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const loginSchema = useMemo(
    () =>
      z
        .object({
          mode: z.enum(["login", "change", "mfa"]),
          username: z.string(),
          password: z.string(),
          newPassword: z.string(),
          confirmPassword: z.string(),
          mfaCode: z.string(),
        })
        .superRefine((values, context) => {
          if (values.mode === "login") {
            if (!values.username.trim())
              context.addIssue({
                code: "custom",
                path: ["username"],
                message: t("login.required"),
              });
            if (!values.password)
              context.addIssue({
                code: "custom",
                path: ["password"],
                message: t("login.required"),
              });
            return;
          }
          if (values.mode === "mfa") {
            if (values.mfaCode.trim().length < 6)
              context.addIssue({ code: "custom", path: ["mfaCode"], message: t("login.mfaInvalid") });
            return;
          }
          const passwordRule = validatePasswordPolicy(values.newPassword, {
            username: values.username,
            previousPassword: values.password,
          });
          if (passwordRule)
            context.addIssue({
              code: "custom",
              path: ["newPassword"],
              message: t(`login.passwordPolicy.${passwordRule}`),
            });
          if (values.newPassword !== values.confirmPassword)
            context.addIssue({
              code: "custom",
              path: ["confirmPassword"],
              message: t("login.passwordMismatch"),
            });
        }),
    [t],
  );
  type LoginForm = z.infer<typeof loginSchema>;
  const {
    register,
    setValue,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: {
      mode: "login",
      username: "",
      password: "",
      newPassword: "",
      confirmPassword: "",
      mfaCode: "",
    },
  });

  const submit = async (values: LoginForm) => {
    if (submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      const session = mfaChallengeId
        ? await completeMfaLogin(api, {
            challenge_id: mfaChallengeId,
            code: values.mfaCode.trim(),
          })
        : challengeId
        ? await completePasswordChange(api, {
            challenge_id: challengeId,
            temporary_password: values.password,
            new_password: values.newPassword,
          })
        : await login(api, {
            username: values.username.trim(),
            password: values.password,
          });
      if (session.session.audience !== "platform") {
        await logout(api);
        setError(t("login.notSuperAdmin"));
        return;
      }
      queryClient.clear();
      const target = session.mfa_state === "enrollment_required"
        ? "/account"
        : search.redirect?.startsWith("/") ? search.redirect : "/";
      void navigate({ to: target as "/" });
    } catch (reason) {
      if (reason instanceof PasswordChangeRequiredError) {
        setChallengeId(reason.challenge.challenge_id);
        setValue("mode", "change", { shouldValidate: false });
        setError(null);
      } else if (reason instanceof MfaRequiredError) {
        setMfaChallengeId(reason.challenge.challenge_id);
        setValue("mode", "mfa", { shouldValidate: false });
        setError(null);
      } else {
        const passwordRule = passwordPolicyRuleFromError(reason);
        const message = passwordRule
          ? t(`login.passwordPolicy.${passwordRule}`)
          : t("login.failed");
        const requestId = apiErrorRequestId(reason);
        setError(
          requestId
            ? `${message} ${t("login.requestReference", { requestId })}`
            : message,
        );
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="argus-login-page">
      <div className="argus-login-page__controls">
        <AppearanceControls />
      </div>
      <form
        className="argus-login-card"
        onSubmit={handleSubmit((values) => void submit(values))}
      >
        <div className="argus-brand argus-login-card__brand">
          <span className="argus-brand__mark argus-brand__mark--platform">
            ◉
          </span>
          <span className="argus-brand__name">
            Argus<small>{t("shell.brand.domain")}</small>
          </span>
        </div>
        <h1>{t(mfaChallengeId ? "login.mfaTitle" : challengeId ? "login.changePasswordTitle" : "login.title")}</h1>
        <p className="argus-login-card__subtitle">
          {t(mfaChallengeId ? "login.mfaSubtitle" : challengeId ? "login.changePasswordSubtitle" : "login.subtitle")}
        </p>
        {error && (
          <p className="argus-login-card__error" role="alert">
            {error}
          </p>
        )}
        {!challengeId && !mfaChallengeId && (
          <Field requirement="required" error={errors.username?.message} label={t("login.username")}>
            <Input
              {...register("username")}
              autoComplete="username"
              autoFocus
              placeholder={t("login.usernamePlaceholder")}
            />
          </Field>
        )}
        {!challengeId && !mfaChallengeId && (
          <Field requirement="required" error={errors.password?.message} label={t("login.password")}>
            <Input
              {...register("password")}
              autoComplete="current-password"
              placeholder={t("login.passwordPlaceholder")}
              type="password"
            />
          </Field>
        )}
        {challengeId && (
          <>
            <Field requirement="required"
              error={errors.newPassword?.message}
              label={t("login.newPassword")}
            >
              <Input
                {...register("newPassword")}
                autoComplete="new-password"
                type="password"
              />
            </Field>
            <Field requirement="required"
              error={errors.confirmPassword?.message}
              label={t("login.confirmPassword")}
            >
              <Input
                {...register("confirmPassword")}
                autoComplete="new-password"
                type="password"
              />
            </Field>
          </>
        )}
        {mfaChallengeId && (
          <Field requirement="required" error={errors.mfaCode?.message} label={t("login.mfaCode")}>
            <Input {...register("mfaCode")} autoComplete="one-time-code" autoFocus inputMode="numeric" />
          </Field>
        )}
        <Button
          className="argus-login-card__submit"
          disabled={submitting}
          type="submit"
          variant="primary"
        >
          {submitting
            ? t("login.submitting")
            : t(mfaChallengeId ? "login.mfaSubmit" : challengeId ? "login.changePasswordSubmit" : "login.submit")}
        </Button>
        <p className="argus-login-card__hint">{t("login.hint")}</p>
      </form>
    </div>
  );
}
