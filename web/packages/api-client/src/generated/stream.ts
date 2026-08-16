export interface paths {
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
        StreamEventEnvelope: components["schemas"]["stream.schema"];
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
        /** StreamEventEnvelope */
        "stream.schema": {
            /** @constant */
            schema_version: "argus.stream_event/v1";
            event_id: string;
            sequence: number;
            /** @enum {unknown} */
            event_type: "agent_event" | "task_event" | "card_action_result" | "authorization_invalidated" | "stream_closing";
            /** Format: date-time */
            occurred_at: string;
            authorization_version?: number;
            resume_cursor?: string;
            /** @default false */
            terminal: boolean;
            /** @enum {unknown} */
            close_reason?: "normal" | "server_drain" | "authorization_revoked" | "session_expired" | "protocol_error" | "message_too_large" | "resource_unavailable" | "replaced_connection" | "internal_error";
            data: components["schemas"]["PublicJsonObject"];
        } & (unknown & unknown & unknown);
    };
    responses: never;
    parameters: {
        RequestId: components["schemas"]["RequestId"];
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
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
