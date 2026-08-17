export interface paths {
    "/enterprise/secrets": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List Secret metadata without values. */
        get: operations["listSecrets"];
        put?: never;
        /** Create an encrypted write-only Secret. */
        post: operations["createSecret"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/secrets/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get Secret metadata. */
        get: operations["getSecret"];
        /** Update Secret metadata. */
        put: operations["updateSecret"];
        post?: never;
        /** Disable an unreferenced Secret. */
        delete: operations["deleteSecret"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/secrets/{id}/rotate": {
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
        /** Rotate the write-only value of a Secret. */
        post: operations["rotateSecret"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/credentials": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List Credential metadata. */
        get: operations["listCredentials"];
        put?: never;
        /** Create a typed Credential referencing a Secret. */
        post: operations["createCredential"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/credentials/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        /** Update a Credential. */
        put: operations["updateCredential"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/managed-accounts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List managed accounts. */
        get: operations["listManagedAccounts"];
        put?: never;
        /** Create a managed account. */
        post: operations["createManagedAccount"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/managed-accounts/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        /** Update a managed account. */
        put: operations["updateManagedAccount"];
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
        Secret: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            type: components["schemas"]["SecretType"];
            description?: string;
            /** @enum {string} */
            status: "active" | "disabled";
            current_version: number;
            reference_count: number;
            /** Format: date-time */
            last_accessed_at?: string;
            /** Format: int64 */
            version: number;
            /** Format: uuid */
            created_by: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        SecretCreate: {
            name: string;
            type: components["schemas"]["SecretType"];
            description?: string;
            value: string;
        };
        SecretUpdate: {
            name?: string;
            description?: string;
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        SecretRotate: {
            value: string;
            /** Format: int64 */
            expected_version: number;
        };
        SecretPage: {
            items: components["schemas"]["Secret"][];
            page: components["schemas"]["CursorPage"];
        };
        Credential: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            /** @enum {string} */
            protocol: "ssh" | "winrm" | "kubernetes" | "http";
            username?: string;
            /** Format: uuid */
            secret_id: string;
            /** @enum {string} */
            status: "active" | "disabled";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        CredentialCreate: {
            name: string;
            /** @enum {string} */
            protocol: "ssh" | "winrm" | "kubernetes" | "http";
            username?: string;
            /** Format: uuid */
            secret_id: string;
        };
        CredentialUpdate: {
            name?: string;
            username?: string;
            /** Format: uuid */
            secret_id?: string;
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        CredentialPage: {
            items: components["schemas"]["Credential"][];
            page: components["schemas"]["CursorPage"];
        };
        ManagedAccount: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** Format: uuid */
            host_id: string;
            username: string;
            /** @enum {string} */
            privilege_level: "standard" | "sudo" | "administrator";
            /** Format: uuid */
            credential_id: string;
            allowed_protocols: ("ssh" | "winrm")[];
            /** @enum {string} */
            status: "active" | "disabled";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ManagedAccountCreate: {
            /** Format: uuid */
            host_id: string;
            username: string;
            /** @enum {string} */
            privilege_level: "standard" | "sudo" | "administrator";
            /** Format: uuid */
            credential_id: string;
            allowed_protocols: ("ssh" | "winrm")[];
        };
        ManagedAccountUpdate: {
            username?: string;
            /** @enum {string} */
            privilege_level?: "standard" | "sudo" | "administrator";
            /** Format: uuid */
            credential_id?: string;
            allowed_protocols?: ("ssh" | "winrm")[];
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        ManagedAccountPage: {
            items: components["schemas"]["ManagedAccount"][];
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
        /** @enum {string} */
        SecretType: "ssh_password" | "ssh_private_key" | "winrm_password" | "kubeconfig" | "api_token" | "basic_auth";
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
        ResourceId: string;
        ExpectedVersion: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listSecrets: {
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
            /** @description Secret page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SecretPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createSecret: {
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
                "application/json": components["schemas"]["SecretCreate"];
            };
        };
        responses: {
            /** @description Secret metadata. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Secret"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getSecret: {
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
            /** @description Secret metadata. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Secret"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateSecret: {
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
                "application/json": components["schemas"]["SecretUpdate"];
            };
        };
        responses: {
            /** @description Updated Secret. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Secret"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    deleteSecret: {
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
            /** @description Secret disabled. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    rotateSecret: {
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
                "application/json": components["schemas"]["SecretRotate"];
            };
        };
        responses: {
            /** @description Rotated Secret metadata. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Secret"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listCredentials: {
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
            /** @description Credential page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CredentialPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createCredential: {
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
                "application/json": components["schemas"]["CredentialCreate"];
            };
        };
        responses: {
            /** @description Credential. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Credential"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateCredential: {
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
                "application/json": components["schemas"]["CredentialUpdate"];
            };
        };
        responses: {
            /** @description Credential. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Credential"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listManagedAccounts: {
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
            /** @description Managed account page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ManagedAccountPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createManagedAccount: {
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
                "application/json": components["schemas"]["ManagedAccountCreate"];
            };
        };
        responses: {
            /** @description Managed account. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ManagedAccount"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateManagedAccount: {
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
                "application/json": components["schemas"]["ManagedAccountUpdate"];
            };
        };
        responses: {
            /** @description Managed account. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ManagedAccount"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
