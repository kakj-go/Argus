import type { operations } from "./remoteaccessapi_operations.js";
export type { operations };

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
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-grants/{id}/enable": {
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
        /** Enable a remote access grant. */
        post: operations["enableRemoteAccessGrant"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-grants/{id}/disable": {
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
        /** Disable a remote access grant. */
        post: operations["disableRemoteAccessGrant"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-grants/{id}/restore": {
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
        /** Restore an archived remote access grant as a draft. */
        post: operations["restoreRemoteAccessGrant"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-grants/{id}/archive": {
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
        /** Archive a remote access grant. */
        post: operations["archiveRemoteAccessGrant"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-grants/{id}/references": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get remote access grant references. */
        get: operations["getRemoteAccessGrantReferences"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access rules. */
        get: operations["listRemoteAccessRules"];
        put?: never;
        /** Create a remote access rule. */
        post: operations["createRemoteAccessRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules/simulate": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** Simulate the current remote access decision without creating runtime state. */
        post: operations["simulateRemoteAccessRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get a remote access rule. */
        get: operations["getRemoteAccessRule"];
        /** Update a remote access rule with optimistic concurrency. */
        put: operations["updateRemoteAccessRule"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules/{id}/enable": {
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
        /** Enable a remote access rule. */
        post: operations["enableRemoteAccessRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules/{id}/disable": {
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
        /** Disable a remote access rule. */
        post: operations["disableRemoteAccessRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules/{id}/restore": {
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
        /** Restore an archived remote access rule as a draft. */
        post: operations["restoreRemoteAccessRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules/{id}/archive": {
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
        /** Archive a remote access rule. */
        post: operations["archiveRemoteAccessRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/remote-access-rules/{id}/references": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get remote access rule references. */
        get: operations["getRemoteAccessRuleReferences"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-workflows": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access approval workflows. */
        get: operations["listApprovalWorkflows"];
        put?: never;
        /** Create a remote access approval workflow. */
        post: operations["createApprovalWorkflow"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-workflows/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get a remote access approval workflow. */
        get: operations["getApprovalWorkflow"];
        /** Update a remote access approval workflow with optimistic concurrency. */
        put: operations["updateApprovalWorkflow"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-workflows/{id}/enable": {
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
        /** Enable an approval workflow. */
        post: operations["enableApprovalWorkflow"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-workflows/{id}/disable": {
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
        /** Disable an approval workflow. */
        post: operations["disableApprovalWorkflow"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-workflows/{id}/restore": {
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
        /** Restore an archived approval workflow as a draft. */
        post: operations["restoreApprovalWorkflow"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-workflows/{id}/archive": {
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
        /** Archive an approval workflow. */
        post: operations["archiveApprovalWorkflow"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/approval-workflows/{id}/references": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get approval workflow references. */
        get: operations["getApprovalWorkflowReferences"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/session-profiles": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access session profiles. */
        get: operations["listSessionProfiles"];
        put?: never;
        /** Create a remote access session profile. */
        post: operations["createSessionProfile"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/session-profiles/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get a remote access session profile. */
        get: operations["getSessionProfile"];
        /** Update a remote access session profile with optimistic concurrency. */
        put: operations["updateSessionProfile"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/session-profiles/{id}/enable": {
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
        /** Enable a session profile. */
        post: operations["enableSessionProfile"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/session-profiles/{id}/disable": {
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
        /** Disable a session profile. */
        post: operations["disableSessionProfile"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/session-profiles/{id}/restore": {
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
        /** Restore an archived session profile as a draft. */
        post: operations["restoreSessionProfile"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/session-profiles/{id}/archive": {
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
        /** Archive a session profile. */
        post: operations["archiveSessionProfile"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/enterprise/session-profiles/{id}/references": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        /** Get session profile references. */
        get: operations["getSessionProfileReferences"];
        put?: never;
        post?: never;
        delete?: never;
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
    "/enterprise/remote-access-requests/{id}/resume": {
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
        /** Re-evaluate an access request after fresh step-up authentication. */
        post: operations["resumeRemoteAccessRequest"];
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
    "/enterprise/remote-access-recordings": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List remote access recordings. */
        get: operations["listRemoteAccessRecordings"];
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
            managed_account_ids: string[];
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            /** Format: date-time */
            valid_from: string;
            /** Format: date-time */
            valid_until: string;
            status: components["schemas"]["RemoteAccessGovernanceStatus"];
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
            managed_account_ids: string[];
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            /** Format: date-time */
            valid_from: string;
            /** Format: date-time */
            valid_until: string;
            /**
             * @default draft
             * @enum {string}
             */
            status: "draft";
        };
        RemoteAccessGrantUpdate: {
            subject_type: components["schemas"]["RemoteAccessSubjectType"];
            /** Format: uuid */
            subject_id: string;
            host_ids: string[];
            managed_account_ids: string[];
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            /** Format: date-time */
            valid_from: string;
            /** Format: date-time */
            valid_until: string;
            /** Format: int64 */
            expected_version: number;
        };
        RemoteAccessGrantPage: {
            items: components["schemas"]["RemoteAccessGrant"][];
            page: components["schemas"]["CursorPage"];
        };
        RemoteAccessRule: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            description: string;
            priority: number;
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            source_cidrs: string[];
            time_windows: components["schemas"]["RemoteAccessTimeWindow"][];
            effects: components["schemas"]["RemoteAccessRuleEffect"][];
            /** Format: uuid */
            approval_workflow_id?: string;
            /** Format: uuid */
            session_profile_id?: string;
            status: components["schemas"]["RemoteAccessGovernanceStatus"];
            /** Format: int64 */
            version: number;
            /** Format: uuid */
            created_by: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        RemoteAccessRuleWrite: {
            name: string;
            description: string;
            priority: number;
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            source_cidrs: string[];
            time_windows: components["schemas"]["RemoteAccessTimeWindow"][];
            effects: components["schemas"]["RemoteAccessRuleEffect"][];
            /** Format: uuid */
            approval_workflow_id?: string;
            /** Format: uuid */
            session_profile_id?: string;
            /**
             * @default draft
             * @enum {string}
             */
            status: "draft";
        };
        RemoteAccessRuleUpdate: {
            name: string;
            description: string;
            priority: number;
            protocols: components["schemas"]["RemoteAccessProtocol"][];
            actions: components["schemas"]["RemoteAccessAction"][];
            source_cidrs: string[];
            time_windows: components["schemas"]["RemoteAccessTimeWindow"][];
            effects: components["schemas"]["RemoteAccessRuleEffect"][];
            /** Format: uuid */
            approval_workflow_id?: string;
            /** Format: uuid */
            session_profile_id?: string;
            /** Format: int64 */
            expected_version: number;
        };
        RemoteAccessRulePage: {
            items: components["schemas"]["RemoteAccessRule"][];
            page: components["schemas"]["CursorPage"];
        };
        ApprovalWorkflow: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            description: string;
            approver_role_ids: string[];
            minimum_approvals: number;
            separation_of_duties: boolean;
            approval_timeout_seconds: number;
            escalation_after_seconds: number;
            /** @enum {string} */
            timeout_effect: "reject" | "expire";
            escalation_role_ids: string[];
            status: components["schemas"]["RemoteAccessGovernanceStatus"];
            /** Format: int64 */
            version: number;
            /** Format: uuid */
            created_by: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        ApprovalWorkflowWrite: {
            name: string;
            description: string;
            approver_role_ids: string[];
            minimum_approvals: number;
            separation_of_duties: boolean;
            approval_timeout_seconds: number;
            escalation_after_seconds: number;
            /** @enum {string} */
            timeout_effect: "reject" | "expire";
            escalation_role_ids: string[];
            /**
             * @default draft
             * @enum {string}
             */
            status: "draft";
        };
        ApprovalWorkflowUpdate: {
            name: string;
            description: string;
            approver_role_ids: string[];
            minimum_approvals: number;
            separation_of_duties: boolean;
            approval_timeout_seconds: number;
            escalation_after_seconds: number;
            /** @enum {string} */
            timeout_effect: "reject" | "expire";
            escalation_role_ids: string[];
            /** Format: int64 */
            expected_version: number;
        };
        ApprovalWorkflowPage: {
            items: components["schemas"]["ApprovalWorkflow"][];
            page: components["schemas"]["CursorPage"];
        };
        SessionProfile: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            readonly enterprise_id: string;
            name: string;
            description: string;
            max_session_seconds: number;
            idle_timeout_seconds: number;
            /** @enum {string} */
            recording_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            command_audit_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            clipboard_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_upload_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_download_mode: "enabled" | "disabled";
            /** @enum {string} */
            port_forward_mode: "enabled" | "disabled";
            /** @enum {string} */
            session_share_mode: "enabled" | "disabled";
            retention_days: number;
            status: components["schemas"]["RemoteAccessGovernanceStatus"];
            /** Format: int64 */
            version: number;
            /** Format: uuid */
            created_by: string;
            /** Format: date-time */
            created_at: string;
            /** Format: date-time */
            updated_at: string;
        };
        SessionProfileWrite: {
            name: string;
            description: string;
            max_session_seconds: number;
            idle_timeout_seconds: number;
            /** @enum {string} */
            recording_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            command_audit_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            clipboard_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_upload_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_download_mode: "enabled" | "disabled";
            /** @enum {string} */
            port_forward_mode: "enabled" | "disabled";
            /** @enum {string} */
            session_share_mode: "enabled" | "disabled";
            retention_days: number;
            /**
             * @default draft
             * @enum {string}
             */
            status: "draft";
        };
        SessionProfileUpdate: {
            name: string;
            description: string;
            max_session_seconds: number;
            idle_timeout_seconds: number;
            /** @enum {string} */
            recording_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            command_audit_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            clipboard_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_upload_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_download_mode: "enabled" | "disabled";
            /** @enum {string} */
            port_forward_mode: "enabled" | "disabled";
            /** @enum {string} */
            session_share_mode: "enabled" | "disabled";
            retention_days: number;
            /** Format: int64 */
            expected_version: number;
        };
        SessionProfilePage: {
            items: components["schemas"]["SessionProfile"][];
            page: components["schemas"]["CursorPage"];
        };
        RemoteAccessReferences: {
            rules: number;
            requests: number;
            leases: number;
            sessions: number;
        };
        RemoteAccessRequirement: {
            /** Format: uuid */
            id: string;
            /** Format: uuid */
            rule_id?: string;
            /** Format: int64 */
            rule_version?: number;
            /** Format: uuid */
            workflow_id?: string;
            /** Format: int64 */
            workflow_version?: number;
            /** Format: uuid */
            session_profile_id?: string;
            /** Format: int64 */
            session_profile_version?: number;
            approval_snapshot?: {
                [key: string]: unknown;
            };
            /** Format: date-time */
            deadline_at?: string;
            /** Format: date-time */
            escalation_at?: string;
            /** Format: date-time */
            escalated_at?: string;
            /** @enum {string} */
            timeout_effect?: "reject" | "expire";
            escalation_role_ids?: string[];
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
            /** @enum {string} */
            decision_outcome?: "allowed" | "denied" | "awaiting_mfa" | "awaiting_approval";
            decision_reason_codes?: string[];
            decision_snapshot?: {
                [key: string]: unknown;
            };
            decision_snapshot_hash?: string;
            matched_grant_snapshots?: {
                [key: string]: unknown;
            }[];
            matched_rule_snapshots?: {
                [key: string]: unknown;
            }[];
            /** Format: date-time */
            decision_at?: string;
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
            decision_snapshot_hash?: string;
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
            /** Format: uuid */
            connector_id?: string;
            /** Format: int64 */
            connector_epoch?: number;
            gateway_instance?: string;
            status: components["schemas"]["RemoteAccessSessionStatus"];
            /** Format: int64 */
            readonly session_fence: number;
            /** Format: int64 */
            readonly authorization_version: number;
            /** @description Reason snapshot captured when the session was created. */
            readonly reason: string;
            /** Format: uuid */
            recording_id?: string;
            idle_timeout_seconds: number;
            max_duration_seconds: number;
            decision_snapshot_hash?: string;
            readonly decision_snapshot?: {
                [key: string]: unknown;
            };
            readonly session_profile_snapshot?: {
                [key: string]: unknown;
            };
            /** @enum {string} */
            recording_mode?: "required" | "optional" | "disabled";
            /** @enum {string} */
            command_audit_mode?: "required" | "optional" | "disabled";
            /** @enum {string} */
            clipboard_mode?: "enabled" | "disabled";
            /** @enum {string} */
            file_upload_mode?: "enabled" | "disabled";
            /** @enum {string} */
            file_download_mode?: "enabled" | "disabled";
            /** @enum {string} */
            port_forward_mode?: "enabled" | "disabled";
            /** @enum {string} */
            session_share_mode?: "enabled" | "disabled";
            retention_days?: number;
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
        RemoteAccessRecordingPage: {
            items: components["schemas"]["RemoteAccessRecording"][];
            page: components["schemas"]["CursorPage"];
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
        /** @enum {string} */
        RemoteAccessProtocol: "ssh" | "winrs";
        /** @enum {string} */
        RemoteAccessAction: "terminal";
        /** @enum {string} */
        RemoteAccessGovernanceStatus: "draft" | "enabled" | "disabled" | "archived";
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
        RemoteAccessTimeWindow: {
            day_of_week: number;
            start: string;
            end: string;
            timezone: string;
        };
        /** @enum {string} */
        RemoteAccessRuleEffect: "deny" | "require_mfa" | "require_approval" | "notify";
        RemoteAccessRuleSimulationRequest: {
            /** Format: uuid */
            host_id: string;
            /** Format: uuid */
            managed_account_id: string;
            protocol: components["schemas"]["RemoteAccessProtocol"];
            action: components["schemas"]["RemoteAccessAction"];
            /** Format: ip */
            source_ip?: string;
            /** Format: date-time */
            evaluation_time?: string;
            step_up_authenticated: boolean;
        };
        RemoteAccessObjectVersion: {
            /** Format: uuid */
            id: string;
            /** Format: int64 */
            version: number;
        };
        RemoteAccessApprovalRequirementSimulation: {
            /** Format: uuid */
            workflow_id: string;
            /** Format: int64 */
            workflow_version: number;
            approver_role_ids: string[];
            minimum_approvals: number;
            separation_of_duties: boolean;
            approval_timeout_seconds: number;
            escalation_after_seconds: number;
            /** @enum {string} */
            timeout_effect: "reject" | "expire";
            escalation_role_ids: string[];
            source_rule_ids: string[];
        };
        RemoteAccessSessionProfileSnapshot: {
            source_profiles: components["schemas"]["RemoteAccessObjectVersion"][];
            max_session_seconds: number;
            idle_timeout_seconds: number;
            /** @enum {string} */
            recording_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            command_audit_mode: "required" | "optional" | "disabled";
            /** @enum {string} */
            clipboard_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_upload_mode: "enabled" | "disabled";
            /** @enum {string} */
            file_download_mode: "enabled" | "disabled";
            /** @enum {string} */
            port_forward_mode: "enabled" | "disabled";
            /** @enum {string} */
            session_share_mode: "enabled" | "disabled";
            retention_days: number;
        };
        RemoteAccessRuleSimulationResult: {
            /** @enum {string} */
            outcome: "allowed" | "denied" | "awaiting_mfa" | "awaiting_approval";
            reason_codes: string[];
            explanation: string[];
            matched_grants: components["schemas"]["RemoteAccessObjectVersion"][];
            matched_rules: components["schemas"]["RemoteAccessObjectVersion"][];
            approval_requirements: components["schemas"]["RemoteAccessApprovalRequirementSimulation"][];
            session_profile: components["schemas"]["RemoteAccessSessionProfileSnapshot"];
            snapshot_hash: string;
        };
        /** @enum {string} */
        RemoteAccessRequestStatus: "requested" | "awaiting_mfa" | "awaiting_approval" | "authorized" | "rejected" | "expired" | "invalidated";
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
        ExpectedVersion: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
