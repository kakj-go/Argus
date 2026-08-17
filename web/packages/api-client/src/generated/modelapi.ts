export interface paths {
    "/enterprise/ai-models": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listAIModels. */
        get: operations["listAIModels"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/ai-models/test-and-create": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** testAndCreateAIModel. */
        post: operations["testAndCreateAIModel"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/ai-models/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** getAIModel. */
        get: operations["getAIModel"];
        /** updateAIModel. */
        put: operations["updateAIModel"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/ai-models/{id}/test": {
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
        /** testAIModel. */
        post: operations["testAIModel"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/model-quotas": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listModelQuotas. */
        get: operations["listModelQuotas"];
        put?: never;
        /** upsertModelQuota. */
        post: operations["upsertModelQuota"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/model-usage": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listModelUsage. */
        get: operations["listModelUsage"];
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
        /** @enum {string} */
        AIModelProtocol: "chat_completions" | "responses";
        AIModel: {
            /** Format: uuid */
            id: string;
            name: string;
            /** Format: uri */
            base_url: string;
            model_id: string;
            api_protocol: components["schemas"]["AIModelProtocol"];
            context_window_tokens: number;
            max_output_tokens: number;
            input_price_per_million: number;
            output_price_per_million: number;
            capabilities?: components["schemas"]["ModelCapabilities"];
            /** @enum {string} */
            status: "enabled" | "disabled";
            /** @enum {string} */
            health_status: "unknown" | "healthy" | "unhealthy";
            revision: number;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            last_tested_at?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        AIModelTestCreate: {
            name: string;
            /** Format: uri */
            base_url: string;
            model_id: string;
            api_protocol: components["schemas"]["AIModelProtocol"];
            api_key: string;
            context_window_tokens: number;
            max_output_tokens: number;
            input_price_per_million: number;
            output_price_per_million: number;
        };
        AIModelUpdate: {
            name?: string;
            /** Format: uri */
            base_url?: string;
            model_id?: string;
            api_protocol?: components["schemas"]["AIModelProtocol"];
            api_key?: string;
            context_window_tokens?: number;
            max_output_tokens?: number;
            input_price_per_million?: number;
            output_price_per_million?: number;
            /** @enum {string} */
            status?: "enabled" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        AIModelTestResult: {
            compatible: boolean;
            model?: components["schemas"]["AIModel"];
            checks: {
                /** @enum {string} */
                name: "basic" | "streaming" | "tool_calling" | "structured_output";
                /** @enum {string} */
                status: "passed" | "failed";
                error_code?: string;
            }[];
        };
        AIModelPage: {
            items: components["schemas"]["AIModel"][];
            page: components["schemas"]["CursorPage"];
        };
        ModelQuota: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            model_id: string;
            /** @enum {string} */
            subject_type: "department" | "user";
            /** Format: uuid */
            subject_id: string;
            monthly_amount: number;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ModelQuotaUpsert: {
            /** Format: uuid */
            model_id: string;
            /** @enum {string} */
            subject_type: "department" | "user";
            /** Format: uuid */
            subject_id: string;
            monthly_amount: number;
            /** Format: int64 */
            expected_version?: number;
        };
        ModelUsage: {
            /** Format: uuid */
            model_id: string;
            month: string;
            /** Format: int64 */
            input_tokens: number;
            /** Format: int64 */
            output_tokens: number;
            amount: number;
            /** Format: int64 */
            request_count: number;
            /** Format: int64 */
            compaction_count: number;
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
        ModelCapabilities: {
            context_window_tokens: number;
            max_output_tokens: number;
            supports_tool_calling: boolean;
            supports_structured_output: boolean;
            /** @enum {unknown} */
            provider_compaction_capability: "none" | "optional";
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
    listAIModels: {
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
            /** @description AI Model page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AIModelPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    testAndCreateAIModel: {
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
                "application/json": components["schemas"]["AIModelTestCreate"];
            };
        };
        responses: {
            /** @description Compatibility result and model. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AIModelTestResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getAIModel: {
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
            /** @description AI Model. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AIModel"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateAIModel: {
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
                "application/json": components["schemas"]["AIModelUpdate"];
            };
        };
        responses: {
            /** @description Updated AI Model. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AIModel"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    testAIModel: {
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
            /** @description Compatibility result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AIModelTestResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listModelQuotas: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Model quotas. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelQuota"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    upsertModelQuota: {
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
                "application/json": components["schemas"]["ModelQuotaUpsert"];
            };
        };
        responses: {
            /** @description Model quota. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelQuota"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listModelUsage: {
        parameters: {
            query?: {
                month?: string;
                model_id?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Model usage. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelUsage"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
