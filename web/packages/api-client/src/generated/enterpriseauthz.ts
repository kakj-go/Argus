export interface paths {
    "/enterprise/permissions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listPermissions. */
        get: operations["listPermissions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/roles": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listRoles. */
        get: operations["listRoles"];
        put?: never;
        /** createRole. */
        post: operations["createRole"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/roles/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        /** updateRole. */
        put: operations["updateRole"];
        post?: never;
        /** disableRole. */
        delete: operations["disableRole"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/role-bindings": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listRoleBindings. */
        get: operations["listRoleBindings"];
        put?: never;
        /** createRoleBinding. */
        post: operations["createRoleBinding"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/role-bindings/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        /** updateRoleBinding. */
        put: operations["updateRoleBinding"];
        post?: never;
        /** disableRoleBinding. */
        delete: operations["disableRoleBinding"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/data-scopes": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listDataScopes. */
        get: operations["listDataScopes"];
        put?: never;
        /** createDataScope. */
        post: operations["createDataScope"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/data-scopes/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        get?: never;
        /** updateDataScope. */
        put: operations["updateDataScope"];
        post?: never;
        /** disableDataScope. */
        delete: operations["disableDataScope"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        PermissionPage: {
            items: components["schemas"]["Permission"][];
            page: components["schemas"]["CursorPage"];
        };
        RoleCreate: {
            name: string;
            description?: string;
            permissions: components["schemas"]["Permission"][];
        };
        RoleUpdate: {
            name?: string;
            description?: string;
            permissions?: components["schemas"]["Permission"][];
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        RolePage: {
            items: components["schemas"]["Role"][];
            page: components["schemas"]["CursorPage"];
        };
        RoleBindingCreate: {
            /** @enum {string} */
            subject_type: "user" | "department" | "service_account";
            /** Format: uuid */
            subject_id: string;
            /** Format: uuid */
            role_id: string;
            data_scope_ids: string[];
            /** Format: date-time */
            valid_from?: string;
            /** Format: date-time */
            valid_until?: string;
        };
        RoleBindingUpdate: {
            data_scope_ids?: string[];
            /** Format: date-time */
            valid_from: string | null;
            /** Format: date-time */
            valid_until: string | null;
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        RoleBindingPage: {
            items: components["schemas"]["RoleBinding"][];
            page: components["schemas"]["CursorPage"];
        };
        DataScopeCreate: {
            name: string;
            description?: string;
            resource_types: ("host" | "kubernetes_cluster" | "kubernetes_namespace")[];
            explicit_resource_ids: string[];
            label_selector?: components["schemas"]["LabelSelector"];
        };
        DataScopeUpdate: {
            name: string;
            description?: string;
            resource_types: ("host" | "kubernetes_cluster" | "kubernetes_namespace")[];
            explicit_resource_ids: string[];
            label_selector?: components["schemas"]["LabelSelector"];
            /** @enum {string} */
            status?: "active" | "disabled";
            /** Format: int64 */
            expected_version: number;
        };
        DataScopePage: {
            items: components["schemas"]["DataScope"][];
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
        Permission: string;
        PartialMetadata: {
            partial: boolean;
            reasons: ("authorization_filtered" | "budget_truncated" | "source_timeout" | "source_unavailable")[];
        };
        CursorPage: {
            next_cursor: string | null;
            has_more: boolean;
            partial: components["schemas"]["PartialMetadata"];
        };
        Role: {
            id: string;
            enterprise_id: string;
            name: string;
            description?: string;
            builtin: boolean;
            permissions: components["schemas"]["Permission"][];
            /** @enum {string} */
            status: "active" | "disabled";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        IdempotencyKey: string;
        RoleBinding: {
            id: string;
            enterprise_id: string;
            /** @enum {string} */
            subject_type: "user" | "department" | "service_account";
            subject_id: string;
            role_id: string;
            data_scope_ids: string[];
            /** Format: date-time */
            valid_from?: string;
            /** Format: date-time */
            valid_until?: string;
            /** @enum {string} */
            status: "active" | "disabled";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        UserLabelKey: string;
        SystemLabelKey: string;
        LabelValue: string;
        LabelRequirement: {
            key: components["schemas"]["UserLabelKey"] | components["schemas"]["SystemLabelKey"];
            /** @constant */
            operator: "eq";
            values: components["schemas"]["LabelValue"][];
        } | {
            key: components["schemas"]["UserLabelKey"] | components["schemas"]["SystemLabelKey"];
            /** @constant */
            operator: "in";
            values: components["schemas"]["LabelValue"][];
        } | {
            key: components["schemas"]["UserLabelKey"] | components["schemas"]["SystemLabelKey"];
            /** @enum {unknown} */
            operator: "exists" | "not_exists";
        };
        LabelSelector: {
            /** @constant */
            schema_version: "argus.label_selector/v1";
            requirements: components["schemas"]["LabelRequirement"][];
        };
        DataScope: {
            id: string;
            enterprise_id: string;
            name: string;
            description?: string;
            resource_types: ("host" | "kubernetes_cluster" | "kubernetes_namespace")[];
            explicit_resource_ids: string[];
            label_selector?: components["schemas"]["LabelSelector"];
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
    listPermissions: {
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
            /** @description Registered permissions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PermissionPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listRoles: {
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
            /** @description Roles. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RolePage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createRole: {
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
                "application/json": components["schemas"]["RoleCreate"];
            };
        };
        responses: {
            /** @description Role. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Role"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateRole: {
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
                "application/json": components["schemas"]["RoleUpdate"];
            };
        };
        responses: {
            /** @description Updated role. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Role"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableRole: {
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
            /** @description Role disabled. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    listRoleBindings: {
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
            /** @description Role bindings. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RoleBindingPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createRoleBinding: {
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
                "application/json": components["schemas"]["RoleBindingCreate"];
            };
        };
        responses: {
            /** @description Role binding. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RoleBinding"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateRoleBinding: {
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
                "application/json": components["schemas"]["RoleBindingUpdate"];
            };
        };
        responses: {
            /** @description Updated binding. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RoleBinding"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableRoleBinding: {
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
            /** @description Binding disabled. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    listDataScopes: {
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
            /** @description Data scopes. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DataScopePage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createDataScope: {
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
                "application/json": components["schemas"]["DataScopeCreate"];
            };
        };
        responses: {
            /** @description Data scope. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DataScope"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateDataScope: {
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
                "application/json": components["schemas"]["DataScopeUpdate"];
            };
        };
        responses: {
            /** @description Updated data scope. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DataScope"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableDataScope: {
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
            /** @description Data scope disabled. */
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
