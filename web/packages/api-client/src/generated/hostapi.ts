export interface paths {
    "/enterprise/hosts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List explicitly authorized Hosts. */
        get: operations["listHosts"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get an explicitly authorized Host. */
        get: operations["getHost"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/actions/preview-create": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Freeze a validated Host creation plan. */
        post: operations["previewCreateHost"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/{id}/actions/preview-update": {
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
        /** Freeze a Host metadata, labels, or path update. */
        post: operations["previewUpdateHost"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/{id}/actions/preview-delete": {
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
        /** Freeze a Host logical deletion plan. */
        post: operations["previewDeleteHost"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/connection-tests": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Start a bounded Host connection test. */
        post: operations["createHostConnectionTest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/{id}/actions/preview-enrollment-rotate": {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Freeze a new self-enrolled Host installation command. */
        post: operations["previewHostEnrollmentRotate"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/hosts/{id}/actions/preview-uninstall-command": {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Freeze a self-enrolled Host uninstall command. */
        post: operations["previewHostUninstallCommand"];
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
        Host: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            hostname?: string;
            /** @description self_enrolled 主机在激活前为空,激活后为自报地址 */
            address: string;
            /** @description self_enrolled 主机在激活前为 0 */
            port: number;
            /** @enum {string} */
            platform: "linux" | "windows";
            /**
             * @description 连接测试探测的目标架构(uname -m 归一化),Linux 连接测试必须成功识别架构,Collector 安装按此选择签名产物
             * @enum {string}
             */
            readonly architecture?: "amd64" | "arm64";
            connection_mode: components["schemas"]["HostConnectionMode"];
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            connector_id?: string;
            environment: components["schemas"]["Environment"];
            labels: components["schemas"]["Labels"];
            /** Format: int64 */
            labels_version: number;
            /** Format: int64 */
            resource_version: number;
            /** @enum {string} */
            connection_status: "online" | "offline" | "onboarding" | "degraded" | "unknown";
            /**
             * @description 周期探活的实时状态(直连主机);key_changed 表示 SSH 主机键与 pin 值不一致
             * @enum {string}
             */
            readonly live_status?: "online" | "offline" | "key_changed";
            /**
             * Format: date-time
             * @description 最近一次探活时间
             */
            readonly last_probe_at?: string;
            /** @description 最近一次探活往返时延 */
            readonly probe_latency_ms?: number;
            pinned_host_key?: string;
            /** Format: date-time */
            last_seen_at?: string;
            /** @enum {string} */
            status: "active" | "disabled" | "deleted";
            onboarding: components["schemas"]["OnboardingProjection"];
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        HostPage: {
            items: components["schemas"]["Host"][];
            page: components["schemas"]["CursorPage"];
        };
        HostPreviewCreate: {
            name: string;
            hostname?: string;
            /** @description self_enrolled 模式不填写;其余模式必填(服务端按模式校验) */
            address?: string;
            /** @description self_enrolled 模式不填写;其余模式必填(服务端按模式校验) */
            port?: number;
            /**
             * @description self_enrolled 第一版仅支持 linux
             * @enum {string}
             */
            platform: "linux" | "windows";
            /** @enum {string} */
            connection_mode: "via_bastion" | "direct_ssh" | "direct_winrm" | "self_enrolled";
            /** Format: uuid */
            bastion_scope_id?: string;
            /**
             * Format: uuid
             * @description self_enrolled 模式不提供
             */
            credential_id?: string;
            /** @description self_enrolled 模式不提供 */
            username?: string;
            /**
             * @description self_enrolled 模式由用户按目标机器选择;其余模式由连接测试探测,不接受填写
             * @enum {string}
             */
            architecture?: "amd64" | "arm64";
            environment: components["schemas"]["Environment"];
            labels: components["schemas"]["UserLabels"];
            /**
             * Format: uuid
             * @description self_enrolled 模式免测试,不提供;其余模式必填(服务端按模式校验)
             */
            connection_test_id?: string;
        };
        HostPreviewUpdate: {
            name?: string;
            hostname?: string;
            address?: string;
            port?: number;
            /** @enum {string} */
            connection_mode?: "via_bastion" | "direct_ssh" | "direct_winrm";
            /** Format: uuid */
            bastion_scope_id?: string;
            environment?: components["schemas"]["Environment"];
            labels?: components["schemas"]["UserLabels"];
            /** Format: uuid */
            connection_test_id?: string;
            /** Format: int64 */
            expected_version: number;
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
        /** @enum {string} */
        HostConnectionMode: "connector_local" | "via_bastion" | "direct_ssh" | "direct_winrm" | "self_enrolled";
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
        Environment: "development" | "staging" | "production";
        UserLabelKey: string;
        SystemLabelKey: string;
        LabelValue: string;
        Labels: {
            [key: string]: components["schemas"]["LabelValue"];
        };
        OnboardingProjection: {
            /** @enum {string} */
            state: "command_available" | "command_consumed" | "command_expired" | "awaiting_approval" | "installing" | "install_failed" | "registered";
            pending_action_ref?: string;
            /** Format: uuid */
            execution_id?: string;
            /** Format: uuid */
            operation_id?: string;
            error_code?: string;
            /** Format: date-time */
            updated_at: string;
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
        ResourcePreviewDelete: {
            /** Format: int64 */
            expected_version: number;
        };
        HostConnectionTestCreate: {
            address: string;
            port: number;
            /** @enum {string} */
            platform: "linux" | "windows";
            /** @enum {string} */
            connection_mode: "via_bastion" | "direct_ssh" | "direct_winrm";
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            credential_id: string;
            username: string;
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
    listHosts: {
        parameters: {
            query?: {
                query?: string;
                connection_mode?: components["schemas"]["HostConnectionMode"];
                bastion_scope_id?: string;
                labels?: string;
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Host page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HostPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getHost: {
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
            /** @description Host. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Host"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    previewCreateHost: {
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
                "application/json": components["schemas"]["HostPreviewCreate"];
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
    previewUpdateHost: {
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
                "application/json": components["schemas"]["HostPreviewUpdate"];
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
    previewDeleteHost: {
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
    createHostConnectionTest: {
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
                "application/json": components["schemas"]["HostConnectionTestCreate"];
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
    previewHostEnrollmentRotate: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
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
    previewHostUninstallCommand: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
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
            /** @description Dangerous Pending Action. */
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
