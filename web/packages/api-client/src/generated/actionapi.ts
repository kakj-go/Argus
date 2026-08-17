export interface paths {
    "/enterprise/pending-actions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List resource-management Pending Actions. */
        get: operations["listPendingActions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/pending-actions/{action_ref}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                action_ref: string;
            };
            cookie?: never;
        };
        /** Get a public Pending Action. */
        get: operations["getPendingAction"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/pending-actions/{action_ref}/confirm": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                action_ref: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Confirm a Pending Action and create approval or asynchronous execution state. */
        post: operations["confirmPendingAction"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/pending-actions/{action_ref}/cancel": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                action_ref: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Cancel a resource Pending Action. */
        post: operations["cancelPendingAction"];
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
        PendingActionCommandResult: {
            pending_action: components["schemas"]["pending-action-public.schema"];
            approval_request?: components["schemas"]["ApprovalRequestView"];
            execution?: components["schemas"]["Execution"];
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
        ApprovalDecision: {
            decision_id: string;
            actor_user_id: string;
            /** @enum {unknown} */
            decision: "approved" | "rejected";
            reason?: string;
            /** Format: date-time */
            decided_at: string;
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
        Execution: {
            execution_id: string;
            action_ref: string;
            /** @enum {unknown} */
            status: "pending" | "running" | "succeeded" | "failed" | "result_unknown" | "cancelled";
            result_ref?: string;
            readonly one_time_result_available?: boolean;
            error_code?: string;
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
    listPendingActions: {
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
            /** @description Pending Action page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        items: components["schemas"]["pending-action-public.schema"][];
                        page: components["schemas"]["CursorPage"];
                    };
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getPendingAction: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                action_ref: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Pending Action. */
            200: {
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
    confirmPendingAction: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                action_ref: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Confirmation state. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PendingActionCommandResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    cancelPendingAction: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                action_ref: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Cancelled Pending Action. */
            200: {
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
