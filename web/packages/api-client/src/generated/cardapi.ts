export interface paths {
    "/enterprise/interactive-cards": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List interactive Cards available to the enterprise. */
        get: operations["listInteractiveCards"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/interactive-cards/{card_id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
            };
            cookie?: never;
        };
        /** Get an interactive Card and its active revision summary. */
        get: operations["getInteractiveCard"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/interactive-cards/{card_id}/versions": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
            };
            cookie?: never;
        };
        /** List immutable versions of an interactive Card. */
        get: operations["listCardVersions"];
        put?: never;
        /** Create a new immutable Card configuration revision. */
        post: operations["createCardConfigurationVersion"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/interactive-cards/{card_id}/versions/{revision}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
                revision: number;
            };
            cookie?: never;
        };
        /** Get one immutable Card version. */
        get: operations["getCardVersion"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/interactive-cards/{card_id}/validation-runs": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Start browser validation for a Card version. */
        post: operations["startCardValidation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/card-validation-runs/{run_id}/evidence": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                run_id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Submit browser validation evidence bound to a Card content hash. */
        post: operations["submitCardValidationEvidence"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/interactive-cards/{card_id}/{state_action}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
                state_action: "activate" | "disable" | "rollback" | "deprecate";
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Activate, disable, roll back, or deprecate an enterprise Card. */
        post: operations["changeInteractiveCardState"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/tool-schema-catalog": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List Card-safe query and preview Tool schemas. */
        get: operations["listToolSchemaCatalog"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/card-instances/{card_instance_id}/presentations": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_instance_id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Reauthorize and materialize a Card presentation for the viewer. */
        post: operations["createCardPresentation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/card-query-bindings/{binding_id}/invoke": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                binding_id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Invoke a short-lived read-only Card query binding. */
        post: operations["invokeCardQueryBinding"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/card-action-bindings/{binding_id}/invoke": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                binding_id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Consume a short-lived Card action binding. */
        post: operations["invokeCardActionBinding"];
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
        InteractiveCard: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            enterprise_id?: string;
            /** @enum {string} */
            source: "system" | "enterprise";
            slug: string;
            name: string;
            description: string;
            lifecycle: components["schemas"]["CardLifecycle"];
            enabled: boolean;
            availability: components["schemas"]["CardAvailability"];
            active_revision?: number;
            latest_revision: number;
            /** Format: int64 */
            version: number;
            /** Format: uuid */
            created_by?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        CardPage: {
            items: components["schemas"]["InteractiveCard"][];
            page: components["schemas"]["CursorPage"];
        };
        CardVersion: {
            /** Format: uuid */
            card_id: string;
            revision: number;
            status: components["schemas"]["CardVersionStatus"];
            content_hash: string;
            manifest_hash: string;
            /** Format: uuid */
            validation_run_id?: string;
            /** Format: uuid */
            created_by?: string;
            /** Format: date-time */
            created_at: string;
            manifest: components["schemas"]["CardManifest"];
            entrypoint_html: string;
            slot_bindings: components["schemas"]["SlotBinding"][];
            demos: components["schemas"]["CardDemo"][];
        };
        CardVersionSummary: {
            /** Format: uuid */
            card_id: string;
            revision: number;
            status: components["schemas"]["CardVersionStatus"];
            content_hash: string;
            manifest_hash: string;
            /** Format: uuid */
            validation_run_id?: string;
            /** Format: uuid */
            created_by?: string;
            /** Format: date-time */
            created_at: string;
        };
        CardVersionPage: {
            items: components["schemas"]["CardVersionSummary"][];
        };
        CardConfigurationVersionCreate: {
            base_revision: number;
            /** Format: int64 */
            expected_version: number;
            name: string;
            description: string;
            slot_bindings: components["schemas"]["SlotBinding"][];
            demos: components["schemas"]["CardDemo"][];
        };
        CardValidationRun: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            card_id: string;
            revision: number;
            content_hash: string;
            runtime_version: string;
            nonce: string;
            /** @enum {string} */
            status: "pending" | "passed" | "failed" | "expired";
            required_scenarios: components["schemas"]["DemoScenario"][];
            passed_scenarios: components["schemas"]["DemoScenario"][];
            issues: components["schemas"]["CardValidationIssue"][];
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
        };
        CardValidationStart: {
            revision: number;
            runtime_version: string;
        };
        CardValidationEvidence: {
            content_hash: string;
            runtime_version: string;
            nonce: string;
            scenarios: {
                scenario: components["schemas"]["DemoScenario"];
                ready: boolean;
                protocol_violations: number;
                runtime_errors: number;
                serious_a11y_violations: number;
                missing_required_slots: string[];
                size_violation: boolean;
            }[];
        };
        CardStateCommand: {
            revision?: number;
            /** Format: int64 */
            expected_version: number;
        };
        ToolSchemaCatalog: {
            items: components["schemas"]["ToolSchemaCatalogEntry"][];
        };
        CardInstance: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            conversation_id: string;
            /** Format: uuid */
            run_id?: string;
            /** Format: uuid */
            card_id: string;
            card_revision: number;
            /** @enum {string} */
            status: "active" | "invalidated";
            /** Format: date-time */
            created_at: string;
        };
        CardPresentationCreate: {
            /** @enum {string} */
            locale: "zh-CN" | "en-US";
            /** @enum {string} */
            color_scheme: "light" | "dark";
        };
        CardPresentation: {
            /** Format: uuid */
            presentation_id: string;
            card_instance: components["schemas"]["CardInstance"];
            manifest: components["schemas"]["CardManifest"];
            render_plan: components["schemas"]["RenderPlan"];
            entrypoint_html: string;
            initial_data: components["schemas"]["PublicJsonObject"];
            partial: boolean;
            locale_fallback: boolean;
            /** Format: date-time */
            expires_at: string;
        };
        CardBindingInvokeResult: {
            /** @enum {string} */
            status: "succeeded" | "awaiting_approval" | "executing" | "failed" | "invalidated";
            data?: components["schemas"]["PublicJsonValue"];
            pending_action?: components["schemas"]["pending-action-public.schema"];
            approval_request?: components["schemas"]["ApprovalRequestView"];
            execution?: components["schemas"]["Execution"];
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
        /** @enum {string} */
        CardLifecycle: "draft" | "active" | "deprecated";
        /** @enum {string} */
        CardAvailability: "available" | "disabled" | "dependency_pending" | "invalidated";
        PartialMetadata: {
            partial: boolean;
            reasons: ("authorization_filtered" | "budget_truncated" | "source_timeout" | "source_unavailable")[];
        };
        CursorPage: {
            next_cursor: string | null;
            has_more: boolean;
            partial: components["schemas"]["PartialMetadata"];
        };
        /** @enum {string} */
        CardVersionStatus: "draft" | "validating" | "validated" | "active" | "retired";
        IdempotencyKey: string;
        /** @enum {unknown} */
        SlotType: "string" | "number" | "boolean" | "array" | "object";
        SlotBinding: {
            slot_name: string;
            /** @enum {unknown} */
            slot_kind: "data" | "query" | "action";
            /** @enum {unknown} */
            mode: "strict" | "preferred";
            tool_id: string;
            output_schema_version: string;
            schema_hash: string;
            path: string;
            value_type: components["schemas"]["SlotType"];
            semantic_type?: string;
        };
        /** @enum {string} */
        DemoScenario: "default" | "empty" | "error" | "large" | "light" | "dark" | "zh-CN" | "en-US";
        PublicJsonValue: unknown;
        PublicJsonObject: unknown;
        CardDemo: {
            scenario: components["schemas"]["DemoScenario"];
            data: components["schemas"]["PublicJsonObject"];
        };
        Slot: {
            name: string;
            /** @enum {unknown} */
            kind: "data" | "query" | "action";
            value_type: components["schemas"]["SlotType"];
            required: boolean;
            /** @default false */
            ai_generated: boolean;
            description?: string;
        };
        CardManifest: {
            /** @constant */
            schema_version: "argus.card_manifest/v1";
            card_id: string;
            revision: number;
            /** @enum {unknown} */
            source: "system" | "enterprise";
            entrypoint_hash: string;
            /** @constant */
            bridge_version: "argus.card_bridge/v1";
            slots: components["schemas"]["Slot"][];
            allowed_resources: ("inline_style" | "inline_script" | "image_data" | "font_data")[];
            supported_locales: ("zh-CN" | "en-US")[];
            /** @enum {unknown} */
            default_locale: "zh-CN" | "en-US";
            supported_color_schemes: ("light" | "dark")[];
            max_message_bytes: number;
            /** @enum {unknown} */
            presentation_kind?: "table" | "detail" | "pending_action" | "metric" | "generic";
        };
        CardValidationIssue: {
            code: string;
            message: string;
            slot_name?: string;
            scenario?: components["schemas"]["DemoScenario"];
        };
        ToolSchemaCatalogEntry: {
            tool_id: string;
            tool_family: string;
            /** @enum {unknown} */
            risk: "read" | "write" | "dangerous" | "critical";
            /** @enum {unknown} */
            execution_mode: "sequential" | "parallel_safe";
            output_schema_version: string;
            compatible_output_versions: string[];
            schema_hash: string;
            output_schema: components["schemas"]["PublicJsonObject"];
            fields: {
                path: string;
                value_type: components["schemas"]["SlotType"];
                semantic_type?: string;
                description?: string;
            }[];
        };
        DataBinding: {
            slot_name: string;
            tool_call_id: string;
            path: string;
            schema_version: string;
        };
        RenderPlan: {
            /** @constant */
            schema_version: "argus.render_plan/v1";
            card_id: string;
            card_revision: number;
            card_instance_id: string;
            data_bindings: components["schemas"]["DataBinding"][];
            query_binding_ids: {
                [key: string]: string;
            };
            action_binding_ids: {
                [key: string]: string;
            };
            /** @enum {unknown} */
            locale: "zh-CN" | "en-US";
            /** @enum {unknown} */
            color_scheme: "light" | "dark";
        };
        /** PendingActionPublic */
        "pending-action-public.schema": {
            /** @constant */
            schema_version: "argus.pending_action/v1";
            action_ref: string;
            action_type: string;
            title: string;
            summary: string;
            /** @enum {unknown} */
            risk: "read" | "write" | "dangerous" | "critical";
            preview: components["schemas"]["PublicJsonObject"];
            diff: {
                /** @enum {unknown} */
                kind: "add" | "remove" | "change" | "note";
                text: string;
            }[];
            /** @enum {unknown} */
            status: "prepared" | "awaiting_confirmation" | "awaiting_approval" | "ready" | "executing" | "succeeded" | "failed" | "result_unknown" | "cancelled" | "expired" | "rejected" | "invalidated";
            available_actions: ("confirm" | "cancel" | "approve" | "reject")[];
            approval?: {
                required: boolean;
                policy_ref?: string;
                minimum_approvers: number;
                approved_count: number;
                separation_of_duty: boolean;
            };
            execution_ref?: string;
            result_summary?: string;
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ApprovalRequirement: {
            /** Format: uuid */
            policy_id: string;
            /** Format: int64 */
            policy_version: number;
            minimum_approvers: number;
            separation_of_duty: boolean;
            approved_count: number;
            /** @enum {string} */
            status: "pending" | "approved" | "rejected" | "invalidated";
        };
        ApprovalDecision: {
            decision_id: string;
            actor_user_id: string;
            /** @enum {unknown} */
            decision: "approved" | "rejected";
            reason?: string;
            /** Format: date-time */
            decided_at: string;
        };
        ApprovalRequestView: {
            /** Format: uuid */
            approval_request_id: string;
            action_ref: string;
            requirements: components["schemas"]["ApprovalRequirement"][];
            decisions: components["schemas"]["ApprovalDecision"][];
            /** @enum {string} */
            status: "pending" | "approved" | "rejected" | "expired" | "invalidated";
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        Execution: {
            execution_id: string;
            action_ref: string;
            /** @enum {unknown} */
            status: "pending" | "running" | "succeeded" | "failed" | "result_unknown" | "cancelled";
            result_ref?: string;
            /** @enum {unknown} */
            readonly one_time_result_state: "unavailable" | "available" | "consumed" | "expired";
            resource_ref?: {
                resource_type: string;
                resource_id: string;
                version: number;
            };
            operation_ref?: {
                /** @constant */
                kind: "connector_install";
                /** Format: uuid */
                id: string;
            };
            error_code?: string;
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
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listInteractiveCards: {
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
            /** @description Card page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getInteractiveCard: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Card. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["InteractiveCard"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listCardVersions: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Versions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardVersionPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createCardConfigurationVersion: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                card_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CardConfigurationVersionCreate"];
            };
        };
        responses: {
            /** @description Version. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardVersion"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getCardVersion: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                card_id: string;
                revision: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Version. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardVersion"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    startCardValidation: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                card_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CardValidationStart"];
            };
        };
        responses: {
            /** @description Validation run. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardValidationRun"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    submitCardValidationEvidence: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                run_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CardValidationEvidence"];
            };
        };
        responses: {
            /** @description Validation run. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardValidationRun"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    changeInteractiveCardState: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                card_id: string;
                state_action: "activate" | "disable" | "rollback" | "deprecate";
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CardStateCommand"];
            };
        };
        responses: {
            /** @description Card. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["InteractiveCard"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listToolSchemaCatalog: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Tool schema catalog. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ToolSchemaCatalog"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createCardPresentation: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                card_instance_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CardPresentationCreate"];
            };
        };
        responses: {
            /** @description Presentation. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardPresentation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    invokeCardQueryBinding: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                binding_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Binding result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardBindingInvokeResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    invokeCardActionBinding: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                binding_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Binding result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CardBindingInvokeResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
