import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { PendingActionPublic } from "@argus/api-client";
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

  const invalidate = () => {
    // 前缀失效同时覆盖列表与外壳徽标 ["approvals","awaiting_approval"]。
    void queryClient.invalidateQueries({ queryKey: ["approvals"] });
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
    mutationFn: (value?: string) => api.approvals.approve(actionRef, value),
    ...mutationOptions,
  });
  const rejectMutation = useMutation({
    mutationFn: (reason: string) => api.approvals.reject(actionRef, reason),
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

      {approval && (
        <section className="argus-approval-block">
          <div className="argus-approval-block__head">
            <h3 className="argus-gov-section-title">
              {t("governance.approvals.detail.approvalTitle")}
            </h3>
            {action.status === "rejected" ? (
              <StatusBadge tone="danger">
                {t("governance.approvals.decision.rejected")}
              </StatusBadge>
            ) : approvedCount >= approval.minimum_approvers ? (
              <StatusBadge tone="success">
                {t("governance.approvals.decision.approved")}
              </StatusBadge>
            ) : null}
          </div>
          <KeyValueGrid
            columns={2}
            items={[
              ...(approval.policy_ref
                ? [
                    {
                      label: t("governance.approvals.detail.policy"),
                      value: approval.policy_ref,
                    },
                  ]
                : []),
              {
                label: t("governance.approvals.detail.minApprovers", {
                  count: approval.minimum_approvers,
                }),
                value: t("governance.approvals.detail.progress", {
                  done: approvedCount,
                  min: approval.minimum_approvers,
                }),
              },
            ]}
          />
          {approval.separation_of_duty && (
            <Alert
              description={t("governance.approvals.detail.progress", {
                done: approvedCount,
                min: approval.minimum_approvers,
              })}
              title={t("governance.approvals.detail.separation")}
              tone="warning"
            />
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

      {!isConfirmable && !isApprovable && action.execution_ref && (
        <section className="argus-approval-block">
          <Link to="/tasks">
            <Button variant="secondary">
              {t("governance.approvals.detail.viewTask")} ·{" "}
              {action.execution_ref}
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
