import { useEffect, type ReactNode } from "react";
import { useApi } from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import { AuthStatePage } from "@argus/ui";

/**
 * 会话恢复：zustand store 从 localStorage 同步水合，
 * 挂载后用 `auth.me()` 校验并刷新会话；失效则清空，
 * 后续导航会被路由守卫重定向到 /login。
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const api = useApi();
  const status = useEnterpriseAuthStore((state) => state.status);
  const error = useEnterpriseAuthStore((state) => state.error);
  const restore = useEnterpriseAuthStore((state) => state.restore);

  useEffect(() => {
    void restore(api);
  }, [api, restore]);

  if (status === "unknown" || status === "checking") {
    return <AuthStatePage status="checking" />;
  }
  if (status === "unavailable") {
    return <AuthStatePage message={error} status="unavailable" />;
  }
  return <>{children}</>;
}
