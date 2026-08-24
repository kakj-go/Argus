import type {
  BastionScope,
  CollectorInstance,
  Environment,
  Host,
} from "@argus/api-client";

type HostConnectionStatus = Host["connection_status"];
type CollectorStatus = CollectorInstance["status"] | "not_installed";
type ProvisionalHostTelemetry = {
  collectorStatus?: CollectorStatus;
  telemetryRoute?: string;
};
type ProvisionalBastionTelemetry = { defaultTelemetryRoute?: string };

/** Direct Executor 对外公布的固定出口地址；mock 使用明确的文档演示地址。 */
export const ARGUS_EGRESS_ADDRESSES = (
  import.meta.env.VITE_DIRECT_EGRESS_ADDRESSES ??
  (import.meta.env.VITE_API_MODE === "mock" ? "203.0.113.1" : "")
)
  .split(",")
  .map((value: string) => value.trim())
  .filter(Boolean);

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

export function environmentTone(env: Environment): Tone {
  if (env === "production") return "danger";
  if (env === "staging") return "warning";
  return "info";
}

export function hostStatusTone(status: HostConnectionStatus): Tone {
  switch (status) {
    case "online":
      return "success";
    case "onboarding":
      return "info";
    case "degraded":
      return "warning";
    case "offline":
      return "danger";
    default:
      return "neutral";
  }
}

export function collectorTone(status: CollectorStatus): Tone {
  switch (status) {
    case "converged":
      return "success";
    case "installing":
      return "info";
    case "backlog":
      return "warning";
    case "degraded":
    case "result_unknown":
      return "danger";
    default:
      return "neutral";
  }
}

/** 连接路径文案：Argus → 堡垒机 → 目标地址 / Direct Executor → 目标地址。 */
export function connectionPathKey(
  host: Host,
): "viaBastion" | "connectorLocal" | "direct" {
  if (host.connection_mode === "via_bastion") return "viaBastion";
  if (host.connection_mode === "connector_local") return "connectorLocal";
  return "direct";
}

export function scopeOf(
  host: Host,
  scopes: BastionScope[],
): BastionScope | undefined {
  return scopes.find((scope) => scope.id === host.bastion_scope_id);
}

export function collectorStatusOf(host: Host): CollectorStatus {
  return (
    (host as Host & ProvisionalHostTelemetry).collectorStatus ?? "not_installed"
  );
}

export function telemetryRouteOf(host: Host): string | undefined {
  return (host as Host & ProvisionalHostTelemetry).telemetryRoute;
}

export function defaultTelemetryRouteOf(
  scope: BastionScope | undefined,
): string | undefined {
  return (scope as (BastionScope & ProvisionalBastionTelemetry) | undefined)
    ?.defaultTelemetryRoute;
}

/** "key=value" 每行一个 的文本 <-> labels 对象。 */
export function parseLabels(text: string): Record<string, string> {
  const labels: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const index = trimmed.indexOf("=");
    if (index <= 0) continue;
    labels[trimmed.slice(0, index).trim()] = trimmed.slice(index + 1).trim();
  }
  return labels;
}

export function labelsToText(labels: Record<string, string>): string {
  return Object.entries(labels)
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

/** 由字符串种子生成确定性伪随机序列（概览页指标演示数据）。 */
export function seededSeries(
  seed: string,
  count: number,
  base: number,
  variance: number,
): number[] {
  let state = 0;
  for (let index = 0; index < seed.length; index += 1) {
    state = (state * 31 + seed.charCodeAt(index)) >>> 0;
  }
  const next = () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let value = state;
    value = Math.imul(value ^ (value >>> 15), value | 1);
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
  };
  const points: number[] = [];
  let current = base;
  for (let index = 0; index < count; index += 1) {
    current += (next() - 0.5) * variance;
    current = Math.max(2, Math.min(98, current));
    points.push(Math.round(current * 10) / 10);
  }
  return points;
}

export function seededNumber(seed: string, min: number, max: number): number {
  return seededSeries(seed, 1, (min + max) / 2, (max - min) / 2)[0] ?? min;
}

export function formatDateTime(iso?: string): string {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString();
}

/** 将 ISO 时间点格式化为 "3 天 2 小时" 风格的时长。 */
export function formatUptime(
  fromIso: string,
  now: number = Date.now(),
): string {
  const start = new Date(fromIso).getTime();
  if (Number.isNaN(start)) return "—";
  const totalMinutes = Math.max(0, Math.floor((now - start) / 60_000));
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}

export function formatDuration(startIso?: string, endIso?: string): string {
  if (!startIso) return "—";
  const start = new Date(startIso).getTime();
  const end = endIso ? new Date(endIso).getTime() : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end)) return "—";
  const totalSeconds = Math.max(0, Math.floor((end - start) / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes >= 60) return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
  return `${minutes}m ${seconds}s`;
}
