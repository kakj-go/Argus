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
    "/enterprise/bastion-scopes/{id}/actions/preview-replacement": {
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
        post: operations["previewReplaceBastionConnector"];
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
            /** Format: uuid */
            connector_host_id?: string;
            /** Format: uuid */
            active_connector_id?: string;
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
            ca_bundle_pem: string;
            gateway_endpoint: string;
            /** Format: date-time */
            certificate_expires_at: string;
            /** @enum {string} */
            result: "registered" | "idempotent_retry";
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
        /** @enum {string} */
        ConnectorRole: "bastion" | "kubernetes";
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
    previewReplaceBastionConnector: {
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
}
