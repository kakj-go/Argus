import type { PendingActionPublic } from "@argus/api-client";

type Translate = (key: string, options?: Record<string, unknown>) => string;
type DiffKind = PendingActionPublic["diff"][number]["kind"];

type ActionDefinition = {
  key: string;
  diffKeys: readonly string[];
};

export const PENDING_ACTION_TYPES = [
  "host.create",
  "host.update",
  "host.delete",
  "host.enrollment.rotate",
  "host.uninstall.command",
  "host.restart",
  "kubernetes.create",
  "kubernetes.update",
  "kubernetes.delete",
  "kubernetes.workload.restart",
  "bastion_scope.create",
  "bastion_scope.update",
  "bastion_scope.delete",
  "bastion.enrollment.rotate",
  "bastion.connector.replace",
  "bastion.connector.install.retry",
  "connector.uninstall",
  "connector.cert.rotate",
  "telemetry.collector.install",
  "telemetry.collector.configure",
  "telemetry.collector.route",
  "telemetry.collector.upgrade",
  "telemetry.collector.repair",
  "telemetry.collector.uninstall",
  "telemetry.node_host_binding.confirm",
] as const;

export type PendingActionType = (typeof PENDING_ACTION_TYPES)[number];

const definitions: Record<PendingActionType, ActionDefinition> =
  Object.fromEntries(
    PENDING_ACTION_TYPES.map((actionType) => {
      const key = actionType.replaceAll(".", "_");
      return [
        actionType,
        { key, diffKeys: [`pendingActions.actions.${key}.diff`] },
      ];
    }),
  ) as unknown as Record<PendingActionType, ActionDefinition>;

definitions["bastion.connector.replace"] = {
  key: "bastion_connector_replace",
  diffKeys: [
    "pendingActions.actions.bastion_connector_replace.diffFence",
    "pendingActions.actions.bastion_connector_replace.diffInstall",
  ],
};

const aliases: Record<string, PendingActionType> = {
  "bastion.scope.create": "bastion_scope.create",
  "bastion.scope.update": "bastion_scope.update",
  "bastion.scope.delete": "bastion_scope.delete",
  "kubernetes.cluster.create": "kubernetes.create",
  "kubernetes.cluster.update": "kubernetes.update",
  "kubernetes.cluster.delete": "kubernetes.delete",
  "telemetry.host.install": "telemetry.collector.install",
  "telemetry.host.configure": "telemetry.collector.configure",
  "telemetry.host.upgrade": "telemetry.collector.upgrade",
  "telemetry.host.repair": "telemetry.collector.repair",
  "telemetry.host.uninstall": "telemetry.collector.uninstall",
  "telemetry.kubernetes.install": "telemetry.collector.install",
  "telemetry.kubernetes.configure": "telemetry.collector.configure",
  "telemetry.kubernetes.upgrade": "telemetry.collector.upgrade",
  "telemetry.kubernetes.repair": "telemetry.collector.repair",
  "telemetry.kubernetes.uninstall": "telemetry.collector.uninstall",
  "telemetry.node_binding.confirm": "telemetry.node_host_binding.confirm",
};

export type PresentedPendingAction = {
  title: string;
  summary: string;
  riskLabel: string;
  diff: PendingActionPublic["diff"];
  affectedName: string;
  resultSummary?: string;
  known: boolean;
};

export type PresentedPendingActionPreview = {
  label: string;
  value: string;
};

const previewFieldAliases: Record<string, string> = {
  connectionMode: "connection_mode",
  installMode: "install_mode",
  targetAddress: "target_address",
  memberCount: "member_count",
  resourceType: "resource_type",
  resourceName: "resource_name",
  routeKind: "route_kind",
  loopbackPort: "loopback_port",
  nodeName: "node_name",
};

