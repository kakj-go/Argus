import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { useEnterpriseAuthStore } from "@argus/auth";
import { AdminShell } from "./components/admin-shell";
import { ChatShell } from "./components/chat-shell";

const LoginPage = lazyRouteComponent(
  () => import("./pages/login-page"),
  "LoginPage",
);
const AccountPage = lazyRouteComponent(
  () => import("./pages/account-page"),
  "AccountPage",
);
const ConversationPage = lazyRouteComponent(
  () => import("./pages/conversation-page"),
  "ConversationPage",
);
const HostsPage = lazyRouteComponent(
  () => import("./pages/hosts-page"),
  "HostsPage",
);
const HostDetailPage = lazyRouteComponent(
  () => import("./pages/host-detail-page"),
  "HostDetailPage",
);
const KubernetesPage = lazyRouteComponent(
  () => import("./pages/kubernetes-page"),
  "KubernetesPage",
);
const KubernetesClusterPage = lazyRouteComponent(
  () => import("./pages/kubernetes-cluster-page"),
  "KubernetesClusterPage",
);
const TasksPage = lazyRouteComponent(
  () => import("./pages/tasks-page"),
  "TasksPage",
);
const ApprovalsPage = lazyRouteComponent(
  () => import("./pages/approvals-page"),
  "ApprovalsPage",
);
const SettingsOrgPage = lazyRouteComponent(
  () => import("./pages/settings-org-page"),
  "SettingsOrgPage",
);
const SettingsAiPage = lazyRouteComponent(
  () => import("./pages/settings-ai-page"),
  "SettingsAiPage",
);
const SettingsInteractiveCardsPage = lazyRouteComponent(
  () => import("./pages/settings-interactive-cards-page"),
  "SettingsInteractiveCardsPage",
);
const SettingsSecretsPage = lazyRouteComponent(
  () => import("./pages/settings-secrets-page"),
  "SettingsSecretsPage",
);
const SettingsAuditPage = lazyRouteComponent(
  () => import("./pages/settings-audit-page"),
  "SettingsAuditPage",
);
const DemoPage = lazyRouteComponent(
  () => import("./pages/demo-page"),
  "DemoPage",
);

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
    const session = useEnterpriseAuthStore.getState().session;
    if (!session || session.session.audience !== "enterprise") {
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
  validateSearch: (search: Record<string, unknown>) => ({
    approval: search.approval === "remote" ? "remote" : "operation",
    scope: ["mine", "created", "done"].includes(String(search.scope))
      ? (String(search.scope) as "mine" | "created" | "done")
      : "mine",
  }),
  component: ApprovalsPage,
});
const accountRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/account",
  component: AccountPage,
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
      accountRoute,
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
