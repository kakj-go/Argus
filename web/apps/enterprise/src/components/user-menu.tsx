import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import { useAuthStore } from "@argus/auth";
import { Avatar, Dropdown } from "@argus/ui";

/** 顶栏/侧栏共用的用户菜单：个人设置占位 + 退出登录。 */
export function UserMenu() {
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
        { label: t("shell.profile") },
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
