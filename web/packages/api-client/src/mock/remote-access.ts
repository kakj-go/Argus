import type { ArgusApiClient, CursorListQuery } from "../client";
import type {
  ApprovalWorkflow,
  ApprovalWorkflowUpdate,
  ApprovalWorkflowWrite,
  RecordingEventPage,
  RemoteAccessGrant,
  RemoteAccessGrantUpdate,
  RemoteAccessGrantWrite,
  RemoteAccessReferences,
  RemoteAccessRule,
  RemoteAccessRuleSimulationRequest,
  RemoteAccessRuleSimulationResult,
  RemoteAccessRuleUpdate,
  RemoteAccessRuleWrite,
  SessionProfile,
  SessionProfileUpdate,
  SessionProfileWrite,
} from "../generated/contracts";
import type { Page } from "../types";
import type { MockContext } from "./context";
import { nextId } from "./store";

type GovernanceStatus = "draft" | "enabled" | "disabled" | "archived";
type GovernanceItem = { id: string; status: GovernanceStatus; version: number; updated_at: string };

function page<T>(items: T[], query?: CursorListQuery): Page<T> {
  const offset = Number.parseInt(query?.cursor ?? "0", 10) || 0;
  const limit = query?.limit ?? 50;
  const result = items.slice(offset, offset + limit);
  const next = offset + result.length;
  return { items: result, nextCursor: next < items.length ? String(next) : null, hasMore: next < items.length };
}

function references(): RemoteAccessReferences {
  return { rules: 0, requests: 0, leases: 0, sessions: 0 };
}

function transition<T extends GovernanceItem>(ctx: MockContext, items: T[], id: string, status: GovernanceStatus): T {
  const index = items.findIndex((value) => value.id === id);
  const item = ctx.mustFind(items, (value) => value.id === id, "remote access governance object");
  const next = { ...item, status, version: item.version + 1, updated_at: ctx.nowIso() } as T;
  items[index] = next;
  ctx.save();
  return next;
}

