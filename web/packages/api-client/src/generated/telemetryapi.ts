export interface paths {
    "/telemetry/collectors/enroll": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Exchange a one-time Collector Enrollment Token and CSR for a Telemetry client certificate. */
        post: operations["enrollTelemetryCollector"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/distributions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List supported Collector distributions. */
        get: operations["listCollectorDistributions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/profiles": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List versioned Collection Profiles. */
        get: operations["listCollectionProfiles"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/collectors": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List authorized Collector instances. */
        get: operations["listCollectorInstances"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/collectors/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get an authorized Collector instance. */
        get: operations["getCollectorInstance"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/routes": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List authorized Telemetry routes. */
        get: operations["listTelemetryRoutes"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/routes/tests": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Start an authorized route test. */
        post: operations["createTelemetryRouteTest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/collection-claims": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List authorized Collection Claims. */
        get: operations["listCollectionClaims"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/node-host-bindings": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List Node to Host bindings. */
        get: operations["listKubernetesNodeHostBindings"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/node-host-bindings/{id}/actions/preview-confirm": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Preview confirmation of a Node to Host binding. */
        post: operations["previewKubernetesNodeHostBinding"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/usage": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get authorized Telemetry usage. */
        get: operations["getTelemetryUsage"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/telemetry/query/overview": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Query the Card-safe Telemetry overview. */
        post: operations["queryTelemetryOverview"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/metrics/query": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Execute a PromQL instant query. */
        post: operations["queryMetricsInstant"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/metrics/query_range": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Execute a PromQL range query. */
        post: operations["queryMetricsRange"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/logs/query": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Execute a KQL log query. */
        post: operations["queryLogsKQL"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/traces/graphql": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Execute a read-only SkyWalking GraphQL trace query. */
        post: operations["queryTracesGraphQL"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/{id}/collector": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get the Collector installed on a Host. */
        get: operations["getHostCollector"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/{id}/collector/actions/preview-{action}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
                action: "install" | "configure" | "upgrade" | "repair" | "uninstall";
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Preview a deterministic Host Collector action. */
        post: operations["previewHostCollectorAction"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/{id}/collector": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get the Collector installed on a Kubernetes cluster. */
        get: operations["getKubernetesCollector"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/{id}/collector/actions/preview-{action}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
                action: "install" | "configure" | "upgrade" | "repair" | "uninstall";
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Preview a deterministic Kubernetes Collector action. */
        post: operations["previewKubernetesCollectorAction"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        CollectorDistributionVersion: {
            /** Format: uuid */
            id: string;
            name: string;
            version: string;
            collector_version: string;
            config_schema_version: string;
            support_status: components["schemas"]["CollectorSupportStatus"];
            components: string[];
            artifacts: components["schemas"]["CollectorArtifact"][];
            /** @description 服务端解析的 Kubernetes 默认镜像引用(未配置时缺省) */
            readonly kubernetes_image?: string;
            /** Format: date-time */
            created_at: string;
        };
        CollectionProfile: {
            /** Format: uuid */
            id: string;
            key: string;
            version: string;
            name: string;
            description: string;
            signals: components["schemas"]["TelemetrySignal"][];
            required_components: string[];
            supported_platforms: components["schemas"]["CollectorPlatform"][];
            claim_types: string[];
            config_schema_version: string;
            support_status: components["schemas"]["CollectorSupportStatus"];
        };
        CollectorInstance: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** @enum {string} */
            resource_type: "host" | "kubernetes_cluster";
            /** Format: uuid */
            resource_id: string;
            /** Format: uuid */
            distribution_version_id: string;
            platform: components["schemas"]["CollectorPlatform"];
            role: components["schemas"]["CollectorRole"];
            status: components["schemas"]["CollectorStatus"];
            /** Format: int64 */
            desired_revision: number;
            /** Format: int64 */
            effective_revision: number;
            route?: components["schemas"]["TelemetryRoute"];
            /** @description 最近一次管理操作失败的错误码(成功或无记录时缺省) */
            readonly last_operation_error_code?: string;
            /** Format: date-time */
            last_seen_at?: string;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        CollectorEnrollmentRequest: {
            /** Format: uuid */
            collector_id: string;
            client_csr_pem: string;
            server_csr_pem: string;
        };
        CollectorCertificateResult: {
            /** Format: uuid */
            collector_id: string;
            client_certificate_pem: string;
            server_certificate_pem: string;
            trust_bundle: components["schemas"]["TrustBundleSnapshot"];
            /** Format: uri */
            readonly ingest_grpc_endpoint: string;
            /** Format: uri */
            readonly ingest_http_endpoint: string;
            /** Format: date-time */
            certificate_expires_at: string;
        };
        CollectorConfigRevision: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            collector_id: string;
            /** Format: int64 */
            revision: number;
            profile_ids: string[];
            config_hash: string;
            status: components["schemas"]["ConfigRevisionStatus"];
            failure_code?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            applied_at?: string;
        };
        TelemetryRoute: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** Format: uuid */
            collector_id: string;
            kind: components["schemas"]["TelemetryRouteKind"];
            transport: components["schemas"]["TelemetryRouteTransport"];
            /** @description 隧道形态下 Collector OTLP 出口的本机回环端口;相邻端口保留给身份注册与轮换;direct 形态为空 */
            loopback_port?: number;
            /**
             * @description 隧道运行状态;direct 形态为空。隧道断开不等于 Collector 故障
             * @enum {string}
             */
            readonly tunnel_status?: "desired" | "establishing" | "established" | "degraded" | "down" | "removed";
            /** Format: date-time */
            readonly tunnel_last_established_at?: string;
            readonly tunnel_last_drop_reason?: string;
            /** Format: uuid */
            gateway_collector_id?: string;
            /** @enum {string} */
            status: "pending" | "testing" | "active" | "degraded" | "invalidated";
            /** Format: date-time */
            last_tested_at?: string;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        CollectionClaim: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            physical_resource_ref: string;
            /** Format: uuid */
            collector_id: string;
            /** Format: uuid */
            profile_id?: string;
            claim_type: string;
            signal: components["schemas"]["TelemetrySignal"];
            selector_hash: string;
            /** @enum {string} */
            ownership: "primary" | "supplemental" | "migration";
            /** @enum {string} */
            status: "active" | "released" | "conflict" | "expired";
            /** Format: date-time */
            expires_at?: string;
            /** Format: date-time */
            created_at: string;
        };
        KubernetesNodeHostBinding: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** Format: uuid */
            kubernetes_cluster_id: string;
            node_uid: string;
            node_name: string;
            /** Format: uuid */
            host_id?: string;
            /** @enum {string} */
            matched_by: "system_uuid" | "provider_id" | "machine_id" | "collector_host_id" | "ip" | "manual";
            evidence_hash: string;
            confidence: number;
            /** @enum {string} */
            status: "proposed" | "verified" | "rejected" | "stale";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        NodeHostBindingPreview: {
            /** Format: uuid */
            host_id: string;
            /** Format: int64 */
            expected_version: number;
        };
        CollectorPreview: {
            /** Format: uuid */
            distribution_version_id: string;
            profile_ids: string[];
            route_kind: components["schemas"]["TelemetryRouteKind"];
            transport: components["schemas"]["TelemetryRouteTransport"];
            /** @description 隧道形态的 OTLP 回环端口;缺省 4317,相邻端口保留给身份注册与轮换 */
            loopback_port?: number;
            /** Format: uuid */
            gateway_collector_id?: string;
            /** Format: int64 */
            expected_version?: number;
            /** @description 集群内网镜像全量地址(含 tag/digest);仅 kubernetes_cluster 资源可填,留空使用服务端默认镜像 */
            kubernetes_image?: string;
            /** @description 目标集群中已存在的 imagePullSecret 名称;Argus 不创建或读取仓库凭据 */
            image_pull_secrets?: string[];
        };
        TelemetryRetentionPolicy: {
            metrics_days: number;
            logs_days: number;
            traces_days: number;
            /** Format: int64 */
            version: number;
        };
        TelemetryUsage: {
            /** Format: date-time */
            period_start: string;
            /** Format: date-time */
            period_end: string;
            /** Format: int64 */
            ingested_bytes: number;
            /** Format: int64 */
            metric_points: number;
            /** Format: int64 */
            log_records: number;
            /** Format: int64 */
            spans: number;
            /** Format: int64 */
            estimated_storage_bytes: number;
        };
        TelemetryOverviewQuery: {
            resource_ids: string[];
            /** @default 3600 */
            lookback_seconds: number;
        };
        TelemetryOverview: {
            resource_count: number;
            healthy_collectors: number;
            degraded_collectors: number;
            /** Format: int64 */
            metric_points: number;
            /** Format: int64 */
            log_records: number;
            /** Format: int64 */
            spans: number;
            window_seconds: number;
            partial: boolean;
        };
        TelemetryQueryTimeRange: {
            /** Format: date-time */
            from: string;
            /** Format: date-time */
            to: string;
        };
        TelemetryQueryBudget: {
            /**
             * Format: int64
             * @default 268435456
             */
            max_scan_bytes: number;
            /** @default 50000 */
            max_rows: number;
            /** @default 5000000 */
            max_samples: number;
            /** @default 100000 */
            max_series: number;
            /** @default 10000 */
            timeout_ms: number;
            /**
             * Format: int64
             * @default 8388608
             */
            max_result_bytes: number;
        };
        PromQLInstantQuery: {
            query: string;
            resource_ids: string[];
            time_range: components["schemas"]["TelemetryQueryTimeRange"];
            budget?: components["schemas"]["TelemetryQueryBudget"];
        };
        PromQLRangeQuery: {
            query: string;
            resource_ids: string[];
            time_range: components["schemas"]["TelemetryQueryTimeRange"];
            step_seconds: number;
            budget?: components["schemas"]["TelemetryQueryBudget"];
        };
        KQLQuery: {
            query: string;
            pipeline?: string;
            resource_ids: string[];
            time_range: components["schemas"]["TelemetryQueryTimeRange"];
            budget?: components["schemas"]["TelemetryQueryBudget"];
        };
        SkyWalkingTraceGraphQLQuery: {
            query: string;
            operation_name?: string;
            variables?: {
                [key: string]: unknown;
            };
            resource_ids: string[];
            time_range: components["schemas"]["TelemetryQueryTimeRange"];
            budget?: components["schemas"]["TelemetryQueryBudget"];
        };
        TelemetryQueryMeta: {
            /** Format: int64 */
            scanned_bytes: number;
            /** Format: int64 */
            scanned_rows: number;
            /** Format: int64 */
            returned_rows: number;
            /** Format: int64 */
            loaded_samples: number;
            /** Format: int64 */
            elapsed_ms: number;
            plan_hash: string;
            engine: string;
            engine_version: string;
            partial: boolean;
        };
        PrometheusQueryData: {
            /** @enum {string} */
            resultType: "scalar" | "string" | "vector" | "matrix";
            result: unknown;
        };
        PrometheusQueryResponse: {
            /** @constant */
            status: "success";
            data: components["schemas"]["PrometheusQueryData"];
            warnings: string[];
            argus_meta: components["schemas"]["TelemetryQueryMeta"];
        };
        KQLQueryResponse: {
            /** @constant */
            schema_version: "argus.kql_result/v1";
            /** @enum {string} */
            result_type: "log_entries" | "log_streams";
            data: unknown;
            warnings: string[];
            partial: boolean;
            meta: components["schemas"]["TelemetryQueryMeta"];
        };
        SkyWalkingGraphQLError: {
            message: string;
        };
        SkyWalkingGraphQLResponse: {
            data: {
                [key: string]: unknown;
            };
            errors?: components["schemas"]["SkyWalkingGraphQLError"][];
            extensions: {
                argus: components["schemas"]["TelemetryQueryMeta"];
            };
        };
        CollectorPage: {
            items: components["schemas"]["CollectorInstance"][];
            page: components["schemas"]["CursorPage"];
        };
        RouteTestCreate: {
            /** Format: uuid */
            collector_id: string;
            route_kind: components["schemas"]["TelemetryRouteKind"];
            transport: components["schemas"]["TelemetryRouteTransport"];
            /** Format: uuid */
            gateway_collector_id?: string;
        };
        RouteTestResult: {
            /** Format: uuid */
            id: string;
            /** @enum {string} */
            status: "pending" | "running" | "succeeded" | "failed" | "expired";
            error_code?: string;
            /** Format: date-time */
            started_at: string;
            /** Format: date-time */
            completed_at?: string;
            /** Format: date-time */
            expires_at: string;
        };
        RequestId: string;
        ApiError: {
            code: string;
            message_key: string;
            params?: {
                [key: string]: string | number | boolean;
            };
            message?: string;
            request_id: components["schemas"]["RequestId"];
            trace_id?: string;
            /** @default false */
            retryable: boolean;
        };
        TrustBundleSnapshot: {
            /** Format: int64 */
            epoch: number;
            /** @enum {string} */
            state: "stable" | "preparing" | "overlapping" | "retiring" | "failed";
            bundle_pem: string;
            bundle_sha256: string;
            current_ca_fingerprints: string[];
            next_ca_fingerprints: string[];
            /** Format: date-time */
            started_at: string;
            /** Format: date-time */
            retire_at?: string;
        };
        /** @enum {string} */
        CollectorSupportStatus: "supported" | "validation_pending" | "retired";
        /** @enum {string} */
        CollectorPlatform: "linux_arm64" | "linux_amd64" | "windows_amd64";
        CollectorArtifact: {
            platform: components["schemas"]["CollectorPlatform"];
            /** Format: uri */
            readonly uri: string;
            sha256: string;
            readonly signature: string;
            readonly signing_key_id: string;
            /** Format: int64 */
            byte_size: number;
        };
        /** @enum {string} */
        TelemetrySignal: "metrics" | "logs" | "traces";
        /** @enum {string} */
        CollectorRole: "direct" | "leaf" | "edge_gateway" | "daemonset" | "kubernetes_gateway";
        /** @enum {string} */
        CollectorStatus: "pending_install" | "installing" | "converged" | "degraded" | "backlog" | "result_unknown" | "uninstalling" | "uninstalled";
        /** @enum {string} */
        TelemetryRouteKind: "direct_argus" | "bastion_gateway";
        /**
         * @description 遥测字节的物理路径,与路由 kind 正交;隧道形态见 PlanV4
         * @enum {string}
         */
        TelemetryRouteTransport: "direct" | "executor_tunnel" | "bastion_tunnel";
        PartialMetadata: {
            partial: boolean;
            reasons: ("authorization_filtered" | "budget_truncated" | "source_timeout" | "source_unavailable")[];
        };
        CursorPage: {
            next_cursor: string | null;
            has_more: boolean;
            partial: components["schemas"]["PartialMetadata"];
        };
        IdempotencyKey: string;
        PublicJsonValue: unknown;
        PublicJsonObject: unknown;
        /** PendingActionPublic */
        "pending-action-public.schema": {
            /** @constant */
            schema_version: "argus.pending_action/v1";
            action_ref: string;
            action_type: string;
            title: string;
            summary: string;
            /** @enum {unknown} */
            risk: "read" | "write" | "dangerous" | "critical";
            preview: components["schemas"]["PublicJsonObject"];
            diff: {
                /** @enum {unknown} */
                kind: "add" | "remove" | "change" | "note";
                text: string;
            }[];
            /** @enum {unknown} */
            status: "prepared" | "awaiting_confirmation" | "awaiting_approval" | "ready" | "executing" | "succeeded" | "failed" | "result_unknown" | "cancelled" | "expired" | "rejected" | "invalidated";
            available_actions: ("confirm" | "cancel" | "approve" | "reject")[];
            approval?: {
                required: boolean;
                policy_ref?: string;
                minimum_approvers: number;
                approved_count: number;
                separation_of_duty: boolean;
            };
            execution_ref?: string;
            result_summary?: string;
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        /** @enum {string} */
        ConfigRevisionStatus: "prepared" | "applying" | "effective" | "failed" | "rolled_back" | "superseded";
    };
    responses: {
        /** @description Stable Argus API error. */
        Error: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ApiError"];
            };
        };
    };
    parameters: {
        Cursor: string;
        Limit: number;
        IdempotencyKey: components["schemas"]["IdempotencyKey"];
        CsrfToken: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    enrollTelemetryCollector: {
        parameters: {
            query?: never;
            header: {
                "X-Argus-Telemetry-Enrollment-Token": string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CollectorEnrollmentRequest"];
            };
        };
        responses: {
            /** @description One-time Collector certificate result. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectorCertificateResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listCollectorDistributions: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Distribution catalog. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectorDistributionVersion"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listCollectionProfiles: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Profile catalog. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectionProfile"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listCollectorInstances: {
        parameters: {
            query?: {
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
                resource_type?: "host" | "kubernetes_cluster";
                resource_id?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Collector page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectorPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getCollectorInstance: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Collector. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectorInstance"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listTelemetryRoutes: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Routes. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TelemetryRoute"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createTelemetryRouteTest: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RouteTestCreate"];
            };
        };
        responses: {
            /** @description Route test. */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RouteTestResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listCollectionClaims: {
        parameters: {
            query?: {
                resource_id?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Claims. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectionClaim"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listKubernetesNodeHostBindings: {
        parameters: {
            query: {
                kubernetes_cluster_id: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Bindings. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["KubernetesNodeHostBinding"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    previewKubernetesNodeHostBinding: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["NodeHostBindingPreview"];
            };
        };
        responses: {
            /** @description Pending action. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["pending-action-public.schema"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getTelemetryUsage: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Usage. */
            200: {
                headers: {
                    "X-Retention-Metrics-Days"?: number;
                    "X-Retention-Logs-Days"?: number;
                    "X-Retention-Traces-Days"?: number;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TelemetryUsage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    queryTelemetryOverview: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["TelemetryOverviewQuery"];
            };
        };
        responses: {
            /** @description Overview. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TelemetryOverview"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    queryMetricsInstant: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PromQLInstantQuery"];
            };
        };
        responses: {
            /** @description Prometheus-compatible PromQL result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrometheusQueryResponse"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    queryMetricsRange: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PromQLRangeQuery"];
            };
        };
        responses: {
            /** @description Prometheus-compatible PromQL range result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrometheusQueryResponse"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    queryLogsKQL: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["KQLQuery"];
            };
        };
        responses: {
            /** @description KQL log result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["KQLQueryResponse"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    queryTracesGraphQL: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SkyWalkingTraceGraphQLQuery"];
            };
        };
        responses: {
            /** @description SkyWalking GraphQL trace result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SkyWalkingGraphQLResponse"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getHostCollector: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Collector. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectorInstance"];
                };
            };
            /** @description No Collector is installed. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    previewHostCollectorAction: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
                action: "install" | "configure" | "upgrade" | "repair" | "uninstall";
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CollectorPreview"];
            };
        };
        responses: {
            /** @description Pending action. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["pending-action-public.schema"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getKubernetesCollector: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Collector. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CollectorInstance"];
                };
            };
            /** @description No Collector is installed. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    previewKubernetesCollectorAction: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
                action: "install" | "configure" | "upgrade" | "repair" | "uninstall";
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CollectorPreview"];
            };
        };
        responses: {
            /** @description Pending action. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["pending-action-public.schema"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
