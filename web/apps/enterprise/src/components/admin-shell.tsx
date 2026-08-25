import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bell,
  Bot,
  ChevronsLeft,
  ChevronsRight,
  Component,
  Container,
  History,
  House,
  KeyRound,
  Menu,
  ScrollText,
  Search,
  Server,
  ShieldCheck,
  Users,
  X,
  type LucideIcon,
} from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { AppShell, Badge, Button, Dialog, Input, Tooltip } from "@argus/ui";
import { useUiStore } from "../store/ui";
import { AccountActions } from "./account-actions";

type AdminNavItem = {
  key: string;
  to: string;
  icon: LucideIcon;
  count?: number;
  alert?: boolean;
};

type AdminNavSection = {
  groupKey: string;
  items: AdminNavItem[];
};
const realMode = import.meta.env.VITE_API_MODE === "real";

function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <Link className="argus-brand" to="/">
      <span className="argus-brand__mark">◉</span>
      {!compact && (
        <span className="argus-brand__name">
          Argus<small>enterprise</small>
        </span>
      )}
    </Link>
  );
}

/** 菜单徽标计数：直接由 API 数据派生。 */
function useAdminCounts() {
  const api = useApi();
  const hosts = useQuery({
    queryKey: ["hosts", "count"],
    queryFn: () => api.hosts.list(),
  });
  const clusters = useQuery({
    queryKey: ["k8s-clusters", "count"],
    queryFn: () => api.kubernetes.listClusters(),
  });
  const approvals = useQuery({
    queryKey: ["approvals", "awaiting_approval"],
    queryFn: () => api.approvals.list({ scope: "mine" }),
    enabled: true,
  });
  return {
    hosts: hosts.data?.items.length,
    clusters: clusters.data?.items.length,
    pendingApprovals: approvals.data?.items.length,
  };
}

function buildSections(
  counts: ReturnType<typeof useAdminCounts>,
): AdminNavSection[] {
  const sections: AdminNavSection[] = [
    {
      groupKey: "shell.groups.resources",
      items: [
        {
          key: "shell.nav.hosts",
          to: "/hosts",
          icon: Server,
          count: counts.hosts,
        },
        {
          key: "shell.nav.kubernetes",
          to: "/kubernetes",
          icon: Container,
          count: counts.clusters,
        },
      ],
    },
    {
      groupKey: "shell.groups.execution",
      items: [
        { key: "shell.nav.tasks", to: "/tasks", icon: History },
        {
          key: "shell.nav.approvals",
          to: "/approvals",
          icon: ShieldCheck,
          count: counts.pendingApprovals,
          alert: (counts.pendingApprovals ?? 0) > 0,
        },
      ],
    },
    {
      groupKey: "shell.groups.settings",
      items: [
        { key: "shell.nav.settingsOrg", to: "/settings/org", icon: Users },
        { key: "shell.nav.settingsAi", to: "/settings/ai", icon: Bot },
        ...(!realMode
          ? [
              {
                key: "shell.nav.settingsInteractiveCards",
                to: "/settings/interactive-cards",
                icon: Component,
              },
            ]
          : []),
        {
          key: "shell.nav.settingsSecrets",
          to: "/settings/secrets",
          icon: KeyRound,
        },
        {
          key: "shell.nav.settingsAudit",
          to: "/settings/audit",
          icon: ScrollText,
        },
      ],
    },
  ];
  return sections;
}

function Sidebar() {
  const { t } = useTranslation();
  const { sidebarCollapsed, toggleSidebar, mobileNavOpen, setMobileNavOpen } =
    useUiStore();
  const sections = buildSections(useAdminCounts());
  return (
    <>
      <div
        aria-hidden
        className={`argus-mobile-scrim ${mobileNavOpen ? "is-visible" : ""}`}
        onClick={() => setMobileNavOpen(false)}
      />
      <aside
        className={`argus-sidebar ${sidebarCollapsed ? "is-collapsed" : ""} ${mobileNavOpen ? "is-mobile-open" : ""}`}
      >
        <div className="argus-sidebar__head">
          <Brand compact={sidebarCollapsed} />
          <Button
            aria-label={t("shell.closeNavigation")}
            className="argus-mobile-close"
            onClick={() => setMobileNavOpen(false)}
            size="icon"
            variant="ghost"
          >
            <X size={17} />
          </Button>
        </div>
        <nav
          aria-label={t("shell.mainNavigation")}
          className="argus-sidebar__nav"
        >
          {sections.map((section) => (
            <div className="argus-nav-section" key={section.groupKey}>
              {!sidebarCollapsed && (
                <div className="argus-nav-section__label">
                  {t(section.groupKey)}
                </div>
              )}
              {section.items.map((item) => (
                <Tooltip content={t(item.key)} key={item.key}>
                  <Link
                    activeProps={{ className: "active" }}
                    className="argus-nav-item"
                    onClick={() => setMobileNavOpen(false)}
                    to={item.to}
                  >
                    <item.icon aria-hidden size={17} />
                    {!sidebarCollapsed && (
                      <>
                        <span>{t(item.key)}</span>
                        {item.alert && <i className="argus-nav-alert" />}
                        {item.count !== undefined && (
                          <small>{item.count}</small>
                        )}
                      </>
                    )}
                  </Link>
                </Tooltip>
              ))}
            </div>
          ))}
        </nav>
        <div className="argus-sidebar__footer">
          <button
            className="argus-collapse-button"
            onClick={toggleSidebar}
            type="button"
          >
            {sidebarCollapsed ? (
              <ChevronsRight size={16} />
            ) : (
              <>
                <ChevronsLeft size={16} />
                <span>{t("shell.collapse")}</span>
                <kbd>⌘ B</kbd>
              </>
            )}
          </button>
        </div>
      </aside>
    </>
  );
}

