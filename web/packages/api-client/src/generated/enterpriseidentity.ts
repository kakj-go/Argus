export interface paths {
    "/enterprise/users": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listEnterpriseUsers. */
        get: operations["listEnterpriseUsers"];
        put?: never;
        /** createEnterpriseUser. */
        post: operations["createEnterpriseUser"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/users/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** getEnterpriseUser. */
        get: operations["getEnterpriseUser"];
        /** updateEnterpriseUser. */
        put: operations["updateEnterpriseUser"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/departments": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listDepartments. */
        get: operations["listDepartments"];
        put?: never;
        /** createDepartment. */
        post: operations["createDepartment"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/departments/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        /** updateDepartment. */
        put: operations["updateDepartment"];
        post?: never;
        /** disableDepartment. */
        delete: operations["disableDepartment"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        EnterpriseUserCreate: {
            username: string;
            display_name: string;
            /** Format: email */
            email?: string;
            /** Format: uuid */
            department_id: string;
            role_ids?: string[];
        };
        EnterpriseUserUpdate: {
            display_name?: string;
            /** Format: email */
            email?: string | null;
            /** Format: uuid */
            department_id?: string;
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
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
        DepartmentCreate: {
            name: string;
            description?: string;
        };
        DepartmentUpdate: {
            name?: string;
            description?: string;
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        DepartmentPage: {
            items: components["schemas"]["Department"][];
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
        Department: {
            id: string;
            enterprise_id: string;
            name: string;
            description?: string;
            is_default: boolean;
            /** @enum {string} */
            status: "active" | "disabled";
            /** Format: int64 */
            version: number;
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
    listEnterpriseUsers: {
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
            /** @description Enterprise users. */
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
    createEnterpriseUser: {
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
                "application/json": components["schemas"]["EnterpriseUserCreate"];
            };
        };
        responses: {
            /** @description User and one-time temporary password. */
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
    getEnterpriseUser: {
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
            /** @description Enterprise user. */
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
    updateEnterpriseUser: {
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
                "application/json": components["schemas"]["EnterpriseUserUpdate"];
            };
        };
        responses: {
            /** @description Updated user. */
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
    listDepartments: {
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
            /** @description Departments. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DepartmentPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createDepartment: {
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
                "application/json": components["schemas"]["DepartmentCreate"];
            };
        };
        responses: {
            /** @description Department. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Department"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateDepartment: {
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
                "application/json": components["schemas"]["DepartmentUpdate"];
            };
        };
        responses: {
            /** @description Updated department. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Department"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableDepartment: {
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
            /** @description Department disabled. */
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
