import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  ApprovalRequestView,
  Execution,
  PendingActionPublic,
} from "@argus/api-client";
import { useApi } from "@argus/api-client";
import {
  Alert,
  Button,
  ConfirmDialog,
  Field,
  KeyValueGrid,
  PreviewCommitCard,
  type PreviewCommitStatus,
  Spinner,
  StatusBadge,
  Textarea,
} from "@argus/ui";
import { formatDateTimeFull, pendingStatusTone } from "./utils";

function diffForCard(action: PendingActionPublic) {
  return action.diff.map((line) => ({
    type:
      line.kind === "add"
        ? ("add" as const)
        : line.kind === "remove"
          ? ("remove" as const)
          : ("context" as const),
    content: line.text.replace(/^[+~-]\s*/, ""),
  }));
}

function cardStatus(action: PendingActionPublic): PreviewCommitStatus {
  switch (action.status) {
    case "succeeded":
      return "success";
    case "failed":
      return "failed";
    case "cancelled":
    case "rejected":
      return "cancelled";
    case "expired":
      return "expired";
    default:
      return "pending";
  }
}

function resultMessage(action: PendingActionPublic): string | undefined {
  return action.result_summary;
}

/**
 * 待审批详情区：完整 PreviewCommitCard + 审批要求 + 按状态区分的操作。
 * awaiting_confirmation 使用卡片自带的确认/取消；awaiting_approval
 * 使用下方审批操作区（批准/驳回，含职责分离禁用）；终态只读。
 */
