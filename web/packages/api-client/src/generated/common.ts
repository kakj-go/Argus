export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        /** @enum {string} */
        Locale: "zh-CN" | "en-US";
        RequestId: string;
        IdempotencyKey: string;
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
        CursorPage: {
            next_cursor: string | null;
            has_more: boolean;
            partial: components["schemas"]["PartialMetadata"];
        };
        BatchResult: {
            /** @description True unless an operation explicitly documents best-effort semantics. */
            atomic: boolean;
            partial: components["schemas"]["PartialMetadata"];
            items: components["schemas"]["BatchItemResult"][];
        };
        ResourceRef: {
            /** @enum {string} */
            resource_type: "host" | "kubernetes_cluster" | "connector" | "managed_account" | "telemetry_query" | "artifact";
            resource_id: string;
            /** Format: int64 */
            version: number;
        };
        PartialMetadata: {
            partial: boolean;
            reasons: ("authorization_filtered" | "budget_truncated" | "source_timeout" | "source_unavailable")[];
        };
        BatchItemResult: {
            item_key: string;
            /** @enum {string} */
            status: "succeeded" | "failed" | "skipped";
            resource_ref?: components["schemas"]["ResourceRef"];
            error?: components["schemas"]["ApiError"];
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