export function createRemoteAccessDomain(ctx: MockContext): ArgusApiClient["remoteAccess"] {
  const grants = () => (ctx.db.remoteAccessGrants ??= []);
  const rules = () => (ctx.db.remoteAccessRules ??= []);
  const workflows = () => (ctx.db.remoteAccessWorkflows ??= []);
  const profiles = () => (ctx.db.remoteAccessSessionProfiles ??= []);
  const recordings = () => (ctx.db.remoteAccessRecordings ??= []);
  const base = () => ({ enterprise_id: ctx.enterpriseId(), created_by: ctx.actor().id, created_at: ctx.nowIso(), updated_at: ctx.nowIso(), version: 1 });

  return {
    async listGrants(query) { await ctx.pause(); return page(grants(), query); },
    async getGrant(id) { await ctx.pause(); return ctx.mustFind(grants(), (item) => item.id === id, "grant"); },
    async createGrant(input: RemoteAccessGrantWrite) {
      await ctx.pause();
      const item: RemoteAccessGrant = { ...input, ...base(), id: nextId(ctx.db, "rag") };
      grants().unshift(item); ctx.save(); return item;
    },
    async updateGrant(id, input: RemoteAccessGrantUpdate) {
      await ctx.pause(); const item = ctx.mustFind(grants(), (value) => value.id === id, "grant");
      if (item.version !== input.expected_version) throw new Error("VERSION_CONFLICT");
      const next = { ...item, ...input, version: item.version + 1, updated_at: ctx.nowIso() }; grants()[grants().indexOf(item)] = next; ctx.save(); return next;
    },
    async enableGrant(id) { return transition(ctx, grants(), id, "enabled"); },
    async disableGrant(id) { return transition(ctx, grants(), id, "disabled"); },
    async restoreGrant(id) { return transition(ctx, grants(), id, "draft"); },
    async archiveGrant(id) { return transition(ctx, grants(), id, "archived"); },
    async getGrantReferences() { await ctx.pause(); return references(); },

    async listRules(query) { await ctx.pause(); return page(rules(), query); },
    async getRule(id) { await ctx.pause(); return ctx.mustFind(rules(), (item) => item.id === id, "rule"); },
    async createRule(input: RemoteAccessRuleWrite) {
      await ctx.pause(); const item: RemoteAccessRule = { ...input, ...base(), id: nextId(ctx.db, "rar") };
      rules().unshift(item); ctx.save(); return item;
    },
    async updateRule(id, input: RemoteAccessRuleUpdate) {
      await ctx.pause(); const item = ctx.mustFind(rules(), (value) => value.id === id, "rule");
      if (item.version !== input.expected_version) throw new Error("VERSION_CONFLICT");
      const next = { ...item, ...input, version: item.version + 1, updated_at: ctx.nowIso() }; rules()[rules().indexOf(item)] = next; ctx.save(); return next;
    },
    async simulateRule(input: RemoteAccessRuleSimulationRequest): Promise<RemoteAccessRuleSimulationResult> {
      await ctx.pause();
      const matched = rules().filter((rule) => rule.status === "enabled" && rule.protocols.includes(input.protocol));
      const denied = matched.some((rule) => rule.effects.includes("deny"));
      const mfa = matched.some((rule) => rule.effects.includes("require_mfa")) && !input.step_up_authenticated;
      const approval = matched.some((rule) => rule.effects.includes("require_approval"));
      const outcome = denied ? "denied" : mfa ? "awaiting_mfa" : approval ? "awaiting_approval" : "allowed";
      const reasonCode = denied ? "REMOTE_ACCESS_RULE_DENY" : mfa ? "REMOTE_ACCESS_MFA_REQUIRED" : approval ? "REMOTE_ACCESS_APPROVAL_REQUIRED" : "REMOTE_ACCESS_ALLOWED";
      const profileIds = new Set(matched.map((rule) => rule.session_profile_id).filter((id): id is string => Boolean(id)));
      const profile = profiles().find((item) => item.status === "enabled" && profileIds.has(item.id));
      return {
        outcome, reason_codes: [reasonCode], explanation: [outcome],
        matched_grants: [], matched_rules: matched.map((rule) => ({ id: rule.id, version: rule.version })), approval_requirements: [],
        session_profile: profile ? { source_profiles: [{ id: profile.id, version: profile.version }], max_session_seconds: profile.max_session_seconds,
          idle_timeout_seconds: profile.idle_timeout_seconds, recording_mode: profile.recording_mode, command_audit_mode: profile.command_audit_mode,
          clipboard_mode: profile.clipboard_mode, file_upload_mode: profile.file_upload_mode, file_download_mode: profile.file_download_mode,
          port_forward_mode: profile.port_forward_mode, session_share_mode: profile.session_share_mode, retention_days: profile.retention_days } :
          { source_profiles: [], max_session_seconds: 3600, idle_timeout_seconds: 900, recording_mode: "required", command_audit_mode: "required",
            clipboard_mode: "disabled", file_upload_mode: "disabled", file_download_mode: "disabled", port_forward_mode: "disabled", session_share_mode: "disabled", retention_days: 90 },
        snapshot_hash: "0".repeat(64),
      };
    },
    async enableRule(id) { return transition(ctx, rules(), id, "enabled"); },
    async disableRule(id) { return transition(ctx, rules(), id, "disabled"); },
    async restoreRule(id) { return transition(ctx, rules(), id, "draft"); },
    async archiveRule(id) { return transition(ctx, rules(), id, "archived"); },
    async getRuleReferences() { await ctx.pause(); return references(); },

    async listApprovalWorkflows(query) { await ctx.pause(); return page(workflows(), query); },
    async getApprovalWorkflow(id) { await ctx.pause(); return ctx.mustFind(workflows(), (item) => item.id === id, "workflow"); },
    async createApprovalWorkflow(input: ApprovalWorkflowWrite) {
      await ctx.pause(); const item: ApprovalWorkflow = { ...input, ...base(), id: nextId(ctx.db, "raw") };
      workflows().unshift(item); ctx.save(); return item;
    },
    async updateApprovalWorkflow(id, input: ApprovalWorkflowUpdate) {
      await ctx.pause(); const item = ctx.mustFind(workflows(), (value) => value.id === id, "workflow");
      if (item.version !== input.expected_version) throw new Error("VERSION_CONFLICT");
      const next = { ...item, ...input, version: item.version + 1, updated_at: ctx.nowIso() }; workflows()[workflows().indexOf(item)] = next; ctx.save(); return next;
    },
    async enableApprovalWorkflow(id) { return transition(ctx, workflows(), id, "enabled"); },
    async disableApprovalWorkflow(id) { return transition(ctx, workflows(), id, "disabled"); },
    async restoreApprovalWorkflow(id) { return transition(ctx, workflows(), id, "draft"); },
    async archiveApprovalWorkflow(id) { return transition(ctx, workflows(), id, "archived"); },
    async getApprovalWorkflowReferences() { await ctx.pause(); return references(); },

    async listSessionProfiles(query) { await ctx.pause(); return page(profiles(), query); },
    async getSessionProfile(id) { await ctx.pause(); return ctx.mustFind(profiles(), (item) => item.id === id, "session profile"); },
    async createSessionProfile(input: SessionProfileWrite) {
      await ctx.pause(); const item: SessionProfile = { ...input, ...base(), id: nextId(ctx.db, "rasp") };
      profiles().unshift(item); ctx.save(); return item;
    },
    async updateSessionProfile(id, input: SessionProfileUpdate) {
      await ctx.pause(); const item = ctx.mustFind(profiles(), (value) => value.id === id, "session profile");
      if (item.version !== input.expected_version) throw new Error("VERSION_CONFLICT");
      const next = { ...item, ...input, version: item.version + 1, updated_at: ctx.nowIso() }; profiles()[profiles().indexOf(item)] = next; ctx.save(); return next;
    },
    async enableSessionProfile(id) { return transition(ctx, profiles(), id, "enabled"); },
    async disableSessionProfile(id) { return transition(ctx, profiles(), id, "disabled"); },
    async restoreSessionProfile(id) { return transition(ctx, profiles(), id, "draft"); },
    async archiveSessionProfile(id) { return transition(ctx, profiles(), id, "archived"); },
    async getSessionProfileReferences() { await ctx.pause(); return references(); },

    async listRequests() { throw new Error("mock remote access requests are unavailable"); },
    async createRequest() { throw new Error("mock remote access requests are unavailable"); },
    async getRequest() { throw new Error("mock remote access requests are unavailable"); },
    async decideRequest() { throw new Error("mock remote access requests are unavailable"); },
    async resumeRequest() { throw new Error("mock remote access requests are unavailable"); },
    async listLeases() { throw new Error("mock remote access leases are unavailable"); },
    async revokeLease() { throw new Error("mock remote access leases are unavailable"); },
    async listSessions() { throw new Error("mock remote access sessions are unavailable"); },
    async createSession() { throw new Error("mock remote access sessions are unavailable"); },
    async getSession() { throw new Error("mock remote access sessions are unavailable"); },
    async createTicket() { throw new Error("mock remote access tickets are unavailable"); },
    async terminateSession() { throw new Error("mock remote access sessions are unavailable"); },
    async listRecordings(query) {
      await ctx.pause();
      return page(recordings().filter((item) => !query?.status || item.status === query.status), query);
    },
    async getRecording(id) { await ctx.pause(); return ctx.mustFind(recordings(), (item) => item.id === id, "recording"); },
    // 事件按 8 条一页返回，完整走一遍游标分页，覆盖前端全量拉取逻辑。
    async listRecordingEvents(id, cursor) {
      await ctx.pause();
      const recording = ctx.mustFind(recordings(), (item) => item.id === id, "recording");
      const events = ctx.db.remoteAccessRecordingEvents?.[id] ?? [];
      const offset = Number.parseInt(cursor ?? "0", 10) || 0;
      const batch = events.slice(offset, offset + 8);
      const next = offset + batch.length;
      const complete = next >= events.length;
      return { recording, events: batch as RecordingEventPage["events"], next_cursor: String(next), complete };
    },
  };
}