const publicPreviewFields = new Set([
  "name",
  "hostname",
  "address",
  "port",
  "platform",
  "environment",
  "connection_mode",
  "protocol",
  "username",
  "resource_type",
  "resource_name",
  "target",
  "action",
  "namespace",
  "workload",
  "ready",
  "fromVersion",
  "toVersion",
  "batch",
  "targets",
  "install_mode",
  "target_address",
  "member_count",
  "role",
  "operation",
  "distribution",
  "profiles",
  "route_kind",
  "transport",
  "loopback_port",
  "kubernetes_image",
  "node_name",
  "cluster",
  "scope",
  "connector",
]);

const localizedPreviewValues = new Set([
  "self_enrolled",
  "direct_ssh",
  "direct_winrm",
  "via_bastion",
  "via_bastion_tunnel",
  "connector_local",
  "command",
  "direct_install",
  "direct_install_tunnel",
  "install",
  "configure",
  "route",
  "upgrade",
  "repair",
  "uninstall",
  "direct_argus",
  "bastion_gateway",
  "direct",
  "executor_tunnel",
  "bastion_tunnel",
  "host",
  "kubernetes_cluster",
  "online",
  "onboarding",
]);

function previewOf(action: PendingActionPublic): Record<string, unknown> {
  return typeof action.preview === "object" && action.preview !== null
    ? (action.preview as Record<string, unknown>)
    : {};
}

function displayName(action: PendingActionPublic, fallback: string): string {
  const preview = previewOf(action);
  for (const key of [
    "name",
    "host_name",
    "cluster_name",
    "scope_name",
    "connector_name",
    "node_name",
    "workload",
    "connector",
    "scope",
  ]) {
    const value = preview[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return fallback;
}

function canonicalType(value: string): PendingActionType | undefined {
  if (PENDING_ACTION_TYPES.includes(value as PendingActionType)) {
    return value as PendingActionType;
  }
  return aliases[value];
}

function localizedDiff(
  action: PendingActionPublic,
  definition: ActionDefinition | undefined,
  t: Translate,
  options: Record<string, unknown>,
): PendingActionPublic["diff"] {
  const keys = definition?.diffKeys ?? ["pendingActions.fallback.diff"];
  return keys.map((key, index) => ({
    kind: (action.diff[index]?.kind ??
      action.diff[0]?.kind ??
      "change") as DiffKind,
    text: t(key, options),
  }));
}

export function presentPendingActionPreview(
  action: PendingActionPublic,
  t: Translate,
): PresentedPendingActionPreview[] {
  return Object.entries(previewOf(action)).flatMap(([sourceKey, rawValue]) => {
    const key = previewFieldAliases[sourceKey] ?? sourceKey;
    if (
      !publicPreviewFields.has(key) ||
      rawValue === undefined ||
      rawValue === null
    )
      return [];
    const values = Array.isArray(rawValue) ? rawValue : [rawValue];
    const formatted = values.map((value) => {
      if (typeof value === "string" && localizedPreviewValues.has(value)) {
        return t(`pendingActions.values.${value}`);
      }
      if (typeof value === "object") return JSON.stringify(value);
      return String(value);
    });
    return [
      {
        label: t(`pendingActions.previewFields.${key}`),
        value: formatted.join(", "),
      },
    ];
  });
}

export function presentPendingAction(
  action: PendingActionPublic,
  t: Translate,
): PresentedPendingAction {
  const actionType = canonicalType(action.action_type);
  const definition = actionType ? definitions[actionType] : undefined;
  const name = displayName(action, t("common.unknown"));
  const options = { name };
  const prefix = definition
    ? `pendingActions.actions.${definition.key}`
    : "pendingActions.fallback";
  const failed =
    action.status === "failed" || action.status === "result_unknown";
  const resultSummary =
    action.status === "rejected" && action.result_summary
      ? action.result_summary
      : failed
        ? t("pendingActions.resultFailed")
        : action.status === "succeeded"
          ? t(`${prefix}.result`, options)
          : undefined;

  return {
    title: t(`${prefix}.title`, options),
    summary: t(`${prefix}.summary`, options),
    riskLabel: t(`governance.approvals.risk.${action.risk}`),
    diff: localizedDiff(action, definition, t, options),
    affectedName: definition ? name : t("pendingActions.fallback.title"),
    resultSummary,
    known: definition !== undefined,
  };
}
