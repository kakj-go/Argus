import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { PendingAction } from "@argus/api-client";
import { useApi } from "@argus/api-client";
import { useAuthStore } from "@argus/auth";
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
import {
  formatDateTimeFull,
  pendingStatusTone,
} from "./utils";

function diffForCard(action: PendingAction) {
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

function cardStatus(action: PendingAction): PreviewCommitStatus {
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

function resultMessage(action: PendingAction): string | undefined {
  if (action.resultSummary) return action.resultSummary;
  if (action.status === "rejected") {
    const rejected = action.approval?.decisions.find(
      (decision) => decision.decision === "rejected",
    );
    return rejected?.reason;
  }
  return undefined;
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
  const currentUserId = useAuthStore((state) => state.session?.user.id);
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
  const approvedCount =
    approval?.decisions.filter((decision) => decision.decision === "approved")
      .length ?? 0;
  const sodBlocked = Boolean(
    approval?.separationOfDuty &&
      currentUserId !== undefined &&
      action.createdBy === currentUserId,
  );
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
        expiresAt={showCountdown ? action.expiresAt : undefined}
        planHash={action.planHash}
        resultMessage={resultMessage(action)}
        risk={action.riskLevel}
        riskLabel={t(`governance.approvals.risk.${action.riskLevel}`)}
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
            ...Object.entries(action.preview).map(([key, value]) => ({
              label: key,
              value: String(value),
            })),
            {
              label: t("governance.approvals.detail.createdBy"),
              value: action.createdByName ?? action.createdBy,
            },
            {
              label: t("governance.approvals.detail.tool"),
              value: <code>{action.tool}</code>,
            },
            {
              label: t("governance.approvals.detail.createdAt"),
              value: formatDateTimeFull(action.createdAt, locale),
            },
            {
              label: t("governance.approvals.detail.source"),
              value: action.conversationId
                ? t("governance.approvals.source.chatbox")
                : t("governance.approvals.source.admin"),
            },
          ]}
        />
      </PreviewCommitCard>

      {approval && (
        <section className="argus-approval-block">
          <h3 className="argus-gov-section-title">
            {t("governance.approvals.detail.approvalTitle")}
          </h3>
          <KeyValueGrid
            columns={2}
            items={[
              ...(approval.policyName
                ? [
                    {
                      label: t("governance.approvals.detail.policy"),
                      value: approval.policyName,
                    },
                  ]
                : []),
              {
                label: t("governance.approvals.detail.minApprovers", {
                  count: approval.minApprovers,
                }),
                value: t("governance.approvals.detail.progress", {
                  done: approvedCount,
                  min: approval.minApprovers,
                }),
              },
            ]}
          />
          {approval.separationOfDuty && (
            <Alert
              description={t("governance.approvals.detail.progress", {
                done: approvedCount,
                min: approval.minApprovers,
              })}
              title={t("governance.approvals.detail.separation")}
              tone="warning"
            />
          )}
          <div>
            <h3 className="argus-gov-section-title">
              {t("governance.approvals.detail.decisions")}
            </h3>
            {approval.decisions.length === 0 ? (
              <span className="argus-approval-decisions__reason">
                {t("governance.approvals.detail.noDecisions")}
              </span>
            ) : (
              <ul className="argus-approval-decisions">
                {approval.decisions.map((decision, index) => (
                  <li className="argus-approval-decisions__item" key={index}>
                    <StatusBadge
                      tone={
                        decision.decision === "approved" ? "success" : "danger"
                      }
                    >
                      {t(`governance.approvals.decision.${decision.decision}`)}
                    </StatusBadge>
                    <span>{decision.userName ?? decision.userId}</span>
                    <span className="argus-approval-decisions__reason">
                      {formatDateTimeFull(decision.at, locale)}
                      {decision.reason ? ` · ${decision.reason}` : ""}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>
      )}

      {isApprovable && (
        <section className="argus-approval-block">
          {sodBlocked ? (
            <Alert
              description={t(
                "governance.approvals.detail.sodBlockedDescription",
              )}
              title={t("governance.approvals.detail.sodBlockedTitle")}
              tone="warning"
            />
          ) : (
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
          )}
          {actionError && (
            <p className="argus-approval-actions__error" role="alert">
              {actionError}
            </p>
          )}
        </section>
      )}

      {!isConfirmable && !isApprovable && action.taskId && (
        <section className="argus-approval-block">
          <Link to="/tasks">
            <Button variant="secondary">
              {t("governance.approvals.detail.viewTask")} · {action.taskId}
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
