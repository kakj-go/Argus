export interface paths {
    "/conversations": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** listConversations. */
        get: operations["listConversations"];
        put?: never;
        /** createConversation. */
        post: operations["createConversation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/conversations/{conversation_id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        /** getConversation. */
        get: operations["getConversation"];
        /** updateConversation. */
        put: operations["updateConversation"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/conversations/{conversation_id}/messages": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** createConversationMessage. */
        post: operations["createConversationMessage"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/conversations/{conversation_id}/ledger": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        /** listConversationEvents. */
        get: operations["listConversationEvents"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/runs/{run_id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                run_id: string;
            };
            cookie?: never;
        };
        /** getRun. */
        get: operations["getRun"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/runs/{run_id}/cancel": {
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
        /** cancelRun. */
        post: operations["cancelRun"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/runs/{run_id}/compact": {
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
        /** compactRun. */
        post: operations["compactRun"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/tool-results/{result_ref}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                result_ref: string;
            };
            cookie?: never;
        };
        /** getToolResult. */
        get: operations["getToolResult"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/conversations/{conversation_id}/events": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Resume a conversation event stream. */
        get: operations["streamConversationEvents"];
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
        Conversation: {
            /** Format: uuid */
            id: string;
            title: string;
            /** Format: uuid */
            selected_model_id: string;
            /** @enum {string} */
            status: "active" | "archived";
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ConversationCreate: {
            title?: string;
            /** Format: uuid */
            selected_model_id: string;
        };
        ConversationUpdate: {
            title?: string;
            /** Format: uuid */
            selected_model_id?: string;
            /** @enum {string} */
            status?: "active" | "archived";
            /** Format: int64 */
            expected_version: number;
        };
        ConversationPage: {
            items: components["schemas"]["Conversation"][];
            page: components["schemas"]["CursorPage"];
        };
        MessageCreate: {
            content: string;
            command?: {
                /** @enum {string} */
                type: "interactive_card.create" | "interactive_card.revise";
                /** Format: uuid */
                card_id?: string;
                expected_revision?: number;
            };
        };
        MessageAccepted: {
            event: components["schemas"]["ConversationEvent"];
            run: components["schemas"]["Run"];
        };
        ConversationEventPage: {
            items: components["schemas"]["ConversationEvent"][];
            page: components["schemas"]["CursorPage"];
        };
        RunCommandResult: {
            run: components["schemas"]["Run"];
            snapshot?: components["schemas"]["ContextSnapshot"];
        };
        ToolResult: {
            result_ref: string;
            tool_call_id: string;
            tool_id: string;
            content_hash: string;
            byte_size: number;
            partial: boolean;
            projection: components["schemas"]["ToolResultProjection"];
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
        PartialMetadata: {
            partial: boolean;
            reasons: ("authorization_filtered" | "budget_truncated" | "source_timeout" | "source_unavailable")[];
        };
        CursorPage: {
            next_cursor: string | null;
            has_more: boolean;
            partial: components["schemas"]["PartialMetadata"];
        };
        IdempotencyKey: string;
        PublicJsonValue: unknown;
        PublicJsonObject: unknown;
        ConversationEvent: {
            /** @constant */
            schema_version: "argus.conversation_event/v1";
            event_id: string;
            sequence: number;
            enterprise_id: string;
            conversation_id: string;
            run_id?: string;
            step_id?: string;
            /** @enum {unknown} */
            event_type: "user_message" | "assistant_message" | "model_usage" | "tool_call_requested" | "tool_call_started" | "tool_call_result" | "pending_action_created" | "user_confirmation" | "approval_update" | "execution_update" | "card_draft_created" | "card_instance_created" | "card_presentation_invalidated" | "card_action_result" | "run_state_changed" | "context_compacted";
            /** @enum {unknown} */
            actor_type?: "user" | "model" | "service" | "worker" | "system";
            actor_id?: string;
            /** Format: date-time */
            occurred_at: string;
            content_hash: string;
            artifact_ref?: string;
            /** @enum {unknown} */
            data_classification?: "public" | "internal" | "sensitive";
            payload: components["schemas"]["PublicJsonObject"];
        };
        Run: {
            run_id: string;
            conversation_id: string;
            enterprise_id: string;
            model_id: string;
            model_revision: number;
            /** @enum {unknown} */
            locale: "zh-CN" | "en-US";
            /** @enum {unknown} */
            status: "pending" | "running" | "waiting_input" | "waiting_approval" | "waiting_system" | "succeeded" | "failed" | "cancelled" | "timed_out";
            current_step_id?: string;
            stop_reason?: string;
            error_code?: string;
            authorization_version: number;
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ResourceRef: {
            resource_type: string;
            resource_id: string;
            version: number;
        };
        RunCheckpoint: {
            /** @constant */
            schema_version: "argus.run_checkpoint/v1";
            run_id: string;
            conversation_id: string;
            enterprise_id: string;
            model_id: string;
            model_revision: number;
            /** @enum {unknown} */
            locale: "zh-CN" | "en-US";
            /** @enum {unknown} */
            status: "pending" | "running" | "waiting_input" | "waiting_approval" | "waiting_system" | "succeeded" | "failed" | "cancelled" | "timed_out";
            current_step_id?: string;
            goal: string;
            user_constraints?: string[];
            plan?: string[];
            completed_step_ids: string[];
            next_step?: string;
            open_questions: string[];
            waiting_reason?: string;
            target_resource_refs: components["schemas"]["ResourceRef"][];
            active_public_pending_action_refs: string[];
            tool_call_refs: string[];
            execution_refs: string[];
            last_error_codes: string[];
            authorization_version: number;
            resource_scope_snapshot_ref?: string;
            version: number;
        };
        ContextSnapshot: {
            /** @constant */
            schema_version: "argus.context_snapshot/v1";
            id: string;
            enterprise_id: string;
            conversation_id: string;
            run_id: string;
            source_from_event_id: string;
            source_through_event_id: string;
            first_kept_event_id: string;
            source_from_sequence: number;
            source_through_sequence: number;
            first_kept_sequence: number;
            typed_checkpoint: components["schemas"]["RunCheckpoint"];
            narrative_summary: string;
            compaction_model_id: string;
            compaction_model_revision: number;
            compaction_prompt_version: string;
            estimated_tokens_before: number;
            actual_tokens_after: number;
            source_hash: string;
            snapshot_hash: string;
            /** @enum {unknown} */
            status: "pending" | "active" | "failed" | "superseded";
            error_code?: string;
            /** Format: date-time */
            created_at: string;
        };
        ToolResultProjection: {
            /** @constant */
            schema_version: "argus.tool_result_projection/v1";
            tool_call_id: string;
            projection_schema_version: string;
            result_ref: string;
            result_hash: string;
            summary: components["schemas"]["PublicJsonObject"];
            samples?: components["schemas"]["PublicJsonValue"][];
            resource_refs: components["schemas"]["ResourceRef"][];
            partial: boolean;
            partial_reasons?: ("authorization_filtered" | "budget_truncated" | "source_timeout" | "source_unavailable")[];
            original_bytes: number;
            projected_bytes: number;
        } & (unknown & unknown);
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
        RequestId: components["schemas"]["RequestId"];
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listConversations: {
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
            /** @description Conversation page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConversationPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createConversation: {
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
                "application/json": components["schemas"]["ConversationCreate"];
            };
        };
        responses: {
            /** @description Conversation. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Conversation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getConversation: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Conversation. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Conversation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateConversation: {
        parameters: {
            query?: never;
            header: {
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConversationUpdate"];
            };
        };
        responses: {
            /** @description Conversation. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Conversation"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createConversationMessage: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["MessageCreate"];
            };
        };
        responses: {
            /** @description Message and Run accepted. */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MessageAccepted"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listConversationEvents: {
        parameters: {
            query?: {
                cursor?: components["parameters"]["Cursor"];
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Immutable event page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConversationEventPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getRun: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                run_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Run. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Run"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    cancelRun: {
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
        requestBody?: never;
        responses: {
            /** @description Cancelled Run. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RunCommandResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    compactRun: {
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
        requestBody?: never;
        responses: {
            /** @description Compaction scheduled. */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RunCommandResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getToolResult: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                result_ref: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Authorized Tool result. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ToolResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    streamConversationEvents: {
        parameters: {
            query?: never;
            header?: {
                "X-Request-ID"?: components["parameters"]["RequestId"];
                "Last-Event-ID"?: string;
            };
            path: {
                conversation_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description SSE frames whose data field validates as StreamEventEnvelope. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/event-stream": string;
                };
            };
            /** @description Resume cursor is expired or bound to a stale authorization version. */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ApiError"];
                };
            };
        };
    };
}
