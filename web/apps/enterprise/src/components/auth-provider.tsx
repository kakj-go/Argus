import { useEffect, type ReactNode } from "react";
import { useApi } from "@argus/api-client";
import { useAuthStore } from "@argus/auth";

/**
 * 会话恢复：zustand store 从 localStorage 同步水合，
 * 挂载后用 `auth.me()` 校验并刷新会话；失效则清空，
 * 后续导航会被路由守卫重定向到 /login。
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const api = useApi();
  const hasSession = useAuthStore((state) => Boolean(state.session));
  const setSession = useAuthStore((state) => state.setSession);

  useEffect(() => {
    if (!hasSession) return;
    let cancelled = false;
    api
      .auth.me()
      .then((session) => {
        if (cancelled) return;
        setSession(
          session.membership && !session.user.platformRole ? session : null,
        );
      })
      .catch(() => {
        if (!cancelled) setSession(null);
      });
    return () => {
      cancelled = true;
    };
    // 仅在挂载时校验一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return <>{children}</>;
}
