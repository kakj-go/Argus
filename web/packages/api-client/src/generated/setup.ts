export interface paths {
    "/setup/status": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** getSetupStatus. */
        get: operations["getSetupStatus"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/setup/initialize": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** initializePlatform. */
        post: operations["initializePlatform"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/auth/login": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** login. */
        post: operations["login"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/auth/session": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        /** getAuthenticatedSession. */
        get: operations["getAuthenticatedSession"];
        put?: never;
        post?: never;
        /** logout. */
        delete: operations["logout"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/auth/complete-password-change": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** completePasswordChange. */
        post: operations["completePasswordChange"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/account/password": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        get?: never;
        /** updateOwnPassword. */
        put: operations["updateOwnPassword"];
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
        PlatformState: "uninitialized" | "initializing" | "initialized";
        SetupStatus: {
            state: components["schemas"]["PlatformState"];
            platform_name?: string;
        };
        SetupInitializeRequest: {
            platform_name: string;
            default_locale: components["schemas"]["Locale"];
            timezone: string;
            /** Format: uri */
            external_url: string;
            super_admin: components["schemas"]["SetupSuperAdminInput"];
        };
        SetupInitializeResult: {
            /** @constant */
            state: "initialized";
            /** Format: uuid */
            platform_user_id: string;
        };
        LoginRequest: {
            username: string;
            password: string;
        };
        PasswordChangeChallenge: {
            challenge_id: string;
            /** @enum {string} */
            audience: "platform" | "enterprise";
            /** Format: date-time */
            expires_at: string;
        };
        AuthenticatedSession: {
            session: components["schemas"]["Session"];
            user: components["schemas"]["PlatformUser"] | components["schemas"]["EnterpriseUser"];
            permissions: components["schemas"]["Permission"][];
            csrf_token: string;
        };
        LoginResult: {
            /** @constant */
            status: "authenticated";
            authenticated_session: components["schemas"]["AuthenticatedSession"];
        } | {
            /** @constant */
            status: "password_change_required";
            password_change_challenge: components["schemas"]["PasswordChangeChallenge"];
        };
        CompletePasswordChangeRequest: {
            challenge_id: string;
            temporary_password: string;
            new_password: string;
        };
        PasswordUpdateRequest: {
            current_password: string;
            new_password: string;
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
        IdempotencyKey: string;
        /** @enum {string} */
        Locale: "zh-CN" | "en-US";
        SetupSuperAdminInput: {
            username: string;
            display_name: string;
            /** Format: email */
            email?: string;
            password: string;
        };
        /** Format: int64 */
        AuthorizationVersion: number;
        Session: {
            id: string;
            /** @enum {string} */
            audience: "platform" | "enterprise";
            user_id: string;
            enterprise_id?: string;
            department_id?: string;
            authorization_version?: components["schemas"]["AuthorizationVersion"];
            /** @enum {string} */
            locale: "zh-CN" | "en-US";
            /** @default true */
            csrf_required: boolean;
            /** Format: date-time */
            issued_at: string;
            /** Format: date-time */
            expires_at: string;
        } & ({
            /** @constant */
            audience?: "platform";
        } | {
            /** @constant */
            audience?: "enterprise";
        });
        PlatformUser: {
            id: string;
            username: string;
            display_name: string;
            /** Format: email */
            email?: string;
            /** @constant */
            role: "platform_super_admin";
            /** @enum {string} */
            status: "active" | "disabled";
            mfa_enabled: boolean;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
        };
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
        Permission: string;
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
        Audience: "platform" | "enterprise";
        CsrfToken: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getSetupStatus: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current initialization state. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SetupStatus"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    initializePlatform: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SetupInitializeRequest"];
            };
        };
        responses: {
            /** @description Platform initialized. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SetupInitializeResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    login: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["LoginRequest"];
            };
        };
        responses: {
            /** @description Authenticated session or required password-change challenge. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LoginResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getAuthenticatedSession: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current authenticated session. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AuthenticatedSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    logout: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Session revoked. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    completePasswordChange: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CompletePasswordChangeRequest"];
            };
        };
        responses: {
            /** @description Password changed and session issued. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AuthenticatedSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateOwnPassword: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PasswordUpdateRequest"];
            };
        };
        responses: {
            /** @description Password changed and other sessions revoked. */
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
