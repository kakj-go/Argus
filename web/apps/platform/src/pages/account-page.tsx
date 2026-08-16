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
  const [submitError, setSubmitError] = useState<string | null>(null);
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
    </PageShell>
  );
}
