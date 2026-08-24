import { useTranslation } from "react-i18next";
import { CheckItem } from "@argus/ui";

/** 设置页共用的本地时间格式。 */
export function formatDateTime(iso?: string): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString();
}

/** docs/02 §4 的企业域权限点目录，按资源分组用于角色编辑矩阵。 */
export const PERMISSION_CATALOG: Array<{
  resource: string;
  actions: string[];
}> = [
  { resource: "connector", actions: ["read", "create", "rotate_credential"] },
  { resource: "bastion_scope", actions: ["read", "create", "manage"] },
  {
    resource: "host",
    actions: ["read", "create", "update", "connection.test", "direct_connect"],
  },
  { resource: "automation.command", actions: ["execute"] },
  { resource: "automation.template", actions: ["execute"] },
  { resource: "remote_access", actions: ["request"] },
  {
    resource: "remote_access.session",
    actions: ["create", "approve", "terminate"],
  },
  { resource: "remote_access.recording", actions: ["read"] },
  { resource: "kubernetes.cluster", actions: ["read", "create"] },
  { resource: "kubernetes.pod", actions: ["read"] },
  { resource: "kubernetes.workload", actions: ["restart"] },
  {
    resource: "telemetry",
    actions: ["read", "live_tail", "export", "sensitive_fields.read"],
  },
  { resource: "telemetry.query", actions: ["metrics", "logs", "traces"] },
  { resource: "telemetry.alert", actions: ["manage"] },
  { resource: "telemetry.dashboard", actions: ["manage"] },
  {
    resource: "telemetry.collector",
    actions: ["read", "install", "configure", "upgrade", "uninstall"],
  },
  { resource: "telemetry.gateway", actions: ["manage"] },
  { resource: "credential", actions: ["manage", "use", "reveal"] },
  { resource: "audit", actions: ["read"] },
  {
    resource: "interactive_card",
    actions: ["read", "create", "update", "delete", "publish"],
  },
  { resource: "ai_model", actions: ["read", "manage"] },
  { resource: "model_quota", actions: ["read", "manage"] },
  { resource: "model_usage", actions: ["read"] },
];

/** 角色编辑用的权限点勾选矩阵（资源 × 动作，CheckItem 网格）。 */
export function PermissionMatrix({
  value,
  onChange,
  disabled,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  const selected = new Set(value);
  const fullAccess = selected.has("*");
  const labelKey = (value: string) => value.replaceAll(".", "_");

  const toggle = (permission: string) => {
    if (disabled) return;
    const next = new Set(selected);
    if (next.has(permission)) next.delete(permission);
    else next.add(permission);
    onChange([...next]);
  };

  return (
    <div className="argus-settings-form">
      <div
        onClick={() => {
          if (disabled) return;
          onChange(fullAccess ? [] : ["*"]);
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            if (!disabled) onChange(fullAccess ? [] : ["*"]);
          }
        }}
        role="checkbox"
        aria-checked={fullAccess}
        tabIndex={disabled ? -1 : 0}
      >
        <CheckItem checked={fullAccess}>
          <span>
            {t("settings.org.rolesTab.fullAccess")}
            <small className="argus-settings-section__hint">
              {" "}
              {t("settings.org.rolesTab.fullAccessHint")}
            </small>
          </span>
        </CheckItem>
      </div>
      <div
        className="argus-perm-matrix"
        style={
          fullAccess ? { opacity: 0.45, pointerEvents: "none" } : undefined
        }
      >
        {PERMISSION_CATALOG.map((group) => (
          <div className="argus-perm-matrix__row" key={group.resource}>
            <span className="argus-perm-matrix__resource">
              {t(
                `settings.org.rolesTab.permissionResources.${labelKey(group.resource)}`,
              )}
            </span>
            <div className="argus-perm-matrix__cells">
              {group.actions.map((action) => {
                const permission = `${group.resource}.${action}`;
                return (
                  <span
                    aria-checked={selected.has(permission)}
                    key={permission}
                    onClick={() => toggle(permission)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        toggle(permission);
                      }
                    }}
                    role="checkbox"
                    tabIndex={disabled ? -1 : 0}
                  >
                    <CheckItem checked={selected.has(permission)}>
                      {t(
                        `settings.org.rolesTab.permissionActions.${labelKey(action)}`,
                      )}
                    </CheckItem>
                  </span>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
