export interface paths {
    "/platform/enterprises": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listEnterprises. */
        get: operations["listEnterprises"];
        put?: never;
        /** createEnterprise. */
        post: operations["createEnterprise"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/enterprises/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** getEnterprise. */
        get: operations["getEnterprise"];
        /** updateEnterprise. */
        put: operations["updateEnterprise"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/enterprises/{id}/{state_action}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
                state_action: "suspend" | "activate" | "disable";
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** changeEnterpriseState. */
        post: operations["changeEnterpriseState"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/enterprise-admins": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listEnterpriseAdmins. */
        get: operations["listEnterpriseAdmins"];
        put?: never;
        /** createEnterpriseAdmin. */
        post: operations["createEnterpriseAdmin"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/enterprise-admins/{id}/reset-password": {
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
        /** resetEnterpriseAdminPassword. */
        post: operations["resetEnterpriseAdminPassword"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/platform/enterprise-admins/{id}/disable": {
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
        /** disableEnterpriseAdmin. */
        post: operations["disableEnterpriseAdmin"];
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
        EnterpriseCreate: {
            name: string;
            code: string;
            timezone: string;
            default_locale?: components["schemas"]["Locale"];
            remark?: string;
        };
        EnterpriseUpdate: {
            name?: string;
            timezone?: string;
            default_locale?: components["schemas"]["Locale"];
            remark?: string;
            /** Format: int64 */
            expected_version: number;
        };
        EnterprisePage: {
            items: components["schemas"]["Enterprise"][];
            page: components["schemas"]["CursorPage"];
        };
        EnterpriseAdminCreate: {
            /** Format: uuid */
            enterprise_id: string;
            username: string;
            display_name: string;
            /** Format: email */
            email?: string;
        };
        CreatedUserCredential: {
            user: components["schemas"]["EnterpriseUser"];
            temporary_password: string;
            /** Format: date-time */
            expires_at: string;
        };
        EnterpriseUserPage: {
            items: components["schemas"]["EnterpriseUser"][];
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
        Enterprise: {
            id: string;
            name: string;
            code: string;
            /** @enum {string} */
            status: "active" | "suspended" | "disabled";
            timezone: string;
            /** @enum {string} */
            default_locale: "zh-CN" | "en-US";
            remark?: string;
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
        /** @enum {string} */
        Locale: "zh-CN" | "en-US";
        /** Format: int64 */
        AuthorizationVersion: number;
        EnterpriseUser: {
            id: string;
            enterprise_id: string;
            department_id: string;
            username: string;
            display_name: string;
            /** Format: email */
            email?: string;
            /** @enum {string} */
            status: "active" | "invited" | "disabled";
            mfa_enabled: boolean;
            authorization_version: components["schemas"]["AuthorizationVersion"];
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            last_login_at?: string;
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
        ResourceId: string;
        ExpectedVersion: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listEnterprises: {
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
            /** @description Enterprise page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EnterprisePage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createEnterprise: {
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
                "application/json": components["schemas"]["EnterpriseCreate"];
            };
        };
        responses: {
            /** @description Enterprise created with default department, roles and empty scope. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Enterprise"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getEnterprise: {
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
            /** @description Enterprise. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Enterprise"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateEnterprise: {
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
                "application/json": components["schemas"]["EnterpriseUpdate"];
            };
        };
        responses: {
            /** @description Updated enterprise. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Enterprise"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    changeEnterpriseState: {
        parameters: {
            query: {
                expected_version: components["parameters"]["ExpectedVersion"];
            };
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
                state_action: "suspend" | "activate" | "disable";
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Enterprise state changed. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Enterprise"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listEnterpriseAdmins: {
        parameters: {
            query?: {
                enterprise_id?: string;
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Enterprise administrators. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EnterpriseUserPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createEnterpriseAdmin: {
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
                "application/json": components["schemas"]["EnterpriseAdminCreate"];
            };
        };
        responses: {
            /** @description Administrator and one-time temporary password. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CreatedUserCredential"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    resetEnterpriseAdminPassword: {
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
        requestBody?: never;
        responses: {
            /** @description New one-time temporary password. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CreatedUserCredential"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableEnterpriseAdmin: {
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
            /** @description Disabled administrator. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["EnterpriseUser"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
