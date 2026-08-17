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
        AuthorizationDecision: {
            /** @enum {string} */
            decision: "ALLOW" | "DENY" | "REQUIRE_MFA" | "REQUIRE_APPROVAL";
            reason_code: string;
            obligations: ("step_up_mfa" | "approval" | "audit_high_priority" | "session_recording")[];
            authorization_version: components["schemas"]["AuthorizationVersion"];
            /** Format: int64 */
            policy_version?: number;
            matched_data_scope_ids: string[];
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
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
