import type {
  ActionOneTimeResult,
  ApprovalPolicy as ApprovalPolicyContract,
  ApprovalPolicyWrite,
  ApprovalRequestView,
  Execution,
  ExecutionPage,
  PendingActionCommandResult,
  PendingActionPublic,
} from "../../generated/contracts";
import {
  ApiError,
  ClientOperationUnavailableError,
} from "../../transport/errors";
import type { ApprovalPolicy, ListQuery } from "../../types";
import { page, type RealDomainContext } from "./context";

function policyView(value: ApprovalPolicyContract): ApprovalPolicy {
  return {
    id: value.id,
    enterpriseId: "",
    name: value.name,
    matchRiskLevels: value.risks,
    toolIds: value.tool_ids,
    resourceTypes: value.resource_types,
    labelSelector: value.label_selector,
    minApprovers: value.minimum_approvers,
    approverRoleIds: value.approver_role_ids,
    separationOfDuty: value.separation_of_duty,
    expiresAfterSeconds: value.expires_after_seconds,
    enabled: value.enabled,
    createdAt: value.created_at,
  };
}

export function installWorkflowDomains(context: RealDomainContext): void {
  const { client, http, remember, expectedVersion, idempotencyKey } = context;

  client.org.listApprovalPolicies = async () => {
    const values = await http.request<ApprovalPolicyContract[]>(
      "enterprise/approval-policies",
    );
    return values.map((value) => policyView(remember(value)));
  };
  client.org.saveApprovalPolicy = async (policy) => {
    const body: ApprovalPolicyWrite = {
      name: policy.name,
      enabled: policy.enabled,
      tool_ids: policy.toolIds ?? [],
      risks: policy.matchRiskLevels,
      resource_types: policy.resourceTypes ?? [],
      label_selector: policy.labelSelector,
      minimum_approvers: policy.minApprovers,
      separation_of_duty: policy.separationOfDuty,
      approver_role_ids: policy.approverRoleIds,
      expires_after_seconds: policy.expiresAfterSeconds ?? 86400,
      expected_version: policy.id ? expectedVersion(policy.id) : 0,
    };
    const value = await http.request<ApprovalPolicyContract>(
      policy.id
        ? `enterprise/approval-policies/${policy.id}`
        : "enterprise/approval-policies",
      {
        method: policy.id ? "PUT" : "POST",
        csrf: true,
        ...(policy.id
          ? {}
          : { headers: { "Idempotency-Key": idempotencyKey() } }),
        body,
      },
    );
    return policyView(remember(value));
  };

  client.approvals = {
    ...client.approvals,
    async list(filter, query: ListQuery = {}) {
      const params = new URLSearchParams();
      if (filter?.scope) params.set("scope", filter.scope);
      if (filter?.query) params.set("query", filter.query);
      for (const status of filter?.status ?? []) params.append("status", status);
      for (const risk of filter?.risk ?? []) params.append("risk", risk);
      if (query.page?.cursor) params.set("cursor", query.page.cursor);
      if (query.page?.limit !== undefined) params.set("limit", String(query.page.limit));
      const suffix = params.toString() ? `?${params.toString()}` : "";
      const value = await http.request<{
        items: PendingActionPublic[];
        page: { next_cursor: string | null; has_more: boolean };
      }>(`enterprise/pending-actions${suffix}`);
      return page(value);
    },
    get: (actionRef) =>
      http.request<PendingActionPublic>(
        `enterprise/pending-actions/${encodeURIComponent(actionRef)}`,
      ),
    confirm: (actionRef) =>
      http.request<PendingActionCommandResult>(
        `enterprise/pending-actions/${encodeURIComponent(actionRef)}/confirm`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
        },
      ),
    cancel: (actionRef) =>
      http.request<PendingActionPublic>(
        `enterprise/pending-actions/${encodeURIComponent(actionRef)}/cancel`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
        },
      ),
    async approve(actionRef, comment) {
      await decide(actionRef, "approved", comment);
      return client.approvals.get(actionRef);
    },
    async reject(actionRef, reason) {
      await decide(actionRef, "rejected", reason);
      return client.approvals.get(actionRef);
    },
  };

  client.approvalRequests = {
    list: () =>
      http.request<ApprovalRequestView[]>("enterprise/approval-requests"),
    get: (id) =>
      http.request<ApprovalRequestView>(
        `enterprise/approval-requests/${encodeURIComponent(id)}`,
      ),
    decide: (id, body) =>
      http.request<ApprovalRequestView>(
        `enterprise/approval-requests/${encodeURIComponent(id)}/decisions`,
        {
          method: "POST",
          csrf: true,
          headers: { "Idempotency-Key": idempotencyKey() },
          body,
        },
      ),
  };

  client.executions = {
    async list() {
      const value = await http.request<ExecutionPage>("enterprise/executions");
      return page(value);
    },
    get: (executionId) =>
      http.request<Execution>(`enterprise/executions/${executionId}`),
    async claimOneTimeResult(executionId) {
      const key = idempotencyKey();
      const request = () =>
        http.request<ActionOneTimeResult>(
          `enterprise/executions/${executionId}/one-time-result`,
          {
            method: "POST",
            csrf: true,
            headers: { "Idempotency-Key": key },
          },
        );
      try {
        return await request();
      } catch (error) {
        if (error instanceof ApiError) throw error;
        return request();
      }
    },
  };

  async function decide(
    actionRef: string,
    decision: "approved" | "rejected",
    reason?: string,
  ): Promise<void> {
    const requests = await client.approvalRequests.list();
    const request = requests.find(
      (item) => item.action_ref === actionRef && item.status === "pending",
    );
    if (!request) {
      throw new ClientOperationUnavailableError("approval.request");
    }
    await client.approvalRequests.decide(request.approval_request_id, {
      decision,
      reason,
    });
  }
}
