import type { TFunction } from "i18next";
import type { Role } from "@argus/api-client";

const builtinRoleNameKeys: Record<string, string> = {
  enterprise_admin: "settings.org.builtinRoles.enterprise_admin",
  iam_admin: "settings.org.builtinRoles.iam_admin",
  security_auditor: "settings.org.builtinRoles.security_auditor",
  resource_admin: "settings.org.builtinRoles.resource_admin",
  resource_operator: "settings.org.builtinRoles.resource_operator",
  resource_viewer: "settings.org.builtinRoles.resource_viewer",
  resource_approver: "settings.org.builtinRoles.resource_approver",
  department_admin: "settings.org.builtinRoles.department_admin",
};

export function roleDisplayName(
  role: Pick<Role, "builtin" | "builtin_key" | "name">,
  t: TFunction,
): string {
  if (!role.builtin || !role.builtin_key) return role.name;
  const translationKey = builtinRoleNameKeys[role.builtin_key];
  return translationKey
    ? t(translationKey, { defaultValue: role.name })
    : role.name;
}
