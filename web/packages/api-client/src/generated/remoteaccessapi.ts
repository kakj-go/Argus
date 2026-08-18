export interface paths {
    "/enterprise/remote-access-grants": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access grants. */
        get: operations["listRemoteAccessGrants"];
        put?: never;
        /** Create a remote access grant. */
        post: operations["createRemoteAccessGrant"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-grants/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get a remote access grant. */
        get: operations["getRemoteAccessGrant"];
        /** Update a remote access grant with optimistic concurrency. */
        put: operations["updateRemoteAccessGrant"];
        post?: never;
        /** Disable a remote access grant. */
        delete: operations["disableRemoteAccessGrant"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-policies": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access approval policies. */
        get: operations["listRemoteAccessPolicies"];
        put?: never;
        /** Create a remote access approval policy. */
        post: operations["createRemoteAccessPolicy"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-policies/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get a remote access approval policy. */
        get: operations["getRemoteAccessPolicy"];
        /** Update a remote access approval policy with optimistic concurrency. */
        put: operations["updateRemoteAccessPolicy"];
        post?: never;
        /** Disable a remote access approval policy. */
        delete: operations["disableRemoteAccessPolicy"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-requests": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access requests visible to the current user. */
        get: operations["listRemoteAccessRequests"];
        put?: never;
        /** Request remote access for the current user. */
        post: operations["createRemoteAccessRequest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-requests/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get a remote access request and approval status. */
        get: operations["getRemoteAccessRequest"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-requests/{id}/decisions": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Approve or reject one remote access requirement. */
        post: operations["decideRemoteAccessRequest"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-leases": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List active and historical remote access leases. */
        get: operations["listRemoteAccessLeases"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-leases/{id}/revoke": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Revoke a remote access lease and its sessions. */
        post: operations["revokeRemoteAccessLease"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-sessions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access sessions. */
        get: operations["listRemoteAccessSessions"];
        put?: never;
        /** Create a session from an authorized access lease. */
        post: operations["createRemoteAccessSession"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-sessions/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get a remote access session. */
        get: operations["getRemoteAccessSession"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-sessions/{id}/tickets": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Issue a one-time short-lived WebSocket ticket. */
        post: operations["createRemoteAccessSessionTicket"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-sessions/{id}/terminate": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Terminate an active remote access session. */
        post: operations["terminateRemoteAccessSession"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-recordings/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get authorized recording metadata. */
        get: operations["getRemoteAccessRecording"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-recordings/{id}/events": {
        parameters: {
            query?: {
                cursor?: string;
            };
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Read an authorized page of asciicast recording events. */
        get: operations["listRemoteAccessRecordingEvents"];
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
        RemoteAccessGrant: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            subject_type: components["schemas"]["RemoteAccessSubjectType"];
            /** Format: uuid */
            subject_id: string;
            host_ids: string[];
            host_selector?: components["schemas"]["LabelSelector"];
            managed_account_ids: string[];
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            /** Format: date-time */
            valid_from: string;
            /** Format: date-time */
            valid_until: string;
            enabled: boolean;
            /** Format: int64 */
            version: number;
            /** Format: uuid */
            created_by?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        RemoteAccessGrantWrite: {
            subject_type: components["schemas"]["RemoteAccessSubjectType"];
            /** Format: uuid */
            subject_id: string;
            host_ids: string[];
            host_selector?: components["schemas"]["LabelSelector"];
            managed_account_ids: string[];
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            /** Format: date-time */
            valid_from: string;
            /** Format: date-time */
            valid_until: string;
            enabled: boolean;
        };
        RemoteAccessGrantUpdate: {
            subject_type: components["schemas"]["RemoteAccessSubjectType"];
            /** Format: uuid */
            subject_id: string;
            host_ids: string[];
            host_selector?: components["schemas"]["LabelSelector"];
            managed_account_ids: string[];
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            /** Format: date-time */
            valid_from: string;
            /** Format: date-time */
            valid_until: string;
            enabled: boolean;
            /** Format: int64 */
            expected_version: number;
        };
        RemoteAccessGrantPage: {
            items: components["schemas"]["RemoteAccessGrant"][];
            page: components["schemas"]["CursorPage"];
        };
        RemoteAccessPolicy: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            enabled: boolean;
            priority: number;
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            host_selector?: components["schemas"]["LabelSelector"];
            approver_role_ids?: string[];
            minimum_approvals: number;
            separation_of_duties: boolean;
            require_mfa: boolean;
            max_session_seconds: number;
            idle_timeout_seconds: number;
            /** Format: int64 */
            version: number;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        RemoteAccessPolicyWrite: {
            name: string;
            enabled: boolean;
            priority: number;
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            host_selector?: components["schemas"]["LabelSelector"];
            approver_role_ids?: string[];
            minimum_approvals: number;
            separation_of_duties: boolean;
            require_mfa: boolean;
            max_session_seconds: number;
            idle_timeout_seconds: number;
        };
        RemoteAccessPolicyUpdate: {
            name: string;
            enabled: boolean;
            priority: number;
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            host_selector?: components["schemas"]["LabelSelector"];
            approver_role_ids?: string[];
            minimum_approvals: number;
            separation_of_duties: boolean;
            require_mfa: boolean;
            max_session_seconds: number;
            idle_timeout_seconds: number;
            /** Format: int64 */
            expected_version: number;
        };
        RemoteAccessPolicyPage: {
            items: components["schemas"]["RemoteAccessPolicy"][];
            page: components["schemas"]["CursorPage"];
        };
        RemoteAccessRequirement: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            policy_id: string;
            /** Format: int64 */
            policy_version: number;
            minimum_approvals: number;
            separation_of_duties: boolean;
            require_mfa: boolean;
            /** @enum {string} */
            status: "pending" | "satisfied" | "rejected" | "invalidated";
            approved_count: number;
        };
        RemoteAccessDecision: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            request_id: string;
            /** Format: uuid */
            requirement_id: string;
            /** @enum {string} */
            decision: "approve" | "reject";
            comment?: string;
            /** Format: uuid */
            decided_by: string;
            /** Format: date-time */
            decided_at: string;
        };
        RemoteAccessDecisionCreate: {
            /** Format: uuid */
            requirement_id: string;
            /** @enum {string} */
            decision: "approve" | "reject";
            comment?: string;
        };
        AccessRequestCreate: {
            /** Format: uuid */
            host_id: string;
            /** Format: uuid */
            managed_account_id: string;
            protocol: components["schemas"]["RemoteAccessProtocol"];
            action: components["schemas"]["RemoteAccessAction"];
            reason: string;
        };
        AccessRequest: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** Format: uuid */
            readonly requester_id: string;
            /** Format: uuid */
            grant_id: string;
            /** Format: uuid */
            host_id: string;
            /** Format: uuid */
            managed_account_id: string;
            protocol: components["schemas"]["RemoteAccessProtocol"];
            action: components["schemas"]["RemoteAccessAction"];
            reason: string;
            status: components["schemas"]["RemoteAccessRequestStatus"];
            requirements: components["schemas"]["RemoteAccessRequirement"][];
            decisions: components["schemas"]["RemoteAccessDecision"][];
            /** Format: int64 */
            authorization_version: number;
            /** Format: date-time */
            expires_at: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        AccessRequestPage: {
            items: components["schemas"]["AccessRequest"][];
            page: components["schemas"]["CursorPage"];
        };
        AccessLease: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            request_id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** Format: uuid */
            readonly user_id: string;
            /** Format: uuid */
            grant_id: string;
            /** Format: uuid */
            host_id: string;
            /** Format: uuid */
            managed_account_id: string;
            protocol: components["schemas"]["RemoteAccessProtocol"];
            action: components["schemas"]["RemoteAccessAction"];
            /** Format: int64 */
            authorization_version: number;
            /** Format: date-time */
            issued_at: string;
            /** Format: date-time */
            expires_at: string;
            revoked: boolean;
            /** Format: date-time */
            revoked_at?: string;
        };
        AccessLeasePage: {
            items: components["schemas"]["AccessLease"][];
            page: components["schemas"]["CursorPage"];
        };
        RemoteAccessSessionCreate: {
            /** Format: uuid */
            lease_id: string;
            terminal_cols: number;
            terminal_rows: number;
        };
        RemoteAccessSession: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** Format: uuid */
            readonly user_id: string;
            /** Format: uuid */
            lease_id: string;
            /** Format: uuid */
            host_id: string;
            /** Format: uuid */
            managed_account_id: string;
            protocol: components["schemas"]["RemoteAccessProtocol"];
            /** @enum {string} */
            connection_mode: "via_bastion" | "connector_local" | "direct_ssh" | "direct_winrm";
            status: components["schemas"]["RemoteAccessSessionStatus"];
            /** Format: int64 */
            readonly session_fence: number;
            /** Format: uuid */
            recording_id: string;
            idle_timeout_seconds: number;
            max_duration_seconds: number;
            /** Format: date-time */
            connect_before: string;
            /** Format: date-time */
            connected_at?: string;
            /** Format: date-time */
            terminated_at?: string;
            termination_reason?: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        RemoteAccessSessionPage: {
            items: components["schemas"]["RemoteAccessSession"][];
            page: components["schemas"]["CursorPage"];
        };
        SessionTicketResult: {
            /** Format: uuid */
            session_id: string;
            ticket: string;
            websocket_url: string;
            /** @constant */
            protocol_version: "argus.remote_access/v1";
            /** Format: date-time */
            expires_at: string;
        };
        SessionTerminate: {
            reason: string;
        };
        RemoteAccessRecording: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            /** Format: uuid */
            session_id: string;
            /** @enum {string} */
            status: "recording" | "available" | "incomplete" | "failed" | "expired";
            /** @constant */
            format: "asciicast_v2";
            /** @constant */
            encrypted: true;
            chunk_count: number;
            event_count: number;
            /** Format: int64 */
            size_bytes: number;
            /** Format: int64 */
            duration_ms?: number;
            final_hash?: string;
            /** Format: date-time */
            retention_until: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            completed_at?: string;
        };
        RecordingEventPage: {
            recording: components["schemas"]["RemoteAccessRecording"];
            events: {
                time: number;
                /** @enum {string} */
                type: "i" | "o" | "r" | "m";
                data: unknown;
            }[];
            next_cursor: string;
            complete: boolean;
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
        RemoteAccessSubjectType: "user" | "department";
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
        /** @enum {string} */
        RemoteAccessProtocol: "ssh" | "winrs";
        /** @enum {string} */
        RemoteAccessAction: "terminal";
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
        /** @enum {string} */
        RemoteAccessRequestStatus: "requested" | "awaiting_approval" | "authorized" | "rejected" | "expired" | "invalidated";
        /** @enum {string} */
        RemoteAccessSessionStatus: "requested" | "awaiting_approval" | "authorized" | "connecting" | "active" | "terminating" | "terminated" | "failed" | "expired" | "connection_lost" | "invalidated";
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
    listRemoteAccessGrants: {
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
            /** @description Grant page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessGrantPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createRemoteAccessGrant: {
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
                "application/json": components["schemas"]["RemoteAccessGrantWrite"];
            };
        };
        responses: {
            /** @description Grant. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessGrant"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getRemoteAccessGrant: {
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
            /** @description Grant. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessGrant"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateRemoteAccessGrant: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RemoteAccessGrantUpdate"];
            };
        };
        responses: {
            /** @description Grant. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessGrant"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableRemoteAccessGrant: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Grant disabled. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    listRemoteAccessPolicies: {
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
            /** @description Policy page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessPolicyPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createRemoteAccessPolicy: {
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
                "application/json": components["schemas"]["RemoteAccessPolicyWrite"];
            };
        };
        responses: {
            /** @description Policy. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessPolicy"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getRemoteAccessPolicy: {
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
            /** @description Policy. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessPolicy"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    updateRemoteAccessPolicy: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RemoteAccessPolicyUpdate"];
            };
        };
        responses: {
            /** @description Policy. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessPolicy"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    disableRemoteAccessPolicy: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Policy disabled. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            default: components["responses"]["Error"];
        };
    };
    listRemoteAccessRequests: {
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
            /** @description Request page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AccessRequestPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createRemoteAccessRequest: {
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
                "application/json": components["schemas"]["AccessRequestCreate"];
            };
        };
        responses: {
            /** @description Request. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AccessRequest"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getRemoteAccessRequest: {
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
            /** @description Request. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AccessRequest"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    decideRemoteAccessRequest: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RemoteAccessDecisionCreate"];
            };
        };
        responses: {
            /** @description Request. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AccessRequest"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listRemoteAccessLeases: {
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
            /** @description Lease page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AccessLeasePage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    revokeRemoteAccessLease: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Revoked lease. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AccessLease"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listRemoteAccessSessions: {
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
            /** @description Session page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessSessionPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createRemoteAccessSession: {
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
                "application/json": components["schemas"]["RemoteAccessSessionCreate"];
            };
        };
        responses: {
            /** @description Session. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getRemoteAccessSession: {
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
            /** @description Session. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    createRemoteAccessSessionTicket: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One-time ticket. */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionTicketResult"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    terminateRemoteAccessSession: {
        parameters: {
            query?: never;
            header: {
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                "X-CSRF-Token": components["parameters"]["CsrfToken"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SessionTerminate"];
            };
        };
        responses: {
            /** @description Session. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessSession"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    getRemoteAccessRecording: {
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
            /** @description Recording metadata. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAccessRecording"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
    listRemoteAccessRecordingEvents: {
        parameters: {
            query?: {
                cursor?: string;
            };
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Authorized recording event page. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RecordingEventPage"];
                };
            };
            default: components["responses"]["Error"];
        };
    };
}
