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
    "/enterprise/users/{id}/role-assignments": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        /** Get direct, inherited, and effective role assignments for an enterprise user. */
        get: operations["getUserRoleAssignments"];
        /** Atomically replace an enterprise user's direct role assignments. */
        put: operations["replaceUserRoleAssignments"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/data-authorizations/{subject_type}/{subject_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List explicit data authorization resources for a subject. */
        get: operations["listDataAuthorizationResources"];
        put?: never;
        /** Add or remove explicit data authorization resources. */
        post: operations["updateDataAuthorization"];
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
            /** Format: date-time */
            valid_from?: string;
            /** Format: date-time */
            valid_until?: string;
        };
        RoleBindingUpdate: {
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
        UserRoleAssignments: {
            direct_role_ids: string[];
            inherited_roles: components["schemas"]["InheritedRoleAssignment"][];
            effective_role_ids: string[];
            /** Format: int64 */
            authorization_version: number;
        };
        UserRoleAssignmentsUpdate: {
            /** Format: uuid */
            department_id: string;
            role_ids: string[];
            /** Format: int64 */
            expected_user_version: number;
            /** Format: int64 */
            expected_authorization_version: number;
        };
        /** @enum {string} */
        DataAuthorizationSubjectType: "user" | "department" | "role" | "service_account";
        /** @enum {string} */
        DataAuthorizationResourceType: "host" | "kubernetes_cluster";
        DataAuthorizationResource: {
            resource_type: components["schemas"]["DataAuthorizationResourceType"];
            /** Format: uuid */
            resource_id: string;
            name: string;
            direct: boolean;
            inherited: boolean;
            sources: string[];
        };
        DataAuthorizationPage: {
            items: components["schemas"]["DataAuthorizationResource"][];
            page: components["schemas"]["CursorPage"];
            /** Format: int64 */
            authorization_version: number;
            /** Format: int64 */
            affected_member_count: number;
        };
        DataAuthorizationBatch: {
            resource_type: components["schemas"]["DataAuthorizationResourceType"];
            resource_ids: string[];
            remove: boolean;
            /** Format: int64 */
            expected_version: number;
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
            builtin_key?: string;
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
        InheritedRoleAssignment: {
            /** Format: uuid */
            role_id: string;
            /** @constant */
            source_type: "department";
            /** Format: uuid */
            source_id: string;
            source_name: string;
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
        /** @description Stable API error. */
        "responses-Error": {
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
        "parameters-Limit": number;
        "parameters-CsrfToken": string;
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
    getUserRoleAssignments: {
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
            /** @description User role assignments. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["UserRoleAssignments"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    replaceUserRoleAssignments: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path: {
                id: components["parameters"]["ResourceId"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["UserRoleAssignmentsUpdate"];
            };
        };
        responses: {
            /** @description Updated user role assignments. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["UserRoleAssignments"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listDataAuthorizationResources: {
        parameters: {
            query: {
                resource_type: components["schemas"]["DataAuthorizationResourceType"];
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["parameters-Limit"];
            };
            header?: never;
            path: {
                subject_type: components["schemas"]["DataAuthorizationSubjectType"];
                subject_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Resource authorization page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DataAuthorizationPage"];
                };
            };
            default: components["responses"]["responses-Error"];
        };
    };
    updateDataAuthorization: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["parameters-CsrfToken"];
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path: {
                subject_type: components["schemas"]["DataAuthorizationSubjectType"];
                subject_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DataAuthorizationBatch"];
            };
        };
        responses: {
            /** @description Authorization updated. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["responses-Error"];
        };
    };
}
