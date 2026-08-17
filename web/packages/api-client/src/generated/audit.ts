export interface paths {
    "/{audience}/audit-events": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        /** listAuditEvents. */
        get: operations["listAuditEvents"];
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
        AuditEvent: {
            /** Format: uuid */
            id: string;
            /** @enum {string} */
            domain: "platform" | "enterprise";
            /** Format: uuid */
            enterprise_id?: string;
            /** @enum {string} */
            actor_type: "platform_user" | "enterprise_user" | "service_account" | "connector" | "direct_executor" | "system";
            actor_id: string;
            action: string;
            resource_type?: string;
            resource_id?: string;
            /** @enum {string} */
            result: "success" | "failure" | "denied";
            details: {
                [key: string]: unknown;
            };
            previous_hash: string;
            event_hash: string;
            /** Format: date-time */
            created_at: string;
        };
        AuditEventPage: {
            items: components["schemas"]["AuditEvent"][];
            page: components["schemas"]["CursorPage"];
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
        PartialMetadata: {
            partial: boolean;
            reasons: ("authorization_filtered" | "budget_truncated" | "source_timeout" | "source_unavailable")[];
        };
        CursorPage: {
            next_cursor: string | null;
            has_more: boolean;
            partial: components["schemas"]["PartialMetadata"];
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
        Audience: "platform" | "enterprise";
        Cursor: string;
        Limit: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listAuditEvents: {
        parameters: {
            query?: {
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
                action?: string;
            };
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Audit events for the authenticated domain. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AuditEventPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
