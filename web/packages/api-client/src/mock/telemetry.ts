import type { ArgusApiClient } from "../client";
import type {
  CollectionProfile,
  CollectorDistributionVersion,
  LogsResult,
  MetricsResult,
  TelemetryOverview,
  TelemetryRoute,
  TelemetryUsage,
  TracesResult,
} from "../generated/contracts";
import type { MockContext } from "./context";

const DISTRIBUTIONS: CollectorDistributionVersion[] = [
  {
    id: "dist-linux-arm64-v1",
    name: "Argus OpenTelemetry Collector",
    version: "0.132.0-argus.1",
    collector_version: "0.132.0",
    config_schema_version: "argus.otelcol/v1",
    support_status: "supported",
    components: ["otlp", "hostmetrics", "journald", "filelog", "prometheus"],
    artifacts: [
      {
        platform: "linux_arm64",
        uri: "https://artifacts.invalid/argus-otelcol-linux-arm64.tar.gz",
        sha256: "a".repeat(64),
        signature: "mock-signature",
        signing_key_id: "argus-release-2026",
        byte_size: 48_000_000,
      },
    ],
    created_at: "2026-08-18T00:00:00Z",
  },
  {
    id: "dist-windows-amd64-v1",
    name: "Argus OpenTelemetry Collector",
    version: "0.132.0-argus.1",
    collector_version: "0.132.0",
    config_schema_version: "argus.otelcol/v1",
    support_status: "validation_pending",
    components: ["otlp", "hostmetrics", "windowseventlog"],
    artifacts: [
      {
        platform: "windows_amd64",
        uri: "https://artifacts.invalid/argus-otelcol-windows-amd64.zip",
        sha256: "b".repeat(64),
        signature: "mock-signature",
        signing_key_id: "argus-release-2026",
        byte_size: 51_000_000,
      },
    ],
    created_at: "2026-08-18T00:00:00Z",
  },
];

const PROFILES: CollectionProfile[] = [
  profile("profile-host-basic", "host-basic", ["metrics"], ["linux_arm64"]),
  profile("profile-linux-journald", "linux-journald", ["logs"], ["linux_arm64"]),
  profile("profile-file-log", "file-log", ["logs"], ["linux_arm64"]),
  profile("profile-prometheus", "prometheus-endpoint", ["metrics"], ["linux_arm64"]),
  profile("profile-otlp", "otlp-receiver", ["metrics", "logs", "traces"], ["linux_arm64"]),
  profile("profile-k8s-node-container", "k8s-node-container", ["metrics", "logs"], ["linux_arm64"]),
  profile("profile-k8s-cluster", "k8s-cluster", ["metrics"], ["linux_arm64"]),
  profile("profile-k8s-otlp-gateway", "k8s-otlp-gateway", ["metrics", "logs", "traces"], ["linux_arm64"]),
  profile("profile-collector-self", "collector-self", ["metrics", "logs"], ["linux_arm64"]),
  {
    ...profile("profile-windows-event-log", "windows-event-log", ["logs"], ["windows_amd64"]),
    support_status: "validation_pending",
  },
];

function profile(
  id: string,
  key: string,
  signals: CollectionProfile["signals"],
  supportedPlatforms: CollectionProfile["supported_platforms"],
): CollectionProfile {
  return {
    id,
    key,
    version: "1.0.0",
    name: key,
    description: `${key} managed profile`,
    signals,
    required_components: [key],
    supported_platforms: supportedPlatforms,
    claim_types: [key],
    config_schema_version: "argus.otelcol/v1",
    support_status: "supported",
  };
}

function meta(resourceIds: string[]) {
  const partial = resourceIds.includes("k8s-prod-east");
  const partialReasons: Array<"row_limit"> = partial ? ["row_limit"] : [];
  return {
    schema_version: "argus.telemetry_result/v1" as const,
    partial,
    partial_reasons: partialReasons,
    applied_resource_count: resourceIds.length,
    scanned_bytes: 4096,
    elapsed_ms: 8,
  };
}

