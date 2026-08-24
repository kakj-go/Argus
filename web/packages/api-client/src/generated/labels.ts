export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        Labels: {
            [key: string]: components["schemas"]["LabelValue"];
        };
        UserLabels: {
            [key: string]: components["schemas"]["LabelValue"];
        };
        LabelSet: {
            labels: components["schemas"]["Labels"];
            labels_version: number;
        };
        UserLabelUpdate: {
            labels: components["schemas"]["UserLabels"];
            expected_labels_version: number;
        };
        LabelSelector: {
            /** @constant */
            schema_version: "argus.label_selector/v1";
            requirements: components["schemas"]["LabelRequirement"][];
        };
        UserLabelKey: string;
        LabelValue: string;
        SystemLabelKey: string;
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
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
