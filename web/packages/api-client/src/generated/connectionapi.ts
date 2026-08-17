export interface paths {
    "/enterprise/connection-tests/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get a connection test result. */
        get: operations["getConnectionTest"];
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
        ResourceId: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getConnectionTest: {
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
            /** @description Connection Test. */
            200: {
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
}
