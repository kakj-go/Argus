export type paths = Record<string, never>;
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
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
        };
        DataBinding: {
            slot_name: string;
            tool_call_id: string;
            path: string;
            schema_version: string;
        };
        QueryBinding: {
            binding_id: string;
            /** @enum {unknown} */
            status: "active" | "consumed" | "expired" | "invalidated";
            /** Format: date-time */
            expires_at: string;
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
        BridgeMessage: {
            /** @constant */
            bridge_version: "argus.card_bridge/v1";
            message_id: string;
            sequence: number;
            nonce: string;
            /** @enum {unknown} */
            type: "host.hello" | "card.ready" | "host.context" | "data.update" | "query.invoke" | "action.invoke" | "card.resize" | "binding.result" | "bridge.error" | "bridge.destroyed";
            payload: components["schemas"]["PublicJsonObject"];
        } & (unknown & unknown);
        /** @enum {unknown} */
        SlotType: "string" | "number" | "boolean" | "array" | "object";
        Slot: {
            name: string;
            /** @enum {unknown} */
            kind: "data" | "query" | "action";
            value_type: components["schemas"]["SlotType"];
            required: boolean;
            /** @default false */
            ai_generated: boolean;
        };
        PublicJsonValue: unknown;
        PublicJsonObject: unknown;
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
