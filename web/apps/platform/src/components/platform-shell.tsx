import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import {
  Boxes,
  Building2,
  ChevronDown,
  CircleUserRound,
  Gauge,
  ScrollText,
  ShieldCheck,
  Users,
  type LucideIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { useAuthStore } from "@argus/auth";
import {
  AppearanceControls,
  Avatar,
  Badge,
  Dropdown,
} from "@argus/ui";

type NavItem = {
  key: string;
  to: string;
  icon: LucideIcon;
  exact?: boolean;
};

const NAV_ITEMS: NavItem[] = [
  { key: "shell.nav.dashboard", to: "/", icon: Gauge, exact: true },
  { key: "shell.nav.enterprises", to: "/enterprises", icon: Building2 },
  { key: "shell.nav.admins", to: "/admins", icon: Users },
  { key: "shell.nav.sandbox", to: "/sandbox", icon: Boxes },
  { key: "shell.nav.audit", to: "/audit", icon: ScrollText },
  { key: "shell.nav.account", to: "/account", icon: CircleUserRound },
];

function Brand() {
  const { t } = useTranslation();
  return (
    <Link className="brand" to="/">
      <span className="brand__mark brand__mark--platform">
        <ShieldCheck size={15} />
      </span>
      <span className="brand__name">
        Argus<small>{t("shell.brand.domain")}</small>
      </span>
    </Link>
  );
}

/** 顶栏用户菜单：我的账号 + 退出登录。 */
function UserMenu() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useAuthStore((state) => state.session);
  const logout = useAuthStore((state) => state.logout);
  if (!session) return null;
  const { user } = session;

  const handleLogout = async () => {
    await logout(api);
    queryClient.clear();
    void navigate({ to: "/login", search: { redirect: undefined } });
  };

  return (
    <Dropdown
      items={[
        {
          label: t("shell.myAccount"),
          onSelect: () => void navigate({ to: "/account" }),
        },
        "separator",
        {
          label: t("shell.signOut"),
          danger: true,
          onSelect: () => void handleLogout(),
        },
      ]}
      trigger={
        <button className="user-menu" type="button">
          <Avatar fallback={user.displayName.slice(0, 1)} />
          <span>
            <b>{user.displayName}</b>
            <small>{user.username}</small>
          </span>
          <ChevronDown size={13} />
        </button>
      }
    />
  );
}

/**
 * 平台超管门户外壳（docs/07 §5）：左侧菜单严格受限——企业、企业管理员、
 * OpenSandbox 基座、平台审计、我的账号；顶栏常驻“平台管理域”标识与企业
 * 门户视觉区隔，不出现任何企业业务数据入口。
 */
export function PlatformShell() {
  const { t } = useTranslation();
  return (
    <div className="app-shell platform-shell">
      <aside className="sidebar">
        <div className="sidebar__head">
          <Brand />
        </div>
        <nav aria-label={t("shell.group.platform")} className="sidebar__nav">
          <div className="nav-section">
            <div className="nav-section__label">{t("shell.group.platform")}</div>
            {NAV_ITEMS.map((item) => (
              <Link
                activeOptions={{ exact: item.exact ?? false }}
                activeProps={{ className: "active" }}
                className="nav-item"
                key={item.key}
                to={item.to}
              >
                <item.icon aria-hidden size={17} />
                <span>{t(item.key)}</span>
              </Link>
            ))}
          </div>
        </nav>
        <div className="sidebar__footer">
          <p className="platform-isolation">{t("shell.isolation")}</p>
        </div>
      </aside>
      <div className="app-main">
        <header className="topbar">
          <Badge tone="info">{t("shell.domainBadge")}</Badge>
          <Badge dot tone="accent">
            {t("shell.roleBadge")}
          </Badge>
          <div className="topbar__actions">
            <AppearanceControls />
            <UserMenu />
          </div>
        </header>
        <main className="page-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
