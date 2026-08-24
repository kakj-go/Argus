import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Outlet,
  redirect,
} from "@tanstack/react-router";
import { usePlatformAuthStore } from "@argus/auth";
import { PlatformShell } from "./components/platform-shell";

const LoginPage = lazyRouteComponent(
  () => import("./pages/login-page"),
  "LoginPage",
);
const DashboardPage = lazyRouteComponent(
  () => import("./pages/dashboard-page"),
  "DashboardPage",
);
const EnterprisesPage = lazyRouteComponent(
  () => import("./pages/enterprises-page"),
  "EnterprisesPage",
);
const AdminsPage = lazyRouteComponent(
  () => import("./pages/admins-page"),
  "AdminsPage",
);
const SandboxPage = lazyRouteComponent(
  () => import("./pages/sandbox-page"),
  "SandboxPage",
);
const AuditPage = lazyRouteComponent(
  () => import("./pages/audit-page"),
  "AuditPage",
);
const AccountPage = lazyRouteComponent(
  () => import("./pages/account-page"),
  "AccountPage",
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

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",
  beforeLoad: () => {
    throw redirect({ to: "/login", search: { redirect: undefined } });
  },
});

/**
 * 受保护区域：未登录或登录者不是平台超管时，beforeLoad 阶段
 * 重定向到 /login，并带上 redirect 回跳参数。
 */
const authedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "authed",
  beforeLoad: ({ location }) => {
    const session = usePlatformAuthStore.getState().session;
    if (!session || session.session.audience !== "platform") {
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
  setupRoute,
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
