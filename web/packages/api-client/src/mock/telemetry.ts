import type { ArgusApiClient } from "../client";
import type {
  CollectionProfile,
  CollectorDistributionVersion,
  TelemetryOverview,
  TelemetryRoute,
  TelemetryUsage,
  PromQLInstantQuery,
  PromQLRangeQuery,
  KQLQuery,
  SkyWalkingTraceGraphQLQuery,
  PrometheusQueryResponse,
  KQLQueryResponse,
  SkyWalkingGraphQLResponse,
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
    async queryMetrics(input: PromQLInstantQuery): Promise<PrometheusQueryResponse> {
      await ctx.pause();
      const now = new Date(input.time_range.to).getTime();
      const partial = input.resource_ids.includes("k8s-prod-east");
      const result = input.resource_ids.map((resource_id, index) => ({ metric: { __name__: "system_cpu_utilization", resource_id }, values: [
          [(now - 120_000) / 1000, String(38 + index)],
          [(now - 60_000) / 1000, String(40 + index)],
          [now / 1000, String(42 + index)],
        ] }));
      return { status: "success", data: { resultType: "matrix", result }, warnings: partial ? ["row_limit"] : [], argus_meta: queryMeta("a", partial) };
    },
    async queryMetricsRange(input: PromQLRangeQuery): Promise<PrometheusQueryResponse> {
      return this.queryMetrics(input);
    },
    async queryLogs(input: KQLQuery): Promise<KQLQueryResponse> {
      await ctx.pause();
      const now = new Date(input.time_range.to).toISOString();
      const data = input.resource_ids.slice(0, 3).map((resource_id, index) => ({ timestamp: now, resource_id, severity_text: index === 0 ? "ERROR" : "INFO", body: index === 0 ? "credential=[REDACTED]" : "request completed", trace_id: String(index + 1).padStart(32, "0"), service_name: "argus-demo" }));
      return { schema_version: "argus.kql_result/v1", result_type: "log_entries", data, warnings: [], partial: false, meta: queryMeta("b", false) };
    },
    async queryTraces(input: SkyWalkingTraceGraphQLQuery): Promise<SkyWalkingGraphQLResponse> {
      await ctx.pause();
      const now = new Date(input.time_range.to).toISOString();
      const traces = input.resource_ids.slice(0, 3).map((resource_id, index) => ({ traceId: String(index + 1).padStart(32, "0"), rootService: "argus-demo", rootOperation: "GET /", startTime: now, duration: 1, spanCount: 1, errorCount: 0, status: "ok", resource_id }));
      return { data: { queryBasicTraces: { total: traces.length, traces } }, extensions: { argus: queryMeta("c", false) } };
    },
  };
}

function queryMeta(seed: string, partial: boolean) {
  return {
    scanned_bytes: 4096,
    scanned_rows: 32,
    returned_rows: 3,
    loaded_samples: 12,
    elapsed_ms: 4,
    plan_hash: seed.repeat(64),
    engine: "mock",
    engine_version: "v1",
    partial,
  };
}
