import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@argus/auth";
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
  Switch,
} from "@argus/ui";
import { formatDateTime } from "../lib/format";

type LoginSessionRow = {
  id: string;
  device: string;
  ip: string;
  lastActiveAt?: string;
  current: boolean;
};

/** 新密码强度：≥12 位且同时包含字母与数字。 */
function passwordStrong(value: string): boolean {
  return (
    value.length >= 12 && /[a-zA-Z]/.test(value) && /\d/.test(value)
  );
}

/** 我的账号：账号信息、修改密码（前端强度校验）、MFA、登录会话。 */
export function AccountPage() {
  const { t, i18n } = useTranslation();
  const session = useAuthStore((state) => state.session);

  const [currentPassword, setCurrentPassword] = useState("");
  const [nextPassword, setNextPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [passwordDone, setPasswordDone] = useState(false);
  const [mfaEnabled, setMfaEnabled] = useState(
    session?.user.mfaEnabled ?? true,
  );
  const [mfaBlocked, setMfaBlocked] = useState(false);

  if (!session) return null;
  const { user } = session;

  const handlePasswordSubmit = (event: FormEvent) => {
    event.preventDefault();
    setPasswordError(null);
    setPasswordDone(false);
    if (!currentPassword || !nextPassword || !confirmPassword) {
      setPasswordError(t("account.password.required"));
      return;
    }
    if (!passwordStrong(nextPassword)) {
      setPasswordError(t("account.password.weak"));
      return;
    }
    if (nextPassword !== confirmPassword) {
      setPasswordError(t("account.password.mismatch"));
      return;
    }
    // mock 环境无改密 API，仅演示前端校验与反馈
    setCurrentPassword("");
    setNextPassword("");
    setConfirmPassword("");
    setPasswordDone(true);
  };

  const handleMfaToggle = (checked: boolean) => {
    if (!checked) {
      // 超管强制 MFA：拒绝关闭并提示
      setMfaBlocked(true);
      setMfaEnabled(true);
      return;
    }
    setMfaBlocked(false);
    setMfaEnabled(true);
  };

  const sessionRows: LoginSessionRow[] = [
    {
      id: "sess-current",
      device: "macOS · Chrome",
      ip: "10.0.0.2",
      lastActiveAt: user.lastLoginAt,
      current: true,
    },
  ];

  return (
    <PageShell description={t("account.description")} title={t("account.title")}>
      <div className="platform-stack">
        <Card>
          <CardHeader title={t("account.profile.title")} />
          <CardContent>
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: t("account.profile.username"),
                  value: <code className="mono">{user.username}</code>,
                },
                {
                  label: t("account.profile.displayName"),
                  value: user.displayName,
                },
                {
                  label: t("account.profile.email"),
                  value: user.email ?? t("common.none"),
                },
                {
                  label: t("account.profile.role"),
                  value: <Badge tone="accent">{user.platformRole}</Badge>,
                },
                {
                  label: t("account.profile.lastLogin"),
                  value: formatDateTime(user.lastLoginAt, i18n.language),
                },
              ]}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.password.title")} />
          <CardContent>
            <form
              className="account-password-form"
              onSubmit={handlePasswordSubmit}
            >
              {passwordError && (
                <Alert
                  description={passwordError}
                  title={t("account.password.title")}
                  tone="danger"
                />
              )}
              {passwordDone && (
                <Alert
                  description={t("account.password.success")}
                  title={t("account.password.title")}
                  tone="success"
                />
              )}
              <Field label={t("account.password.current")}>
                <Input
                  autoComplete="current-password"
                  onChange={(event) => setCurrentPassword(event.target.value)}
                  type="password"
                  value={currentPassword}
                />
              </Field>
              <Field
                hint={t("account.password.rule")}
                label={t("account.password.next")}
              >
                <Input
                  autoComplete="new-password"
                  onChange={(event) => setNextPassword(event.target.value)}
                  type="password"
                  value={nextPassword}
                />
              </Field>
              <Field label={t("account.password.confirm")}>
                <Input
                  autoComplete="new-password"
                  onChange={(event) => setConfirmPassword(event.target.value)}
                  type="password"
                  value={confirmPassword}
                />
              </Field>
              <Button type="submit" variant="primary">
                {t("account.password.submit")}
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader title={t("account.mfa.title")} />
          <CardContent>
            <div className="platform-stack">
              <Alert
                description={t("account.mfa.required.description")}
                title={t("account.mfa.required.title")}
                tone="info"
              />
              {mfaBlocked && (
                <Alert
                  description={t("account.mfa.required.description")}
                  title={t("account.mfa.required.title")}
                  tone="warning"
                />
              )}
              <label className="switch-row">
                <Switch
                  checked={mfaEnabled}
                  label={t("account.mfa.toggle")}
                  onChange={handleMfaToggle}
                />
                <span>{t("account.mfa.toggle")}</span>
                <StatusText enabled={mfaEnabled} />
              </label>
            </div>
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
                  render: (row) => <code className="mono">{row.ip}</code>,
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
                      <Badge tone="success">{t("account.sessions.current")}</Badge>
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

function StatusText({ enabled }: { enabled: boolean }) {
  const { t } = useTranslation();
  return (
    <Badge tone={enabled ? "success" : "neutral"}>
      {t(enabled ? "common.enabled" : "common.disabled")}
    </Badge>
  );
}
