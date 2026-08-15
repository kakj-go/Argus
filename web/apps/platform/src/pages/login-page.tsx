import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { useAuthStore } from "@argus/auth";
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
  const login = useAuthStore((state) => state.login);
  const logout = useAuthStore((state) => state.logout);
  const search = useSearch({ strict: false }) as { redirect?: string };
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      const session = await login(api, { username: username.trim(), password });
      if (session.user.platformRole !== "platform_super_admin") {
        await logout(api);
        setError(t("login.notSuperAdmin"));
        return;
      }
      queryClient.clear();
      const target = search.redirect?.startsWith("/") ? search.redirect : "/";
      void navigate({ to: target as "/" });
    } catch {
      setError(t("login.failed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="login-page">
      <div className="login-page__controls">
        <AppearanceControls />
      </div>
      <form className="login-card" onSubmit={(event) => void handleSubmit(event)}>
        <div className="brand login-card__brand">
          <span className="brand__mark brand__mark--platform">◉</span>
          <span className="brand__name">
            Argus<small>{t("shell.brand.domain")}</small>
          </span>
        </div>
        <h1>{t("login.title")}</h1>
        <p className="login-card__subtitle">{t("login.subtitle")}</p>
        {error && (
          <p className="login-card__error" role="alert">
            {error}
          </p>
        )}
        <Field label={t("login.username")}>
          <Input
            autoComplete="username"
            autoFocus
            onChange={(event) => setUsername(event.target.value)}
            placeholder={t("login.usernamePlaceholder")}
            required
            value={username}
          />
        </Field>
        <Field label={t("login.password")}>
          <Input
            autoComplete="current-password"
            onChange={(event) => setPassword(event.target.value)}
            placeholder={t("login.passwordPlaceholder")}
            required
            type="password"
            value={password}
          />
        </Field>
        <Button
          className="login-card__submit"
          disabled={submitting}
          type="submit"
          variant="primary"
        >
          {submitting ? t("login.submitting") : t("login.submit")}
        </Button>
        <p className="login-card__hint">{t("login.hint")}</p>
      </form>
    </div>
  );
}