export function ApprovalDetail({ actionRef }: { actionRef: string }) {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const locale = i18n.resolvedLanguage === "en-US" ? "en-US" : "zh-CN";

  const [comment, setComment] = useState("");
  const [rejectOpen, setRejectOpen] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [actionError, setActionError] = useState<string | null>(null);

  const { data: action } = useQuery({
    queryKey: ["approvals", "detail", actionRef],
    queryFn: () => api.approvals.get(actionRef),
  });
  const { data: approvalRequest = null } = useQuery({
    queryKey: ["approval-requests", "action", actionRef],
    queryFn: async (): Promise<ApprovalRequestView | null> => {
      const request = (await api.approvalRequests.list()).find(
        (item) => item.action_ref === actionRef,
      );
      return request
        ? api.approvalRequests.get(request.approval_request_id)
        : null;
    },
  });
  const { data: execution = null } = useQuery({
    queryKey: ["executions", "action", actionRef],
    queryFn: async (): Promise<Execution | null> =>
      (await api.executions.list()).items.find(
        (item) => item.action_ref === actionRef,
      ) ?? null,
  });

  const invalidate = () => {
    // 前缀失效同时覆盖列表与外壳徽标 ["approvals","awaiting_approval"]。
    void queryClient.invalidateQueries({ queryKey: ["approvals"] });
    void queryClient.invalidateQueries({ queryKey: ["approval-requests"] });
    void queryClient.invalidateQueries({ queryKey: ["executions"] });
    void queryClient.invalidateQueries({ queryKey: ["tasks"] });
  };

  const mutationOptions = {
    onSuccess: () => {
      setActionError(null);
      setComment("");
      setRejectReason("");
      setRejectOpen(false);
      invalidate();
    },
    onError: () => setActionError(t("governance.approvals.actionFailed")),
  };

  const confirmMutation = useMutation({
    mutationFn: () => api.approvals.confirm(actionRef),
    ...mutationOptions,
  });
  const cancelMutation = useMutation({
    mutationFn: () => api.approvals.cancel(actionRef),
    ...mutationOptions,
  });
  const approveMutation = useMutation({
    mutationFn: async (value?: string) => {
      if (approvalRequest) {
        await api.approvalRequests.decide(
          approvalRequest.approval_request_id,
          { decision: "approved", ...(value ? { reason: value } : {}) },
        );
        return;
      }
      await api.approvals.approve(actionRef, value);
    },
    ...mutationOptions,
  });
  const rejectMutation = useMutation({
    mutationFn: async (reason: string) => {
      if (approvalRequest) {
        await api.approvalRequests.decide(
          approvalRequest.approval_request_id,
          { decision: "rejected", reason },
        );
        return;
      }
      await api.approvals.reject(actionRef, reason);
    },
    ...mutationOptions,
  });

  if (!action) {
    return <Spinner label={t("common.loading")} />;
  }

  const approval = action.approval;
  const approvedCount = approval?.approved_count ?? 0;
  const preview =
    typeof action.preview === "object" && action.preview !== null
      ? (action.preview as Record<string, unknown>)
      : {};
  const busy =
    confirmMutation.isPending ||
    cancelMutation.isPending ||
    approveMutation.isPending ||
    rejectMutation.isPending;

  const isConfirmable = action.status === "awaiting_confirmation";
  const isApprovable = action.status === "awaiting_approval";
  const showCountdown =
    action.status === "awaiting_confirmation" ||
    action.status === "awaiting_approval" ||
    action.status === "ready";

  return (
    <div className="argus-approval-detail">
      {action.status === "executing" || action.status === "ready" ? (
        <StatusBadge pulse tone={pendingStatusTone(action.status)}>
          {t(`governance.approvals.status.${action.status}`)}
        </StatusBadge>
      ) : null}

      <PreviewCommitCard
        className={isConfirmable ? undefined : "argus-approval-card--static"}
        confirming={confirmMutation.isPending || cancelMutation.isPending}
        diff={diffForCard(action)}
        expiresAt={showCountdown ? action.expires_at : undefined}
        resultMessage={resultMessage(action)}
        risk={action.risk}
        riskLabel={t(`governance.approvals.risk.${action.risk}`)}
        status={cardStatus(action)}
        title={action.title}
        {...(isConfirmable
          ? {
              confirmLabel: t("governance.approvals.detail.confirm"),
              cancelLabel: t("governance.approvals.detail.cancel"),
              onConfirm: () => confirmMutation.mutate(),
              onCancel: () => cancelMutation.mutate(),
            }
          : {})}
      >
        <KeyValueGrid
          columns={2}
          items={[
            ...Object.entries(preview).map(([key, value]) => ({
              label: key,
              value: String(value),
            })),
            {
              label: t("governance.approvals.detail.createdAt"),
              value: formatDateTimeFull(action.created_at, locale),
            },
          ]}
        />
      </PreviewCommitCard>

      {(approvalRequest || approval) && (
        <section className="argus-approval-block">
          <div className="argus-approval-block__head">
            <h3 className="argus-gov-section-title">
              {t("governance.approvals.detail.approvalTitle")}
            </h3>
            {approvalRequest?.status === "rejected" ||
            action.status === "rejected" ? (
              <StatusBadge tone="danger">
                {t("governance.approvals.decision.rejected")}
              </StatusBadge>
            ) : approvalRequest?.status === "approved" ||
              (approval && approvedCount >= approval.minimum_approvers) ? (
              <StatusBadge tone="success">
                {t("governance.approvals.decision.approved")}
              </StatusBadge>
            ) : null}
          </div>
          <div className="argus-approval-requirements">
            {(approvalRequest?.requirements ??
              (approval
                ? [
                    {
                      policy_id: approval.policy_ref ?? "-",
                      policy_version: 1,
                      minimum_approvers: approval.minimum_approvers,
                      approved_count: approvedCount,
                      separation_of_duty: approval.separation_of_duty,
                      status: "pending" as const,
                    },
                  ]
                : [])).map((requirement) => (
              <div
                className="argus-approval-requirement"
                key={`${requirement.policy_id}:${requirement.policy_version}`}
              >
                <div className="argus-approval-block__head">
                  <strong>{requirement.policy_id}</strong>
                  <StatusBadge
                    tone={
                      requirement.status === "approved"
                        ? "success"
                        : requirement.status === "pending"
                          ? "warning"
                          : "danger"
                    }
                  >
                    {t(
                      `governance.approvals.requirementStatus.${requirement.status}`,
                    )}
                  </StatusBadge>
                </div>
                <KeyValueGrid
                  columns={2}
                  items={[
                    {
                      label: t("governance.approvals.detail.policyVersion"),
                      value: String(requirement.policy_version),
                    },
                    {
                      label: t("governance.approvals.detail.minApprovers", {
                        count: requirement.minimum_approvers,
                      }),
                      value: t("governance.approvals.detail.progress", {
                        done: requirement.approved_count,
                        min: requirement.minimum_approvers,
                      }),
                    },
                  ]}
                />
                {requirement.separation_of_duty && (
                  <Alert
                    description={t(
                      "governance.approvals.detail.separationDescription",
                    )}
                    title={t("governance.approvals.detail.separation")}
                    tone="warning"
                  />
                )}
              </div>
            ))}
          </div>
          {approvalRequest && (
            <div className="argus-approval-decisions">
              <h4 className="argus-gov-section-title">
                {t("governance.approvals.detail.decisions")}
              </h4>
              {approvalRequest.decisions.length === 0 ? (
                <p>{t("governance.approvals.detail.noDecisions")}</p>
              ) : (
                approvalRequest.decisions.map((decision) => (
                  <div
                    className="argus-approval-decisions__item"
                    key={decision.decision_id}
                  >
                    <StatusBadge
                      tone={
                        decision.decision === "approved" ? "success" : "danger"
                      }
                    >
                      {t(
                        `governance.approvals.decision.${decision.decision}`,
                      )}
                    </StatusBadge>
                    <span>{decision.actor_user_id}</span>
                    <time>{formatDateTimeFull(decision.decided_at, locale)}</time>
                    {decision.reason && (
                      <p className="argus-approval-decisions__reason">
                        {decision.reason}
                      </p>
                    )}
                  </div>
                ))
              )}
            </div>
          )}
          {action.status === "rejected" && action.result_summary && (
            <p className="argus-approval-decisions__reason">
              {action.result_summary}
            </p>
          )}
        </section>
      )}

      {isApprovable && (
        <section className="argus-approval-block">
          <div className="argus-approval-actions">
            <Field label={t("governance.approvals.detail.approve")}>
              <Textarea
                onChange={(event) => setComment(event.target.value)}
                placeholder={t(
                  "governance.approvals.detail.commentPlaceholder",
                )}
                rows={2}
                value={comment}
              />
            </Field>
            <div className="argus-approval-actions__buttons">
              <Button
                disabled={busy}
                onClick={() => setRejectOpen(true)}
                variant="secondary"
              >
                {t("governance.approvals.detail.reject")}
              </Button>
              <Button
                loading={approveMutation.isPending}
                onClick={() =>
                  approveMutation.mutate(comment.trim() || undefined)
                }
                variant="primary"
              >
                {t("governance.approvals.detail.approve")}
              </Button>
            </div>
          </div>
          {actionError && (
            <p className="argus-approval-actions__error" role="alert">
              {actionError}
            </p>
          )}
        </section>
      )}

      {!isConfirmable && !isApprovable && (execution || action.execution_ref) && (
        <section className="argus-approval-block">
          {execution && (
            <Alert
              description={
                execution.error_code ??
                t("governance.approvals.detail.executionStatusDescription")
              }
              title={t(
                `governance.approvals.executionStatus.${execution.status}`,
              )}
              tone={
                execution.status === "succeeded"
                  ? "success"
                  : execution.status === "result_unknown"
                    ? "warning"
                    : execution.status === "failed"
                      ? "danger"
                      : "info"
              }
            />
          )}
          <Link to="/tasks">
            <Button variant="secondary">
              {t("governance.approvals.detail.viewTask")} ·{" "}
              {execution?.execution_id ?? action.execution_ref}
            </Button>
          </Link>
        </section>
      )}

      {isConfirmable && actionError && (
        <p className="argus-approval-actions__error" role="alert">
          {actionError}
        </p>
      )}

      <ConfirmDialog
        danger
        confirmLabel={t("governance.approvals.detail.rejectConfirm")}
        description={t("governance.approvals.detail.rejectDialogDescription")}
        loading={rejectMutation.isPending}
        onConfirm={() => {
          const reason = rejectReason.trim();
          if (reason) rejectMutation.mutate(reason);
        }}
        onOpenChange={setRejectOpen}
        open={rejectOpen}
        title={t("governance.approvals.detail.rejectDialogTitle")}
      >
        <Field label={t("governance.approvals.detail.reject")}>
          <Textarea
            autoFocus
            onChange={(event) => setRejectReason(event.target.value)}
            placeholder={t(
              "governance.approvals.detail.rejectReasonPlaceholder",
            )}
            rows={3}
            value={rejectReason}
          />
        </Field>
      </ConfirmDialog>
    </div>
  );
}
