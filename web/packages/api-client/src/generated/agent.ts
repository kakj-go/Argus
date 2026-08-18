export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
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
        AgentEvent: {
            /** @constant */
            schema_version: "argus.agent_event/v1";
            event_id: string;
            sequence: number;
            run_id: string;
            message_id?: string;
            /** @enum {unknown} */
            event_type: "run_started" | "message_started" | "message_delta" | "message_completed" | "tool_call_started" | "tool_call_completed" | "pending_action_created" | "card_draft_created" | "card_instance_created" | "card_presentation_invalidated" | "waiting_input" | "waiting_approval" | "context_compaction_started" | "context_compaction_completed" | "context_compaction_failed" | "run_completed" | "run_failed";
            /** Format: date-time */
            occurred_at: string;
            payload: components["schemas"]["PublicJsonObject"];
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
        Step: {
            step_id: string;
            run_id: string;
            sequence: number;
            /** @enum {unknown} */
            step_type: "model_call" | "tool_call" | "pending_action" | "execution" | "context_compaction" | "verification";
            /** @enum {unknown} */
            status: "pending" | "leased" | "running" | "waiting_input" | "waiting_approval" | "succeeded" | "failed" | "cancelled" | "timed_out";
            attempt: number;
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        Task: {
            task_id: string;
            run_id: string;
            step_id: string;
            /** @enum {unknown} */
            status: "pending" | "leased" | "running" | "succeeded" | "failed" | "cancelled" | "timed_out";
            attempt: number;
            last_error_code?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
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
        ToolMetadata: {
            tool_name: string;
            input_schema_version: string;
            output_schema_version: string;
            /** @enum {unknown} */
            risk: "read" | "write" | "dangerous" | "critical";
            /** @enum {unknown} */
            agent_visibility: "visible" | "hidden";
            /** @enum {unknown} */
            execution_mode: "sequential" | "parallel_safe";
            result_projection_schema: string;
            required_permissions: string[];
        } & (unknown & unknown);
        ModelCapabilities: {
            context_window_tokens: number;
            max_output_tokens: number;
            supports_tool_calling: boolean;
            supports_structured_output: boolean;
            /** @enum {unknown} */
            provider_compaction_capability: "none" | "optional";
        };
        ContextBudget: {
            context_window_tokens: number;
            reserved_output_tokens: number;
            safety_margin_tokens: number;
            system_tokens: number;
            tool_schema_tokens: number;
            checkpoint_tokens: number;
            summary_tokens: number;
            recent_tail_tokens: number;
            current_input_tokens: number;
            estimated_input_tokens: number;
            hard_input_limit_tokens: number;
            soft_compaction_threshold_tokens?: number;
        };
        ModelCall: {
            model_call_id: string;
            run_id: string;
            model_id: string;
            model_revision: number;
            projection_hash: string;
            context_snapshot_id?: string;
            estimated_input_tokens: number;
            actual_input_tokens?: number;
            output_tokens?: number;
            reserved_output_tokens: number;
            input_price_per_million_snapshot: number;
            output_price_per_million_snapshot: number;
            currency: string;
            compaction: boolean;
            /** @enum {unknown} */
            stop_reason?: "completed" | "tool_call" | "output_limit" | "cancelled" | "provider_error" | "content_filtered";
            /** Format: date-time */
            created_at: string;
        };
        PublicJsonValue: unknown;
        PublicJsonObject: unknown;
        ResourceRef: {
            resource_type: string;
            resource_id: string;
            version: number;
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