function CommandDialog() {
  const { t } = useTranslation();
  const { commandOpen, setCommandOpen } = useUiStore();
  return (
    <Dialog
      description={t("shell.command.description")}
      onOpenChange={setCommandOpen}
      open={commandOpen}
      title={t("shell.command.title")}
    >
      <div className="argus-command-search">
        <Search size={16} />
        <Input autoFocus placeholder={t("shell.command.placeholder")} />
      </div>
      <div className="argus-command-list">
        <span>{t("shell.command.suggestion")}</span>
        <Link onClick={() => setCommandOpen(false)} to="/">
          <span className="argus-bot-line">◉</span>
          {t("shell.command.newChat")}
          <kbd>↵</kbd>
        </Link>
        <Link onClick={() => setCommandOpen(false)} to="/hosts">
          <Search size={15} />
          {t("shell.command.findHost")}
        </Link>
        <Link
          onClick={() => setCommandOpen(false)}
          search={{ approval: "operation", scope: "mine" }}
          to="/approvals"
        >
          <ShieldCheck size={15} />
          {t("shell.command.approvals")}
        </Link>
      </div>
    </Dialog>
  );
}

const pageTitles: Record<string, string> = {
  "/hosts": "shell.nav.hosts",
  "/kubernetes": "shell.nav.kubernetes",
  "/tasks": "shell.nav.tasks",
  "/approvals": "shell.nav.approvals",
  "/settings/org": "shell.nav.settingsOrg",
  "/settings/ai": "shell.nav.settingsAi",
  "/settings/interactive-cards": "shell.nav.settingsInteractiveCards",
  "/settings/secrets": "shell.nav.settingsSecrets",
  "/settings/audit": "shell.nav.settingsAudit",
  "/demo": "shell.nav.demo",
};

function Header() {
  const { t } = useTranslation();
  const { setCommandOpen, setMobileNavOpen } = useUiStore();
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const titleKey =
    pageTitles[pathname] ??
    (pathname.startsWith("/hosts/")
      ? "shell.nav.hostDetail"
      : pathname.startsWith("/kubernetes/")
        ? "shell.nav.clusterDetail"
        : undefined);
  return (
    <header className="argus-topbar">
      <Button
        aria-label={t("shell.openNavigation")}
        className="argus-mobile-menu"
        onClick={() => setMobileNavOpen(true)}
        size="icon"
        variant="ghost"
      >
        <Menu size={18} />
      </Button>
      <Link className="argus-back-to-chat" to="/">
        <ArrowLeft size={14} />
        <span>{t("shell.backToChat")}</span>
      </Link>
      <div className="argus-topbar__title">
        <span>{titleKey ? t(titleKey) : "Argus"}</span>
        <Badge dot tone="success">
          {t("shell.healthy")}
        </Badge>
      </div>
      <button
        className="argus-search-trigger"
        onClick={() => setCommandOpen(true)}
        type="button"
      >
        <Search size={15} />
        <span>{t("shell.search")}</span>
        <kbd>⌘ K</kbd>
      </button>
      <div className="argus-topbar__actions">
        <AccountActions />
        <Tooltip content={t("shell.notifications")}>
          <Button
            aria-label={t("shell.notifications")}
            size="icon"
            variant="ghost"
          >
            <Bell size={17} />
          </Button>
        </Tooltip>
      </div>
    </header>
  );
}

function MobileNavigation() {
  const { t } = useTranslation();
  const items = [
    { key: "shell.nav.conversation", to: "/", icon: House, exact: true },
    { key: "shell.nav.hosts", to: "/hosts", icon: Server, exact: false },
    ...(!realMode
      ? [
          {
            key: "shell.nav.approvals",
            to: "/approvals",
            icon: ShieldCheck,
            exact: false,
          },
        ]
      : []),
    {
      key: "shell.nav.settings",
      to: "/settings/org",
      icon: Users,
      exact: false,
    },
  ];
  return (
    <nav className="argus-mobile-primary">
      {items.map((item) => (
        <Link
          activeOptions={{ exact: item.exact }}
          activeProps={{ className: "is-active" }}
          key={item.key}
          to={item.to}
        >
          <item.icon size={18} />
          <span>{t(item.key)}</span>
        </Link>
      ))}
    </nav>
  );
}

/**
 * 管理后台布局（docs/07）：资源 / 执行治理 / 企业设置三组菜单，
 * 顶栏提供返回会话、企业切换、主题与语言、用户菜单和 ⌘K 命令面板。
 */
export function AdminShell() {
  const collapsed = useUiStore((state) => state.sidebarCollapsed);
  const setCommandOpen = useUiStore((state) => state.setCommandOpen);
  const toggleSidebar = useUiStore((state) => state.toggleSidebar);
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });

  useEffect(() => {
    const frame = window.requestAnimationFrame(() => {
      window.scrollTo({ top: 0, left: 0 });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [pathname]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey)) return;
      if (event.key === "k") {
        event.preventDefault();
        setCommandOpen(true);
      } else if (event.key === "b") {
        event.preventDefault();
        toggleSidebar();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [setCommandOpen, toggleSidebar]);

  return (
    <AppShell
      className={`argus-admin-shell ${collapsed ? "is-collapsed" : ""}`}
      header={<Header />}
      overlay={
        <>
          <MobileNavigation />
          <CommandDialog />
        </>
      }
      sidebar={<Sidebar />}
    >
      <Outlet />
    </AppShell>
  );
}
