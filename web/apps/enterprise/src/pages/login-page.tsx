import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
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
  const search = useSearch({ strict: false }) as { redirect?: string };
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const platformPortalUrl =
    import.meta.env.VITE_PLATFORM_URL ??
    `${window.location.protocol}//${window.location.hostname}:4174/login`;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      const session = await login(api, { username: username.trim(), password });
      if (session.session.audience !== "enterprise") {
        await logout(api);
        setError(t("login.wrongPortal"));
        return;
      }
      queryClient.clear();
      const target = search.redirect?.startsWith("/") ? search.redirect : "/";
      void navigate({ to: target as "/" });
    } catch (reason) {
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
        onSubmit={(event) => void handleSubmit(event)}
      >
        <div className="argus-brand argus-login-card__brand">
          <span className="argus-brand__mark">◉</span>
          <span className="argus-brand__name">
            Argus<small>enterprise</small>
          </span>
        </div>
        <h1>{t("login.title")}</h1>
        <p className="argus-login-card__subtitle">{t("login.subtitle")}</p>
        {error && (
          <p className="argus-login-card__error" role="alert">
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
          className="argus-login-card__submit"
          disabled={submitting}
          type="submit"
          variant="primary"
        >
          {submitting ? t("login.submitting") : t("login.submit")}
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
