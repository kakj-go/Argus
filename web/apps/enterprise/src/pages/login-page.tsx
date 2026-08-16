import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { PasswordChangeRequiredError, useApi } from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import { AppearanceControls, Button, Field, Input } from "@argus/ui";
import "../styles/auth.css";

/** 企业用户登录页：只接受 enterprise audience 的身份。 */
export function LoginPage() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const login = useEnterpriseAuthStore((state) => state.login);
  const logout = useEnterpriseAuthStore((state) => state.logout);
  const completePasswordChange = useEnterpriseAuthStore(
    (state) => state.completePasswordChange,
  );
  const search = useSearch({ strict: false }) as { redirect?: string };
  const [challengeId, setChallengeId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const loginSchema = useMemo(
    () =>
      z
        .object({
          mode: z.enum(["login", "change"]),
          username: z.string(),
          password: z.string(),
          newPassword: z.string(),
          confirmPassword: z.string(),
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
          if (values.newPassword.length < 12)
            context.addIssue({
              code: "custom",
              path: ["newPassword"],
              message: t("login.passwordTooShort"),
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
    },
  });
  const platformPortalUrl =
    import.meta.env.VITE_PLATFORM_URL ??
    `${window.location.protocol}//${window.location.hostname}:4174/login`;

  const submit = async (values: LoginForm) => {
    if (submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      const session = challengeId
        ? await completePasswordChange(api, {
            challenge_id: challengeId,
            temporary_password: values.password,
            new_password: values.newPassword,
          })
        : await login(api, {
            username: values.username.trim(),
            password: values.password,
          });
      if (session.session.audience !== "enterprise") {
        await logout(api);
        setError(t("login.wrongPortal"));
        return;
      }
      queryClient.clear();
      const target = search.redirect?.startsWith("/") ? search.redirect : "/";
      void navigate({ to: target as "/" });
    } catch (reason) {
      if (reason instanceof PasswordChangeRequiredError) {
        setChallengeId(reason.challenge.challenge_id);
        setValue("mode", "change", { shouldValidate: false });
        setError(null);
      } else
        setError(
          reason instanceof Error &&
            reason.message.includes("Unexpected enterprise")
            ? t("login.wrongPortal")
            : t("login.failed"),
        );
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
          <span className="argus-brand__mark">◉</span>
          <span className="argus-brand__name">
            Argus<small>enterprise</small>
          </span>
        </div>
        <h1>{t(challengeId ? "login.changePasswordTitle" : "login.title")}</h1>
        <p className="argus-login-card__subtitle">
          {t(challengeId ? "login.changePasswordSubtitle" : "login.subtitle")}
        </p>
        {error && (
          <p className="argus-login-card__error" role="alert">
            {error}
          </p>
        )}
        {!challengeId && (
          <Field error={errors.username?.message} label={t("login.username")}>
            <Input
              {...register("username")}
              autoComplete="username"
              autoFocus
              placeholder={t("login.usernamePlaceholder")}
            />
          </Field>
        )}
        {!challengeId && (
          <Field error={errors.password?.message} label={t("login.password")}>
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
            <Field
              error={errors.newPassword?.message}
              label={t("login.newPassword")}
            >
              <Input
                {...register("newPassword")}
                autoComplete="new-password"
                type="password"
              />
            </Field>
            <Field
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
        <Button
          className="argus-login-card__submit"
          disabled={submitting}
          type="submit"
          variant="primary"
        >
          {submitting
            ? t("login.submitting")
            : t(challengeId ? "login.changePasswordSubmit" : "login.submit")}
        </Button>
        <p className="argus-login-card__hint">
          {t("login.demoHint")}
          <br />
          <a href={platformPortalUrl}>{t("login.platformPortal")}</a>
        </p>
      </form>
    </div>
  );
}
