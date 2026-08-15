import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useApi, type Role, type RoleBinding } from "@argus/api-client";
import { useAuthStore } from "@argus/auth";

/** 与 mock resolvePermissions 一致的有效期判断（active 且在 validFrom/validUntil 内）。 */
function isEffective(binding: RoleBinding, nowIso: string): boolean {
  if (binding.status !== "active") return false;
  if (binding.validFrom && binding.validFrom > nowIso) return false;
  if (binding.validUntil && binding.validUntil <= nowIso) return false;
  return true;
}

/**
 * 当前用户的有效角色绑定：user 主体绑定 ∪ 所在部门 department 主体绑定，
 * 过滤 status==="active" 与有效期。与 mock 层 resolvePermissions 同一解析规则。
 */
export function useMyEffectiveBindings(): RoleBinding[] {
  const api = useApi();
  const userId = useAuthStore((state) => state.session?.user.id);
  const departmentId = useAuthStore(
    (state) => state.session?.membership?.departmentId,
  );
  const { data } = useQuery({
    queryKey: ["org", "role-bindings"],
    queryFn: () => api.org.listRoleBindings(),
    enabled: Boolean(userId),
  });
  return useMemo(() => {
    const now = new Date().toISOString();
    return (data ?? []).filter(
      (binding) =>
        ((binding.subjectType === "user" && binding.subjectId === userId) ||
          (binding.subjectType === "department" &&
            departmentId !== undefined &&
            binding.subjectId === departmentId)) &&
        isEffective(binding, now),
    );
  }, [data, userId, departmentId]);
}

/** 当前用户经由有效绑定获得的角色列表。 */
export function useMyRoles(): Role[] {
  const api = useApi();
  const bindings = useMyEffectiveBindings();
  const { data: roles } = useQuery({
    queryKey: ["org", "roles"],
    queryFn: () => api.org.listRoles(),
  });
  return useMemo(() => {
    const roleIds = new Set(bindings.map((binding) => binding.roleId));
    return (roles ?? []).filter((role) => roleIds.has(role.id));
  }, [roles, bindings]);
}

/** 当前用户的权限点并集（取自有效绑定对应角色的 permissions）。 */
export function useMyPermissions(): Set<string> {
  const roles = useMyRoles();
  return useMemo(
    () => new Set(roles.flatMap((role) => role.permissions)),
    [roles],
  );
}

/** 权限点判断；拥有 `*` 视为全量。 */
export function usePermission(permission: string): boolean {
  const permissions = useMyPermissions();
  return permissions.has("*") || permissions.has(permission);
}