export function createTelemetryDomain(ctx: MockContext): ArgusApiClient["telemetry"] {
  return {
    async listDistributions() {
      await ctx.pause();
      return DISTRIBUTIONS.map((item) => ({ ...item, artifacts: [...item.artifacts] }));
    },
    async listProfiles() {
      await ctx.pause();
      return PROFILES.map((item) => ({ ...item }));
    },
    async listCollectors() {
      await ctx.pause();
      const enterpriseId = ctx.enterpriseId();
      return ctx.db.collectors.filter((item) => item.enterprise_id === enterpriseId);
    },
    async listRoutes() {
      await ctx.pause();
      return ctx.db.collectors.flatMap((collector): TelemetryRoute[] =>
        collector.route ? [collector.route] : [],
      );
    },
    async listClaims(resourceId) {
      await ctx.pause();
      return ctx.db.collectionClaims.filter(
        (entry) => entry.enterprise_id === ctx.enterpriseId() &&
          (!resourceId || entry.physical_resource_ref.includes(resourceId)),
      );
    },
    async testRoute(input) {
      await ctx.pause();
      const collector = ctx.db.collectors.find(
        (entry) => entry.id === input.collector_id && entry.enterprise_id === ctx.enterpriseId(),
      );
      if (!collector || (input.route_kind === "bastion_gateway" && !input.gateway_collector_id)) {
        throw new Error("TELEMETRY_ROUTE_INVALID");
      }
      const now = new Date();
      return {
        id: crypto.randomUUID(),
        status: "succeeded" as const,
        started_at: now.toISOString(),
        completed_at: now.toISOString(),
        expires_at: new Date(now.getTime() + 10 * 60_000).toISOString(),
      };
    },
    async usage(): Promise<TelemetryUsage> {
      await ctx.pause();
      const end = new Date();
      const start = new Date(end.getTime() - 30 * 86_400_000);
      return {
        period_start: start.toISOString(),
        period_end: end.toISOString(),
        ingested_bytes: 2_684_354_560,
        metric_points: 4_280_000,
        log_records: 184_000,
        spans: 96_000,
        estimated_storage_bytes: 1_342_177_280,
      };
    },
    async queryMetrics(input): Promise<MetricsResult> {
      await ctx.pause();
      const end = Date.parse(input.to);
      return {
        series: input.resource_ids.map((resourceId, resourceIndex) => ({
          resource_id: resourceId,
          metric_name: input.metric_name,
          unit: "%",
          points: Array.from({ length: 12 }, (_, index) => ({
            timestamp: new Date(end - (11 - index) * input.step_seconds * 1000).toISOString(),
            value: 35 + resourceIndex * 8 + ((index * 7) % 23),
          })),
        })),
        meta: meta(input.resource_ids),
      };
    },
    async queryLogs(input): Promise<LogsResult> {
      await ctx.pause();
      return {
        records: input.resource_ids.slice(0, 3).map((resourceId, index) => ({
          timestamp: new Date(Date.parse(input.to) - index * 45_000).toISOString(),
          resource_id: resourceId,
          service_name: input.service_name ?? "argus-demo",
          severity: index === 0 ? "ERROR" : "INFO",
          body: index === 0 ? "request failed: credential=[REDACTED]" : "request completed",
          trace_id: String(index + 1).padStart(32, "0"),
        })),
        meta: meta(input.resource_ids),
      };
    },
    async queryTraces(input): Promise<TracesResult> {
      await ctx.pause();
      return {
        traces: input.resource_ids.slice(0, 4).map((resourceId, index) => ({
          trace_id: String(index + 101).padStart(32, "0"),
          resource_id: resourceId,
          service_name: input.service_name ?? "checkout-api",
          root_span_name: input.operation ?? "POST /checkout",
          started_at: new Date(Date.parse(input.to) - index * 120_000).toISOString(),
          duration_ms: 85 + index * 47,
          span_count: 6 + index,
          status: index === 0 ? "error" : "ok",
        })),
        meta: meta(input.resource_ids),
      };
    },
    async overview(input): Promise<TelemetryOverview> {
      await ctx.pause();
      const collectors = ctx.db.collectors.filter((item) =>
        input.resource_ids.includes(item.resource_id),
      );
      return {
        resource_count: input.resource_ids.length,
        healthy_collectors: collectors.filter((item) => item.status === "converged").length,
        degraded_collectors: collectors.filter((item) => item.status !== "converged").length,
        metric_points: input.resource_ids.length * 7200,
        log_records: input.resource_ids.length * 320,
        spans: input.resource_ids.length * 140,
        window_seconds: input.lookback_seconds,
        partial: false,
      };
    },
  };
}
