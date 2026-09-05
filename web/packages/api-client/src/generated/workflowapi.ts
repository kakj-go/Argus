export interface paths {
    "/enterprise/approval-policies": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listApprovalPolicies. */
        get: operations["listApprovalPolicies"];
        put?: never;
        /** createApprovalPolicy. */
        post: operations["createApprovalPolicy"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-policies/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        /** updateApprovalPolicy. */
        put: operations["updateApprovalPolicy"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-requests": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listApprovalRequests. */
        get: operations["listApprovalRequests"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-requests/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** getApprovalRequest. */
        get: operations["getApprovalRequest"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-requests/{id}/decisions": {
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
        /** decideApprovalRequest. */
        post: operations["decideApprovalRequest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/executions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listExecutions. */
        get: operations["listExecutions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/executions/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** getExecution. */
        get: operations["getExecution"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/executions/{id}/one-time-result": {
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
        /** Claim an encrypted short-lived action result exactly once. */
        post: operations["claimExecutionOneTimeResult"];
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
        ApprovalPolicy: {
            /** Format: uuid */
            id: string;
            name: string;
            enabled: boolean;
            tool_ids: string[];
            risks: ("write" | "dangerous" | "critical")[];
            resource_types: string[];
            minimum_approvers: number;
            separation_of_duty: boolean;
            approver_role_ids: string[];
            /** @default 86400 */
            expires_after_seconds: number;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ApprovalPolicyWrite: {
            name: string;
            enabled: boolean;
            tool_ids: string[];
            risks: ("write" | "dangerous" | "critical")[];
            resource_types: string[];
            minimum_approvers: number;
            separation_of_duty: boolean;
            approver_role_ids: string[];
            /** @default 86400 */
            expires_after_seconds: number;
            /** Format: int64 */
            expected_version: number;
        };
        ApprovalRequirement: {
            /** Format: uuid */
            policy_id: string;
            /** Format: int64 */
            policy_version: number;
            minimum_approvers: number;
            separation_of_duty: boolean;
            approved_count: number;
            /** @enum {string} */
            status: "pending" | "approved" | "rejected" | "invalidated";
        };
        ApprovalRequestView: {
            /** Format: uuid */
            approval_request_id: string;
            action_ref: string;
            requirements: components["schemas"]["ApprovalRequirement"][];
            decisions: components["schemas"]["ApprovalDecision"][];
            /** @enum {string} */
            status: "pending" | "approved" | "rejected" | "expired" | "invalidated";
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ApprovalDecisionCreate: {
            /** @enum {string} */
            decision: "approved" | "rejected";
            reason?: string;
        };
        PendingActionCommandResult: {
            pending_action: components["schemas"]["pending-action-public.schema"];
            approval_request?: components["schemas"]["ApprovalRequestView"];
            execution?: components["schemas"]["Execution"];
        };
        ExecutionPage: {
            items: components["schemas"]["Execution"][];
            page: components["schemas"]["CursorPage"];
        };
        ActionOneTimeResult: {
            /** @constant */
            schema_version: "argus.action_one_time_result/v3";
            /** Format: uuid */
            execution_id: string;
            /** @enum {string} */
            result_kind: "host_install_command" | "host_uninstall_command" | "connector_install_command";
            instruction_sets: components["schemas"]["InstallInstructionSet"][];
            /** Format: date-time */
            expires_at: string;
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
        IdempotencyKey: string;
        ApprovalDecision: {
            decision_id: string;
            actor_user_id: string;
            /** @enum {unknown} */
            decision: "approved" | "rejected";
            reason?: string;
            /** Format: date-time */
            decided_at: string;
        };
        Execution: {
            execution_id: string;
            action_ref: string;
            /** @enum {unknown} */
            status: "pending" | "running" | "succeeded" | "failed" | "result_unknown" | "cancelled";
            result_ref?: string;
            /** @enum {unknown} */
            readonly one_time_result_state: "unavailable" | "available" | "consumed" | "expired";
            resource_ref?: {
                resource_type: string;
                resource_id: string;
                version: number;
            };
            operation_ref?: {
                /** @constant */
                kind: "connector_install";
                /** Format: uuid */
                id: string;
            };
            error_code?: string;
            /** Format: date-time */
            created_at: string;
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
        IdempotencyKey: components["schemas"]["IdempotencyKey"];
        CsrfToken: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listApprovalPolicies: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Approval policies. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApprovalPolicy"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createApprovalPolicy: {
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
                "application/json": components["schemas"]["ApprovalPolicyWrite"];
            };
        };
        responses: {
            /** @description Approval policy. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApprovalPolicy"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateApprovalPolicy: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ApprovalPolicyWrite"];
            };
        };
        responses: {
            /** @description Approval policy. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApprovalPolicy"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listApprovalRequests: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Approval requests. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApprovalRequestView"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getApprovalRequest: {
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
            /** @description Approval request. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApprovalRequestView"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    decideApprovalRequest: {
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
                "application/json": components["schemas"]["ApprovalDecisionCreate"];
            };
        };
        responses: {
            /** @description Updated approval request. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApprovalRequestView"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listExecutions: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Execution page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ExecutionPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getExecution: {
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
            /** @description Execution. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Execution"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    claimExecutionOneTimeResult: {
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
        requestBody?: never;
        responses: {
            /** @description One-time action result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ActionOneTimeResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
