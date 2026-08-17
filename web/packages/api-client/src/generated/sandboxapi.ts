export interface paths {
    "/platform/sandbox/backends": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listSandboxBackends. */
        get: operations["listSandboxBackends"];
        put?: never;
        /** createSandboxBackend. */
        post: operations["createSandboxBackend"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/backends/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        /** updateSandboxBackend. */
        put: operations["updateSandboxBackend"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/backends/{id}/test": {
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
        /** testSandboxBackend. */
        post: operations["testSandboxBackend"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/images": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listSandboxImages. */
        get: operations["listSandboxImages"];
        put?: never;
        /** createSandboxImage. */
        post: operations["createSandboxImage"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/images/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        /** updateSandboxImage. */
        put: operations["updateSandboxImage"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/profiles": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listSandboxProfiles. */
        get: operations["listSandboxProfiles"];
        put?: never;
        /** createSandboxProfile. */
        post: operations["createSandboxProfile"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/profiles/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        /** updateSandboxProfile. */
        put: operations["updateSandboxProfile"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/enterprise-quotas/{enterprise_id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                enterprise_id: string;
            };
            cookie?: never;
        };
        /** getSandboxQuota. */
        get: operations["getSandboxQuota"];
        /** updateSandboxQuota. */
        put: operations["updateSandboxQuota"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/sessions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listSandboxSessions. */
        get: operations["listSandboxSessions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/sessions/{id}/terminate": {
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
        /** terminateSandboxSession. */
        post: operations["terminateSandboxSession"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/sandbox/usage": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listSandboxUsage. */
        get: operations["listSandboxUsage"];
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
        SandboxBackend: {
            /** Format: uuid */
            id: string;
            name: string;
            /** Format: uri */
            endpoint: string;
            /** @enum {string} */
            status: "enabled" | "disabled";
            /** @enum {string} */
            health_status: "unknown" | "healthy" | "unhealthy";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        SandboxBackendWrite: {
            name: string;
            /** Format: uri */
            endpoint: string;
            api_key?: string;
            /** @enum {string} */
            status: "enabled" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        SandboxImage: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            backend_id: string;
            name: string;
            image_ref: string;
            digest: string;
            /** @enum {string} */
            status: "enabled" | "disabled";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        SandboxImageWrite: {
            /** Format: uuid */
            backend_id: string;
            name: string;
            image_ref: string;
            digest: string;
            /** @enum {string} */
            status: "enabled" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        SandboxProfile: {
            /** Format: uuid */
            id: string;
            name: string;
            /** Format: uuid */
            backend_id: string;
            /** Format: uuid */
            image_id: string;
            task_kinds: ("smoke" | "attachment_processing")[];
            cpu_millis: number;
            memory_mib: number;
            timeout_seconds: number;
            /** @enum {string} */
            network_mode: "none" | "restricted";
            /** @enum {string} */
            status: "enabled" | "disabled";
            revision: number;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        SandboxProfileWrite: {
            name: string;
            /** Format: uuid */
            backend_id: string;
            /** Format: uuid */
            image_id: string;
            task_kinds: ("smoke" | "attachment_processing")[];
            cpu_millis: number;
            memory_mib: number;
            timeout_seconds: number;
            /** @enum {string} */
            network_mode: "none" | "restricted";
            /** @enum {string} */
            status: "enabled" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        SandboxQuota: {
            /** Format: uuid */
            enterprise_id: string;
            max_concurrent_sessions: number;
            /** Format: int64 */
            monthly_session_seconds: number;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        SandboxQuotaWrite: {
            max_concurrent_sessions: number;
            /** Format: int64 */
            monthly_session_seconds: number;
            /** Format: int64 */
            expected_version: number;
        };
        SandboxSession: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            enterprise_id: string;
            /** Format: uuid */
            task_id: string;
            /** Format: uuid */
            profile_id: string;
            profile_revision: number;
            upstream_session_id: string;
            /** @enum {string} */
            status: "creating" | "running" | "terminating" | "terminated" | "failed" | "unknown";
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        SandboxUsage: {
            /** Format: uuid */
            enterprise_id: string;
            month: string;
            /** Format: int64 */
            session_count: number;
            /** Format: int64 */
            session_seconds: number;
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
    listSandboxBackends: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Sandbox backends. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxBackend"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createSandboxBackend: {
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
                "application/json": components["schemas"]["SandboxBackendWrite"];
            };
        };
        responses: {
            /** @description Sandbox backend. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxBackend"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateSandboxBackend: {
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
                "application/json": components["schemas"]["SandboxBackendWrite"];
            };
        };
        responses: {
            /** @description Sandbox backend. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxBackend"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    testSandboxBackend: {
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
            /** @description Tested Sandbox backend. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxBackend"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listSandboxImages: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Sandbox images. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxImage"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createSandboxImage: {
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
                "application/json": components["schemas"]["SandboxImageWrite"];
            };
        };
        responses: {
            /** @description Sandbox image. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxImage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateSandboxImage: {
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
                "application/json": components["schemas"]["SandboxImageWrite"];
            };
        };
        responses: {
            /** @description Sandbox image. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxImage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listSandboxProfiles: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Sandbox profiles. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxProfile"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createSandboxProfile: {
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
                "application/json": components["schemas"]["SandboxProfileWrite"];
            };
        };
        responses: {
            /** @description Sandbox profile. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxProfile"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateSandboxProfile: {
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
                "application/json": components["schemas"]["SandboxProfileWrite"];
            };
        };
        responses: {
            /** @description Sandbox profile. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxProfile"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getSandboxQuota: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                enterprise_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Sandbox quota. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxQuota"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateSandboxQuota: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                enterprise_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SandboxQuotaWrite"];
            };
        };
        responses: {
            /** @description Sandbox quota. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxQuota"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listSandboxSessions: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Sandbox sessions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxSession"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    terminateSandboxSession: {
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
            /** @description Sandbox Session. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listSandboxUsage: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Sandbox usage. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SandboxUsage"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
