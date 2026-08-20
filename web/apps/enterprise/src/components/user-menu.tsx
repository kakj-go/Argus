import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import { PortalUserMenu } from "@argus/ui";

/** 顶栏/侧栏共用的用户菜单：个人设置占位 + 退出登录。 */
export function UserMenu() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useEnterpriseAuthStore((state) => state.session);
  const logout = useEnterpriseAuthStore((state) => state.logout);
  if (!session) return null;
  const { user } = session;

  const handleLogout = async () => {
    await logout(api);
    queryClient.clear();
    void navigate({ to: "/login", search: { redirect: undefined } });
  };

  return (
    <PortalUserMenu
      displayName={user.display_name}
      items={[
        { label: t("shell.profile"), onSelect: () => void navigate({ to: "/account" }) },
        "separator",
        {
          label: t("shell.signOut"),
          danger: true,
          onSelect: () => void handleLogout(),
        },
      ]}
      username={user.username}
    />
  );
}
