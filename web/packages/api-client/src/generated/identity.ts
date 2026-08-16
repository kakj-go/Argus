export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        Enterprise: {
            id: string;
            name: string;
            /** @enum {string} */
            status: "active" | "suspended" | "disabled";
            /** @enum {string} */
            default_locale: "zh-CN" | "en-US";
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
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
            /** Format: date-time */
            last_login_at?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
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
        ServiceAccount: {
            id: string;
            enterprise_id: string;
            name: string;
            description?: string;
            /** @enum {string} */
            status: "active" | "disabled";
            allowed_tool_ids?: string[];
            data_scope_ids?: string[];
            authorization_version: components["schemas"]["AuthorizationVersion"];
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ApiKey: {
            id: string;
            enterprise_id: string;
            service_account_id: string;
            name: string;
            prefix: string;
            /** @enum {string} */
            status: "active" | "revoked" | "expired";
            /** Format: date-time */
            expires_at?: string;
            /** Format: date-time */
            last_used_at?: string;
            /** Format: date-time */
            created_at: string;
        };
        /** Format: int64 */
        AuthorizationVersion: number;
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
