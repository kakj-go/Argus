type Translate = (key: string, options?: Record<string, unknown>) => string;

export function connectorInstallStageLabel(
  t: Translate,
  stage: string,
): string {
  return t(`hosts.bastionOperation.${stage}`);
}

export function connectorInstallStatusLabel(
  t: Translate,
  status: string,
): string {
  return t(`hosts.bastionOperationStatus.${status}`);
}

export function connectorInstallEventStatusLabel(
  t: Translate,
  status: string,
): string {
  return t(`hosts.bastionOperationEventStatus.${status}`);
}

export function connectorControlTunnelStatusLabel(
  t: Translate,
  status: string | undefined,
): string {
  return t(`hosts.components.installed.tunnelStatus.${status ?? "desired"}`);
}
