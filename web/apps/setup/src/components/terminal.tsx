import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircle2, ShieldCheck } from "lucide-react";
import { Button, Card, CardContent, StatusBadge } from "@argus/ui";

/**
 * 平台控制台登录入口。真实部署中指向平台控制台应用；
 * 当前仓库内无独立登录路由，先落到 "/" 占位。
 */
const LOGIN_URL = "/";

function TerminalLayout({
  badge,
  badgeTone,
  icon,
  title,
  description,
  actionLabel,
}: {
  badge: string;
  badgeTone: "accent" | "success";
  icon: ReactNode;
  title: string;
  description: string;
  actionLabel: string;
}) {
  return (
    <Card className="setup-card">
      <CardContent>
        <div className="setup-terminal">
          <span className={`setup-terminal__icon is-${badgeTone}`}>{icon}</span>
          <StatusBadge tone={badgeTone === "success" ? "success" : "accent"}>
            {badge}
          </StatusBadge>
          <h2>{title}</h2>
          <p>{description}</p>
          <Button
            onClick={() => {
              window.location.href = LOGIN_URL;
            }}
            variant="primary"
          >
            {actionLabel}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

/** 系统已初始化：终态页，不再渲染任何表单。 */
export function InitializedTerminal() {
  const { t } = useTranslation();
  return (
    <TerminalLayout
      actionLabel={t("setup.initialized.goLogin")}
      badge={t("setup.initialized.badge")}
      badgeTone="accent"
      description={t("setup.initialized.description")}
      icon={<ShieldCheck size={26} />}
      title={t("setup.initialized.title")}
    />
  );
}

/** 初始化提交成功：终态页。 */
export function SuccessTerminal() {
  const { t } = useTranslation();
  return (
    <TerminalLayout
      actionLabel={t("setup.success.goLogin")}
      badge={t("setup.success.badge")}
      badgeTone="success"
      description={t("setup.success.description")}
      icon={<CheckCircle2 size={26} />}
      title={t("setup.success.title")}
    />
  );
}
