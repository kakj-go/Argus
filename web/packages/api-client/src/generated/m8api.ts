export interface paths {
    "/{audience}/account/mfa/totp/enroll": {
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
        /** Start a one-time TOTP enrollment. */
        post: operations["enrollTotp"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/account/mfa/totp/verify": {
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
        /** Verify and enable TOTP. */
        post: operations["verifyTotpEnrollment"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/account/mfa/totp/disable": {
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
        /** Disable TOTP after fresh proof. */
        post: operations["disableTotp"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/account/mfa/recovery-codes/regenerate": {
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
        /** Replace all recovery codes after fresh proof. */
        post: operations["regenerateRecoveryCodes"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/auth/mfa/complete": {
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
        /** Complete an MFA login challenge. */
        post: operations["completeMfaLogin"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/{audience}/auth/step-up": {
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
        /** Bind a fresh MFA proof to the current session. */
        post: operations["stepUpAuthentication"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/break-glass-sessions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List the current user's break-glass sessions. */
        get: operations["listBreakGlassSessions"];
        put?: never;
        /** Create a narrowly-scoped local hardening break-glass session. */
        post: operations["createBreakGlassSession"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/break-glass-sessions/{id}/revoke": {
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
        /** Revoke a break-glass session. */
        post: operations["revokeBreakGlassSession"];
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
        AuthenticationMethod: "password" | "temporary_password" | "totp" | "recovery_code";
        /** @enum {string} */
        MfaState: "disabled" | "enrollment_required" | "enabled";
        MfaChallenge: {
            challenge_id: string;
            /** @enum {string} */
            audience: "platform" | "enterprise";
            /** Format: date-time */
            expires_at: string;
        };
        MfaCompleteRequest: {
            challenge_id: string;
            code: string;
        };
        TotpEnrollment: {
            enrollment_id: string;
            /** @example JBSWY3DPEHPK3PXP */
            secret: string;
            /** Format: uri */
            otpauth_uri: string;
            /** Format: date-time */
            expires_at: string;
        };
        TotpVerifyRequest: {
            enrollment_id: string;
            /** @example 123456 */
            code: string;
        };
        MfaCodeRequest: {
            code: string;
        };
        RecoveryCodesResult: {
            codes: string[];
            /** Format: date-time */
            generated_at: string;
        };
        StepUpSession: {
            /** Format: date-time */
            expires_at: string;
            amr: components["schemas"]["AuthenticationMethod"][];
        };
        BreakGlassSession: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            enterprise_id: string;
            /** Format: uuid */
            user_id: string;
            reason: string;
            ticket_ref: string;
            /** @enum {string} */
            status: "active" | "revoked" | "expired";
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            revoked_at?: string;
        };
        BreakGlassCreate: {
            reason: string;
            ticket_ref: string;
        };
        AuthenticatedSession: {
            session: components["schemas"]["Session"];
            user: components["schemas"]["PlatformUser"] | components["schemas"]["EnterpriseUser"];
            permissions: components["schemas"]["Permission"][];
            csrf_token: string;
            amr: components["schemas"]["AuthenticationMethod"][];
            mfa_state: components["schemas"]["MfaState"];
            /** Format: date-time */
            authenticated_at: string;
            /** Format: date-time */
            step_up_expires_at?: string;
        };
        IdempotencyKey: string;
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
        /** @description Stable API error. */
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
        Audience: "platform" | "enterprise";
        CsrfToken: string;
        IdempotencyKey: components["schemas"]["IdempotencyKey"];
        ResourceId: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    enrollTotp: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path: {
                audience: components["parameters"]["Audience"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One-time enrollment material. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TotpEnrollment"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    verifyTotpEnrollment: {
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
                "application/json": components["schemas"]["TotpVerifyRequest"];
            };
        };
        responses: {
            /** @description Recovery codes returned once. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RecoveryCodesResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableTotp: {
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
                "application/json": components["schemas"]["MfaCodeRequest"];
            };
        };
        responses: {
            /** @description TOTP disabled and other sessions revoked. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    regenerateRecoveryCodes: {
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
                "application/json": components["schemas"]["MfaCodeRequest"];
            };
        };
        responses: {
            /** @description Replacement recovery codes returned once. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RecoveryCodesResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    completeMfaLogin: {
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
                "application/json": components["schemas"]["MfaCompleteRequest"];
            };
        };
        responses: {
            /** @description Authenticated session. */
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
    stepUpAuthentication: {
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
                "application/json": components["schemas"]["MfaCodeRequest"];
            };
        };
        responses: {
            /** @description Step-up assurance. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["StepUpSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listBreakGlassSessions: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Break-glass sessions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BreakGlassSession"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createBreakGlassSession: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["BreakGlassCreate"];
            };
        };
        responses: {
            /** @description Break-glass session. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BreakGlassSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    revokeBreakGlassSession: {
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
        requestBody?: never;
        responses: {
            /** @description Break-glass session revoked. */
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
