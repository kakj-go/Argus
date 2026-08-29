export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        Permission: string;
        /** Format: int64 */
        AuthorizationVersion: number;
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
        AuthorizationDecision: {
            /** @enum {string} */
            decision: "ALLOW" | "DENY" | "REQUIRE_MFA" | "REQUIRE_APPROVAL";
            reason_code: string;
            obligations: ("step_up_mfa" | "approval" | "audit_high_priority" | "session_recording")[];
            authorization_version: components["schemas"]["AuthorizationVersion"];
            /** Format: int64 */
            policy_version?: number;
            matched_resource_ids?: string[];
        };
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
