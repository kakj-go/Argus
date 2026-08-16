export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        TelemetryQuery: components["schemas"]["query.schema"];
        TrustedTelemetryIdentity: {
            enterprise_id: string;
            resource_id: string;
            collector_id: string;
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
        /** TelemetryQuery */
        "query.schema": {
            /** @constant */
            schema_version: "argus.telemetry_query/v1";
            /** @enum {unknown} */
            signal: "metrics" | "logs" | "traces";
            resource_ids: string[];
            label_selector?: components["schemas"]["LabelSelector"];
            time_range: {
                /** Format: date-time */
                from: string;
                /** Format: date-time */
                to: string;
            };
            filters?: Record<string, never>;
            field_projection: string[];
            budget: {
                max_rows: number;
                max_scan_bytes: number;
                timeout_ms: number;
            };
            $defs: {
                TrustedTelemetryIdentity: {
                    enterprise_id: string;
                    resource_id: string;
                    collector_id: string;
                };
            };
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
