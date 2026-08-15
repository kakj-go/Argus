import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { useAuthStore } from "@argus/auth";
import { PlatformShell } from "./components/platform-shell";
import { AccountPage } from "./pages/account-page";
import { AdminsPage } from "./pages/admins-page";
import { AuditPage } from "./pages/audit-page";
import { DashboardPage } from "./pages/dashboard-page";
import { EnterprisesPage } from "./pages/enterprises-page";
import { LoginPage } from "./pages/login-page";
import { SandboxPage } from "./pages/sandbox-page";

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
 * 受保护区域：未登录或登录者不是平台超管时，beforeLoad 阶段
 * 重定向到 /login，并带上 redirect 回跳参数。
 */
const authedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "authed",
  beforeLoad: ({ location }) => {
    const session = useAuthStore.getState().session;
    if (!session || session.user.platformRole !== "platform_super_admin") {
      throw redirect({
        to: "/login",
        search: { redirect: location.href },
      });
    }
  },
  component: PlatformShell,
});

const indexRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/",
  component: DashboardPage,
});
const enterprisesRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/enterprises",
  component: EnterprisesPage,
});
const adminsRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/admins",
  component: AdminsPage,
});
const sandboxRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/sandbox",
  component: SandboxPage,
});
const auditRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/audit",
  component: AuditPage,
});
const accountRoute = createRoute({
  getParentRoute: () => authedRoute,
  path: "/account",
  component: AccountPage,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  authedRoute.addChildren([
    indexRoute,
    enterprisesRoute,
    adminsRoute,
    sandboxRoute,
    auditRoute,
    accountRoute,
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
