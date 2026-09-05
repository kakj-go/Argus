export interface paths {
    "/enterprise/bastion-scopes": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List Bastion Scopes. */
        get: operations["listBastionScopes"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/bastion-scopes/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get a Bastion Scope. */
        get: operations["getBastionScope"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/bastion-scopes/actions/preview-create": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Freeze Bastion Scope creation and enrollment policy. */
        post: operations["previewCreateBastionScope"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/bastion-scopes/{id}/actions/preview-update": {
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
        /** Freeze a Bastion Scope update or migration. */
        post: operations["previewUpdateBastionScope"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/bastion-scopes/{id}/actions/preview-delete": {
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
        /** Freeze a Bastion Scope deletion. */
        post: operations["previewDeleteBastionScope"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/bastion-scopes/{id}/actions/preview-enrollment-rotate": {
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
        /** Freeze a new mode A Connector installation command. */
        post: operations["previewBastionEnrollmentRotate"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/bastion-scopes/{id}/actions/preview-connector-replacement": {
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
        /** Fence the active Connector through a confirmed replacement action. */
        post: operations["previewBastionConnectorReplacement"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/connector-install-operations/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get the durable Connector installation progress and public event timeline. */
        get: operations["getConnectorInstallOperation"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/connector-install-operations/{id}/actions/preview-retry": {
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
        /** Freeze a retry of a terminal failed Connector installation operation. */
        post: operations["previewRetryConnectorInstallOperation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/connectors": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List Connector diagnostics. */
        get: operations["listConnectors"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/connectors/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get Connector diagnostics. */
        get: operations["getConnector"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/connectors/{id}/rotate-certificate": {
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
        /** Request short-lived Connector certificate rotation. */
        post: operations["requestConnectorCertificateRotation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/connectors/{id}/actions/preview-uninstall": {
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
        /** Freeze a Connector uninstall command and its fencing context. */
        post: operations["previewUninstallConnector"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/connectors/enroll": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Consume a one-time enrollment token and sign a Connector CSR. */
        post: operations["enrollConnector"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/connectors/bootstrap-script": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Download a strict Connector bootstrap script using a pending enrollment token.
         * @description 公开目标机端点；短命令可仅对本次脚本下载放宽 TLS，返回脚本内嵌版本化 Trust Bundle，后续请求保持严格校验。读取脚本不消费 enrollment token。
         */
        get: operations["getConnectorBootstrapScript"];
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
        BastionScope: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            environment: components["schemas"]["Environment"];
            labels: components["schemas"]["Labels"];
            /** @enum {string} */
            status: "pending" | "active" | "suspected_offline" | "offline" | "uninstalling" | "uninstalled" | "deleted";
            /** @enum {string} */
            readonly onboarding_mode: "command" | "direct_install" | "direct_install_tunnel";
            onboarding: components["schemas"]["OnboardingProjection"];
            /** Format: uuid */
            connector_host_id?: string;
            /** Format: uuid */
            active_connector_id?: string;
            /**
             * @description 模式 C 长期控制隧道的权威状态；与 Connector 在线状态独立
             * @enum {string}
             */
            readonly control_tunnel_status?: "desired" | "establishing" | "established" | "degraded" | "down" | "removed";
            /** Format: int64 */
            fencing_generation: number;
            member_count: number;
            /** Format: int64 */
            resource_version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        BastionScopePage: {
            items: components["schemas"]["BastionScope"][];
            page: components["schemas"]["CursorPage"];
        };
        BastionPreviewCreate: {
            name: string;
            environment: components["schemas"]["Environment"];
            labels: components["schemas"]["UserLabels"];
            /** @enum {string} */
            install_mode: "command" | "direct_install" | "direct_install_tunnel";
            address?: string;
            port?: number;
            username?: string;
            /** Format: uuid */
            credential_id?: string;
            /**
             * Format: uuid
             * @description 代装模式必填:与本表单字段匹配且成功的 direct_ssh 主机连接测试
             */
            connection_test_id?: string;
        };
        ConnectorInstallOperation: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            connector_id: string;
            /** Format: uuid */
            bastion_scope_id: string;
            /** Format: uuid */
            host_id: string;
            /** Format: uuid */
            retry_of?: string;
            /** Format: uuid */
            connection_test_id?: string;
            /** Format: uuid */
            release_version_id?: string;
            /** @enum {string} */
            install_mode: "direct_install" | "direct_install_tunnel";
            /** @enum {string} */
            stage: "queued" | "ssh_connecting" | "artifact_verifying" | "artifact_transferring" | "service_installing" | "control_tunnel_establishing" | "enrolling" | "waiting_connector_online" | "completed";
            /** @enum {string} */
            status: "queued" | "running" | "succeeded" | "failed" | "result_unknown" | "expired" | "cancelled";
            attempt: number;
            max_attempts: number;
            /** Format: date-time */
            connector_online_at?: string;
            /** @enum {string} */
            control_tunnel_status?: "desired" | "establishing" | "established" | "degraded" | "down" | "removed";
            error_code?: string;
            events: components["schemas"]["ConnectorInstallOperationEvent"][];
            /** Format: date-time */
            started_at?: string;
            /** Format: date-time */
            completed_at?: string;
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        Connector: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            role: components["schemas"]["ConnectorRole"];
            name: string;
            /** Format: uuid */
            host_id?: string;
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            kubernetes_cluster_id?: string;
            software_version?: string;
            /** @enum {string} */
            status: "pending" | "online" | "suspected_offline" | "offline" | "draining" | "uninstalled" | "revoked";
            capabilities: string[];
            /** Format: int64 */
            connection_epoch: number;
            /** Format: date-time */
            certificate_expires_at: string;
            /** Format: date-time */
            connected_at?: string;
            /** Format: date-time */
            last_heartbeat_at?: string;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ConnectorPage: {
            items: components["schemas"]["Connector"][];
            page: components["schemas"]["CursorPage"];
        };
        ConnectorEnrollRequest: {
            csr_pem: string;
            device_fingerprint: string;
            instance_id: string;
            /** @enum {string} */
            architecture: "amd64" | "arm64";
            name: string;
            software_version: string;
            capabilities: string[];
        };
        ConnectorEnrollResult: {
            /** Format: uuid */
            connector_id: string;
            role: components["schemas"]["ConnectorRole"];
            /** Format: uuid */
            host_id?: string;
            /** Format: uuid */
            bastion_scope_id?: string;
            /** Format: uuid */
            kubernetes_cluster_id?: string;
            certificate_pem: string;
            trust_bundle: components["schemas"]["TrustBundleSnapshot"];
            gateway_endpoint: string;
            /** Format: date-time */
            certificate_expires_at: string;
            /** @enum {string} */
            result: "registered" | "idempotent_retry";
        };
        InstallInstructionSet: {
            /** @enum {string} */
            scope: "linux-system" | "linux-user" | "kubernetes";
            /** @description 唯一面向用户展示的一键安装命令。Host 与手工 Connector 下载动态引导脚本；Kubernetes 使用等价的单命令临时脚本执行。 */
            command: string;
            /** @enum {string} */
            download_tls_mode?: "strict" | "insecure-first-fetch";
            /** Format: date-time */
            expires_at: string;
            /** Format: int64 */
            trust_bundle_epoch: number;
            trust_bundle_sha256: string;
            installer_sha256: string;
            capability_warnings: string[];
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
        ResourcePreviewUpdate: {
            name?: string;
            environment?: components["schemas"]["Environment"];
            labels?: components["schemas"]["UserLabels"];
            /** Format: uuid */
            connection_test_id?: string;
            /** Format: int64 */
            expected_version: number;
        };
        ResourcePreviewDelete: {
            /** Format: int64 */
            expected_version: number;
        };
        BastionConnectorReplacementPreview: {
            /** Format: int64 */
            expected_version: number;
            /** @description 模式 B/C 必填；必须与本次连接测试一致 */
            address?: string;
            port?: number;
            username?: string;
            /** Format: uuid */
            credential_id?: string;
            /**
             * Format: uuid
             * @description 模式 B/C 必填；必须是当前有效的 direct_ssh 测试
             */
            connection_test_id?: string;
        };
        ConnectorInstallOperationEvent: {
            /** Format: uuid */
            id: string;
            /** @enum {string} */
            stage: "queued" | "ssh_connecting" | "artifact_verifying" | "artifact_transferring" | "service_installing" | "control_tunnel_establishing" | "enrolling" | "waiting_connector_online" | "completed";
            /** @enum {string} */
            status: "started" | "succeeded" | "failed" | "retrying";
            error_code?: string;
            detail?: string;
            /** Format: date-time */
            occurred_at: string;
        };
        /** @enum {string} */
        ConnectorRole: "bastion" | "kubernetes";
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
        ExpectedVersion: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listBastionScopes: {
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
            /** @description Bastion Scope page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BastionScopePage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getBastionScope: {
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
            /** @description Bastion Scope. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BastionScope"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    previewCreateBastionScope: {
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
                "application/json": components["schemas"]["BastionPreviewCreate"];
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
    previewUpdateBastionScope: {
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
                "application/json": components["schemas"]["ResourcePreviewUpdate"];
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
    previewDeleteBastionScope: {
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
    previewBastionEnrollmentRotate: {
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
    previewBastionConnectorReplacement: {
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
                "application/json": components["schemas"]["BastionConnectorReplacementPreview"];
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
    getConnectorInstallOperation: {
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
            /** @description Connector installation operation. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectorInstallOperation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    previewRetryConnectorInstallOperation: {
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
        requestBody?: never;
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
    listConnectors: {
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
            /** @description Connector page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectorPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getConnector: {
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
            /** @description Connector. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Connector"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    requestConnectorCertificateRotation: {
        parameters: {
            query: {
                expected_version: components["parameters"]["ExpectedVersion"];
            };
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Connector rotation requested. */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Connector"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    previewUninstallConnector: {
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
    enrollConnector: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConnectorEnrollRequest"];
            };
        };
        responses: {
            /** @description Connector identity and certificate. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectorEnrollResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getConnectorBootstrapScript: {
        parameters: {
            query: {
                scope: "linux-system" | "linux-user";
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Generated strict bootstrap script. */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    "Content-Disposition"?: string;
                    "X-Content-Type-Options"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "text/x-shellscript": string;
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
