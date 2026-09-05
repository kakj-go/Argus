const ACTION_TYPE_ALIASES: Record<string, string> = {
  "bastion.scope.create": "bastion_scope.create",
  "bastion.scope.update": "bastion_scope.update",
  "bastion.scope.delete": "bastion_scope.delete",
  "kubernetes.cluster.create": "kubernetes.create",
  "kubernetes.cluster.update": "kubernetes.update",
  "kubernetes.cluster.delete": "kubernetes.delete",
  "telemetry.node_binding.confirm": "telemetry.node_host_binding.confirm",
};

/** Keep mock execution tool names independent from the public domain action type. */
export function publicActionType(tool: string): string {
  const collectorMatch = tool.match(
    /^telemetry\.(?:host|kubernetes)\.(install|configure|upgrade|repair|uninstall)$/,
  );
  if (collectorMatch) return `telemetry.collector.${collectorMatch[1]}`;
  return ACTION_TYPE_ALIASES[tool] ?? tool;
}
