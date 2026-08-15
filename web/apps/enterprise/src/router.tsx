import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { useAuthStore } from "@argus/auth";
import { AdminShell } from "./components/admin-shell";
import { ChatShell } from "./components/chat-shell";
import { ApprovalsPage } from "./pages/approvals-page";
import { ConversationPage } from "./pages/conversation-page";
import { DemoPage } from "./pages/demo-page";
import { HostDetailPage } from "./pages/host-detail-page";
import { HostsPage } from "./pages/hosts-page";
import { KubernetesClusterPage } from "./pages/kubernetes-cluster-page";
import { KubernetesPage } from "./pages/kubernetes-page";
import { LoginPage } from "./pages/login-page";
import { SettingsAiPage } from "./pages/settings-ai-page";
import { SettingsAuditPage } from "./pages/settings-audit-page";
import { SettingsInteractiveCardsPage } from "./pages/settings-interactive-cards-page";
import { SettingsOrgPage } from "./pages/settings-org-page";
import { SettingsSecretsPage } from "./pages/settings-secrets-page";
import { TasksPage } from "./pages/tasks-page";

const rootRoute = createRootRoute({ component: () => <Outlet /> });

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (search: Record<string, unknown>) => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  component: LoginPage,
});

/**
 * 受保护区域：未登录访问任何子路由都会在 beforeLoad 阶段
 * 重定向到 /login，并带上 redirect 回跳参数。
 */
const authedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "authed",
  beforeLoad: ({ location }) => {
    const session = useAuthStore.getState().session;
    if (!session?.membership || session.user.platformRole) {
      throw redirect({
        to: "/login",
        search: { redirect: location.href },
      });
    }
  },
  component: () => <Outlet />,
});

// Chatbox 布局：企业门户的默认模式
const chatRoute = createRoute({
  getParentRoute: () => authedRoute,
  id: "chat",
  component: ChatShell,
});
const indexRoute = createRoute({
  getParentRoute: () => chatRoute,
  path: "/",
  validateSearch: (search: Record<string, unknown>) => ({
    c: typeof search.c === "string" && search.c ? search.c : undefined,
  }),
  component: ConversationPage,
});

// 管理后台布局
const adminRoute = createRoute({
  getParentRoute: () => authedRoute,
  id: "admin",
  component: AdminShell,
});
const hostsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/hosts",
  component: HostsPage,
});
const hostDetailRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/hosts/$hostId",
  component: HostDetailPage,
});
const kubernetesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/kubernetes",
  component: KubernetesPage,
});
const kubernetesClusterRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/kubernetes/$clusterId",
  component: KubernetesClusterPage,
});
const tasksRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/tasks",
  component: TasksPage,
});
const approvalsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/approvals",
  component: ApprovalsPage,
});
const settingsOrgRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/settings/org",
  component: SettingsOrgPage,
});
const settingsAiRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/settings/ai",
  component: SettingsAiPage,
});
const settingsInteractiveCardsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/settings/interactive-cards",
  component: SettingsInteractiveCardsPage,
});
const settingsSecretsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/settings/secrets",
  component: SettingsSecretsPage,
});
const settingsAuditRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/settings/audit",
  component: SettingsAuditPage,
});

// 组件展厅仅开发环境注册，不进入任何菜单
const demoRoute = import.meta.env.DEV
  ? createRoute({
      getParentRoute: () => adminRoute,
      path: "/demo",
      component: DemoPage,
    })
  : undefined;

const routeTree = rootRoute.addChildren([
  loginRoute,
  authedRoute.addChildren([
    chatRoute.addChildren([indexRoute]),
    adminRoute.addChildren([
      hostsRoute,
      hostDetailRoute,
      kubernetesRoute,
      kubernetesClusterRoute,
      tasksRoute,
      approvalsRoute,
      settingsOrgRoute,
      settingsAiRoute,
      settingsInteractiveCardsRoute,
      settingsSecretsRoute,
      settingsAuditRoute,
      ...(demoRoute ? [demoRoute] : []),
    ]),
  ]),
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
