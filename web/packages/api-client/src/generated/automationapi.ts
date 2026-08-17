export interface paths {
    "/enterprise/automations": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listAutomations. */
        get: operations["listAutomations"];
        put?: never;
        /** createAutomation. */
        post: operations["createAutomation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/automations/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** getAutomation. */
        get: operations["getAutomation"];
        /** updateAutomation. */
        put: operations["updateAutomation"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/automations/{id}/{state_action}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
                state_action: "enable" | "disable";
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** changeAutomationState. */
        post: operations["changeAutomationState"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/automations/{id}/runs": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** listAutomationRuns. */
        get: operations["listAutomationRuns"];
        put?: never;
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
        Automation: {
            /** Format: uuid */
            id: string;
            name: string;
            /** Format: uuid */
            service_account_id: string;
            tool_id: string;
            tool_input: components["schemas"]["PublicJsonObject"];
            cron: string;
            timezone: string;
            /** @enum {string} */
            status: "enabled" | "disabled";
            /** Format: date-time */
            next_run_at: string;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        AutomationWrite: {
            name: string;
            /** Format: uuid */
            service_account_id: string;
            tool_id: string;
            tool_input: components["schemas"]["PublicJsonObject"];
            cron: string;
            timezone: string;
            /** Format: int64 */
            expected_version?: number;
        };
        AutomationRun: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            automation_id: string;
            automation_revision: number;
            /** Format: date-time */
            scheduled_for: string;
            /** @enum {string} */
            status: "pending" | "running" | "waiting_approval" | "succeeded" | "failed" | "skipped" | "cancelled";
            pending_action_ref?: string;
            result_ref?: string;
            error_code?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
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
        PublicJsonValue: unknown;
        PublicJsonObject: unknown;
        IdempotencyKey: string;
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
        CsrfToken: string;
        ExpectedVersion: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listAutomations: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Automations. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Automation"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createAutomation: {
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
                "application/json": components["schemas"]["AutomationWrite"];
            };
        };
        responses: {
            /** @description Automation. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Automation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getAutomation: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Automation. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Automation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateAutomation: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["AutomationWrite"];
            };
        };
        responses: {
            /** @description Automation. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Automation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    changeAutomationState: {
        parameters: {
            query: {
                expected_version: components["parameters"]["ExpectedVersion"];
            };
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
                state_action: "enable" | "disable";
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Automation. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Automation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listAutomationRuns: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Automation runs. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AutomationRun"][];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
