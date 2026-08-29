export interface paths {
    "/enterprise/service-accounts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listServiceAccounts. */
        get: operations["listServiceAccounts"];
        put?: never;
        /** createServiceAccount. */
        post: operations["createServiceAccount"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/service-accounts/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        /** updateServiceAccount. */
        put: operations["updateServiceAccount"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/service-accounts/{service_account_id}/api-keys": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                service_account_id: string;
            };
            cookie?: never;
        };
        /** listApiKeys. */
        get: operations["listApiKeys"];
        put?: never;
        /** createApiKey. */
        post: operations["createApiKey"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/api-keys/{id}/rotate": {
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
        /** rotateApiKey. */
        post: operations["rotateApiKey"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/api-keys/{id}/revoke": {
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
        /** revokeApiKey. */
        post: operations["revokeApiKey"];
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
        ServiceAccountCreate: {
            name: string;
            description?: string;
            allowed_tool_ids: string[];
        };
        ServiceAccountUpdate: {
            description?: string;
            allowed_tool_ids?: string[];
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        ServiceAccountPage: {
            items: components["schemas"]["ServiceAccount"][];
            page: components["schemas"]["CursorPage"];
        };
        ApiKeyCreate: {
            name: string;
            /** Format: date-time */
            expires_at?: string;
        };
        CreatedApiKeySecret: {
            api_key: components["schemas"]["ApiKey"];
            secret: string;
        };
        ApiKeyPage: {
            items: components["schemas"]["ApiKey"][];
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
        /** Format: int64 */
        AuthorizationVersion: number;
        ServiceAccount: {
            id: string;
            enterprise_id: string;
            name: string;
            description?: string;
            /** @enum {string} */
            status: "active" | "disabled";
            allowed_tool_ids?: string[];
            authorization_version: components["schemas"]["AuthorizationVersion"];
            /** Format: int64 */
            version: number;
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
        ApiKey: {
            id: string;
            enterprise_id: string;
            service_account_id: string;
            name: string;
            prefix: string;
            /** @enum {string} */
            status: "active" | "revoked" | "expired";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            expires_at?: string;
            /** Format: date-time */
            last_used_at?: string;
            /** Format: date-time */
            created_at: string;
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
        ResourceId: string;
        ExpectedVersion: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listServiceAccounts: {
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
            /** @description Service accounts. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ServiceAccountPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createServiceAccount: {
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
                "application/json": components["schemas"]["ServiceAccountCreate"];
            };
        };
        responses: {
            /** @description Service account. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ServiceAccount"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateServiceAccount: {
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
        requestBody: {
            content: {
                "application/json": components["schemas"]["ServiceAccountUpdate"];
            };
        };
        responses: {
            /** @description Updated service account. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ServiceAccount"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listApiKeys: {
        parameters: {
            query?: {
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                service_account_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description API keys. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApiKeyPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createApiKey: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                service_account_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ApiKeyCreate"];
            };
        };
        responses: {
            /** @description API key and one-time secret. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CreatedApiKeySecret"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    rotateApiKey: {
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
            /** @description Rotated API key and one-time secret. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CreatedApiKeySecret"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    revokeApiKey: {
        parameters: {
            query: {
                expected_version: components["parameters"]["ExpectedVersion"];
            };
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description API key revoked. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
}
