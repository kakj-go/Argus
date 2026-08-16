import type { RoleBinding } from "../types";
import type { MockDb } from "./store";

function isEffective(binding: RoleBinding, nowIso: string): boolean {
  if (binding.status !== "active") return false;
  if (binding.valid_from && binding.valid_from > nowIso) return false;
  if (binding.valid_until && binding.valid_until <= nowIso) return false;
  return true;
}

/**
 * 解析用户在企业内的有效权限：user 主体绑定 ∪ 所在部门 department 主体
 * 绑定（仅 active 且在 validFrom/validUntil 有效期内），取对应 role 的
 * permissions 并集。`now` 缺省为当前时间，mock 域内传入 ctx.nowIso()。
 */
export function resolvePermissions(
  db: MockDb,
  enterpriseId: string,
  userId: string,
  now: string = new Date().toISOString(),
): Set<string> {
  const departmentId = db.enterpriseUsers.find(
    (m) => m.userId === userId && m.enterpriseId === enterpriseId,
  )?.departmentId;
  const permissions = new Set<string>();
  for (const binding of db.roleBindings) {
    if (binding.enterprise_id !== enterpriseId) continue;
    const isUserBinding =
      binding.subject_type === "user" && binding.subject_id === userId;
    const isDepartmentBinding =
      binding.subject_type === "department" &&
      departmentId !== undefined &&
      binding.subject_id === departmentId;
    if (!isUserBinding && !isDepartmentBinding) continue;
    if (!isEffective(binding, now)) continue;
    const role = db.roles.find((entry) => entry.id === binding.role_id);
    for (const permission of role?.permissions ?? []) {
      permissions.add(permission);
    }
  }
  return permissions;
}
