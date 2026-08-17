export interface paths {
    "/enterprise/kubernetes-clusters": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List DataScope-filtered Kubernetes clusters. */
        get: operations["listKubernetesClusters"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get a DataScope-filtered Kubernetes cluster. */
        get: operations["getKubernetesCluster"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/actions/preview-create": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Freeze a Kubernetes cluster creation plan. */
        post: operations["previewCreateKubernetesCluster"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/{id}/actions/preview-update": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Freeze a Kubernetes cluster update. */
        post: operations["previewUpdateKubernetesCluster"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/{id}/actions/preview-delete": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Freeze a Kubernetes cluster logical deletion. */
        post: operations["previewDeleteKubernetesCluster"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/connection-tests": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Start a bounded Kubernetes connection test. */
        post: operations["createKubernetesConnectionTest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/{id}/resources": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** List bounded Kubernetes resources after cluster and Namespace DataScope checks. */
        get: operations["listKubernetesResources"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/kubernetes-clusters/{id}/pod-logs": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Read at most one MiB of Pod logs. */
        get: operations["getKubernetesPodLogs"];
        put?: never;
        post?: never;
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
        KubernetesCluster: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            /** Format: uri */
            api_server: string;
            connection_mode: components["schemas"]["KubernetesConnectionMode"];
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            connector_id?: string;
            /** Format: uuid */
            credential_id?: string;
            default_namespace?: string;
            environment: components["schemas"]["Environment"];
            labels: components["schemas"]["Labels"];
            /** Format: int64 */
            labels_version: number;
            /** Format: int64 */
            resource_version: number;
            /** @enum {string} */
            connection_status: "pending_connector" | "connected" | "degraded" | "disconnected";
            kubernetes_version?: string;
            node_count?: number;
            ready_node_count?: number;
            /** @enum {string} */
            status: "active" | "disabled" | "deleted";
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        KubernetesClusterPage: {
            items: components["schemas"]["KubernetesCluster"][];
            page: components["schemas"]["CursorPage"];
        };
        KubernetesPreviewCreate: {
            name: string;
            /** Format: uri */
            api_server: string;
            connection_mode: components["schemas"]["KubernetesConnectionMode"];
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            credential_id?: string;
            default_namespace?: string;
            environment: components["schemas"]["Environment"];
            labels: components["schemas"]["UserLabels"];
            /** Format: uuid */
            connection_test_id?: string;
        };
        KubernetesPreviewUpdate: {
            name?: string;
            /** Format: uri */
            api_server?: string;
            connection_mode?: components["schemas"]["KubernetesConnectionMode"];
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            credential_id?: string;
            default_namespace?: string;
            environment?: components["schemas"]["Environment"];
            labels?: components["schemas"]["UserLabels"];
            /** Format: uuid */
            connection_test_id?: string;
            /** Format: int64 */
            expected_version: number;
        };
        KubernetesResource: {
            /** Format: uuid */
            cluster_id: string;
            /** @enum {string} */
            resource_type: "namespace" | "node" | "pod" | "deployment" | "statefulset" | "daemonset" | "service";
            namespace?: string;
            name: string;
            labels: components["schemas"]["Labels"];
            summary: {
                [key: string]: unknown;
            };
        };
        KubernetesResourcePage: {
            items: components["schemas"]["KubernetesResource"][];
            page: components["schemas"]["CursorPage"];
        };
        PodLogs: {
            /** Format: uuid */
            cluster_id: string;
            namespace: string;
            pod: string;
            container?: string;
            content: string;
            truncated: boolean;
            bytes: number;
        };
        ConnectionTest: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** @enum {string} */
            target_type: "host" | "kubernetes_cluster";
            /** Format: uuid */
            resource_id?: string;
            /** @enum {string} */
            path: "connector" | "direct" | "in_cluster";
            /** @enum {string} */
            status: "queued" | "running" | "succeeded" | "failed" | "result_unknown" | "expired";
            checks: {
                name: string;
                /** @enum {string} */
                status: "passed" | "failed" | "skipped";
                detail?: string;
            }[];
            latency_ms?: number;
            resolved_ips?: string[];
            host_key_fingerprint?: string;
            remote_version?: string;
            error_code?: string;
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
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
        /** @enum {string} */
        KubernetesConnectionMode: "via_bastion" | "direct" | "in_cluster";
        /** @enum {string} */
        Environment: "development" | "staging" | "production";
        UserLabelKey: string;
        SystemLabelKey: string;
        LabelValue: string;
        Labels: {
            [key: string]: components["schemas"]["LabelValue"];
        };
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
        UserLabels: {
            [key: string]: components["schemas"]["LabelValue"];
        };
        PublicJsonValue: unknown;
        PublicJsonObject: unknown;
        /** PendingActionPublic */
        "pending-action-public.schema": {
            /** @constant */
            schema_version: "argus.pending_action/v1";
            action_ref: string;
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
        ResourcePreviewDelete: {
            /** Format: int64 */
            expected_version: number;
        };
        KubernetesConnectionTestCreate: {
            /** Format: uri */
            api_server: string;
            /** @enum {string} */
            connection_mode: "via_bastion" | "direct";
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            credential_id?: string;
        };
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
        ResourceId: string;
        IdempotencyKey: components["schemas"]["IdempotencyKey"];
        CsrfToken: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listKubernetesClusters: {
        parameters: {
            query?: {
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Cluster page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["KubernetesClusterPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getKubernetesCluster: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Kubernetes cluster. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["KubernetesCluster"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    previewCreateKubernetesCluster: {
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
                "application/json": components["schemas"]["KubernetesPreviewCreate"];
            };
        };
        responses: {
            /** @description Pending Action. */
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
    previewUpdateKubernetesCluster: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["KubernetesPreviewUpdate"];
            };
        };
        responses: {
            /** @description Pending Action. */
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
    previewDeleteKubernetesCluster: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ResourcePreviewDelete"];
            };
        };
        responses: {
            /** @description Pending Action. */
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
    createKubernetesConnectionTest: {
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
                "application/json": components["schemas"]["KubernetesConnectionTestCreate"];
            };
        };
        responses: {
            /** @description Connection Test. */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectionTest"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listKubernetesResources: {
        parameters: {
            query: {
                resource_type: "namespace" | "node" | "pod" | "deployment" | "statefulset" | "daemonset" | "service";
                namespace?: string;
                query?: string;
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Kubernetes resource page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["KubernetesResourcePage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getKubernetesPodLogs: {
        parameters: {
            query: {
                namespace: string;
                pod: string;
                container?: string;
                tail_lines?: number;
            };
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Bounded Pod logs. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PodLogs"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
