export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        PendingActionPublic: components["schemas"]["pending-action-public.schema"];
        ActionBinding: {
            binding_id: string;
            /** @enum {unknown} */
            action: "confirm" | "cancel" | "approve" | "reject";
            /** Format: date-time */
            expires_at: string;
            /** @enum {unknown} */
            status: "pending" | "consumed" | "cancelled" | "expired" | "invalidated";
        };
        ApprovalRequest: {
            approval_request_id: string;
            action_ref: string;
            minimum_approvers: number;
            separation_of_duty: boolean;
            decisions: components["schemas"]["ApprovalDecision"][];
            /** @enum {unknown} */
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
            error_code?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
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
        ApprovalDecision: {
            decision_id: string;
            actor_user_id: string;
            /** @enum {unknown} */
            decision: "approved" | "rejected";
            reason?: string;
            /** Format: date-time */
            decided_at: string;
        };
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
